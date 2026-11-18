// 1.13: an HTML mail fits the frame's width – one style at the end of the
// head, the document otherwise as titand sent it.
import { expect, test } from 'vitest';
import { FIT_STYLE, fitMailHtml } from '#lib/tmail/html.ts';

test('the fitting style goes before </head>, or in front without a head', () => {
	const doc = '<!doctype html><html><HEAD><meta charset="utf-8"></HEAD ><body>x</body></html>';
	const fitted = fitMailHtml(doc);
	expect(fitted).toBe(
		'<!doctype html><html><HEAD><meta charset="utf-8">' +
			FIT_STYLE +
			'</HEAD ><body>x</body></html>',
	);
	expect(fitMailHtml('<p>x</p>')).toBe(FIT_STYLE + '<p>x</p>');
	// It loads nothing and runs nothing; fixed widths give way only when narrow.
	expect(FIT_STYLE).not.toMatch(/url\(|@import|script|expression/i);
	expect(FIT_STYLE).toMatch(/@media \(max-width:700px\)\{.*width:auto!important/);
});
