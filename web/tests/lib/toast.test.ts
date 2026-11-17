import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import Toasts from '#lib/components/Toasts.svelte';
import {
	dismissToast,
	holdToast,
	releaseToast,
	showToast,
	TOAST_MS,
	toasts,
} from '#lib/toast.svelte.ts';

beforeEach(() => vi.useFakeTimers());
afterEach(() => {
	for (const x of toasts.list) dismissToast(x.id);
	vi.useRealTimers();
});

test('a notice goes after its time; the same text again only restarts it', () => {
	const id = showToast('Docker: nein');
	vi.advanceTimersByTime(TOAST_MS - 1000);
	expect(showToast('Docker: nein')).toBe(id);
	expect(toasts.list).toHaveLength(1);
	vi.advanceTimersByTime(TOAST_MS - 1000);
	expect(toasts.list).toHaveLength(1);
	vi.advanceTimersByTime(1000);
	expect(toasts.list).toHaveLength(0);
});

test('at most three at once, the oldest goes first', () => {
	for (const n of [1, 2, 3, 4]) showToast(`Hinweis ${n}`);
	expect(toasts.list.map((x) => x.text)).toEqual(['Hinweis 2', 'Hinweis 3', 'Hinweis 4']);
});

test('a held notice stays until it is let go', () => {
	const id = showToast('lesen');
	holdToast(id);
	vi.advanceTimersByTime(TOAST_MS * 3);
	expect(toasts.list).toHaveLength(1);
	releaseToast(id);
	vi.advanceTimersByTime(TOAST_MS);
	expect(toasts.list).toHaveLength(0);
});

test('errors are read out as an alert, other notices as a status; both can be closed', async () => {
	const { container } = render(Toasts);
	const [polite, assertive] = [...container.querySelectorAll('[aria-live]')];
	// The live regions are there before any text (a new region is often not
	// read), but an empty one is no status of the page.
	expect(polite.getAttribute('aria-live')).toBe('polite');
	expect(assertive.getAttribute('aria-live')).toBe('assertive');
	expect(screen.queryByRole('status')).toBeNull();
	showToast('Gespeichert', 'info');
	showToast('Speichern fehlgeschlagen');
	flushSync();
	expect(screen.getByRole('status').textContent).toContain('Gespeichert');
	expect(polite.contains(screen.getByRole('status'))).toBe(true);
	const alert = screen.getByRole('alert');
	expect(alert.textContent).toContain('Speichern fehlgeschlagen');
	expect(assertive.contains(alert)).toBe(true);
	// Not by colour alone: a sign for the kind.
	expect(alert.querySelector('.sign')?.textContent).toBe('!');
	const close = alert.querySelector('button')!;
	expect(close.getAttribute('aria-label')).toBeTruthy();
	await fireEvent.click(close);
	expect(screen.queryByRole('alert')).toBeNull();
});
