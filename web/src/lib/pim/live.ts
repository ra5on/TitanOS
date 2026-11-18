// 1.13: GET /calendar/stream – live changes of the own calendars and
// address book while Kalender or Kontakte is open (docs/KALENDER.md
// "Live-Aktualisierung"). Any change, from this page, another window,
// an iPhone over CalDAV/CardDAV or the agent, comes as an event "pim"
// with the collection's new ctag (generation); several changes within a
// quarter second come as one per collection. Read like T-Mail's stream
// (fetch, not EventSource: same cookies and paths as every request). A
// lost stream is opened again after a growing pause (1 s … 30 s); while
// the page is hidden it stays closed and says "resync" when it opens
// again (what changed meanwhile is unknown: read anew).
import { apiFetch } from '../api.ts';

export type PimKind = 'contacts' | 'calendar';

export type PimEvent =
	/** The stream is open; `generations` maps "<kind>/<collection>" to its ctag. */
	| { type: 'ready'; account: string; generations: Record<string, string> }
	/** A collection changed; generation "" when it was deleted. */
	| { type: 'pim'; account: string; kind: PimKind; collection: string; generation: string }
	/** Events were left out (or the stream was away): read everything again. */
	| { type: 'resync' };

const MIN_PAUSE = 1000;
const MAX_PAUSE = 30_000;

/** One block of the stream ("event: …", "data: …"); null for anything else. */
export function parsePimBlock(block: string): PimEvent | null {
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
	if (type === 'resync') return { type };
	if (!account) return null;
	if (type === 'ready') {
		const generations: Record<string, string> = {};
		const g = raw.generations;
		if (g && typeof g === 'object') {
			for (const [k, v] of Object.entries(g as Record<string, unknown>)) {
				if (typeof v === 'string') generations[k] = v;
			}
		}
		return { type, account, generations };
	}
	if (type === 'pim') {
		const kind = raw.kind;
		if (kind !== 'contacts' && kind !== 'calendar') return null;
		const collection = typeof raw.collection === 'string' ? raw.collection : '';
		if (!collection) return null;
		const generation = typeof raw.generation === 'string' ? raw.generation : '';
		return { type, account, kind, collection, generation };
	}
	return null;
}

/** The pause before the n-th attempt in a row (0 = the first after a loss). */
export function pimPauseFor(attempt: number): number {
	return Math.min(MAX_PAUSE, MIN_PAUSE * 2 ** attempt);
}

/**
 * Follows the stream until the returned function is called. A 503 (no
 * data volume) is tried again like a lost stream.
 */
export function watchPim(onEvent: (e: PimEvent) => void): () => void {
	let stopped = false;
	let controller: AbortController | null = null;
	let timer: ReturnType<typeof setTimeout> | undefined;
	let attempt = 0;
	let hadGap = false;

	async function connect() {
		if (stopped || document.visibilityState === 'hidden') return;
		controller = new AbortController();
		const own = controller;
		try {
			const res = await apiFetch('/calendar/stream', {
				signal: own.signal,
				headers: { Accept: 'text/event-stream' },
			});
			if (!res.ok || !res.body) throw new Error('no stream');
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
					const e = parsePimBlock(buffer.slice(0, cut));
					buffer = buffer.slice(cut).replace(/^\r?\n\r?\n/, '');
					if (e) {
						attempt = 0;
						onEvent(e);
					}
				}
			}
			if (Date.now() - opened > 10_000) attempt = 0;
		} catch {
			// Lost, refused or stopped: tried again below unless stopped.
		}
		if (stopped || own.signal.aborted) return;
		hadGap = true;
		timer = setTimeout(() => void connect(), pimPauseFor(attempt++));
	}

	function visibility() {
		if (document.visibilityState === 'hidden') {
			clearTimeout(timer);
			controller?.abort();
			hadGap = true;
		} else if (!stopped) {
			clearTimeout(timer);
			controller?.abort();
			attempt = 0;
			void connect();
		}
	}

	document.addEventListener('visibilitychange', visibility);
	void connect();
	return () => {
		stopped = true;
		clearTimeout(timer);
		controller?.abort();
		document.removeEventListener('visibilitychange', visibility);
	};
}
