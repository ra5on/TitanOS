// 1.07/1.12: the agent's side bar keeps its separator after it became
// ColumnResize – value, keys, the key in localStorage, double click.
import { afterEach, expect, test } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/svelte';
import SideResize, { readSideWidth } from '#lib/components/agent/SideResize.svelte';
import { i18n } from '#lib/i18n/index.svelte.ts';

i18n.locale = 'de';
afterEach(() => localStorage.clear());

test('the agent side bar separator: arrows, Home/End, kept per device, double click resets', async () => {
	localStorage.setItem('titanos.agent.sideWidth', '300');
	expect(readSideWidth()).toBe(300);
	render(SideResize, { width: readSideWidth() });
	const sep = screen.getByRole('separator', { name: 'Breite der Seitenleiste' });
	expect(sep.getAttribute('aria-valuenow')).toBe('300');
	expect(sep.getAttribute('aria-valuemin')).toBe('200');
	expect(sep.getAttribute('aria-valuemax')).toBe('480');
	await fireEvent.keyDown(sep, { key: 'ArrowRight' });
	expect(sep.getAttribute('aria-valuenow')).toBe('316');
	expect(localStorage.getItem('titanos.agent.sideWidth')).toBe('316');
	await fireEvent.keyDown(sep, { key: 'End' });
	expect(sep.getAttribute('aria-valuenow')).toBe('480');
	await fireEvent.dblClick(sep);
	expect(sep.getAttribute('aria-valuenow')).toBe('264');
	expect(localStorage.getItem('titanos.agent.sideWidth')).toBeNull();
	localStorage.setItem('titanos.agent.sideWidth', '9999');
	expect(readSideWidth()).toBe(480);
});
