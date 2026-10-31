import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { attention, needLabel, needList, smrForLevel, virtAdvice } from '../src/lib/hwcheck.ts';

const texts = Object.fromEntries(
	['de', 'en'].map((lang) => [
		lang,
		JSON.parse(readFileSync(new URL(`../src/lib/i18n/${lang}.json`, import.meta.url), 'utf8')),
	]),
);

const virt = (over = {}) => ({
	vendor: 'intel',
	cpuFlag: true,
	kvm: true,
	disabledByFirmware: false,
	iommu: true,
	iommuTable: true,
	...over,
});

test('instruction sets read as people know them', () => {
	assert.equal(needLabel('avx2'), 'AVX2');
	assert.equal(needLabel('sse4_2'), 'SSE4.2');
	assert.equal(needLabel('x86-64-v3'), 'x86-64-v3');
	assert.equal(needLabel('pni'), 'SSE3');
	assert.equal(needList(['avx', 'x86-64-v2']), 'AVX, x86-64-v2');
});

test('SMR disks warn only for RAID 5 and 6 and only when selected', () => {
	const disks = [
		{ name: 'sda', model: 'WDC WD40EFAX', smr: 'known_model', hints: [] },
		{ name: 'sdb', model: 'WDC WD40EFRX', hints: [] },
		{ name: 'sdc', model: 'HGST', smr: 'zoned', hints: [] },
	];
	assert.deepEqual(
		smrForLevel('raid5', ['sda', 'sdb'], disks).map((d) => d.name),
		['sda'],
	);
	assert.deepEqual(
		smrForLevel('raid6', ['/dev/sdc'], disks).map((d) => d.name),
		['sdc'],
	);
	assert.deepEqual(smrForLevel('raid1', ['sda'], disks), []);
	assert.deepEqual(smrForLevel('raid5', ['sdb'], disks), []);
});

test('virtualization advice names what to switch on', () => {
	assert.deepEqual(
		virtAdvice(virt({ kvm: false, cpuFlag: false, disabledByFirmware: true })).map((a) => a.key),
		['hwcheck.virt.off.intel', 'hwcheck.virt.byFirmware'],
	);
	assert.deepEqual(
		virtAdvice(virt()).map((a) => a.key),
		['hwcheck.virt.ok', 'hwcheck.iommu.ok'],
	);
	assert.deepEqual(
		virtAdvice(virt({ vendor: 'amd', iommu: false })).map((a) => a.key),
		['hwcheck.virt.ok', 'hwcheck.iommu.table.amd'],
	);
	assert.deepEqual(
		virtAdvice(virt({ iommu: false, iommuTable: false })).map((a) => a.key)[1],
		'hwcheck.iommu.off.intel',
	);
	assert.equal(
		virtAdvice(virt({ vendor: 'other', iommu: false }))[1].key,
		'hwcheck.iommu.off.other',
	);
});

test('devices that need no driver are not counted as problems', () => {
	const devices = [{ hint: 'not_needed' }, { hint: 'plugin' }, { hint: 'unsupported' }];
	assert.equal(attention(devices).length, 2);
});

test('every text the check can show exists in both languages', () => {
	const keys = [];
	for (const vendor of ['intel', 'amd', 'other'])
		keys.push(`hwcheck.virt.off.${vendor}`, `hwcheck.iommu.off.${vendor}`);
	keys.push('hwcheck.iommu.table.intel', 'hwcheck.iommu.table.amd', 'hwcheck.virt.byFirmware');
	// Codes the backend writes (backend/internal/hwcheck).
	for (const code of [
		'no_virtualization',
		'no_iommu',
		'old_cpu',
		'unbound_plugin',
		'unbound_kernel',
		'unbound_unsupported',
		'firmware_missing',
		'hw_raid',
		'intel_raid_mode',
		'smr',
		'uas_quirk',
		'memory_errors',
	])
		keys.push(`hwcheck.finding.${code}`);
	for (const code of [
		'smr',
		'hw_raid',
		'intel_raid_mode',
		'uas_quirk',
		'uas_quirk_active',
		'sas_standby',
	])
		keys.push(`hwcheck.hint.${code}`);
	for (const hint of ['plugin', 'kernel', 'not_needed', 'unsupported'])
		keys.push(`hwcheck.hintChip.${hint}`);
	keys.push(
		'apps.cpuMissing',
		'create.smrWarning',
		'welcome.todoHardware',
		'welcome.todoHardwareText',
	);
	for (const lang of ['de', 'en'])
		for (const key of keys) assert.ok(texts[lang][key], `${lang}: ${key}`);
});
