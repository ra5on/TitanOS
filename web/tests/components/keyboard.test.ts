// 1.03 (D102-49): tabs, segments and dropdown menus by keyboard after the
// WAI-ARIA patterns – one Tab stop per group, arrow keys, Home and End, and
// a chosen menu item does not drop the focus.
import { beforeAll, expect, test } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/svelte';
import { tick } from 'svelte';
import KeysProbe from './fixtures/KeysProbe.svelte';

beforeAll(() => {
	// jsdom keeps a popover hidden (and its items unfocusable): shown, it
	// becomes a plain element.
	const proto = HTMLElement.prototype as unknown as Record<string, unknown>;
	proto.showPopover = function (this: HTMLElement) {
		this.removeAttribute('popover');
	};
	proto.hidePopover ??= function () {};
});

const key = async (key: string) => {
	const target = document.activeElement ?? document.body;
	await fireEvent.keyDown(target, { key });
	await tick();
};
// The panel waits a tick before it shows and focuses its first item.
const settle = async () => {
	for (let i = 0; i < 4; i++) await tick();
};
const tabStops = (role: string) =>
	screen.getAllByRole(role).map((el) => el.getAttribute('tabindex'));

test('tabs: one Tab stop, arrows choose, Home and End', async () => {
	render(KeysProbe);
	expect(tabStops('tab')).toEqual(['0', '-1', '-1']);
	screen.getByRole('tab', { name: 'A' }).focus();
	await key('ArrowRight');
	expect(document.activeElement).toBe(screen.getByRole('tab', { name: 'B' }));
	expect(screen.getByRole('tab', { name: 'B' }).getAttribute('aria-selected')).toBe('true');
	expect(tabStops('tab')).toEqual(['-1', '0', '-1']);
	await key('End');
	expect(document.activeElement).toBe(screen.getByRole('tab', { name: 'C' }));
	await key('ArrowRight');
	expect(document.activeElement).toBe(screen.getByRole('tab', { name: 'A' }));
	await key('Home');
	expect(document.activeElement).toBe(screen.getByRole('tab', { name: 'A' }));
	// Up and down are the page's (a horizontal row of tabs).
	await key('ArrowDown');
	expect(document.activeElement).toBe(screen.getByRole('tab', { name: 'A' }));
});

test('a row without a chosen tab can still be reached', () => {
	render(KeysProbe, { tab: 'none', mode: 'none' });
	expect(tabStops('tab')).toEqual(['0', '-1', '-1']);
	expect(tabStops('radio')).toEqual(['0', '-1']);
});

test('segments: one Tab stop, arrows choose', async () => {
	render(KeysProbe);
	expect(tabStops('radio')).toEqual(['0', '-1']);
	screen.getByRole('radio', { name: 'X' }).focus();
	await key('ArrowDown');
	const y = screen.getByRole('radio', { name: 'Y' });
	expect(document.activeElement).toBe(y);
	expect(y.getAttribute('aria-checked')).toBe('true');
	expect(tabStops('radio')).toEqual(['-1', '0']);
});

test('dropdown: arrow down opens at the first item; a chosen item gives the focus back', async () => {
	render(KeysProbe);
	const button = screen.getByRole('button', { name: 'more' });
	button.focus();
	await key('ArrowDown');
	await settle();
	expect(document.activeElement?.textContent).toBe('one');
	await key('ArrowDown');
	expect(document.activeElement?.textContent).toBe('two');
	(document.activeElement as HTMLElement).click();
	await tick();
	expect(screen.queryByRole('menu')).toBeNull();
	expect(document.activeElement).toBe(button);
	// Arrow up opens at the last item; Tab leaves the menu from the button.
	await key('ArrowUp');
	await settle();
	expect(document.activeElement?.textContent).toBe('two');
	await key('Tab');
	expect(screen.queryByRole('menu')).toBeNull();
	expect(document.activeElement).toBe(button);
});

test('dropdown: an item chosen by click keeps the focus on the button', async () => {
	render(KeysProbe);
	const button = screen.getByRole('button', { name: 'more' });
	await fireEvent.click(button);
	await settle();
	const one = screen.getByRole('menuitem', { name: 'one' });
	one.focus();
	await fireEvent.click(one);
	await tick();
	expect(document.querySelector('[data-chosen]')?.textContent).toBe('one');
	expect(document.activeElement).toBe(button);
});
