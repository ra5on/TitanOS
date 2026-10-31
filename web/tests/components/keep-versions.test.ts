import { expect, test } from 'vitest';
import { fireEvent, render } from '@testing-library/svelte';
import KeepVersions, { keepPayload, lastOf } from '#lib/components/KeepVersions.svelte';
import { keepChoicePayload } from '#lib/components/KeepChoice.svelte';

test('keep is one number, or every version until full', () => {
	// Rules by day, week and month from 0.41.0 show as their total.
	expect(lastOf({ daily: 7, weekly: 4, monthly: 6 })).toBe(17);
	expect(lastOf({ last: 5, daily: 7, weekly: 0, monthly: 0 })).toBe(5);
	expect(lastOf({ daily: 0, weekly: 0, monthly: 0 })).toBe(10);
	expect(keepPayload({ last: 5, daily: 7, weekly: 4, monthly: 6, dailyAt: '03:00' })).toEqual({
		last: 5,
		daily: 0,
		weekly: 0,
		monthly: 0,
	});
	expect(
		keepPayload({ daily: 7, weekly: 0, monthly: 0, mode: 'rolling', maxAgeMonths: 3 }),
	).toEqual({
		daily: 0,
		weekly: 0,
		monthly: 0,
		mode: 'rolling',
	});
	// Snapshots, stick and replication: the count; rules only for "until full".
	expect(keepChoicePayload(7, { daily: 7, weekly: 4, monthly: 6 }, 1, 100)).toEqual({
		keep: 7,
		versions: undefined,
	});
	expect(keepChoicePayload(0, null, 2, 365)).toEqual({ keep: 2, versions: undefined });
	expect(
		keepChoicePayload(7, { daily: 0, weekly: 0, monthly: 0, mode: 'rolling' }, 2, 365),
	).toMatchObject({
		keep: 365,
		versions: { mode: 'rolling' },
	});
});

test('the keep block offers "until full" only where asked', async () => {
	const keep = { last: 10, daily: 0, weekly: 0, monthly: 0 };
	const { container, rerender } = render(KeepVersions, { props: { keep, target: 'drive' } });
	expect(container.textContent).toContain('Keep until the drive is full');
	const radios = container.querySelectorAll('input[type=radio]');
	expect(radios).toHaveLength(2);
	await fireEvent.click(radios[1]);
	expect(container.querySelector<HTMLInputElement>('input[type=number]')!.disabled).toBe(true);
	await rerender({ keep, rolling: false, unit: 'snapshots' });
	expect(container.querySelectorAll('input[type=radio]')).toHaveLength(0);
	expect(container.textContent).toContain('snapshots');
	expect(container.textContent).not.toContain('until');
});
