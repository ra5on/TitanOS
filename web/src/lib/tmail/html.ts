// 1.05: an HTML mail for the sandboxed frame (docs/TMAIL.md, "Sicherheit der
// Anzeige", "Festlegungen des Backends › Lesen"). titand sends the mail as
// a whole document, cleaned, with its own CSP meta and the embedded
// pictures as data: URLs; it goes into `srcdoc` unchanged. The frame has no
// scripts and no same origin, so the page cannot reach into it: the real
// targets of the links are read here from an inert copy (DOMParser – nothing
// runs and nothing loads there, and no node of it enters the live page) and
// listed below the frame. Whether a link's text names another domain is
// titand's judgement (`data-tmail-mismatch`, with "⚠ <domain>" in the
// document): the list says the same, never something of its own.
import { hostOf } from './text.ts';

/** The sandbox of the frame: links may open a new tab, nothing else (contract). */
export const MAIL_SANDBOX = 'allow-popups allow-popups-to-escape-sandbox';

/** A link of the message with its real target and, if its text names another domain, that domain. */
export type MailLink = { href: string; text: string; host: string; warn: string };

/** The links of a message (http, https, mailto), each with its real target. */
export function mailLinks(html: string): MailLink[] {
	const doc = new DOMParser().parseFromString(html, 'text/html');
	const links: MailLink[] = [];
	for (const a of doc.querySelectorAll('a[href]')) {
		const href = (a.getAttribute('href') ?? '').trim();
		const scheme = /^([a-z][a-z0-9+.-]*):/i.exec(href)?.[1]?.toLowerCase() ?? '';
		if (!['http', 'https', 'mailto'].includes(scheme)) continue;
		const text = (a.textContent ?? '').replace(/\s+/g, ' ').trim();
		const warn = a.hasAttribute('data-tmail-mismatch') ? hostOf(href) || href : '';
		links.push({ href, text: text || href, host: hostOf(href), warn });
	}
	return links;
}
