import { afterEach, expect, test, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/svelte';
import EventPlayback from '#lib/nvr/EventPlayback.svelte';
import type { NvrEvent } from '#lib/nvr/types.ts';
import { reply } from './fetch.ts';

const at = Math.floor(new Date(2026, 10, 26, 9, 0, 5).getTime() / 1000);
const event: NvrEvent = {
	id: 'vehicle-17',
	camera: 'drive',
	start: at,
	end: at + 20,
	label: 'car',
	subLabel: '',
	plate: '',
	score: 0.9,
	reviewed: false,
	kept: false,
	hasClip: true,
	hasSnapshot: true,
	zones: [],
};
afterEach(() => {
	vi.restoreAllMocks();
	vi.unstubAllGlobals();
});

test('event timeline seeks the recording and maps its time through a gap in the camera index', async () => {
	vi.spyOn(HTMLMediaElement.prototype, 'canPlayType').mockReturnValue('probably');
	vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue();
	vi.stubGlobal(
		'fetch',
		vi.fn(async () =>
			reply(200, {
				segments: [
					{ start: at - 5, end: at - 1, motion: 0, objects: 0 },
					{ start: at + 5, end: at + 13, motion: 0, objects: 1 },
				],
			}),
		),
	);
	const { container } = render(EventPlayback, { event, phone: true });
	const video = container.querySelector('video')!;
	await waitFor(() => expect(video.src).toContain(`/drive/vod/${at - 5}/${at + 25}/master.m3u8`));
	Object.defineProperty(video, 'duration', { value: 12 });
	await fireEvent.loadedMetadata(video);
	const slider = (await screen.findByRole('slider')) as HTMLInputElement;
	expect(slider.max).toBe('12');
	await fireEvent.input(slider, { target: { value: '6' } });
	expect(video.currentTime).toBe(6);
	await waitFor(() =>
		expect(container.querySelector('time')?.dateTime).toBe(new Date((at + 7) * 1000).toISOString()),
	);
	video.currentTime = 7;
	await fireEvent.timeUpdate(video);
	expect(slider.value).toBe('7');
	expect(container.querySelectorAll('.frames img')).toHaveLength(6);
});

test('a recording that cannot play shows the real event snapshot and offers no timeline', async () => {
	vi.spyOn(HTMLMediaElement.prototype, 'canPlayType').mockReturnValue('probably');
	vi.stubGlobal(
		'fetch',
		vi.fn(async () => reply(200, { segments: [] })),
	);
	const { container } = render(EventPlayback, { event, phone: true });
	await fireEvent.error(container.querySelector('video')!);
	expect((await screen.findByRole('img')).getAttribute('src')).toContain(
		'/vehicle-17/snapshot.jpg',
	);
	expect(screen.queryByRole('slider')).toBeNull();
});
