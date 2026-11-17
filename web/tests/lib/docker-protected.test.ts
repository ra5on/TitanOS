// N-102-29: an image the NVR or the speech services keep says so; a reason
// this page does not know yet falls back to a general text.
import { expect, test } from 'vitest';
import { protectedKey } from '#lib/docker-resources.ts';
import { i18n, tOr } from '#lib/i18n/index.svelte.ts';

test('each reason the NAS gives for keeping an image has its own text', () => {
	for (const locale of ['de', 'en'] as const) {
		i18n.locale = locale;
		const texts = ['viewer', 'sandbox', 'nvr', 'speech', 'later'].map((r) =>
			tOr(protectedKey(r), ''),
		);
		expect(texts.every((x) => x !== '')).toBe(true);
		expect(new Set(texts).size).toBe(5);
	}
	expect(protectedKey('nvr')).toBe('dres.protected.nvr');
	expect(protectedKey('later')).toBe('dres.protected.titanos');
});
