import { test } from 'node:test';
import assert from 'node:assert/strict';
import { sanitizeSound, SoundGate, notes } from '../src/lib/sounds.ts';
test('sound settings reject malformed values and bound volume', () => {
 assert.deepEqual(sanitizeSound({tone:'external-url',volume:Infinity}), {tone:'none',volume:35,severity:'important'});
 assert.equal(sanitizeSound({tone:'wood',volume:999}).volume,100);
 assert.equal(sanitizeSound({volume:-2}).volume,0);
 for(const melody of Object.values(notes)) for(const [frequency,,duration] of melody) { assert.ok(frequency>=200&&frequency<1000);assert.ok(duration>0&&duration<1); }
});
test('initial history is silent, severity filtered and bursts grouped', () => {
 const gate=new SoundGate(),settings=sanitizeSound({tone:'soft',volume:35});
 const old={id:1,severity:'error'};
 assert.equal(gate.accept([old],settings,0),false);
 assert.equal(gate.accept([old,{id:2,severity:'info'}],settings,100),false);
 assert.equal(gate.accept([old,{id:2,severity:'info'},{id:3,severity:'error'}],settings,200),true);
 assert.equal(gate.accept([{id:3,severity:'error'},{id:4,severity:'warning'}],settings,300),false);
 assert.equal(gate.accept([{id:3,severity:'error'},{id:4,severity:'warning'}],settings,11000),false);
 assert.equal(gate.accept([{id:5,severity:'warning'}],settings,12000),true);
});
