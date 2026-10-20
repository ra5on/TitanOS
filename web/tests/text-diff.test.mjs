import {test} from 'node:test';
import assert from 'node:assert/strict';
import {textDiff} from '../src/lib/text-diff.ts';
test('text comparison aligns inserted and removed lines with original numbers',()=>{
 const {rows}=textDiff('a\nb\nc','a\nnew\nb\nc');
 assert.deepEqual(rows.map(r=>r.kind),['same','added','same','same']);
 assert.equal(rows[2].beforeNumber,2);assert.equal(rows[2].afterNumber,3);
 assert.deepEqual(textDiff('a\nold\nz','a\nz').rows.map(r=>r.kind),['same','removed','same']);
});
test('changes, Unicode, empty lines and final newline are preserved',()=>{
 const {rows}=textDiff('Ä\nold\n','Ä\nnew\n');assert.equal(rows[1].kind,'changed');assert.equal(rows[2].after,'');
 for(const [a,b] of [['','a'],['a\n','a'],['a\na\nb','a\nb\nb']]){
  const result=textDiff(a,b);assert.equal(result.rows.filter(r=>r.before!==undefined).map(r=>r.before).join('\n'),a);assert.equal(result.rows.filter(r=>r.after!==undefined).map(r=>r.after).join('\n'),b);
 }
});
test('large unrelated files use bounded positional comparison without lost content',()=>{
 const a='same\n'+Array.from({length:2000},(_,i)=>'old'+i).join('\n')+'\nend',b='same\n'+Array.from({length:2000},(_,i)=>'new'+i).join('\n')+'\nend';
 const result=textDiff(a,b);assert.equal(result.approximate,true);assert.equal(result.rows[0].kind,'same');assert.equal(result.rows.at(-1).kind,'same');assert.equal(result.rows.length,2002);
});
