import { afterEach, expect, test, vi } from 'vitest';
import { render, waitFor } from '@testing-library/svelte';
import AgentVolume from '#lib/components/agent/AgentVolume.svelte';
import { reply } from './fetch.ts';

afterEach(() => vi.unstubAllGlobals());

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

// Review 1.02: leaving the page while a question about the move was on its
// way started the next one after the page was gone, every second until the
// move ended.
test('the move is not asked about any more once the page is gone', async () => {
	const move = {
		from: 'tank',
		to: 'ssd',
		state: 'copying',
		files: 1,
		bytes: 1,
		total: 10,
	};
	let asked = 0;
	let release: () => void = () => {};
	vi.stubGlobal(
		'fetch',
		vi.fn(async (url: string) => {
			if (url.endsWith('/agent/volume/move')) {
				asked++;
				// The first question hangs until the page is gone.
				if (asked === 1) await new Promise<void>((r) => (release = r));
				return reply(200, { move });
			}
			return reply(200, { volumes: [], move });
		}),
	);
	const { unmount } = render(AgentVolume, { props: { current: 'tank', onchosen: () => {} } });
	await waitFor(() => expect(asked).toBe(1), { timeout: 3000 });
	unmount();
	release();
	await sleep(2500);
	expect(asked).toBe(1);
}, 10000);
