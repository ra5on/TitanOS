import { afterEach, expect, test, vi } from 'vitest';
import { fireEvent, render, waitFor } from '@testing-library/svelte';
import ProtectGuard from '#lib/components/agent/features/ProtectGuard.svelte';
import { t } from '#lib/i18n/index.svelte.ts';
import { reply } from './fetch.ts';

afterEach(() => vi.unstubAllGlobals());

const alert = (id: string, share: string, state: string) => ({
	id,
	share,
	volume: 'fast',
	client: { ip: '192.168.1.57', machine: 'PC', user: 'lena' },
	reason: 'known-extension',
	mode: 'lock',
	first: '2026-11-16T10:00:00Z',
	last: '2026-11-16T10:01:00Z',
	count: 40,
	files: [],
	state,
	locked: true,
	btrfs: true,
	snapshot: 'agent-20261116-100000',
	snapshotAt: '2026-11-16T10:00:00Z',
});

function button(container: HTMLElement, text: string) {
	// The last one: the dialog comes after the page.
	const b = [...container.querySelectorAll('button')].findLast(
		(x) => x.textContent?.trim() === text,
	);
	if (!b) throw new Error('no button ' + text);
	return b as HTMLButtonElement;
}

// Review 1.02: the confirm dialog acted on whichever alarm was shown when
// its button was clicked. A second attack on another share, read in by the
// poll while the dialog was open, got the "false alarm": that share was
// opened again during the attack.
test('the dialog acts on the alarm it was opened for', async () => {
	let reads = 0;
	const posts: string[] = [];
	vi.stubGlobal(
		'fetch',
		vi.fn(async (url: string, init?: RequestInit) => {
			if (init?.method === 'POST') {
				posts.push(url);
				return reply(200, alert('a', 'Documents', 'dismissed'));
			}
			reads++;
			const alerts =
				reads === 1
					? [alert('a', 'Documents', 'active')]
					: [alert('b', 'Fotos', 'active'), alert('a', 'Documents', 'active')];
			return reply(200, { enabled: true, watching: true, alerts });
		}),
	);
	const { container } = render(ProtectGuard);
	const dismiss = await waitFor(() => button(container, t('agent.f.protect.g.falseAlarm')));
	await fireEvent.click(dismiss);
	// The poll (every 5 s while an attack goes on) brings the second alarm.
	await waitFor(() => expect(reads).toBeGreaterThan(1), { timeout: 7000 });
	const ok = await waitFor(() => button(document.body, t('agent.f.protect.g.confirmBtn.dismiss')));
	await fireEvent.click(ok);
	await waitFor(() => expect(posts.length).toBe(1));
	expect(posts[0]).toContain('/guard/a/dismiss');
}, 12000);

// An automatic restore runs in the background: the page keeps asking
// until it is done, instead of stopping at "restoring" with a button that
// only gets a refusal.
test('the page follows a restore to its end', async () => {
	let reads = 0;
	vi.stubGlobal(
		'fetch',
		vi.fn(async () => {
			reads++;
			const state = reads === 1 ? 'restoring' : 'restored';
			return reply(200, {
				enabled: true,
				watching: true,
				alerts: [alert('a', 'Documents', state)],
			});
		}),
	);
	const { container } = render(ProtectGuard);
	const restore = await waitFor(() => button(container, t('agent.f.protect.g.restoreBtn')));
	expect(restore.disabled).toBe(true);
	await waitFor(() => expect(reads).toBe(2), { timeout: 7000 });
}, 12000);
