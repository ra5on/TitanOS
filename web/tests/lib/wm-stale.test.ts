// PLAN 86: a page that reports itself late, after its window was sent to
// another page, must not take the window back – but a redirect or a page
// the user opens inside the window is always taken.
import { beforeEach, expect, test } from 'vitest';
import { navigateWindow, staleReport, wm, type Win } from '#lib/desktop/wm.svelte.ts';

function win(href: string): Win {
	return {
		id: 'w1',
		app: 'app:settings',
		href,
		title: 'Systemsteuerung',
		x: 0,
		y: 0,
		w: 800,
		h: 600,
		z: 1,
		minimized: false,
		snap: null,
	} as Win;
}

beforeEach(() => {
	wm.windows = [win('/settings/cluster')];
});

test('a late report of the page left behind is ignored, the new page is taken', () => {
	navigateWindow('w1', '/settings/wireguard');
	expect(staleReport('w1', '/settings/cluster')).toBe(true);
	expect(staleReport('w1', '/settings/wireguard')).toBe(false);
	// The move is over: the old page reports again later (a link inside the window).
	expect(staleReport('w1', '/settings/cluster')).toBe(false);
});

test('a redirect elsewhere ends the watch; the old page counts as late only a moment', () => {
	navigateWindow('w1', '/settings/stick');
	expect(staleReport('w1', '/settings/backups/configuration')).toBe(false);
	expect(staleReport('w1', '/settings/cluster')).toBe(false);
	wm.windows = [win('/settings/backups/data')];
	navigateWindow('w1', '/settings/backups#backup-daily');
	// A redirect back to the page left: taken once the moment is over.
	expect(staleReport('w1', '/settings/backups/data', Date.now() + 3000)).toBe(false);
});

test('a jump mark on the same page is no move', () => {
	navigateWindow('w1', '/settings/cluster#cluster-join');
	expect(staleReport('w1', '/settings/cluster')).toBe(false);
});
