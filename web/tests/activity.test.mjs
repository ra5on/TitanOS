import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { activityLabel } from '../src/lib/activity.ts';

test('activity entries get a named or an area label with a translation', () => {
 assert.deepEqual(activityLabel('login_failed'), { kind: 'named', key: 'loginFailed' });
 assert.deepEqual(activityLabel('POST /api/v1/files/delete'), { kind: 'named', key: 'fileDelete' });
 assert.deepEqual(activityLabel('PUT /api/v1/system/timezone'), { kind: 'generic', area: 'system', verb: 'change' });
 assert.deepEqual(activityLabel('DELETE /api/v1/docker/apps/{name}'), { kind: 'generic', area: 'apps', verb: 'remove' });
 assert.deepEqual(activityLabel('POST /api/v1/somewhere-new'), { kind: 'generic', area: 'other', verb: 'run' });
 assert.deepEqual(activityLabel('bogus'), { kind: 'generic', area: 'other', verb: 'run' });
 const source = readFileSync(new URL('../src/lib/activity.ts', import.meta.url), 'utf8');
 for (const lang of ['de', 'en']) {
  const texts = JSON.parse(readFileSync(new URL(`../src/lib/i18n/${lang}.json`, import.meta.url), 'utf8'));
  for (const key of source.matchAll(/:\s*'([a-zA-Z]+)',?\n/g)) {
   const k = key[1];
   assert.ok(texts[`activity.do.${k}`] || texts[`activity.area.${k}`], `${lang}: ${k}`);
  }
  for (const verb of ['run', 'change', 'remove', 'open']) assert.ok(texts[`activity.verb.${verb}`]);
  assert.ok(texts['activity.area.other']);
 }
});
