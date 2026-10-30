import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
	createProblem,
	growLimit,
	growProblem,
	mibToBytes,
	partitionJob,
	sameDrive,
	segments,
	typeName,
} from '../src/routes/files/partitions.ts';

const MiB = 2 ** 20;
const disk = {
	id: 'a',
	device: '/dev/sdb',
	model: 'Ultra Fit',
	serial: '4C53',
	size: 32e9,
	transport: 'usb',
	kind: 'removable',
	readOnly: false,
	table: 'gpt',
	partitions: [],
	formattable: true,
};
const part = (number, start, size, fsType = 'ext4', growBy = 0) => ({
	number,
	device: `/dev/sdb${number}`,
	start,
	size,
	type: '0FC63DAF-8483-4772-8E79-3D69D8477DE4',
	fsType,
	label: '',
	uuid: '',
	mounted: false,
	growBy,
	renamable: true,
});

test('partitions and gaps come in the order they lie on the drive', () => {
	const layout = {
		disk,
		table: 'gpt',
		sectorSize: 512,
		partitions: [part(2, 1025 * MiB, 1024 * MiB), part(1, MiB, 512 * MiB, 'vfat')],
		gaps: [
			{ start: 513 * MiB, size: 512 * MiB, after: 1 },
			{ start: 2049 * MiB, size: 28000 * MiB, after: 2 },
		],
		job: null,
	};
	const rows = segments(layout);
	assert.deepEqual(
		rows.map((r) => `${r.kind}:${r.start / MiB}`),
		['part:1', 'gap:513', 'part:1025', 'gap:2049'],
	);
	// Small parts stay visible on the bar.
	assert.ok(rows.every((r) => r.percent >= 2));
	assert.ok(rows[3].percent > 80);
});

test('partition types have readable names', () => {
	assert.equal(typeName('gpt', 'ebd0a0a2-b9e5-4433-87c0-68b6b72699c7'), 'Microsoft basic data');
	assert.equal(typeName('gpt', 'C12A7328-F81F-11D2-BA4B-00A0C93EC93B'), 'EFI');
	assert.equal(typeName('dos', 'c'), 'FAT32');
	assert.equal(typeName('dos', '83'), 'Linux');
	assert.equal(typeName('dos', 'a5'), '0xa5');
});

test('a new partition must fit into its gap and suit the filesystem', () => {
	const gap = { start: 513 * MiB, size: 512 * MiB, after: 1 };
	assert.equal(createProblem(gap, 512 * MiB, 'exfat', 1, 'gpt'), '');
	assert.equal(createProblem(gap, 513 * MiB, 'exfat', 1, 'gpt'), 'files.partitions.sizeTooLarge');
	assert.equal(createProblem(gap, mibToBytes('abc'), '', 1, 'gpt'), 'files.partitions.sizeInvalid');
	assert.equal(createProblem(gap, 200 * MiB, 'xfs', 1, 'gpt'), 'files.formatTooSmall');
	assert.equal(createProblem(gap, 200 * MiB, '', 4, 'dos'), 'files.partitions.mbrFull');
	assert.equal(mibToBytes('100,7'), 100 * MiB);
	assert.ok(Number.isNaN(mibToBytes('0')));
});

test('growing only goes up, into the free space right after, not for FAT', () => {
	const p = part(3, 2561.5 * MiB, 1000 * MiB, 'xfs', 30000 * MiB);
	// The end is put on a whole MiB.
	assert.equal(growLimit(p), 30999.5 * MiB);
	assert.equal((p.start + growLimit(p)) % MiB, 0);
	assert.equal(growProblem(p, growLimit(p)), '');
	assert.equal(growProblem(p, 500 * MiB), 'files.partitions.growSize');
	assert.equal(growProblem(p, growLimit(p) + MiB), 'files.partitions.growSize');
	assert.equal(growProblem({ ...p, fsType: 'exfat' }, growLimit(p)), 'files.partitions.growFs');
	assert.equal(growProblem({ ...p, growBy: 0 }, growLimit(p)), 'files.partitions.growNoSpace');
});

test('the same drive is found again after its ID changed', () => {
	assert.equal(sameDrive(disk, { ...disk, id: 'b', table: 'dos' }), true);
	assert.equal(sameDrive(disk, { ...disk, id: 'b', serial: 'other' }), false);
	assert.equal(partitionJob({ action: 'grow' }), true);
	assert.equal(partitionJob({ phase: 'done' }), false);
});
