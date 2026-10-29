// Loads a model into Ollama; the NAS passes Ollama's progress on as server-
// sent events. Ollama keeps what it already has, so a retry continues.
import { ApiError } from './api.ts';

export type PullProgress = { status: string; total: number; completed: number };

/** Resolves when the model is there; throws ApiError (code, detail in message). */
export async function pullModel(model: string, onprogress: (p: PullProgress) => void, signal?: AbortSignal): Promise<void> {
	const res = await fetch('/api/v1/assistant/models/pull', {
		method: 'POST',
		credentials: 'same-origin',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify({ model }),
		signal,
	});
	if (!res.ok || !res.body) {
		let code = 'internal';
		try {
			code = (await res.json()).error?.code ?? code;
		} catch {
			// The generic text will do.
		}
		throw new ApiError(res.status, code, '');
	}
	const reader = res.body.getReader();
	const decoder = new TextDecoder();
	let buffer = '';
	let finished = false;
	for (;;) {
		const { done, value } = await reader.read();
		buffer += decoder.decode(value ?? new Uint8Array(), { stream: !done });
		let cut;
		while ((cut = buffer.indexOf('\n\n')) >= 0) {
			const raw = buffer.slice(0, cut);
			buffer = buffer.slice(cut + 2);
			const kind = /^event: (.*)$/m.exec(raw)?.[1];
			const data = JSON.parse(/^data: (.*)$/m.exec(raw)?.[1] ?? '{}');
			if (kind === 'progress') onprogress({ status: data.status ?? '', total: data.total ?? 0, completed: data.completed ?? 0 });
			else if (kind === 'problem') throw new ApiError(502, data.code ?? 'ollama_failed', data.detail ?? '');
			else if (kind === 'done') finished = true;
		}
		if (done) break;
	}
	if (!finished) throw new ApiError(502, 'ollama_failed', '');
}
