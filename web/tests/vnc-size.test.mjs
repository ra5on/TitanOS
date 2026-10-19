import test from 'node:test';
import assert from 'node:assert/strict';
import { vncWidth } from '../src/lib/vnc-size.ts';
test('restores custom widths and safely handles old or damaged settings', () => {
 assert.equal(vncWidth({width:63}),63);
 for (const value of [null, {}, {width:'75'}, {width:NaN}, {width:Infinity}]) assert.equal(vncWidth(value),75);
 assert.equal(vncWidth({width:5}),40);
 assert.equal(vncWidth({width:120}),100);
 assert.equal(vncWidth({width:63.7}),64);
});
