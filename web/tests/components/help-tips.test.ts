import { afterEach, expect, test, vi } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import HardwarePicker from '#lib/components/HardwarePicker.svelte';
import PluginSteps from '#lib/components/PluginSteps.svelte';
import type { PluginProgress } from '#lib/plugin-progress.ts';
import { reply } from './fetch.ts';

afterEach(() => vi.unstubAllGlobals());

// PLAN 10b: the "?" sits right behind the technical term.
test('an NVIDIA card for containers explains CDI', async () => {
	vi.stubGlobal(
		'fetch',
		vi.fn(async () =>
			reply(200, [
				{
					id: 'a',
					title: 'Intel UHD 770',
					kind: 'igpu',
					vendor: '8086',
					available: true,
					reason: '',
				},
				{ id: 'b', title: 'NVIDIA RTX 3060', kind: 'nvidia', available: true, reason: '' },
			]),
		),
	);
	render(HardwarePicker, { selected: [] });
	const help = await screen.findAllByRole('button', { name: /CDI/ });
	// Only the NVIDIA card goes through CDI.
	expect(help).toHaveLength(1);
	expect(help[0].closest('label')?.textContent).toContain('NVIDIA RTX 3060');
});

test('the sysext step of a plugin installation explains sysext', () => {
	const step = (id: string) => ({ id, state: 'done' as const });
	const progress = {
		id: 'p',
		plugin: 'nvidia',
		action: 'install',
		state: 'done',
		steps: [step('download'), step('sysext')],
		bytes: 0,
		total: 0,
		speedBytesPerSecond: 0,
		started: '2026-11-03T10:00:00Z',
	} as PluginProgress;
	render(PluginSteps, { progress });
	expect(screen.getAllByRole('button', { name: /sysext/ })).toHaveLength(1);
});
