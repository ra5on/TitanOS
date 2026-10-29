import { test } from 'node:test';
import assert from 'node:assert/strict';
import { dashboardVisual, backgroundURL, BACKGROUND_IDS } from '../src/lib/dashboard/visual.ts';
import { historySlots, historyPath, historyFrame } from '../src/lib/metric-history.ts';
import { tileOptions, tileStyle } from '../src/lib/dashboard/options.ts';
test('backgrounds reject injected URLs and missing image references', () => {
	assert.equal(BACKGROUND_IDS.length, 10);
	assert.equal(
		dashboardVisual({ background: { source: 'builtin', id: '../secret' } }).background.source,
		'inherit',
	);
	assert.equal(
		dashboardVisual({ background: { source: 'custom', id: 'a'.repeat(64) } }).background.source,
		'inherit',
	);
	const v = dashboardVisual({
		background: { source: 'custom', id: 'a'.repeat(64) },
		images: [{ id: 'a'.repeat(64), name: 'Mine.webp' }],
		glowColor: 'red;display:none',
		blur: Infinity,
		transparency: 999,
	});
	assert.equal(backgroundURL(v), '/api/v1/background/' + 'a'.repeat(64));
	assert.equal(v.transparency, 90);
	assert.equal(v.blur, 14);
	assert.equal(v.glowColor, '');
});
test('the background is meant for every page unless switched off', () => {
	assert.equal(dashboardVisual({}).background.everywhere, true);
	assert.equal(dashboardVisual({ background: { everywhere: 'no' } }).background.everywhere, true);
	assert.equal(dashboardVisual({ background: { everywhere: false } }).background.everywhere, false);
});
test('tiles have no design of their own: stored design values are ignored', () => {
	const style = tileStyle(
		tileOptions(
			{
				cpu: {
					surfaceOverride: true,
					opacity: 60,
					radius: 30,
					valueSize: 56,
					titleColor: '#123456',
				},
			},
			'cpu',
		),
		'cpu',
	);
	assert.doesNotMatch(style, /60%|30px|56px|#123456|--tile-radius|--tile-background/);
	// Sizes in rem follow the font size of Settings › Appearance.
	assert.match(style, /--tile-value-size:[\d.]+rem/);
	assert.match(style, /--tile-padding:var\(--tile-pad\)/);
});
test('history retains time slots and unknown gaps without animating missing samples as zero', () => {
	assert.deepEqual(historySlots([12, null, 18], 5), [null, null, 12, null, 18]);
	const path = historyPath([12, null, 18]);
	assert.equal((path.match(/M/g) || []).length, 2);
	assert.ok(!path.includes('L'));
	assert.deepEqual(historyFrame([10, 40, null], [30, null, 50], 0.5), [20, null, 50]);
	assert.notEqual(
		historyPath([100], 200),
		historyPath([200], 200),
		'container usage above 100 remains distinguishable',
	);
});

test('glow and animation belong to the appearance; older dashboard values are taken over once', async () => {
	const { sanitizeAppearance, cssVariables, defaultAppearance } =
		await import('../src/lib/theme/themes.ts');
	const fresh = sanitizeAppearance({});
	assert.equal(fresh.glow, 0);
	assert.equal(fresh.motion, true);
	const legacy = sanitizeAppearance(
		{ theme: 'graphit' },
		{ glow: 40, motion: false, duration: 800, glowColor: '#112233' },
	);
	assert.deepEqual(
		[legacy.glow, legacy.motion, legacy.duration, legacy.glowColor],
		[40, false, 800, '#112233'],
	);
	// Once the appearance has its own values, the dashboard ones no longer count.
	assert.equal(sanitizeAppearance({ glow: 10 }, { glow: 40 }).glow, 10);
	const vars = cssVariables({ ...defaultAppearance(), glow: 50, motion: false }, false);
	assert.match(vars['--panel-glow'], /50%/);
	assert.equal(vars['--metric-duration'], '0ms');
	assert.equal(cssVariables(defaultAppearance(), false)['--panel-glow'], '0 0 0 transparent');
});
