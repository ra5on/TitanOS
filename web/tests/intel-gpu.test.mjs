import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { activeMode, applicableGPUs, gucModes, gucValue, hucState } from '../src/lib/intel-gpu.ts';
import { activityLabel } from '../src/lib/activity.ts';

const fw = (over = {}) => ({ file: '', present: true, loaded: '', version: '', state: '', authenticated: false, ...over });
const gpu = (over = {}) => ({
	address: '0000:00:02.0', device: '5912', revision: 2, platform: 'kabylake', name: 'Kaby Lake', generation: '9.5',
	driver: 'i915', renderNode: 'renderD128', card: 'card0', applies: true, submission: false, submissionUsed: null,
	guc: fw(), huc: fw(), source: 'debugfs', messages: [], ...over,
});

test('the modes write the documented enable_guc values', () => {
	assert.deepEqual(gucModes, ['auto', 'huc', 'guc']);
	assert.deepEqual(gucValue, { auto: '', huc: '2', guc: '3' });
	assert.equal(activeMode(''), 'auto');
	assert.equal(activeMode('2'), 'huc');
	assert.equal(activeMode('3'), 'guc');
	assert.equal(activeMode('1'), 'other');
});

test('the setting is offered for i915 Gen9 to Gen11 only', () => {
	assert.deepEqual(applicableGPUs(null), []);
	const status = { gpus: [gpu(), gpu({ address: '0000:03:00.0', driver: 'xe', applies: false, platform: '' })] };
	assert.deepEqual(applicableGPUs(status).map((g) => g.address), ['0000:00:02.0']);
});

test('the HuC state is read from what the kernel reported', () => {
	assert.equal(hucState(gpu({ huc: fw({ state: 'RUNNING', authenticated: true }) })), 'loaded');
	assert.equal(hucState(gpu({ huc: fw({ state: 'disabled' }) })), 'off');
	assert.equal(hucState(gpu({ huc: fw({ state: 'LOAD FAIL' }) })), 'failed');
	assert.equal(hucState(gpu({ huc: fw({ state: 'MISSING' }) })), 'failed');
	assert.equal(hucState(gpu({ source: 'journal' })), 'off');
	assert.equal(hucState(gpu({ source: '' })), 'unknown');
});

test('texts for every mode, state and error exist in both languages', () => {
	assert.deepEqual(activityLabel('PUT /api/v1/hardware/intel-gpu'), { kind: 'named', key: 'intelGuc' });
	const component = readFileSync(new URL('../src/routes/settings/hardware/IntelGuc.svelte', import.meta.url), 'utf8');
	const used = [...component.matchAll(/t\('(intelGuc\.[A-Za-z.]+)'/g)].map((m) => m[1]);
	assert.ok(used.length > 20);
	for (const lang of ['de', 'en']) {
		const texts = JSON.parse(readFileSync(new URL(`../src/lib/i18n/${lang}.json`, import.meta.url), 'utf8'));
		const keys = [
			...used,
			...gucModes.flatMap((m) => [`intelGuc.mode.${m}`, `intelGuc.mode.${m}.hint`]),
			...['loaded', 'off', 'failed', 'unknown'].map((s) => `intelGuc.huc.${s}`),
			...['debugfs', 'journal', 'none'].map((s) => `intelGuc.source.${s}`),
			'activity.do.intelGuc',
			'error.intel_guc_not_applicable',
			'error.intel_guc_unavailable',
			'error.intel_guc_failed',
		];
		for (const key of keys) assert.ok(texts[key], `${lang}: ${key}`);
	}
});
