import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import {
	findingTone,
	outcomeTone,
	scenarioKey,
	scenariosFor,
	yearsOf,
} from '../src/lib/features/foresight.ts';

const base = { dataAtRiskBytes: 0, gaps: [], safe: [], advice: [], scenario: { kind: 'disk' } };

test('what-if scenarios of a volume: every member, full, and the other levels', () => {
	const md = {
		name: 'Data',
		level: 'raid1',
		filesystem: 'ext4',
		members: [{ disk: 'sda' }, { disk: 'sdb' }, { disk: '' }],
	};
	const keys = scenariosFor(md, ['raid1', 'raid5', 'raid6', 'raid10', 'raid1c3']).map(scenarioKey);
	assert.deepEqual(keys, [
		'disk:sda:Data:',
		'disk:sdb:Data:',
		'full::Data:',
		'level::Data:raid5',
		'level::Data:raid6',
		'level::Data:raid10',
	]);
	const btrfs = { name: 'Fast', level: 'raid1', filesystem: 'btrfs', members: [{ disk: 'sdc' }] };
	assert.ok(scenariosFor(btrfs, ['raid1', 'raid1c3']).some((s) => s.level === 'raid1c3'));
});

test('tones are never only a colour: each has its word in the texts', () => {
	assert.equal(outcomeTone({ ...base, outcome: 'lost' }), 'bad');
	assert.equal(outcomeTone({ ...base, outcome: 'noRedundancy' }), 'warn');
	assert.equal(outcomeTone({ ...base, outcome: 'redundant' }), 'ok');
	assert.equal(
		outcomeTone({ ...base, full: { freeBytes: 1, usedPct: 50, daysToFull: 30, effects: [] } }),
		'warn',
	);
	assert.equal(findingTone('fail'), 'bad');
	assert.equal(findingTone('tight'), 'warn');
	assert.equal(yearsOf(36800), 4.2);
	const de = JSON.parse(
		readFileSync(new URL('../src/lib/i18n/features/foresight.de.json', import.meta.url)),
	);
	const en = JSON.parse(
		readFileSync(new URL('../src/lib/i18n/features/foresight.en.json', import.meta.url)),
	);
	assert.deepEqual(Object.keys(de).sort(), Object.keys(en).sort());
	for (const level of ['ok', 'watch', 'tight', 'fail'])
		assert.ok(de['agent.f.foresight.badge.' + level]);
	for (const k of Object.keys(de)) assert.ok(k.startsWith('agent.f.foresight.'), k);
});
