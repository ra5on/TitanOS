import { expect, test, vi } from 'vitest';
import { render } from '@testing-library/svelte';
import { tick } from 'svelte';
import ResultCards, { appRows, deviceCards } from '#lib/components/agent/ResultCards.svelte';

// jsdom has no Web Animations: a transition ends at once.
Element.prototype.animate = () => {
	const a = { cancel() {}, finish() {}, onfinish: null as null | (() => void) };
	queueMicrotask(() => a.onfinish?.());
	return a as never;
};

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

test('a row opens its actions; each asks the agent in words; an old answer has none', async () => {
	const onask = vi.fn();
	const lamp = step('ha_state', {
		entity_id: 'light.stehlampe',
		state: 'on',
		attributes: { friendly_name: 'Stehlampe' },
	});
	const { getByRole, queryByRole, container, rerender } = render(ResultCards, {
		steps: [docker, lamp],
		active: true,
		onask,
	});
	expect(container.textContent).toContain('2 of 3 running');
	expect(container.textContent).toContain('update available');
	// Closed rows show no actions.
	expect(queryByRole('button', { name: 'Restart' })).toBeNull();
	// A stopped container: start, log, details, why.
	getByRole('button', { name: /immich-redis-1/ }).click();
	await tick();
	getByRole('button', { name: 'Start' }).click();
	expect(onask).toHaveBeenLastCalledWith('Start the container immich-redis-1');
	getByRole('button', { name: 'Why stopped?' }).click();
	expect(onask.mock.lastCall?.[0]).toContain('Why is the container immich-redis-1 not running');
	expect(queryByRole('button', { name: 'Stop' })).toBeNull();
	// A running one with an update: the update first, then restart, stop …
	getByRole('button', { name: /ollama-ollama-1/ }).click();
	await tick();
	await new Promise((r) => setTimeout(r, 20));
	expect(queryByRole('button', { name: 'Start' })).toBeNull();
	getByRole('button', { name: 'Update' }).click();
	expect(onask).toHaveBeenLastCalledWith('Update ollama-ollama-1 to the new version');
	getByRole('button', { name: 'Stop' }).click();
	expect(onask).toHaveBeenLastCalledWith('Stop the container ollama-ollama-1');
	getByRole('button', { name: 'Look for errors' }).click();
	expect(onask.mock.lastCall?.[0]).toContain('errors and warnings');
	getByRole('button', { name: 'Switch off' }).click();
	expect(onask).toHaveBeenLastCalledWith('turn off Stehlampe');
	await rerender({ active: false });
	expect(container.querySelectorAll('button').length).toBe(0);
	expect(container.textContent).toContain('immich-redis-1');
});
