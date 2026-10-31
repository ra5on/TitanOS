import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
	beginFileDrag,
	endFileDrag,
	fileDragType,
	hasFileDrag,
	takeFileDrag,
} from '../src/lib/file-drag.ts';

// What the tests need of a DragEvent: its dataTransfer.
function dragEvent(types = []) {
	const data = {};
	return {
		dataTransfer: {
			types,
			effectAllowed: '',
			setData(type, value) {
				data[type] = value;
				types.push(type);
			},
		},
		data,
	};
}

test('file drag: the paths stay in the page, the transfer only carries a marker', () => {
	const start = dragEvent();
	const paths = ['a.txt', 'b'];
	beginFileDrag(start, { root: 'share/Data', paths });
	assert.deepEqual(start.data, { [fileDragType]: '1' });
	assert.equal(start.dataTransfer.effectAllowed, 'copyMove');
	paths.push('changed later');
	const drop = dragEvent([fileDragType]);
	assert.equal(hasFileDrag(drop), true);
	assert.deepEqual(takeFileDrag(drop), { root: 'share/Data', paths: ['a.txt', 'b'] });
	// Taken once only.
	assert.equal(takeFileDrag(drop), null);
});

test('file drag: a drop from outside the page is not a file drag', () => {
	beginFileDrag(dragEvent(), { root: 'r', paths: ['x'] });
	assert.equal(hasFileDrag(dragEvent(['text/plain'])), false);
	assert.equal(takeFileDrag(dragEvent(['Files'])), null);
	endFileDrag();
	assert.equal(hasFileDrag(dragEvent([fileDragType])), false);
});

test('file drag: an event without dataTransfer starts nothing', () => {
	endFileDrag();
	beginFileDrag({ dataTransfer: null }, { root: 'r', paths: ['x'] });
	assert.equal(hasFileDrag(dragEvent([fileDragType])), false);
});
