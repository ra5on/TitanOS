// 1.13: the shared frame of Mail, Kontakte and Kalender – two separators
// with their own keys, folding kept per device, one level at a time when
// narrow – and sorting the side bar's entries with the keys.
import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/svelte';
import { tick } from 'svelte';
import { columnWidths, RAIL } from '#lib/components/ColumnFrame.svelte';
import { sortStatus } from '#lib/components/SortGrip.svelte';
import { i18n } from '#lib/i18n/index.svelte.ts';
import FrameProbe from './fixtures/FrameProbe.svelte';

i18n.locale = 'de';
// jsdom measures nothing (bind:clientWidth): the frame takes its default width.
beforeEach(() => {
	vi.stubGlobal(
		'ResizeObserver',
		class {
			observe() {}
			unobserve() {}
			disconnect() {}
		},
	);
});
afterEach(() => {
	localStorage.clear();
	vi.unstubAllGlobals();
});

const W = { min: 170, max: 420, fallback: 240 };
const L = { min: 280, max: 720, fallback: 380 };

test('the widths give way to the content, the side bar keeps its minimum first', () => {
	const at = (frame: number, side = 300, list: number | null = 500, folded = false) =>
		columnWidths({ frame, side, list, folded, sideW: W, listW: L, contentMin: 320 });
	expect(at(1440)).toMatchObject({ side: 300, list: 500 });
	expect(at(1000)).toMatchObject({ side: 300, list: 380 });
	expect(at(800)).toMatchObject({ side: 200, list: 280 });
	expect(at(600)).toMatchObject({ side: 170, list: 280 });
	expect(at(1440, 300, null)).toMatchObject({ side: 300, list: 0 });
	expect(at(1440, 300, 500, true)).toMatchObject({ side: RAIL, list: 500 });
});

test('columns: two separators with their keys, folding to the rail kept per device', async () => {
	localStorage.setItem('titanos.probe.listWidth', '400');
	render(FrameProbe);
	const seps = screen.getAllByRole('separator');
	expect(seps.map((s) => s.getAttribute('aria-label'))).toEqual([
		'Breite der Seitenleiste',
		'Breite der Liste',
	]);
	expect(seps[1].getAttribute('aria-valuenow')).toBe('400');
	await fireEvent.keyDown(seps[0], { key: 'ArrowRight' });
	expect(localStorage.getItem('titanos.probe.sideWidth')).toBe('256');
	// Dragging writes once, when the pointer lets go.
	const set = vi.spyOn(Storage.prototype, 'setItem');
	await fireEvent.pointerDown(seps[1], { button: 0, clientX: 100, pointerId: 1 });
	await fireEvent.pointerMove(seps[1], { clientX: 120, pointerId: 1 });
	await fireEvent.pointerMove(seps[1], { clientX: 140, pointerId: 1 });
	expect(set).not.toHaveBeenCalled();
	await fireEvent.pointerUp(seps[1], { pointerId: 1 });
	expect(set).toHaveBeenCalledTimes(1);
	expect(localStorage.getItem('titanos.probe.listWidth')).toBe('440');
	set.mockRestore();

	await fireEvent.click(screen.getByRole('button', { name: 'Seitenleiste einklappen' }));
	expect(localStorage.getItem('titanos.probe.sideFolded')).toBe('1');
	expect(screen.getByText('Rail')).toBeTruthy();
	expect(screen.queryByText('eins')).toBeNull();
	expect(screen.getByRole('button', { name: 'Neu' }).textContent).toBe('+');
	expect(screen.getAllByRole('separator')).toHaveLength(1);
	await fireEvent.click(screen.getByRole('button', { name: 'Seitenleiste ausklappen' }));
	expect(localStorage.getItem('titanos.probe.sideFolded')).toBeNull();
	expect(screen.getByText('eins')).toBeTruthy();
});

test('one level at a time: no separator, no folding, the list kept hidden', () => {
	render(FrameProbe, { columns: false, level: 'side' });
	expect(screen.queryAllByRole('separator')).toHaveLength(0);
	expect(screen.queryByRole('button', { name: /Seitenleiste/ })).toBeNull();
	expect(screen.getByText('eins')).toBeTruthy();
	expect(screen.queryByText('Inhalt')).toBeNull();
	expect(document.querySelector('.cf-list')!.hasAttribute('hidden')).toBe(true);
});

test('sorting with the keys: arrows on the grip, Alt+arrow on the entry, said in the status line', async () => {
	render(FrameProbe);
	const names = () => [...document.querySelectorAll('.entry')].map((e) => e.textContent);
	const grip = screen.getByRole('button', { name: '„eins“ verschieben' });
	await fireEvent.keyDown(grip, { key: 'ArrowDown' });
	expect(names()).toEqual(['zwei', 'eins', 'drei']);
	expect(sortStatus.text).toBe('„eins“ steht jetzt an Stelle 2 von 3.');
	await tick();
	expect(document.activeElement).toBe(grip);
	await fireEvent.keyDown(grip, { key: 'End' });
	expect(names()).toEqual(['zwei', 'drei', 'eins']);
	const drei = screen.getByRole('button', { name: 'drei' });
	await fireEvent.keyDown(drei, { key: 'ArrowUp' });
	expect(names()).toEqual(['zwei', 'drei', 'eins']);
	await fireEvent.keyDown(drei, { key: 'ArrowUp', altKey: true });
	expect(names()).toEqual(['drei', 'zwei', 'eins']);
	expect(sortStatus.text).toBe('„drei“ steht jetzt an Stelle 1 von 3.');
	// At the top nothing moves and nothing is said again.
	await fireEvent.keyDown(drei, { key: 'ArrowUp', altKey: true });
	expect(names()).toEqual(['drei', 'zwei', 'eins']);
});
