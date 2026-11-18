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

/**
 * 1.13 (owner 18 Nov 2026, the phone): the mail fits the frame's width
 * instead of being cut off at the right. The one change to titand's
 * document: a style element before </head> (its CSP allows inline
 * styles; nothing runs, nothing loads). Pictures never grow wider than the
 * frame; below 700 px (the phone, narrow windows) tables and blocks give
 * up fixed widths and long words break, so newsletters made for 600–700
 * px reflow. Wide frames keep the mail's own layout. Pinch zoom stays.
 */
export const FIT_STYLE =
	'<style data-titanos-fit>' +
	'html{overflow-x:auto;-webkit-text-size-adjust:100%;text-size-adjust:100%}' +
	'img,video{max-width:100%!important;height:auto}' +
	'pre{white-space:pre-wrap!important}' +
	'@media (max-width:700px){' +
	'table,td,th,div,p,center,section{max-width:100%!important;box-sizing:border-box}' +
	'table[width],td[width],th[width],table[style*="width"],td[style*="width"],th[style*="width"]{width:auto!important}' +
	'body{overflow-wrap:anywhere}' +
	'}</style>';

/** The document with FIT_STYLE at the end of its head (or in front, without one). */
export function fitMailHtml(html: string): string {
	const at = html.search(/<\/head\s*>/i);
	return at < 0 ? FIT_STYLE + html : html.slice(0, at) + FIT_STYLE + html.slice(at);
}
