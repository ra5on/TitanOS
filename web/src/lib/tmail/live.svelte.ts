// 1.05: GET /mail/events – new mail, the state of a mailbox and changed
// marks while T-Mail is open. Read like the chat stream of the agent
// (fetch, not EventSource: same cookies and paths as every request). A lost
// stream is opened again after a growing pause (1 s … 30 s); while the page
// is hidden it stays closed and opens again when the page is seen.
import { apiFetch } from '../api.ts';
import type { MailStatus } from './types.ts';

export type MailEvent =
	| { type: 'mail'; account: string; folder: string; uid: number }
	| { type: 'status'; account: string; status: MailStatus }
	| { type: 'flags'; account: string; folder: string; uids: number[] }
	/** The stream is back after a gap: what came meanwhile is unknown, read anew. */
	| { type: 'resync' };

/** `down`: the stream has been gone for a while (the page says so). */
export const live = $state({ connected: false, down: false });

const MIN_PAUSE = 1000;
const MAX_PAUSE = 30_000;
/** Only a gap longer than this is shown. */
const SHOW_DOWN_AFTER = 8000;

/** One block of an event stream ("event: …", "data: …"). */
export function parseBlock(block: string): MailEvent | null {
	let type = 'message';
	const data: string[] = [];
	for (const line of block.split(/\r?\n/)) {
		if (line.startsWith(':')) continue;
		const at = line.indexOf(':');
		const field = at < 0 ? line : line.slice(0, at);
		const value = at < 0 ? '' : line.slice(at + 1).replace(/^ /, '');
		if (field === 'event') type = value;
		else if (field === 'data') data.push(value);
	}
	if (!data.length) return null;
	let raw: Record<string, unknown>;
	try {
		raw = JSON.parse(data.join('\n'));
	} catch {
		return null;
	}
	if (!raw || typeof raw !== 'object') return null;
	const account = typeof raw.account === 'string' ? raw.account : '';
	if (!account) return null;
	const folder = typeof raw.folder === 'string' ? raw.folder : '';
	if (type === 'mail') {
		const uid = Number(raw.uid);
		return { type, account, folder, uid: Number.isFinite(uid) ? uid : 0 };
	}
	if (type === 'status') {
		// Either {account, status:{state,code,since}} or {account, state, code, since}.
		const s = (raw.status && typeof raw.status === 'object' ? raw.status : raw) as Record<
			string,
			unknown
		>;
		const state = s.state;
		if (
			state !== 'ok' &&
			state !== 'connecting' &&
			state !== 'auth_failed' &&
			state !== 'unreachable' &&
			state !== 'off'
		)
			return null;
		return {
			type,
			account,
			status: {
				state,
				code: typeof s.code === 'string' ? s.code : '',
				since: typeof s.since === 'string' ? s.since : '',
			},
		};
	}
	if (type === 'flags') {
		const list = Array.isArray(raw.uids) ? raw.uids : raw.uid !== undefined ? [raw.uid] : [];
		const uids = list.map(Number).filter((n) => Number.isFinite(n));
		return { type, account, folder, uids };
	}
	return null;
}

/** The pause before the n-th attempt in a row (0 = the first after a loss). */
export function pauseFor(attempt: number): number {
	return Math.min(MAX_PAUSE, MIN_PAUSE * 2 ** attempt);
}

/** Follows the stream until the returned function is called. */
export function watchMail(onEvent: (e: MailEvent) => void): () => void {
	let stopped = false;
	let controller: AbortController | null = null;
	let timer: ReturnType<typeof setTimeout> | undefined;
	let downTimer: ReturnType<typeof setTimeout> | undefined;
	let attempt = 0;
	let hadGap = false;

	function markDown() {
		live.connected = false;
		downTimer ??= setTimeout(() => (live.down = true), SHOW_DOWN_AFTER);
	}
	function markUp() {
		clearTimeout(downTimer);
		downTimer = undefined;
		live.connected = true;
		live.down = false;
	}

	async function connect() {
		if (stopped || document.visibilityState === 'hidden') return;
		controller = new AbortController();
		const own = controller;
		try {
			const res = await apiFetch('/mail/events', {
				signal: own.signal,
				headers: { Accept: 'text/event-stream' },
			});
			if (!res.ok || !res.body) throw new Error('no stream');
			markUp();
			if (hadGap) onEvent({ type: 'resync' });
			hadGap = false;
			const opened = Date.now();
			const reader = res.body.pipeThrough(new TextDecoderStream()).getReader();
			let buffer = '';
			for (;;) {
				const { value, done } = await reader.read();
				if (done) break;
				buffer += value;
				let cut: number;
				while ((cut = buffer.search(/\r?\n\r?\n/)) >= 0) {
					const block = buffer.slice(0, cut);
					buffer = buffer.slice(cut).replace(/^\r?\n\r?\n/, '');
					const e = parseBlock(block);
					if (e) {
						attempt = 0;
						onEvent(e);
					}
				}
			}
			// A stream that held a while starts the pauses again from the shortest.
			if (Date.now() - opened > 10_000) attempt = 0;
		} catch {
			// Lost, refused or stopped: tried again below unless stopped.
		}
		if (stopped || own.signal.aborted) return;
		hadGap = true;
		markDown();
		timer = setTimeout(() => void connect(), pauseFor(attempt++));
	}

	function visibility() {
		if (document.visibilityState === 'hidden') {
			clearTimeout(timer);
			controller?.abort();
			// Closed on purpose: no gap shown, but opened again when seen.
			live.connected = false;
			hadGap = true;
		} else if (!live.connected) {
			clearTimeout(timer);
			attempt = 0;
			void connect();
		}
	}

	document.addEventListener('visibilitychange', visibility);
	void connect();
	return () => {
		stopped = true;
		clearTimeout(timer);
		clearTimeout(downTimer);
		controller?.abort();
		live.connected = false;
		live.down = false;
		document.removeEventListener('visibilitychange', visibility);
	};
}
