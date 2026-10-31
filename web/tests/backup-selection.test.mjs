import { test } from 'node:test';
import assert from 'node:assert/strict';
import { selectionOfIn, shareOfIn } from '../src/routes/settings/backups/selection.ts';

const shares = [
	{ name: 'Fotos', volume: 'Data' },
	{ name: 'Dokumente', volume: 'Data' },
];

test('chosen folders become shares, folders in shares, volumes and places', () => {
	const got = selectionOfIn(shares, [
		{ volume: 'Data', path: 'Fotos' },
		{ volume: 'Data', path: 'Fotos/2024' },
		{ volume: 'Data', path: 'Dokumente/Steuer' },
		{ volume: 'Data', path: 'scans/alt' },
		{ volume: 'Fast', path: '' },
		{ volume: 'Fast', path: 'vms' },
		{ volume: '', path: '' },
	]);
	assert.deepEqual(got, {
		sources: ['Fotos', 'Dokumente'],
		folders: { Dokumente: ['Steuer'] },
		volumes: ['Fast'],
		places: ['Data/scans/alt'],
	});
});

test('a share is recognised only on its own volume and at the top', () => {
	assert.equal(shareOfIn(shares, { volume: 'Fast', path: 'Fotos' }), null);
	assert.equal(shareOfIn(shares, { volume: 'Data', path: 'x/Fotos' }), null);
	assert.deepEqual(shareOfIn(shares, { volume: 'Data', path: 'Fotos/a/b' }), {
		name: 'Fotos',
		rest: 'a/b',
	});
});
