import { expect, test, vi } from 'vitest';
import { render } from '@testing-library/svelte';
import ResultCards, { appRows, deviceCards } from '#lib/components/agent/ResultCards.svelte';

const at = '2026-11-15T20:00:00Z';
const step = (tool: string, result: unknown, ok = true) =>
	({
		id: tool + Math.random(),
		role: 'tool',
		tool,
		ok,
		at,
		result: JSON.stringify(result),
	}) as never;
const docker = step('docker_list', {
	observedAt: at,
	containers: [
		{ name: 'ollama-ollama-1', state: 'running', memoryBytes: 2147483648, updateAvailable: true },
		{ name: 'immich-redis-1', state: 'exited', memoryBytes: 0 },
		{ name: 'bambu-1', state: 'running', memoryBytes: 1048576 },
		{ broken: true },
	],
});

test('the containers of a turn: stopped first, nothing from a broken result', () => {
	const rows = appRows([docker]);
	expect(rows.map((r) => r.name)).toEqual(['immich-redis-1', 'bambu-1', 'ollama-ollama-1']);
	expect(rows[2].update).toBe(true);
	expect(appRows([step('docker_list', { containers: 'cut off' })])).toEqual([]);
	expect(appRows([{ ...(docker as object), ok: false } as never])).toEqual([]);
	expect(
		appRows([{ ...(docker as object), result: '{"containers":[{"name":"x","sta' } as never]),
	).toEqual([]);
});

test('a device that was read: once, with its newest state; sensors get no switch', () => {
	const cards = deviceCards([
		step('ha_state', {
			entity_id: 'light.stehlampe',
			state: 'off',
			attributes: { friendly_name: 'Stehlampe' },
		}),
		step('ha_state', {
			entity_id: 'light.stehlampe',
			state: 'on',
			attributes: { friendly_name: 'Stehlampe' },
		}),
		step('ha_state', { entity_id: 'sensor.temp', state: '21.5', attributes: {} }),
	]);
	expect(cards).toEqual([
		{ id: 'light.stehlampe', name: 'Stehlampe', state: 'on', domain: 'light' },
	]);
});

test('the buttons ask the agent in words; an old answer has none', async () => {
	const onask = vi.fn();
	const lamp = step('ha_state', {
		entity_id: 'light.stehlampe',
		state: 'on',
		attributes: { friendly_name: 'Stehlampe' },
	});
	const { getAllByRole, getByRole, container, rerender } = render(ResultCards, {
		steps: [docker, lamp],
		active: true,
		onask,
	});
	expect(container.textContent).toContain('2 of 3 running');
	expect(container.textContent).toContain('update available');
	getAllByRole('button', { name: 'Start' })[0].click();
	expect(onask).toHaveBeenLastCalledWith('Start the container immich-redis-1');
	getAllByRole('button', { name: 'Restart' })[0].click();
	expect(onask).toHaveBeenLastCalledWith('Restart the container bambu-1');
	getByRole('button', { name: 'Switch off' }).click();
	expect(onask).toHaveBeenLastCalledWith('turn off Stehlampe');
	await rerender({ active: false });
	expect(container.querySelectorAll('button').length).toBe(0);
	expect(container.textContent).toContain('immich-redis-1');
});
