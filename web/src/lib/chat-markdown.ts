// A small, safe part of Markdown for the answers of the assistant. All text
// is escaped first; only code, bold, italics, headings, lists, tables and
// paragraphs become HTML. Links only lead to pages of this web UI from a
// fixed list; nothing a model writes can become another link, an image or
// a script.

export function escapeHtml(text: string): string {
	return text.replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c]!);
}

const NAME = '[A-Za-z0-9_.-]{1,64}';
/** Pages of the web UI the assistant may link to. */
const PAGE = new RegExp(
	`^/(?:|storage|shares|files|users|docker|docker/apps/${NAME}|apps|vms|vms\\?vm=${NAME}|plugins|tools|account|assistant|settings|settings/[a-z-]{2,30})$`
);

/** Reports whether a path is a page of the web UI the assistant may link to. */
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
	return line.trim().replace(/^\|/, '').replace(/\|$/, '').split('|').map((cell) => inline(cell.trim()));
}

function table(lines: string[]): string {
	const [head, , ...body] = lines;
	const th = cells(head).map((c) => `<th>${c}</th>`).join('');
	const rows = body.map((line) => `<tr>${cells(line).map((c) => `<td>${c}</td>`).join('')}</tr>`).join('');
	return `<div class="table"><table><thead><tr>${th}</tr></thead><tbody>${rows}</tbody></table></div>`;
}

function block(paragraph: string): string {
	const lines = paragraph.split('\n').filter((line) => line.trim() !== '');
	if (lines.length === 0) return '';
	if (lines.length >= 2 && ROW.test(lines[0]) && RULE.test(lines[1]) && lines.slice(2).every((l) => ROW.test(l))) return table(lines);
	const out: string[] = [];
	const open: { list: { tag: 'ul' | 'ol'; items: string[] } | null } = { list: null };
	let text: string[] = [];
	const flushText = () => {
		if (text.length) out.push(`<p>${text.map(inline).join('<br>')}</p>`);
		text = [];
	};
	const flushList = () => {
		const list = open.list;
		if (list) out.push(`<${list.tag}>${list.items.map((i) => `<li>${inline(i)}</li>`).join('')}</${list.tag}>`);
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
			open.list ??= { tag, items: [] };
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

export function chatMarkdown(text: string): string {
	return escapeHtml(text)
		.split(/```[A-Za-z0-9_+-]*\n?/)
		.map((part, index) => (index % 2 === 1 ? `<pre><code>${part.replace(/\n$/, '')}</code></pre>` : part.split(/\n{2,}/).map(block).join('')))
		.join('');
}
