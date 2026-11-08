import { test } from 'node:test';
import assert from 'node:assert/strict';
import { highlight, queryTerms } from '../src/routes/files/excerpt.ts';

test('marks words beginning with a query word', () => {
	assert.deepEqual(queryTerms('Kfz  Versicherung, x'), ['kfz', 'versicherung']);
	assert.deepEqual(highlight('… die Kfz-Versicherung wird gekündigt …', 'versicherung kfz'), [
		{ text: '… die ', hit: false },
		{ text: 'Kfz', hit: true },
		{ text: '-', hit: false },
		{ text: 'Versicherung', hit: true },
		{ text: ' wird gekündigt …', hit: false },
	]);
	assert.deepEqual(highlight('Kündigungen', 'kündigung'), [{ text: 'Kündigungen', hit: true }]);
	assert.deepEqual(highlight('<b>nichts</b>', ''), [{ text: '<b>nichts</b>', hit: false }]);
});
