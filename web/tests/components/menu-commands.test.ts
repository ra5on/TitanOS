// PLAN 67-1: commands a page registers for the menu bar – states follow the
// page, disabled commands do not run, everything goes when the page goes.
import { expect, test, vi } from 'vitest';
import { fireEvent, render } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import { localCommandStates, runLocalCommand } from '#lib/menu-commands.svelte.ts';
import MenuProbe from './fixtures/MenuProbe.svelte';

test('registered commands run, follow their state and end with the page', async () => {
	const onmkdir = vi.fn();
	const { unmount, getByText } = render(MenuProbe, { onmkdir });
	expect(localCommandStates()).toEqual({
		'files.mkdir': { enabled: false },
		'files.hidden': { enabled: true, checked: false },
	});
	expect(runLocalCommand('files.mkdir')).toBe(false);
	expect(onmkdir).not.toHaveBeenCalled();
	await fireEvent.click(getByText('writable'));
	expect(runLocalCommand('files.mkdir')).toBe(true);
	expect(onmkdir).toHaveBeenCalledTimes(1);
	runLocalCommand('files.hidden');
	flushSync();
	expect(localCommandStates()['files.hidden']).toEqual({ enabled: true, checked: true });
	expect(runLocalCommand('files.nothing')).toBe(false);
	unmount();
	expect(localCommandStates()).toEqual({});
});
