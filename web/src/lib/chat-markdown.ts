// A small, safe part of Markdown for the answers of the assistant. All text
// is escaped first; only code, bold, lists and paragraphs become HTML, and
// nothing a model writes can become a link, an image or a script.

export function escapeHtml(text: string): string {
	return text.replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c]!);
}

function inline(text: string): string {
	return text.replace(/`([^`]+)`/g, '<code>$1</code>').replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');
}

const ITEM = /^\s*(?:[-*]|\d+\.)\s+/;

export function chatMarkdown(text: string): string {
	return escapeHtml(text)
		.split(/```[a-z]*\n?/)
		.map((block, index) => {
			if (index % 2 === 1) return `<pre><code>${block.replace(/\n$/, '')}</code></pre>`;
			return block
				.split(/\n{2,}/)
				.map((paragraph) => {
					const lines = paragraph.split('\n').filter((line) => line.trim() !== '');
					if (lines.length === 0) return '';
					if (lines.every((line) => ITEM.test(line))) return `<ul>${lines.map((line) => `<li>${inline(line.replace(ITEM, ''))}</li>`).join('')}</ul>`;
					return `<p>${lines.map(inline).join('<br>')}</p>`;
				})
				.join('');
		})
		.join('');
}
