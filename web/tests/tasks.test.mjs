import {test} from 'node:test';
import assert from 'node:assert/strict';
import {taskItems,needsAttention,isActive} from '../src/lib/operations/tasks.ts';
test('failed Docker and VM work and unavailable maintenance cannot show all-clear',()=>{
 const tasks=taskItems({transfers:[],docker:{app:{state:'failed',output:'image pull failed'}},vms:[{name:'vm',job:{state:'failed',text:'copy failed'}}],maintenance:{error:'missing volume',jobs:[],history:[],settings:{enabled:true}},unavailable:['assistant']});
 assert.equal(tasks.filter(needsAttention).length,4);
 assert.equal(tasks.filter(isActive).length,0);
});
test('own assistant, automation, upload and recovery states keep their distinct lifetimes',()=>{
 const tasks=taskItems({transfers:[],assistant:[{id:'mine',title:'Report',run:{state:'confirmation',detail:'Review'}}],automations:[{id:'auto',name:'Plan',enabled:true,next:'2026-10-25T03:00:00Z'}],uploads:[{id:'up',name:'file',size:100,offset:25,busy:true}],applicationRecovery:{operation:{id:'recovery',job:'backup'},active:false}});
 assert.equal(tasks.find(t=>t.id==='assistant:mine').state,'blocked');
 assert.equal(tasks.find(t=>t.id==='automation:auto').state,'scheduled');
 assert.equal(tasks.find(t=>t.id==='upload:up').progress,25);
 assert.equal(tasks.find(t=>t.id==='application-recovery').state,'blocked');
});
test('completed results remain visible and unknown progress is not fabricated',()=>{
 const tasks=taskItems({transfers:[{id:'done',state:'done',from:'a',to:'b'},{id:'paused',state:'running',from:'a',to:'b',paused:true,bytes:0,copied:0}],maintenance:{jobs:[],history:[{id:'check',name:'Check',started:'a',finished:'b',error:''}],settings:{enabled:false}}});
 assert.equal(tasks.filter(t=>t.state==='done').length,2);
 const paused=tasks.find(t=>t.id==='file:paused');assert.equal(paused.progress,undefined);assert.equal(paused.actions[0].label,'resume');
});
test('event automations wait for an event instead of inventing a year-one schedule',()=>{
 const tasks=taskItems({transfers:[],automations:[{id:'event',name:'USB',kind:'event',enabled:true,next:'0001-01-01T00:00:00Z'}]});
 assert.equal(tasks[0].state,'waiting');assert.equal(tasks[0].at,undefined);assert.equal(tasks[0].wait,'event');
});
test('unloaded backup states and an unavailable enabled maintenance volume stay unknown',()=>{
 const tasks=taskItems({transfers:[],backups:[{job:{id:'unknown',destination:{type:'local'}},stateKnown:false,state:{running:false}}],maintenance:{settings:{enabled:true},available:false,jobs:[],history:[],error:''}});
 assert.equal(tasks.filter(needsAttention).length,2);
 assert.equal(tasks.find(t=>t.id==='backup-unknown:unknown').state,'unknown');
});
