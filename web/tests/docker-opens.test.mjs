import { test } from 'node:test';
import assert from 'node:assert/strict';
import { joinCommand, mountedShares, splitCommand } from '../src/lib/docker-opens-command.ts';

test('the program line splits into arguments without a shell', () => {
	assert.deepEqual(splitCommand('bambu-studio {file}'), ['bambu-studio', '{file}']);
	assert.deepEqual(splitCommand('  /opt/app/bin/x  --open   {file} '), [
		'/opt/app/bin/x',
		'--open',
		'{file}',
	]);
	assert.deepEqual(splitCommand('"/opt/My App/run" {file}'), ['/opt/My App/run', '{file}']);
	// Shell characters are plain text: one argument each, nothing runs.
	assert.deepEqual(splitCommand('x;rm -rf / {file}'), ['x;rm', '-rf', '/', '{file}']);
	assert.deepEqual(splitCommand(''), []);
});

test('joining and splitting keep the arguments', () => {
	for (const args of [
		['bambu-studio', '{file}'],
		['/opt/My App/run', '--flag', '{file}'],
	])
		assert.deepEqual(splitCommand(joinCommand(args)), args);
});

test('only shares mounted at /shares/<name> count', () => {
	const shares = mountedShares({
		mounts: [
			{ source: '/mnt/Data/appdata/x/config', target: '/config', readOnly: false },
			{ source: '/mnt/Data/Documents', target: '/shares/Documents', readOnly: false },
			{ source: '/mnt/Data/Media', target: '/shares/Media', readOnly: true },
			{ source: '/mnt/Data/Media/Filme', target: '/shares/Media/Filme', readOnly: true },
		],
	});
	assert.deepEqual(shares, [
		{ share: 'Documents', readOnly: false },
		{ share: 'Media', readOnly: true },
	]);
});
