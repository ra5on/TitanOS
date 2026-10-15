import { test } from 'node:test';
import assert from 'node:assert/strict';
import { sanitizeLayout, defaultLayout } from '../src/lib/dashboard/tiles.ts';
import { tileOptions } from '../src/lib/dashboard/options.ts';
test('compact layouts retain chosen dimensions, with bounds for malformed preferences', () => {
 const tiles = [{id:'vm:home-assistant',w:2,h:2,x:10,y:20},{id:'network',w:3,h:2,x:0,y:0}];
 assert.deepEqual(sanitizeLayout({tiles}), tiles);
 assert.equal(sanitizeLayout({tiles:[{id:'cpu',w:Infinity,h:-1} ]})[0].h,2);
 assert.ok(defaultLayout().some(t=>t.id==='cpu'));
 assert.ok(defaultLayout().some(t=>t.id==='memory'));
});
test('tile options retain deliberate hiding and reject invalid interface paths', () => {
 const value = tileOptions({network:{ipv6:false,details:false,rates:false,interface:'br0'}},'network');
 assert.equal(value.ipv6,false); assert.equal(value.interface,'br0'); assert.equal(value.rates,false);
 assert.equal(tileOptions({network:{interface:'../../sys'}},'network').interface,'');
 assert.equal(tileOptions({network:{ipv6:'false'}},'network').ipv6,true);
});
