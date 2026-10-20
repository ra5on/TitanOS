import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFavorites,rangeSelection,previewKind,filePreferences} from '../src/lib/file-actions.ts';
test('selection ranges respect displayed ordering in both directions',()=>{
 assert.deepEqual(rangeSelection(['B','A','C'],'C','B'),['B','A','C']);
 assert.deepEqual(rangeSelection(['A','B'],'gone','B'),['B']);
 assert.deepEqual(rangeSelection(['A'],'A','gone'),[]);
});
test('favorites discard malformed storage and traversal paths',()=>{
 assert.deepEqual(readFavorites('{broken'),[]);
 assert.deepEqual(readFavorites(JSON.stringify([{root:'share/Backup',path:'Fotos'},{root:'volume/data',path:''},{root:'share/Backup',path:'../secret'},{root:'stick',path:''}])),[{root:'share/Backup',path:'Fotos'},{root:'volume/data',path:''}]);
});
test('preview excludes active image documents and enforces limits',()=>{
 assert.equal(previewKind('Foto.JPG',100),'image');
 assert.equal(previewKind('Foto.svg',100),null);
 assert.equal(previewKind('Foto.JPG',21*1024*1024),null);
 assert.equal(previewKind('index.html',100),'text');
 assert.equal(previewKind('log.txt',513*1024),null);
});

test('account preferences reject unknown settings without changing valid names', async()=>{
 const {filePreferences,folderPath}=await import('../src/lib/file-actions.ts');
 assert.deepEqual(filePreferences(null),{root:'',split:false,showHidden:false,view:'list',sort:'name',descending:false,favorites:[],folders:[]});
 assert.deepEqual(filePreferences({view:'large',sort:'size',descending:true,favorites:[{root:'share/Backup',path:'MeineFotos'}]}),{root:'',split:false,showHidden:false,view:'large',sort:'size',descending:true,favorites:[{root:'share/Backup',path:'MeineFotos'}],folders:[]});
 assert.equal(filePreferences({view:'unexpected',sort:'unexpected'}).view,'list');
 assert.equal(folderPath('/Fotos/Urlaub/'),'Fotos/Urlaub');
 assert.equal(folderPath('/'),'');
 for(const path of ['../etc','Fotos/../etc','./Fotos','Fotos//Urlaub','Fotos\0'])assert.equal(folderPath(path),null);
});

test('favorite labels and folder views survive account preference validation',()=>{
 const result=filePreferences({favorites:[{root:'share/Backup',path:'Fotos',label:'Urlaub'}],folders:[{root:'share/Backup',path:'Fotos',view:'large',sort:'modified',descending:true},{root:'share/Backup',path:'../bad',view:'list',sort:'name',descending:false}]});
 assert.equal(result.favorites[0].label,'Urlaub');assert.equal(result.folders.length,1);assert.equal(result.folders[0].view,'large');
});
