import { test } from 'node:test';
import assert from 'node:assert/strict';
import { cpusetCpus, cpusetText, memoryBytes, pullProgress, volumeOf } from '../src/lib/appform.ts';

test('cpusets read into CPUs and back with ranges', () => {
	assert.deepEqual(cpusetCpus(''), []);
	assert.deepEqual(cpusetCpus('0,2,8-11'), [0, 2, 8, 9, 10, 11]);
	assert.equal(cpusetCpus('3-1'), null);
	assert.equal(cpusetCpus('a'), null);
	assert.equal(cpusetText([11, 8, 9, 10, 0, 2]), '0,2,8-11');
	assert.equal(cpusetText([4, 5]), '4,5');
	assert.equal(cpusetText([]), '');
});

test('memory values of Compose are bytes', () => {
	assert.equal(memoryBytes('4g'), 4 * 1024 ** 3);
	assert.equal(memoryBytes('512m'), 512 * 1024 ** 2);
	assert.equal(memoryBytes('1073741824'), 1073741824);
	assert.ok(Number.isNaN(memoryBytes('4 GB')));
});

test('a folder belongs to the volume with the longest matching mount point', () => {
	const volumes = [
		{ name: 'Data', mountPoint: '/mnt/Data', sizeBytes: 10, usedBytes: 4 },
		{ name: 'Data2', mountPoint: '/mnt/Data2', sizeBytes: 10, usedBytes: 1 },
	];
	assert.equal(volumeOf('/mnt/Data/Fotos', volumes)?.name, 'Data');
	assert.equal(volumeOf('/mnt/Data2/x/', volumes)?.name, 'Data2');
	assert.equal(volumeOf('/mnt/Data', volumes)?.name, 'Data');
	assert.equal(volumeOf('/srv/x', volumes), null);
});

test('the job output becomes images, bytes and other lines', () => {
	const progress = pullProgress(
		[
			'cache Pulled',
			'web Pulling',
			'034d6572bf28 Pull complete',
			'4abcf2066143 Downloading [===>   ]  1.5MB/3MB',
			'9b1f2e7d81aa Extracting [=>   ]  1MB/2MB',
			'Container demo-web-1 Started',
		].join('\n'),
	);
	assert.deepEqual(progress.images, [
		{ name: 'cache', state: 'done' },
		{ name: 'web', state: 'pulling' },
	]);
	assert.equal(progress.totalBytes, 5e6);
	assert.equal(progress.doneBytes, 3.5e6);
	assert.deepEqual(progress.other, ['Container demo-web-1 Started']);
});
