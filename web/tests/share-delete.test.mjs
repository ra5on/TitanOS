import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { canDeleteData, deletionPercent, usageKey } from '../src/lib/share-delete.ts';
import { taskItems } from '../src/lib/operations/tasks.ts';

test('every usage kind has a text in both languages', () => {
 const go = readFileSync(new URL('../../backend/internal/shares/purge.go', import.meta.url), 'utf8');
 const kinds = [...go.matchAll(/\bUse[A-Za-z]+\s*=\s*"([a-z-]+)"/g)].map((m) => m[1]);
 assert.ok(kinds.length >= 15);
 for (const lang of ['de', 'en']) {
  const texts = JSON.parse(readFileSync(new URL(`../src/lib/i18n/${lang}.json`, import.meta.url), 'utf8'));
  for (const kind of kinds) assert.notEqual(usageKey(kind), 'share.use.other', kind);
  for (const kind of [...kinds, 'other']) assert.ok(texts[`share.use.${kind}`], `${lang}: ${kind}`);
  for (const kind of ['share_data_deleted', 'share_data_delete_failed'])
   assert.ok(texts[`notify.kind.${kind}.title`] && texts[`notify.kind.${kind}.text`], `${lang}: ${kind}`);
 }
 assert.equal(usageKey('something-new'), 'share.use.other');
});

test('deleting needs a preview without blockers', () => {
 const preview = { blockers: [], warnings: [{ kind: 'volume-snapshots' }] };
 assert.equal(canDeleteData(null), false);
 assert.equal(canDeleteData(preview), true);
 assert.equal(canDeleteData({ ...preview, blockers: [{ kind: 'vm', name: 'win' }] }), false);
});

test('share deletions appear as tasks', () => {
 const base = { share: 'Fotos', volume: 'tank', path: '/mnt/tank/Fotos', started: 'a', bytes: 0, totalBytes: 0, shareRemoved: true, dataKept: false };
 const running = { ...base, id: 'r', phase: 'deleting', files: 25, totalFiles: 100 };
 const failed = { ...base, id: 'f', phase: 'failed', files: 0, totalFiles: 3, dataKept: true, error: 'in use', finished: 'b' };
 const done = { ...base, id: 'd', phase: 'done', files: 3, totalFiles: 3, finished: 'c' };
 assert.equal(deletionPercent(running), 25);
 assert.equal(deletionPercent({ ...running, phase: 'checking' }), undefined);
 const tasks = taskItems({ transfers: [], shareDeletions: [running, failed, done] });
 assert.deepEqual(tasks.map((t) => [t.id, t.state, t.kind]), [['share-delete:r', 'running', 'files'], ['share-delete:f', 'failed', 'files'], ['share-delete:d', 'done', 'files']]);
 assert.equal(tasks[0].progress, 25);
 assert.equal(tasks[1].detail, 'in use');
 assert.equal(tasks[0].href, '/shares');
});
