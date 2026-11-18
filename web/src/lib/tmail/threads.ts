// 1.12 (owner 18 Nov 2026, "Unterhaltungen"): the mails of a list that
// belong together, as one entry. Linked by Message-ID, In-Reply-To and
// References (titand sends at most 20 of them per mail); two mails that
// name the same earlier mail belong together even when that one is not in
// the list. The subject joins only an answer ("Re:", "AW:" …) without any
// of these heads to an earlier mail of the same subject within 30 days –
// otherwise every "Ihr Paket kommt heute" would be one conversation.
import type { MessageRow } from './types.ts';

/** A row of a list; search results carry their folder. */
export type Row = MessageRow & { folder?: string };

export type Thread<T extends Row = Row> = {
	/** The newest mail's key: stays the same while the conversation is shown. */
	key: string;
	/** Newest first. */
	rows: T[];
};

const PREFIX = /^\s*((re|aw|antw|sv|vs|fwd?|wg|tr)(\[\d+\])?\s*:\s*|\[[^\]]{1,40}\]\s*)/i;
const MONTH = 30 * 24 * 3600 * 1000;

/** The subject without reply and forward prefixes, folded for comparing. */
export function baseSubject(subject: string): string {
	let s = (subject ?? '').trim();
	for (let i = 0; i < 10; i++) {
		const next = s.replace(PREFIX, '');
		if (next === s) break;
		s = next;
	}
	return s.replace(/\s+/g, ' ').toLocaleLowerCase();
}

/** Whether a subject starts with a reply or forward prefix. */
export function isReply(subject: string): boolean {
	return /^\s*(re|aw|antw|sv|vs|fwd?|wg|tr)(\[\d+\])?\s*:/i.test(subject ?? '');
}

/** The key of a row: the folder (in a search) and the uid. */
export const rowKey = (r: Pick<Row, 'uid' | 'folder'>) => `${r.folder ?? ''}\u0000${r.uid}`;

const ids = (s: string | undefined) => (s ?? '').split(/\s+/).filter((x) => /^<[^<>\s]+>$/.test(x));

/** The rows as conversations, in the order of their newest mail in `rows`; off: one each. */
export function threadRows<T extends Row>(rows: T[], on = true): Thread<T>[] {
	if (!on) return rows.map((r) => ({ key: rowKey(r), rows: [r] }));
	const parent = new Map<string, string>();
	const find = (x: string): string => {
		let root = x;
		while (parent.has(root) && parent.get(root) !== root) root = parent.get(root)!;
		let at = x;
		while (at !== root) {
			const next = parent.get(at)!;
			parent.set(at, root);
			at = next;
		}
		return root;
	};
	const union = (a: string, b: string) => {
		const ra = find(a);
		const rb = find(b);
		if (ra !== rb) parent.set(ra, rb);
	};
	for (const r of rows) {
		const node = 'r:' + rowKey(r);
		parent.set(node, find(node));
		for (const id of [...ids(r.messageId), ...ids(r.inReplyTo), ...ids(r.references)])
			union(node, 'i:' + id.toLowerCase());
	}
	// The subject: an answer without heads joins the newest earlier mail of
	// the same subject within 30 days.
	const time = (r: Row) => new Date(r.date).getTime() || 0;
	for (const r of rows) {
		if (!isReply(r.subject) || ids(r.inReplyTo).length || ids(r.references).length) continue;
		const subject = baseSubject(r.subject);
		if (!subject) continue;
		let best: T | null = null;
		for (const o of rows) {
			if (o === r || baseSubject(o.subject) !== subject) continue;
			const d = time(r) - time(o);
			if (d < 0 || d > MONTH) continue;
			if (!best || time(o) > time(best)) best = o;
		}
		if (best) union('r:' + rowKey(r), 'r:' + rowKey(best));
	}
	const groups = new Map<string, T[]>();
	const order: string[] = [];
	for (const r of rows) {
		const root = find('r:' + rowKey(r));
		if (!groups.has(root)) {
			groups.set(root, []);
			order.push(root);
		}
		groups.get(root)!.push(r);
	}
	return order.map((root) => {
		const list = groups.get(root)!;
		list.sort((a, b) => time(b) - time(a) || b.uid - a.uid);
		return { key: rowKey(list[0]), rows: list };
	});
}
