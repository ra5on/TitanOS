import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
	cssVariables,
	defaultAppearance,
	FLOATING_INHERIT_MIN,
	floatingAlpha,
	floatingValues,
	resolvePalette,
	sanitizeAppearance,
	THEMES,
} from '../src/lib/theme/themes.ts';

test('menus and windows: older appearances follow the tiles', () => {
	const old = sanitizeAppearance({
		theme: 'graphit',
		style: 'glass',
		glass: 60,
		blur: 10,
		shine: 40,
	});
	assert.deepEqual(old.menus, defaultAppearance().menus);
	assert.equal(old.menus.own, false);
	assert.equal(old.dialogs.own, false);
	assert.equal(old.dialogs.dim, 70);
	assert.deepEqual(floatingValues(old, old.menus), { glass: 60, blur: 10, shine: 40 });
	const vars = cssVariables(old, false);
	assert.equal(vars['--menu-blur'], '10px');
	assert.equal(vars['--menu-shine'], '0.40');
	assert.equal(vars['--dialog-blur'], '10px');
	// The overlay behind dialogs keeps the 70 % it always had.
	assert.match(vars['--overlay'], /0\.70\)$/);
});

test('menus and windows: broken or out-of-range values are fixed', () => {
	const a = sanitizeAppearance({
		menus: { own: 'yes', glass: 500, blur: -3, shine: 'x', extra: 1 },
		dialogs: { own: true, glass: 12.4, blur: 99, shine: 61, dim: 140 },
	});
	assert.deepEqual(a.menus, { own: false, glass: 90, blur: 0, shine: 20 });
	assert.deepEqual(a.dialogs, { own: true, glass: 12, blur: 40, shine: 60, dim: 90 });
	assert.deepEqual(
		sanitizeAppearance({ menus: null, dialogs: [] }).dialogs,
		defaultAppearance().dialogs,
	);
	assert.equal(sanitizeAppearance({ dialogs: { dim: -5 } }).dialogs.dim, 0);
});

test('menus and windows: following the tiles keeps menus at least 70 % opaque', () => {
	const a = sanitizeAppearance({ style: 'glass', glass: 90 });
	const dark = resolvePalette(a, false);
	assert.equal(floatingAlpha(a, a.menus, dark), FLOATING_INHERIT_MIN);
	assert.equal(cssVariables(a, false)['--menu-alpha'], '0.70');
	const solid = sanitizeAppearance({ style: 'glass', glass: 10 });
	assert.equal(floatingAlpha(solid, solid.menus, dark), 0.9);
});

test('menus and windows: own values use the glass limits, also on a plain background', () => {
	for (const bg of ['shapes', 'solid']) {
		const a = sanitizeAppearance({
			bg,
			menus: { own: true, glass: 90 },
			dialogs: { own: true, glass: 90 },
		});
		assert.equal(floatingAlpha(a, a.menus, THEMES.graphit), 0.35);
		assert.equal(floatingAlpha(a, a.dialogs, THEMES.hell), 0.5);
	}
	const a = sanitizeAppearance({ menus: { own: true, glass: 20, blur: 6, shine: 0 } });
	assert.equal(floatingAlpha(a, a.menus, THEMES.graphit), 0.8);
	const vars = cssVariables(a, false);
	assert.equal(vars['--menu-alpha'], '0.80');
	assert.equal(vars['--menu-blur'], '6px');
	assert.equal(vars['--menu-shine'], '0.00');
	// Dialogs still follow the tiles.
	assert.equal(vars['--dialog-blur'], `${a.blur}px`);
});

test('menus and windows: dimming behind dialogs', () => {
	const a = sanitizeAppearance({ dialogs: { dim: 0 } });
	assert.match(cssVariables(a, false)['--overlay'], /0\.00\)$/);
	const b = sanitizeAppearance({ dialogs: { dim: 45 } });
	assert.match(cssVariables(b, false)['--overlay'], /0\.45\)$/);
});

test('menus and windows: a theme file round trip keeps them', () => {
	const a = sanitizeAppearance({
		menus: { own: true, glass: 50, blur: 30, shine: 10 },
		dialogs: { own: true, glass: 40, blur: 12, shine: 5, dim: 55 },
	});
	const file = JSON.parse(JSON.stringify({ titanosTheme: 1, ...a }));
	const back = sanitizeAppearance(file);
	assert.deepEqual(back.menus, a.menus);
	assert.deepEqual(back.dialogs, a.dialogs);
});

test('menus and windows: every style and fallback sets the surfaces; menus use them', async () => {
	const { readFile } = await import('node:fs/promises');
	const css = await readFile(new URL('../src/app.css', import.meta.url), 'utf8');
	const block = (start) => css.slice(css.indexOf(start), css.indexOf('}', css.indexOf(start)));
	for (const start of [
		"[data-surface='flat'] {",
		"[data-surface='glass'] {",
		"[data-surface='outline'] {",
	]) {
		for (const name of [
			'--menu-bg',
			'--menu-border',
			'--menu-backdrop',
			'--menu-shadow',
			'--dialog-bg',
			'--dialog-border',
			'--dialog-backdrop',
			'--dialog-shadow',
		]) {
			assert.match(block(start), new RegExp(`${name}:`), `${start} ${name}`);
		}
	}
	// Opaque without backdrop-filter and with reduced transparency.
	for (const start of [
		'@supports not ((backdrop-filter',
		'@media (prefers-reduced-transparency: reduce)',
	]) {
		const part = css.slice(css.indexOf(start), css.indexOf('}\n}', css.indexOf(start)));
		assert.match(part, /--menu-backdrop: none/);
		assert.match(part, /--dialog-backdrop: none/);
		assert.match(part, /--menu-bg: var\(--surface\)/);
		assert.match(part, /--dialog-bg: var\(--surface\)/);
	}
	for (const file of [
		'AccountMenu',
		'Dropdown',
		'ContextMenu',
		'Header',
		'Notifications',
		'TerminalPanel',
	]) {
		const source = await readFile(
			new URL(`../src/lib/components/${file}.svelte`, import.meta.url),
			'utf8',
		);
		assert.match(source, /floating-menu/, file);
	}
	const dialog = await readFile(
		new URL('../src/lib/components/Dialog.svelte', import.meta.url),
		'utf8',
	);
	assert.match(dialog, /var\(--dialog-bg\)/);
	assert.match(dialog, /var\(--dialog-backdrop\)/);
	const palette = await readFile(
		new URL('../src/lib/components/CommandPalette.svelte', import.meta.url),
		'utf8',
	);
	assert.match(palette, /surface="menu"/);
});

test('bars stay opaque unless they get own glass values', () => {
	const plain = sanitizeAppearance({ style: 'glass' });
	const header = resolvePalette(plain, false).header;
	assert.equal(plain.bars.own, false);
	assert.equal(cssVariables(plain, false)['--bar-bg'], header);
	assert.equal(cssVariables(plain, false)['--bar-backdrop'], 'none');
	const glass = sanitizeAppearance({ style: 'glass', bars: { own: true, glass: 60, blur: 20 } });
	assert.notEqual(cssVariables(glass, false)['--bar-bg'], header);
	assert.equal(cssVariables(glass, false)['--bar-backdrop'], 'blur(20px) saturate(160%)');
	// Flat and outline keep them opaque even with own values.
	const flat = sanitizeAppearance({ style: 'flat', bars: { own: true, glass: 60 } });
	assert.equal(cssVariables(flat, false)['--bar-bg'], resolvePalette(flat, false).header);
});
