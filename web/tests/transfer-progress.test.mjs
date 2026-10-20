import {test} from 'node:test';
import assert from 'node:assert/strict';
import {transferSample,transferProblem} from '../src/lib/transfer-progress.ts';
import {previewKind} from '../src/lib/file-actions.ts';
test('rates use measured deltas and discard pause, reset and stale intervals',()=>{
 const a=transferSample(undefined,100,1000,true);assert.equal(a.speed,0);
 assert.equal(transferSample(a,2100,3000,true).speed,1000);
 assert.equal(transferSample(a,2100,3000,false).speed,0);
 assert.equal(transferSample({...a,active:false},2100,3000,true).speed,0);
 assert.equal(transferSample(a,10,3000,true).speed,0);
 assert.equal(transferSample(a,2100,30000,true).speed,0);
 assert.equal(transferProblem('files: not enough free space'),'space');
 assert.equal(transferProblem('connection interrupted'),'connection');
});
test('media previews allow known formats while bounding PDF and excluding SVG',()=>{
 assert.equal(previewKind('Urlaub.MP4',20e9),'video');assert.equal(previewKind('Musik.FLAC',1e9),'audio');
 assert.equal(previewKind('Dokument.PDF',100),'pdf');assert.equal(previewKind('Dokument.pdf',21*1024*1024),null);assert.equal(previewKind('active.svg',100),null);
});
