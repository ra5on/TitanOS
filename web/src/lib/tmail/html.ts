// 1.05: an HTML mail for the sandboxed frame (docs/TMAIL.md, "Sicherheit der
// Anzeige"). titand sends the HTML already cleaned; this module only wraps
// it. The frame has no scripts and no same origin, so the page cannot reach
// into it afterwards: the real targets of links and the warnings about them
// are written into the HTML before it becomes `srcdoc`. Parsing uses an
// inert document (DOMParser): nothing runs and nothing loads there, and no
// node of it ever enters the live page.
import { hostOf, linkMismatch } from './text.ts';

/** The sandbox of the frame: links may open a new tab, nothing else (contract). */
export const MAIL_SANDBOX = 'allow-popups allow-popups-to-escape-sandbox';

/**
 * The Content-Security-Policy of the frame. Embedded pictures (cid:) come in
 * as data: URLs the page fetched itself; external pictures only after
 * "laden" (https:).
 */
export function mailCsp(images: boolean): string {
	return `default-src 'none'; style-src 'unsafe-inline'; img-src data:${images ? ' https:' : ''}; font-src 'none'`;
}

/** A link of the message with its real target and, if its text names another domain, that domain. */
export type MailLink = { href: string; text: string; host: string; warn: string };

export type Prepared = { srcdoc: string; links: MailLink[] };

const BASE_STYLE = [
	':root{color-scheme:light}',
	'body{margin:12px 16px;font:15px/1.5 system-ui,-apple-system,"Segoe UI",sans-serif;background:Canvas;color:CanvasText;overflow-wrap:anywhere;word-break:normal}',
	'img{max-width:100%;height:auto}',
	'pre{white-space:pre-wrap}',
	'table{max-width:100%}',
	'blockquote{margin:0 0 0 .6em;padding-left:.8em;border-left:3px solid GrayText}',
	'.tmail-warn{display:inline-block;margin-left:.3em;padding:0 .3em;border:1px solid CanvasText;border-radius:3px;background:Mark;color:MarkText;font-size:.85em;font-weight:600}',
	'.tmail-img{display:inline-block;padding:.2em .4em;border:1px dashed GrayText;color:GrayText;font-size:.85em}',
].join('');

// Defence in depth: titand removes these already; the frame would not run
// them either.
const DROP = 'script,iframe,frame,frameset,object,embed,applet,form,meta,base,link,noscript';

/**
 * The HTML of a message as a whole document for `srcdoc`: the CSP meta
 * first in <head>, then the base style, the mail's own style blocks and its
 * body. `cids` maps the Content-IDs of embedded pictures to data: URLs.
 * `warnText(host)` is the visible warning after a link whose text names
 * another domain; `missing` stands in for an embedded picture not loaded.
 */
export function prepareHtml(
	html: string,
	opts: {
		images: boolean;
		cids?: Map<string, string>;
		warnText: (host: string) => string;
		missing?: string;
	},
): Prepared {
	const doc = new DOMParser().parseFromString(html, 'text/html');
	doc.querySelectorAll(DROP).forEach((el) => el.remove());
	for (const el of doc.querySelectorAll('*'))
		for (const attr of [...el.attributes])
			if (/^on/i.test(attr.name) || attr.name.toLowerCase() === 'srcdoc')
				el.removeAttribute(attr.name);

	const links: MailLink[] = [];
	for (const a of doc.querySelectorAll('a')) {
		const href = (a.getAttribute('href') ?? '').trim();
		const scheme = /^([a-z][a-z0-9+.-]*):/i.exec(href)?.[1]?.toLowerCase() ?? '';
		if (!href || !['http', 'https', 'mailto'].includes(scheme)) {
			a.removeAttribute('href');
			continue;
		}
		a.setAttribute('target', '_blank');
		a.setAttribute('rel', 'noopener noreferrer');
		// The real target, for the tooltip and the browser's status line.
		a.setAttribute('title', href);
		const text = (a.textContent ?? '').replace(/\s+/g, ' ').trim();
		const warn = scheme === 'mailto' ? '' : linkMismatch(href, text);
		if (warn) {
			const mark = doc.createElement('span');
			mark.className = 'tmail-warn';
			mark.textContent = opts.warnText(warn);
			a.after(mark);
		}
		links.push({ href, text: text || href, host: hostOf(href), warn });
	}

	for (const img of doc.querySelectorAll('img')) {
		const src = (img.getAttribute('src') ?? '').trim();
		if (!/^cid:/i.test(src)) continue;
		const cid = src.slice(4).replace(/^<|>$/g, '');
		const data = opts.cids?.get(cid);
		if (data) img.setAttribute('src', data);
		else {
			const note = doc.createElement('span');
			note.className = 'tmail-img';
			note.textContent = img.getAttribute('alt') || opts.missing || '';
			img.replaceWith(note);
		}
	}

	const styles = [...doc.querySelectorAll('style')]
		.map((s) => {
			const text = s.textContent ?? '';
			s.remove();
			// Nothing may load through a style block (the CSP stops it too).
			return text.replace(/@import[^;]*;?/gi, '');
		})
		.filter(Boolean);
	const csp = escapeAttr(mailCsp(opts.images));
	const head =
		`<meta http-equiv="Content-Security-Policy" content="${csp}">` +
		'<meta charset="utf-8">' +
		'<meta name="referrer" content="no-referrer">' +
		`<style>${BASE_STYLE}</style>` +
		styles.map((s) => `<style>${s.replace(/<\/style/gi, '')}</style>`).join('');
	const srcdoc = `<!doctype html><html><head>${head}</head><body>${doc.body?.innerHTML ?? ''}</body></html>`;
	return { srcdoc, links };
}

function escapeAttr(value: string): string {
	return value.replace(/&/g, '&amp;').replace(/"/g, '&quot;').replace(/</g, '&lt;');
}

/** The Content-IDs a message's HTML asks for (cid:…), each once. */
export function cidsIn(html: string): string[] {
	const out = new Set<string>();
	for (const m of html.matchAll(/\bsrc\s*=\s*["']?\s*cid:([^"'\s>]+)/gi))
		out.add(m[1].replace(/^<|>$/g, ''));
	return [...out];
}
