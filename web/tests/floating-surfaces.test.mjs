import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
	cssVariables,
	defaultAppearance,
	defaultBar,
	FLOATING_INHERIT_MIN,
	floatingAlpha,
	floatingValues,
	resolvePalette,
	sanitizeAppearance,
	THEMES,
} from '../src/lib/theme/themes.ts';
import { withAlpha } from '../src/lib/theme/color.ts';

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

test('bars stay opaque unless they get tile or own glass values', () => {
	const plain = sanitizeAppearance({ style: 'glass' });
	const header = resolvePalette(plain, false).header;
	assert.equal(plain.bars.mode, 'opaque');
	assert.equal(plain.bars.own, false);
	assert.equal(cssVariables(plain, false)['--bar-bg'], header);
	assert.equal(cssVariables(plain, false)['--bar-backdrop'], 'none');
	// Stored before `mode` existed: `own` decides.
	const glass = sanitizeAppearance({ style: 'glass', bars: { own: true, glass: 60, blur: 20 } });
	assert.equal(glass.bars.mode, 'own');
	assert.notEqual(cssVariables(glass, false)['--bar-bg'], header);
	assert.equal(cssVariables(glass, false)['--bar-backdrop'], 'blur(20px) saturate(160%)');
	// Like the tiles: the tiles' transparency and blur.
	const tiles = sanitizeAppearance({
		style: 'glass',
		glass: 40,
		blur: 12,
		bars: { mode: 'tiles' },
	});
	assert.equal(tiles.bars.own, false);
	assert.equal(cssVariables(tiles, false)['--bar-bg'], withAlpha(header, 0.6));
	assert.equal(cssVariables(tiles, false)['--bar-backdrop'], 'blur(12px) saturate(160%)');
	// Flat and outline keep them opaque even with own values.
	const flat = sanitizeAppearance({ style: 'flat', bars: { own: true, glass: 60 } });
	assert.equal(cssVariables(flat, false)['--bar-bg'], resolvePalette(flat, false).header);
});

test('sidebar: older appearances follow the tiles, the bars keep their own setting', () => {
	const old = sanitizeAppearance({
		style: 'glass',
		glass: 70,
		blur: 14,
		bars: { own: false, glass: 30, blur: 24, shine: 20 },
	});
	assert.deepEqual(old.sidebar, { ...defaultBar('tiles') });
	assert.equal(old.sidebar.mode, 'tiles');
	assert.equal(old.bars.mode, 'opaque');
	assert.deepEqual(defaultAppearance().sidebar, defaultBar('tiles'));
	assert.deepEqual(defaultAppearance().bars, defaultBar('opaque'));
	const vars = cssVariables(old, false);
	const header = THEMES.graphit.header;
	// 70 % transparency = 30 % opaque, raised to the dark limit of 35 %.
	assert.equal(vars['--sidebar-bg'], withAlpha(header, 0.35));
	assert.equal(vars['--sidebar-backdrop'], 'blur(14px) saturate(160%)');
	assert.equal(vars['--sidebar-edge'], 'none');
	assert.equal(vars['--bar-bg'], header);
	assert.equal(vars['--bar-backdrop'], 'none');
});

test('sidebar: the readability limit holds on light themes and plain backgrounds', () => {
	for (const bg of ['shapes', 'solid']) {
		const a = sanitizeAppearance({ theme: 'hell', style: 'glass', bg, glass: 90 });
		assert.equal(cssVariables(a, false)['--sidebar-bg'], withAlpha(THEMES.hell.header, 0.5));
		const own = sanitizeAppearance({
			theme: 'graphit',
			style: 'glass',
			bg,
			sidebar: { mode: 'own', glass: 90, blur: 30, shine: 40 },
		});
		assert.equal(cssVariables(own, false)['--sidebar-bg'], withAlpha(THEMES.graphit.header, 0.35));
	}
});

test('sidebar: opaque, own values with shine, and only with the glass style', () => {
	const opaque = sanitizeAppearance({ style: 'glass', sidebar: { mode: 'opaque' } });
	assert.equal(opaque.sidebar.own, false);
	assert.equal(cssVariables(opaque, false)['--sidebar-bg'], THEMES.graphit.header);
	assert.equal(cssVariables(opaque, false)['--sidebar-backdrop'], 'none');

	const own = sanitizeAppearance({
		style: 'glass',
		sidebar: { mode: 'own', glass: 20, blur: 6, shine: 40 },
		bars: { mode: 'opaque' },
	});
	assert.equal(own.sidebar.own, true);
	const vars = cssVariables(own, false);
	assert.equal(vars['--sidebar-bg'], withAlpha(THEMES.graphit.header, 0.8));
	assert.equal(vars['--sidebar-backdrop'], 'blur(6px) saturate(160%)');
	assert.match(vars['--sidebar-edge'], /^inset -1px 0 0 rgba\(255, 255, 255, 0\.20\)$/);
	// The header and status bar are not affected.
	assert.equal(vars['--bar-bg'], THEMES.graphit.header);

	for (const style of ['flat', 'outline']) {
		const a = sanitizeAppearance({ style, sidebar: { mode: 'own', glass: 60 } });
		const v = cssVariables(a, false);
		assert.equal(v['--sidebar-bg'], THEMES.graphit.header, style);
		assert.equal(v['--sidebar-backdrop'], 'none', style);
		assert.equal(v['--sidebar-edge'], 'none', style);
	}
});

test('sidebar: broken values are fixed; a theme file round trip keeps it', () => {
	const a = sanitizeAppearance({
		sidebar: { mode: 'glas', own: 'x', glass: 500, blur: -1, shine: 99 },
		bars: { mode: 'tiles', own: true },
	});
	assert.deepEqual(a.sidebar, { own: false, mode: 'tiles', glass: 90, blur: 0, shine: 60 });
	// A valid mode wins over a contradicting `own`.
	assert.equal(a.bars.mode, 'tiles');
	assert.equal(a.bars.own, false);
	assert.deepEqual(sanitizeAppearance({ sidebar: null }).sidebar, defaultBar('tiles'));
	const b = sanitizeAppearance({
		sidebar: { mode: 'own', glass: 50, blur: 30, shine: 10 },
		bars: { mode: 'tiles' },
	});
	const file = JSON.parse(JSON.stringify({ titanosTheme: 1, ...b }));
	const back = sanitizeAppearance(file);
	assert.deepEqual(back.sidebar, b.sidebar);
	assert.deepEqual(back.bars, b.bars);
});

test('sidebar: the side navigation uses its own variables', async () => {
	const { readFile } = await import('node:fs/promises');
	const css = await readFile(new URL('../src/app.css', import.meta.url), 'utf8');
	const rule = css.slice(css.indexOf('.sidebar-surface::before {'));
	assert.match(rule.slice(0, rule.indexOf('}')), /background: var\(--sidebar-bg\)/);
	assert.match(rule.slice(0, rule.indexOf('}')), /backdrop-filter: var\(--sidebar-backdrop\)/);
	const nav = await readFile(
		new URL('../src/lib/components/SideNav.svelte', import.meta.url),
		'utf8',
	);
	assert.match(nav, /class="side-nav bar-surface sidebar-surface"/);
	// Opaque without backdrop-filter and with reduced transparency.
	for (const start of [
		'/* Without backdrop-filter or with reduced transparency the bars',
		'@media (prefers-reduced-transparency: reduce) {\n\t.bar-surface',
	]) {
		const at = css.indexOf(start);
		assert.notEqual(at, -1, start);
		const part = css.slice(at, css.indexOf('}\n}', at));
		assert.match(part, /\.sidebar-surface::before/);
		assert.match(part, /background: var\(--header\)/);
		assert.match(part, /backdrop-filter: none/);
		assert.match(part, /box-shadow: none/);
	}
	for (const file of ['Header', 'StatusBar']) {
		const source = await readFile(
			new URL(`../src/lib/components/${file}.svelte`, import.meta.url),
			'utf8',
		);
		assert.doesNotMatch(source, /sidebar-surface/, file);
	}
});
