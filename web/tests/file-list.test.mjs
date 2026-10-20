import { test } from 'node:test';
import assert from 'node:assert/strict';
import { fileList } from '../src/lib/file-list.ts';
const entries = [
 {name:'Datei10.txt',dir:false,size:10,modified:'2026-01-03T00:00:00Z'},
 {name:'Backup',dir:true,size:999,modified:'2026-01-02T00:00:00Z'},
 {name:'Datei2.txt',dir:false,size:20,modified:'2026-01-01T00:00:00Z'},
 {name:'Äpfel.txt',dir:false,size:5,modified:'2026-01-02T00:00:00Z'}
];
const names = (query,sort='name',desc=false) => fileList(entries,query,sort,desc,'de').map(e=>e.name);
test('folder filtering preserves original names and handles case and Unicode',()=>{
 assert.deepEqual(names('BACK'),['Backup']);
 assert.deepEqual(names('a\u0308PFEL'),['Äpfel.txt']);
 assert.deepEqual(names('missing'),[]);
});
test('natural name, size and date ordering always keep directories first',()=>{
 assert.deepEqual(names('Datei'),['Datei2.txt','Datei10.txt']);
 assert.deepEqual(names('','size'),['Backup','Äpfel.txt','Datei10.txt','Datei2.txt']);
 assert.deepEqual(names('','size',true),['Backup','Datei2.txt','Datei10.txt','Äpfel.txt']);
 assert.deepEqual(names('','modified'),['Backup','Datei2.txt','Äpfel.txt','Datei10.txt']);
 assert.deepEqual(names('','modified',true),['Backup','Datei10.txt','Äpfel.txt','Datei2.txt']);
 assert.equal(entries[0].name,'Datei10.txt');
});
