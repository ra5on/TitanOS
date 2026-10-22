// The chat with the assistant: open or closed, and the conversation. It
// lives outside the pages, so the chat goes on when the page changes. The
// conversation is kept in this browser tab only (sessionStorage), never on
// the NAS.
import { api, ApiError } from '#lib/api.ts';

export type ChatTool = { name: string; ok: boolean };
export type ChatMessage = {
	role: 'user' | 'assistant';
	content: string;
	/** The tools the assistant used for this answer. */
	tools?: ChatTool[];
	/** An answer that did not come: an error code (and what Ollama said). */
	problem?: { code: string; detail?: string };
};
export type AssistantSettings = { enabled: boolean; ollama: string; model: string };

const STORAGE_KEY = 'titanos.assistant';

function remembered(): { messages: ChatMessage[]; model: string } {
	try {
		const stored = JSON.parse(sessionStorage.getItem(STORAGE_KEY) ?? '{}');
		const messages = Array.isArray(stored.messages)
			? stored.messages.filter((m: ChatMessage) => (m?.role === 'user' || m?.role === 'assistant') && typeof m.content === 'string')
			: [];
		return { messages, model: typeof stored.model === 'string' ? stored.model : '' };
	} catch {
		return { messages: [], model: '' };
	}
}

export const assistant = $state({
	open: false,
	busy: false,
	/** null until the settings were read; they say whether the chat is on. */
	settings: null as AssistantSettings | null,
	models: [] as string[],
	/** What happens right now: the tool being read, or '' while thinking. */
	step: '',
	...remembered()
});

function remember() {
	try {
		sessionStorage.setItem(STORAGE_KEY, JSON.stringify({ messages: assistant.messages.slice(-40), model: assistant.model }));
	} catch {
		// A private window: the chat still works, only without memory.
	}
}

export async function loadAssistant() {
	try {
		assistant.settings = await api.get<AssistantSettings>('/assistant');
	} catch {
		assistant.settings = { enabled: false, ollama: '', model: '' };
	}
}

export async function loadModels() {
	try {
		assistant.models = (await api.post<{ models: string[] }>('/assistant/models', { ollama: '' })).models;
	} catch {
		assistant.models = [];
	}
}

export function toggleAssistant() {
	assistant.open = !assistant.open;
	if (assistant.open && assistant.models.length === 0) loadModels();
}

export function setModel(model: string) {
	assistant.model = model;
	remember();
}

export function clearChat() {
	if (assistant.busy) return;
	assistant.messages = [];
	remember();
}

/** Asks the assistant and adds its answer, as it comes, to the conversation. */
export async function ask(question: string) {
	const text = question.trim();
	if (!text || assistant.busy) return;
	assistant.busy = true;
	assistant.step = '';
	assistant.messages.push({ role: 'user', content: text });
	assistant.messages.push({ role: 'assistant', content: '', tools: [] });
	const answer = assistant.messages[assistant.messages.length - 1];
	remember();
	// Earlier answers that never came are not part of the conversation.
	const history = assistant.messages.slice(0, -1).filter((m) => !m.problem && m.content).map((m) => ({ role: m.role, content: m.content }));
	try {
		const res = await fetch('/api/v1/assistant/chat', {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({ model: assistant.model, messages: history })
		});
		if (!res.ok || !res.body) {
			let code = 'internal';
			try {
				code = (await res.json()).error?.code ?? code;
			} catch {
				// Not JSON: the generic text will do.
			}
			throw new ApiError(res.status, code, '');
		}
		const reader = res.body.getReader();
		const decoder = new TextDecoder();
		let buffer = '';
		for (;;) {
			const { done, value } = await reader.read();
			buffer += decoder.decode(value ?? new Uint8Array(), { stream: !done });
			let cut;
			while ((cut = buffer.indexOf('\n\n')) >= 0) {
				const raw = buffer.slice(0, cut);
				buffer = buffer.slice(cut + 2);
				const event = /^event: (.*)$/m.exec(raw)?.[1];
				const data = JSON.parse(/^data: (.*)$/m.exec(raw)?.[1] ?? '{}');
				if (event === 'tool') {
					answer.tools = [...(answer.tools ?? []), { name: data.name, ok: data.ok }];
					assistant.step = data.name;
				} else if (event === 'answer') {
					answer.content = data.text ?? '';
				} else if (event === 'problem') {
					answer.problem = { code: data.code ?? 'internal', detail: data.detail };
				}
			}
			if (done) break;
		}
		if (!answer.content && !answer.problem) answer.problem = { code: 'internal' };
	} catch (err) {
		answer.problem = { code: err instanceof ApiError ? err.code : 'internal' };
	}
	assistant.busy = false;
	assistant.step = '';
	remember();
}
