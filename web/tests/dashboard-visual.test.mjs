import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
	dashboardVisual,
	backgroundURL,
	visualStyle,
	BACKGROUND_IDS,
} from '../src/lib/dashboard/visual.ts';
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
	assert.ok(!visualStyle(v).includes('display:none'));
});
test('old default opacity inherits global settings, deliberate tile overrides survive', () => {
	assert.equal(tileOptions({ cpu: { opacity: 100 } }, 'cpu').surfaceOverride, false);
	assert.equal(tileOptions({ cpu: { opacity: 70 } }, 'cpu').surfaceOverride, true);
	assert.match(tileStyle(tileOptions({ cpu: { opacity: 100 } }, 'cpu')), /var\(--dashboard-alpha/);
	assert.match(
		tileStyle(tileOptions({ cpu: { surfaceOverride: true, opacity: 60 } }, 'cpu')),
		/60%,transparent/,
	);
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
