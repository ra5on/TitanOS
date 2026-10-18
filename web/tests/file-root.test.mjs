import {test} from 'node:test';
import assert from 'node:assert/strict';
import {preferredFileRoot} from '../src/lib/file-root.ts';
const roots=[{id:'stick',kind:'stick'},{id:'usb/disk',kind:'usb'},{id:'share/docs',kind:'share'},{id:'volume/data',kind:'volume'}];
test('files prefer a data volume, remembering only authorized data sources',()=>{
 assert.equal(preferredFileRoot(roots,''),'volume/data');
 assert.equal(preferredFileRoot(roots,'share/docs'),'share/docs');
 for(const remembered of ['stick','usb/disk','volume/removed']) assert.equal(preferredFileRoot(roots,remembered),'volume/data');
 assert.equal(preferredFileRoot(roots.slice(0,3),''),'share/docs');
 assert.equal(preferredFileRoot(roots.slice(0,2),''),'');
 assert.equal(preferredFileRoot([], 'volume/data'),'');
});
