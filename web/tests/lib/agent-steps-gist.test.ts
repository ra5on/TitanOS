import { describe, expect, it } from 'vitest';
import { i18n } from '#lib/i18n/index.svelte.ts';
import { stepsGist } from '#lib/components/agent/chat/chat-text.svelte.ts';
import { photoHits } from '#lib/components/agent/PhotoGrid.svelte';
import type { Entry } from '#lib/agent.ts';

const step = (tool: string): Entry => ({ id: tool, role: 'tool', tool, ok: true }) as Entry;

// 1.07: the short summary of the steps never ends in "…".
describe('stepsGist', () => {
	it('names up to three and counts the rest in words', () => {
		const prev = i18n.locale;
		try {
			i18n.locale = 'de';
			const steps = ['tool_a', 'tool_b', 'tool_c', 'tool_d', 'tool_e'].map(step);
			const gist = stepsGist(steps);
			expect(gist).not.toContain('…');
			expect(gist.endsWith('und 2 weitere')).toBe(true);
			expect(stepsGist(steps.slice(0, 4)).endsWith('und 1 weiterer')).toBe(true);
			expect(stepsGist(steps.slice(0, 3))).toBe('tool a, tool b, tool c');
			i18n.locale = 'en';
			expect(stepsGist(steps).endsWith('and 2 more')).toBe(true);
		} finally {
			i18n.locale = prev;
		}
	});
});

// 1.07: the photo card reads only well-formed results of photos_search.
describe('photoHits', () => {
	const id = 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa';
	it('takes the newest readable result and only valid ids', () => {
		const result = JSON.stringify({
			photos: [
				{ id, date: '2025-07-14T18:02:00.000Z', place: 'Rom, Italien' },
				{ id: '../users/me', date: '' },
				{ id: id.replace('a', 'b'), video: true },
			],
		});
		const hits = photoHits([
			{ ...step('photos_search'), result } as Entry,
			{ ...step('docker_list'), result: '{}' } as Entry,
		]);
		expect(hits.map((h) => h.id)).toEqual([id, id.replace('a', 'b')]);
		expect(hits[1].video).toBe(true);
		expect(photoHits([{ ...step('photos_search'), result: '{"photos":[{"id":' } as Entry])).toEqual(
			[],
		);
	});
});
