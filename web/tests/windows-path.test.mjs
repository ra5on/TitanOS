import { test } from 'node:test';
import assert from 'node:assert/strict';
import { windowsFolder, windowsMapping, windowsCopy } from '../src/lib/windows-path.ts';
const share = { name: 'docs', volume: 'data', smb: true, available: true };
test('maps only existing available SMB shares with exact folder boundaries', () => {
	assert.equal(
		windowsFolder({ kind: 'volume', name: 'data' }, 'docs/deep/folder', [share], 'nas'),
		'\\\\nas\\docs\\deep\\folder',
	);
	assert.equal(
		windowsFolder({ kind: 'share', name: 'docs' }, 'deep', [share], 'nas'),
		'\\\\nas\\docs\\deep',
	);
	for (const dir of [
		'docsevil',
		'docs/../secret',
		'docs/CON.txt',
		'docs/trailing.',
		'docs/bad:name',
	])
		assert.equal(windowsFolder({ kind: 'volume', name: 'data' }, dir, [share], 'nas'), null);
	assert.equal(windowsFolder({ kind: 'usb', name: 'docs' }, '', [share], 'nas'), null);
	assert.equal(
		windowsFolder({ kind: 'share', name: 'docs' }, '', [{ ...share, smb: false }], 'nas'),
		null,
	);
});
test('PowerShell commands quote literal paths and preserve extra target files', () => {
	assert.equal(
		windowsMapping('\\\\nas\\docs', 'z'),
		"New-SmbMapping -LocalPath 'Z:' -RemotePath '\\\\nas\\docs' -Persistent $false",
	);
	assert.equal(windowsMapping('\\\\nas\\docs', 'Z;whoami'), '');
	assert.equal(
		windowsCopy("C:\\John's $files", '\\\\nas\\docs'),
		"& robocopy 'C:\\John''s $files' '\\\\nas\\docs' /E /Z /R:2 /W:2 /XJ",
	);
	assert.equal(windowsCopy('relative', '\\\\nas\\docs'), '');
	assert.equal(windowsCopy('C:\\bad\npath', '\\\\nas\\docs'), '');
});
