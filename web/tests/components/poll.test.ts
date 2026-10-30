import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import { render } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import PollProbe from './fixtures/PollProbe.svelte';

beforeEach(() => vi.useFakeTimers());
afterEach(() => vi.useRealTimers());

test('an effect with poll() does not start itself again after each answer', async () => {
	const onload = vi.fn();
	const { unmount } = render(PollProbe, { onload });
	// Let answers come in and effects settle, without time passing.
	for (let i = 0; i < 20; i++) {
		await Promise.resolve();
		flushSync();
	}
	expect(onload).toHaveBeenCalledTimes(1);
	await vi.advanceTimersByTimeAsync(3000);
	expect(onload).toHaveBeenCalledTimes(4);
	unmount();
	await vi.advanceTimersByTimeAsync(5000);
	expect(onload).toHaveBeenCalledTimes(4);
});
