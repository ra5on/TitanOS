// 1.38 (owner 28.11.2026, §122): the answers of the personal Agent are
// Markdown. Until 1.37 they were shown as plain text, with every "**", "#",
// "---" and "|" visible. markdown-it reads CommonMark and tables; HTML in the
// answer stays text (`html: false`), links only go to http(s), mailto and the
// pages of TitanOS, and open outside in a new tab.
import MarkdownIt from 'markdown-it';
import { msg } from './labels.ts';

const md = new MarkdownIt({ html: false, linkify: true, breaks: true, typographer: false });

const SAFE_LINK = /^(https?:\/\/|mailto:|\/(?!\/))/i;
md.validateLink = (url: string) => SAFE_LINK.test(url.trim());

const defaultLink =
	md.renderer.rules.link_open ??
	((tokens, idx, options, _env, self) => self.renderToken(tokens, idx, options));
md.renderer.rules.link_open = (tokens, idx, options, env, self) => {
	const token = tokens[idx];
	const href = token.attrGet('href') ?? '';
	if (/^https?:/i.test(href)) {
		token.attrSet('target', '_blank');
		token.attrSet('rel', 'noopener noreferrer');
	}
	return defaultLink(tokens, idx, options, env, self);
};

// A table scrolls sideways inside its own frame instead of widening the chat.
md.renderer.rules.table_open = () => '<div class="pa-table"><table>';
md.renderer.rules.table_close = () => '</table></div>';

// "- [ ]" and "- [x]" as boxes that only show the state (with a name for
// screen readers: the state is never told by the look alone).
function taskBoxes(html: string): string {
	return html.replace(/<li>(<p>)?\[( |x|X)\]\s?/g, (_all, p: string | undefined, mark: string) => {
		const done = mark.toLowerCase() === 'x';
		return `<li class="pa-task">${p ?? ''}<span class="pa-box${done ? ' done' : ''}" role="img" aria-label="${msg(done ? 'taskDone' : 'taskOpen')}"></span>`;
	});
}
/** The answer as safe HTML. */
export function answerHtml(text: string): string {
	return taskBoxes(md.render(text ?? ''));
}
