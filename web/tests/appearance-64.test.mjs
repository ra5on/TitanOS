// PLAN 64: the simple appearance page – new optional fields, the global
// sliders and the dock's list in the settings.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
	cssVariables,
	defaultAppearance,
	globalLook,
	resolveAccent,
	resolvePalette,
	sanitizeAppearance,
	withCorners,
	ACCENTS,
} from '../src/lib/theme/themes.ts';
import { contrast, saturate } from '../src/lib/theme/color.ts';
import {
	defaultDock,
	dockPinned,
	moveItem,
	sanitizeDesktop,
	sanitizeDock,
} from '../src/lib/desktop/prefs.ts';

test('colour strength: optional, 100 without it, limited to 50–150', () => {
	assert.equal(defaultAppearance().saturation, 100);
	// Stored by an older version: no field.
	const { saturation: _gone, ...old } = defaultAppearance();
	assert.equal(sanitizeAppearance(old).saturation, 100);
	assert.equal(sanitizeAppearance({ saturation: 20 }).saturation, 50);
	assert.equal(sanitizeAppearance({ saturation: 400 }).saturation, 150);
	assert.equal(sanitizeAppearance({ saturation: 'x' }).saturation, 100);
	assert.equal(sanitizeAppearance({ saturation: 87 }).saturation, 87);
	// Old fields are still read.
	const kept = sanitizeAppearance({
		...old,
		rTile: 20,
		rCtl: 4,
		glass: 60,
		nav: { layout: 'side' },
	});
	assert.equal(kept.rTile, 20);
	assert.equal(kept.rCtl, 4);
	assert.equal(kept.nav.layout, 'side');
});

test('colour strength changes the accent, never below 3:1 on light themes', () => {
	const dark = defaultAppearance();
	const p = resolvePalette(dark, false);
	assert.equal(resolveAccent(dark, p).toLowerCase(), ACCENTS.blau.hex.toLowerCase());
	assert.notEqual(resolveAccent({ ...dark, saturation: 50 }, p), resolveAccent(dark, p));
	for (const saturation of [50, 100, 150]) {
		const light = { ...defaultAppearance(), theme: 'hell', saturation };
		const lp = resolvePalette(light, true);
		assert.ok(contrast(resolveAccent(light, lp), lp.bg) >= 3, `contrast at ${saturation}`);
	}
	// Grey stays grey; hue and lightness stay.
	assert.equal(saturate('#808080', 1.5), '#808080');
	assert.equal(saturate('#6CB6FF', 1), '#6CB6FF');
});

test('the CSS variables for the desktop follow the sliders', () => {
	const a = { ...defaultAppearance(), glass: 90, blur: 30, rTile: 28, saturation: 120 };
	const v = cssVariables(a, false);
	assert.equal(v['--radius-scale'], '2.00');
	assert.equal(v['--desk-blur'], '36px');
	assert.equal(v['--wallpaper-filter'], 'saturate(1.20)');
	// Desktop glass keeps text readable: at least 45 % opaque on dark themes.
	assert.match(v['--desk-surface'], /0\.45\)$/);
	const flat = cssVariables(globalLook({ ...a, glass: 0, blur: 0 }), false);
	assert.equal(flat['--desk-blur'], '0px');
});

test('corners: one slider sets tiles and controls', () => {
	const a = withCorners(defaultAppearance(), 20);
	assert.equal(a.rTile, 20);
	assert.equal(a.rCtl, 14);
	assert.equal(withCorners(defaultAppearance(), 99).rTile, 32);
	assert.equal(withCorners(defaultAppearance(), 32).rCtl, 22);
});

test('the sliders count everywhere: glass style, menus and dialogs follow them', () => {
	const old = sanitizeAppearance({
		style: 'outline',
		menus: { own: true, glass: 10 },
		dialogs: { own: true, glass: 10, dim: 40 },
	});
	const g = globalLook(old);
	assert.equal(g.style, 'glass');
	assert.equal(g.menus.own, false);
	assert.equal(g.dialogs.own, false);
	// The stored own values stay for the way back; the dimming stays.
	assert.equal(g.menus.glass, 10);
	assert.equal(g.dialogs.dim, 40);
	assert.equal(globalLook({ ...old, glass: 0, blur: 0 }).style, 'flat');
});

test('dock settings: order, sanitising and the standard set', () => {
	assert.deepEqual(moveItem(['a', 'b', 'c'], 0, 2), ['b', 'c', 'a']);
	assert.deepEqual(moveItem(['a', 'b', 'c'], 2, 0), ['c', 'a', 'b']);
	assert.deepEqual(moveItem(['a', 'b', 'c'], 1, 1), ['a', 'b', 'c']);
	assert.deepEqual(moveItem(['a', 'b'], 5, 0), ['a', 'b']);
	const d = sanitizeDock({
		show: false,
		pinned: ['app:files', 'app:files', '../x', 'docker:immich'],
		customPinned: true,
		size: 200,
		opacity: 5,
		magnify: 7,
		position: 'top',
	});
	assert.deepEqual(d, {
		show: false,
		pinned: ['app:files', 'docker:immich'],
		customPinned: true,
		size: 80,
		opacity: 30,
		magnify: 2,
		position: 'bottom',
		// PLAN 73: with the frame unless switched off.
		border: true,
	});
	assert.deepEqual(sanitizeDock(null), defaultDock());
	const prefs = sanitizeDesktop({});
	const known = new Set(['app:files', 'app:docker', 'app:settings']);
	assert.deepEqual(dockPinned(prefs, known), ['app:files', 'app:docker', 'app:settings']);
	prefs.dock = { ...prefs.dock, pinned: ['app:settings', 'app:gone'], customPinned: true };
	assert.deepEqual(dockPinned(prefs, known), ['app:settings']);
});
