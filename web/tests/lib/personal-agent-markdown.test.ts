import { describe, expect, it } from 'vitest';
import { answerHtml } from '../../src/lib/personal-agent/markdown.ts';

// 1.38 (§122): the 13 forms the owner's models write; none may show its raw marks.
describe('answerHtml', () => {
	const cases: [string, string, RegExp][] = [
		[
			'bold with italics inside',
			'**Achtung: *wichtig* bitte**',
			/<strong>Achtung: <em>wichtig<\/em> bitte<\/strong>/,
		],
		['bold italic', '***Hinweis***', /<em><strong>Hinweis<\/strong><\/em>/],
		['underscore bold', '__Status__: läuft', /<strong>Status<\/strong>: läuft/],
		['rule', 'A\n\n---\n\nB', /<hr>/],
		['level 5 heading', '##### Details', /<h5>Details<\/h5>/],
		['quote', '> Tipp', /<blockquote>\s*<p>Tipp<\/p>/],
		['strike', '~~alt~~ neu', /<s>alt<\/s> neu/],
		[
			'external link',
			'[Doku](https://immich.app/docs)',
			/<a href="https:\/\/immich.app\/docs" target="_blank" rel="noopener noreferrer">Doku<\/a>/,
		],
		['plus list', '+ eins\n+ zwei', /<ul>\s*<li>eins<\/li>\s*<li>zwei<\/li>/],
		[
			'task list',
			'- [x] erledigt\n- [ ] offen',
			/<li class="pa-task"><span class="pa-box done"[^>]*><\/span>erledigt/,
		],
		['nested', '1. Schritt\n   - Unterpunkt', /<ol>\s*<li>Schritt\s*<ul>\s*<li>Unterpunkt/],
		['table', '| a | b |\n|---|---|\n| 1 | 2 |', /<div class="pa-table"><table>/],
		['inline code', 'Volume `Backup`', /<code>Backup<\/code>/],
	];
	for (const [name, text, want] of cases)
		it(name, () => {
			const html = answerHtml(text);
			expect(html).toMatch(want);
			expect(html.replace(/<code>[^<]*<\/code>/g, '')).not.toMatch(/\*\*|__|~~|^#|\| a \|/m);
		});

	it('keeps HTML as text and drops unsafe links', () => {
		const html = answerHtml(
			'<img src=x onerror=alert(1)> [x](javascript:alert(1)) [y](data:text/html,hi)',
		);
		expect(html).not.toMatch(/<img|href="javascript|href="data/);
		expect(html).toContain('&lt;img');
	});

	it('links pages of TitanOS without a new tab', () => {
		expect(answerHtml('[Sicherungen](/backups)')).toMatch(/<a href="\/backups">Sicherungen<\/a>/);
	});
});
