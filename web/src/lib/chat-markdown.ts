// A small, safe part of Markdown for the answers of the KI-Agent. All text
// is escaped first; only code, bold, italics, headings, lists, tables and
// paragraphs become HTML. Links only lead to pages of this web UI from a
// fixed list; nothing a model writes can become another link, an image or
// a script.

import { highlight } from './code-highlight.ts';

export function escapeHtml(text: string): string {
	return text.replace(
		/[&<>"']/g,
		(c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c]!,
	);
}

function unescapeHtml(text: string): string {
	return text.replace(
		/&(amp|lt|gt|quot|#39);/g,
		(_, name: string) => ({ amp: '&', lt: '<', gt: '>', quot: '"', '#39': "'" })[name]!,
	);
}

const NAME = '[A-Za-z0-9_.-]{1,64}';
/** Pages of the web UI the agent may link to. */
const PAGE = new RegExp(
	`^/(?:|storage|shares|files|users|docker|docker/apps/${NAME}|apps|vms|vms\\?vm=${NAME}|plugins|tools|account|agent|settings|settings/[a-z-]{2,30})$`,
);

/** Reports whether a path is a page of the web UI the agent may link to. */
export function internalPage(path: string): boolean {
	return PAGE.test(path);
}

function inline(text: string): string {
	// Code first, so that nothing inside it becomes markup.
	const codes: string[] = [];
	text = text.replace(/`([^`]+)`/g, (_, code: string) => {
		codes.push(code);
		return `\u0000${codes.length - 1}\u0000`;
	});
	text = text
		.replace(/\[([^\]\n]{1,200})\]\(([^)\s]{1,220})\)/g, (all, label: string, href: string) => {
			const path = href.replace(/&amp;/g, '&');
			return internalPage(path) ? `<a href="${escapeHtml(path)}">${label}</a>` : all;
		})
		.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>')
		.replace(/(^|[\s(])\*([^*\s][^*]*)\*(?=[\s).,:;!?]|$)/g, '$1<em>$2</em>');
	return text.replace(/\u0000(\d+)\u0000/g, (_, i: string) => `<code>${codes[Number(i)]}</code>`);
}

const ITEM = /^\s*[-*]\s+/;
const NUMBERED = /^\s*\d+[.)]\s+/;
const HEADING = /^(#{1,4})\s+(.*)$/;
const ROW = /^\s*\|.*\|\s*$/;
const RULE = /^\s*\|?\s*:?-{2,}:?\s*(\|\s*:?-{2,}:?\s*)*\|?\s*$/;

function cells(line: string): string[] {
	return line
		.trim()
		.replace(/^\|/, '')
		.replace(/\|$/, '')
		.split('|')
		.map((cell) => inline(cell.trim()));
}

function table(lines: string[]): string {
	const [head, , ...body] = lines;
	const th = cells(head)
		.map((c) => `<th>${c}</th>`)
		.join('');
	const rows = body
		.map(
			(line) =>
				`<tr>${cells(line)
					.map((c) => `<td>${c}</td>`)
					.join('')}</tr>`,
		)
		.join('');
	return `<div class="table table-wrap"><table class="data-table"><thead><tr>${th}</tr></thead><tbody>${rows}</tbody></table></div>`;
}

// While an answer streams in (0.94, owner 15 Nov 2026: the screen flickered
// between table and raw text), the last line of a table is usually half a
// row and the line under the head half a rule. Both count as what they
// are about to become, so the table stands from its head row on and grows
// row by row.
const OPEN_ROW = /^\s*\|/;
const OPEN_RULE = /^\s*\|?[\s:|-]*$/;

function block(paragraph: string, live = false): string {
	const lines = paragraph.split('\n').filter((line) => line.trim() !== '');
	if (lines.length === 0) return '';
	if (
		lines.length >= 2 &&
		ROW.test(lines[0]) &&
		RULE.test(lines[1]) &&
		lines
			.slice(2)
			.every((l, i, rest) => ROW.test(l) || (live && i === rest.length - 1 && OPEN_ROW.test(l)))
	)
		return table(lines);
	if (
		live &&
		ROW.test(lines[0]) &&
		lines[0].split('|').length > 3 &&
		(lines.length === 1 || (lines.length === 2 && OPEN_RULE.test(lines[1])))
	)
		return table([lines[0], '']);
	const out: string[] = [];
	// `start`: the number the model gave the first item. A list the model
	// split by empty lines or a nested bullet goes on counting instead of
	// starting at 1 again each time (0.99).
	const open: { list: { tag: 'ul' | 'ol'; items: string[]; start: number } | null } = {
		list: null,
	};
	let text: string[] = [];
	const flushText = () => {
		if (text.length) out.push(`<p>${text.map(inline).join('<br>')}</p>`);
		text = [];
	};
	const flushList = () => {
		const list = open.list;
		if (list)
			out.push(
				`<${list.tag}${list.tag === 'ol' && list.start > 1 ? ` start="${list.start}"` : ''}>${list.items.map((i) => `<li>${inline(i)}</li>`).join('')}</${list.tag}>`,
			);
		open.list = null;
	};
	for (const line of lines) {
		const heading = HEADING.exec(line);
		const tag = ITEM.test(line) ? 'ul' : NUMBERED.test(line) ? 'ol' : null;
		if (heading) {
			flushText();
			flushList();
			out.push(`<h4>${inline(heading[2])}</h4>`);
		} else if (tag) {
			flushText();
			if (open.list?.tag !== tag) flushList();
			open.list ??= {
				tag,
				items: [],
				start: tag === 'ol' ? Math.min(parseInt(/\d+/.exec(line)?.[0] ?? '1', 10) || 1, 9999) : 1,
			};
			open.list.items.push(line.replace(tag === 'ul' ? ITEM : NUMBERED, ''));
		} else if (open.list && /^\s{2,}\S/.test(line)) {
			open.list.items[open.list.items.length - 1] += ' ' + line.trim();
		} else {
			flushList();
			text.push(line);
		}
	}
	flushText();
	flushList();
	return out.join('');
}

/** live: the text is still streaming in (its last table row may be half). */
export function chatMarkdown(text: string, live = false): string {
	// Text and code take turns; a fence names the language of its block,
	// which stands in the head of the block (ChatView adds "Kopieren").
	const parts = escapeHtml(text).split(/```([A-Za-z0-9_+-]*)\n?/);
	let out = '';
	for (let i = 0; i < parts.length; i += 2) {
		const part = parts[i];
		// parts: text, lang, code, lang (of the closing fence, empty), text …
		if ((i / 2) % 2 === 1) {
			const lang = parts[i - 1] ?? '';
			// The colors are made from the code as written (highlight escapes
			// every piece itself).
			const code = unescapeHtml(part.replace(/\n$/, ''));
			out += `<div class="code"><div class="code-head"><span>${lang || 'text'}</span></div><pre><code>${highlight(code, lang)}</code></pre></div>`;
		} else
			out += part
				.split(/\n{2,}/)
				.map((paragraph, n, all) =>
					block(paragraph, live && n === all.length - 1 && i === parts.length - 1),
				)
				.join('');
	}
	return out;
}
