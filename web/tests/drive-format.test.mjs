import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
	formatRunning,
	hasForeignData,
	labelProblem,
	resolvedScheme,
	sizeProblem,
	suggestedFilesystem,
} from '../src/routes/files/drive-format.ts';
const GiB = 2 ** 30,
	TiB = 2 ** 40;
const drive = (kind, size, partitions = []) => ({
	id: 'x',
	device: '/dev/sdb',
	model: '',
	serial: '',
	size,
	transport: 'usb',
	kind,
	readOnly: false,
	table: '',
	partitions,
	formattable: true,
});
test('automatic partition table: MBR for Windows filesystems up to 2 TiB, like the server', () => {
	assert.equal(resolvedScheme('exfat', 32 * GiB, 'auto'), 'mbr');
	assert.equal(resolvedScheme('vfat', 2 * TiB, 'auto'), 'mbr');
	assert.equal(resolvedScheme('ntfs', 4 * TiB, 'auto'), 'gpt');
	assert.equal(resolvedScheme('ext4', 32 * GiB, 'auto'), 'gpt');
	assert.equal(resolvedScheme('ext4', 32 * GiB, 'mbr'), 'mbr');
});
test('sizes the filesystems cannot hold are named before asking the NAS', () => {
	assert.equal(sizeProblem('vfat', 3 * TiB, 'auto'), 'files.formatFatTooLarge');
	assert.equal(sizeProblem('exfat', 3 * TiB, 'mbr'), 'files.formatMbrTooLarge');
	assert.equal(sizeProblem('xfs', 200 * 2 ** 20, 'auto'), 'files.formatTooSmall');
	assert.equal(sizeProblem('exfat', 3 * TiB, 'auto'), '');
});
test('labels follow the rules of each filesystem', () => {
	assert.equal(labelProblem('vfat', 'MY STICK'), '');
	assert.equal(labelProblem('vfat', 'Fotos ä'), 'files.formatLabelFat');
	assert.equal(labelProblem('vfat', 'TWELVE CHARS'), 'files.formatLabelLong');
	assert.equal(labelProblem('exfat', 'Ürlaub 2026 ÄÖ'), '');
	assert.equal(labelProblem('exfat', 'sixteen chars ab'), 'files.formatLabelLong');
	assert.equal(labelProblem('ntfs', 'a:b'), 'files.formatLabelChars');
	assert.equal(labelProblem('ext4', 'äääääääää'), 'files.formatLabelLong');
});
test('suggestions and hints', () => {
	assert.equal(suggestedFilesystem(drive('sd', 32 * GiB)), 'vfat');
	assert.equal(suggestedFilesystem(drive('sd', 64 * GiB)), 'exfat');
	assert.equal(suggestedFilesystem(drive('hdd', 4 * TiB)), 'exfat');
	assert.equal(
		hasForeignData(
			drive('hdd', TiB, [
				{ device: '/dev/sdb1', fsType: 'linux_raid_member', label: '', size: 1, mounted: false },
			]),
		),
		true,
	);
	assert.equal(
		hasForeignData(
			drive('hdd', TiB, [
				{ device: '/dev/sdb1', fsType: 'ntfs', label: '', size: 1, mounted: false },
			]),
		),
		false,
	);
	assert.equal(formatRunning({ finished: undefined }), true);
	assert.equal(formatRunning({ finished: 'x' }), false);
	assert.equal(formatRunning(null), false);
});
