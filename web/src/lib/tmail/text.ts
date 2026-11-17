// 1.05: plain helpers of T-Mail – dates, addresses, subjects, quoting and
// the real targets of links. No DOM, no requests.
import type { Address, Message } from './types.ts';

function sameDay(a: Date, b: Date) {
	return (
		a.getFullYear() === b.getFullYear() &&
		a.getMonth() === b.getMonth() &&
		a.getDate() === b.getDate()
	);
}

/** The date in the list: the time for today, else the day with its date (contract: "Datum mit Tag, wenn nicht heute"). */
export function listDate(iso: string, locale: string, now = new Date()): string {
	const date = new Date(iso);
	if (Number.isNaN(date.getTime())) return '';
	if (sameDay(date, now))
		return new Intl.DateTimeFormat(locale, { hour: '2-digit', minute: '2-digit' }).format(date);
	return new Intl.DateTimeFormat(locale, {
		weekday: 'short',
		day: 'numeric',
		month: 'short',
		year: date.getFullYear() === now.getFullYear() ? undefined : 'numeric',
	}).format(date);
}

/** The full date and time in the head of a message. */
export function fullDate(iso: string, locale: string): string {
	const date = new Date(iso);
	if (Number.isNaN(date.getTime())) return '';
	return new Intl.DateTimeFormat(locale, {
		weekday: 'long',
		day: 'numeric',
		month: 'long',
		year: 'numeric',
		hour: '2-digit',
		minute: '2-digit',
	}).format(date);
}

/** The name of a sender, or the address when it has none. */
export function senderName(a: Address | null | undefined): string {
	if (!a) return '';
	return a.name.trim() || a.addr;
}

/** "Name <addr>", or the address alone. */
export function addressText(a: Address): string {
	return a.name.trim() ? `${a.name.trim()} <${a.addr}>` : a.addr;
}

const EMAIL = /^[^\s@<>(),;:"]+@[^\s@<>(),;:"]+\.[^\s@<>(),;:"]+$/;

/** Whether a string is an e-mail address (the plain form, as the backend takes it). */
export function validAddress(addr: string): boolean {
	return EMAIL.test(addr.trim());
}

/** The address of "Name <addr>" or of a plain address. */
export function addrOf(text: string): string {
	const m = /<([^<>]+)>\s*$/.exec(text);
	return (m ? m[1] : text).trim();
}

/** Typed recipients split at commas, semicolons and line breaks. */
export function splitRecipients(text: string): string[] {
	return text
		.split(/[,;\n]+/)
		.map((part) => part.trim())
		.filter(Boolean);
}

function prefixed(subject: string, prefix: string, known: RegExp): string {
	const s = subject.trim();
	return known.test(s) ? s : `${prefix} ${s}`.trim();
}

/** "Re: …" once, also after "AW:" from German mail programs. */
export function replySubject(subject: string): string {
	return prefixed(subject, 'Re:', /^(re|aw|antw)\s*:/i);
}

/** "Fwd: …" once, also after "WG:". */
export function forwardSubject(subject: string): string {
	return prefixed(subject, 'Fwd:', /^(fwd?|wg)\s*:/i);
}

/** Every line of a text with "> " in front. */
export function quote(text: string): string {
	return text
		.replace(/\r\n/g, '\n')
		.replace(/\n+$/, '')
		.split('\n')
		.map((line) => (line.startsWith('>') ? '>' + line : '> ' + line))
		.join('\n');
}

const lower = (s: string) => s.trim().toLowerCase();

/** Who a reply goes to: Reply-To or the sender; for "Allen antworten" also the others, never oneself. */
export function replyRecipients(
	msg: Pick<Message, 'from' | 'to' | 'cc' | 'replyTo'>,
	me: string,
	all: boolean,
): { to: string[]; cc: string[] } {
	const first = (msg.replyTo?.length ? msg.replyTo : [msg.from]).filter(Boolean);
	const to = unique(first.map(addressText), [me]);
	if (!all) return { to, cc: [] };
	const others = [...(msg.to ?? []), ...(msg.cc ?? [])].map(addressText);
	const cc = unique(others, [me, ...to.map(addrOf)]);
	return { to, cc };
}

/** The entries in order, each address once, without the ones in `skip`. */
export function unique(list: string[], skip: string[] = []): string[] {
	const seen = new Set(skip.map(lower).filter(Boolean));
	const out: string[] = [];
	for (const entry of list) {
		const key = lower(addrOf(entry));
		if (!key || seen.has(key)) continue;
		seen.add(key);
		out.push(entry);
	}
	return out;
}

/** A piece of plain text: words, or a link with its real target. */
export type Piece = { text: string; href?: string };

const URL_RE = /\bhttps?:\/\/[^\s<>"'()[\]{}]+[^\s<>"'()[\]{}.,;:!?]/gi;

/** Plain text cut into words and links (http/https only); nothing becomes HTML. */
export function linkify(text: string): Piece[] {
	const out: Piece[] = [];
	let last = 0;
	for (const m of text.matchAll(URL_RE)) {
		const at = m.index ?? 0;
		if (at > last) out.push({ text: text.slice(last, at) });
		out.push({ text: m[0], href: m[0] });
		last = at + m[0].length;
	}
	if (last < text.length) out.push({ text: text.slice(last) });
	return out;
}

/** The host of a link, without "www."; '' for anything that is not http(s). */
export function hostOf(href: string): string {
	try {
		const url = new URL(href);
		if (url.protocol !== 'http:' && url.protocol !== 'https:') return '';
		return url.hostname.toLowerCase().replace(/^www\./, '');
	} catch {
		return '';
	}
}

/** The domain of an address, for servers guessed when no provider is known. */
export function domainOf(email: string): string {
	return email.trim().toLowerCase().split('@')[1] ?? '';
}
