import { afterEach, expect, test, vi } from 'vitest';
import { fireEvent, render, waitFor } from '@testing-library/svelte';
import NotesPanel from '#lib/components/agent/NotesPanel.svelte';
import { reply } from './fetch.ts';

// jsdom has no Web Animations: a transition ends at once.
Element.prototype.animate = () => {
	const a = { cancel() {}, finish() {}, onfinish: null as null | (() => void) };
	queueMicrotask(() => a.onfinish?.());
	return a as never;
};

afterEach(() => vi.unstubAllGlobals());

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

// Review 1.02: typing on while a save was still on its way sent the next
// save with the old time of the file. titand had written the first one in
// between and answered "changed elsewhere": the note showed a conflict and
// the last words were not saved.
test('a save waits for the one before it and sends its time', async () => {
	let modified = '2026-11-16T10:00:00Z';
	let content = 'Milch';
	let stamp = 0;
	const conflicts: string[] = [];
	vi.stubGlobal(
		'fetch',
		vi.fn(async (url: string, init?: RequestInit) => {
			if (url.includes('/agent/notes/file') && init?.method === 'PUT') {
				const req = JSON.parse(String(init.body));
				// titand writes at once; the answer is slow (a phone on the way).
				if (req.modified !== modified) {
					conflicts.push(req.content);
					await sleep(50);
					return reply(409, { error: { code: 'note_changed', message: 'changed' } });
				}
				content = req.content;
				modified = `2026-11-16T10:00:0${++stamp}Z`;
				const answer = modified;
				await sleep(1500);
				return reply(200, { path: req.path, modified: answer });
			}
			if (url.includes('/agent/notes/file')) {
				return reply(200, { path: 'Einkauf.md', content, modified });
			}
			if (url.endsWith('/agent/notes')) {
				return reply(200, {
					folders: [],
					notes: [{ path: 'Einkauf.md', title: 'Einkauf', folder: '', modified, size: 5 }],
				});
			}
			return reply(200, {});
		}),
	);
	const { container } = render(NotesPanel, { props: { onclose: () => {} } });
	const row = await waitFor(() => {
		const b = container.querySelector<HTMLButtonElement>('button.note');
		if (!b) throw new Error('no note yet');
		return b;
	});
	await fireEvent.click(row);
	const editor = await waitFor(() => {
		const e = container.querySelector<HTMLTextAreaElement>('textarea');
		if (!e) throw new Error('no editor yet');
		return e;
	});
	await fireEvent.input(editor, { target: { value: 'Milch Brot' } });
	// The first save starts after 700 ms and is answered 1.5 s later; the
	// second word comes in between.
	await sleep(1000);
	await fireEvent.input(editor, { target: { value: 'Milch Brot Eier' } });
	await waitFor(() => expect(content).toBe('Milch Brot Eier'), { timeout: 4000 });
	expect(conflicts).toEqual([]);
	expect(container.querySelector('.conflict')).toBeNull();
}, 10000);
