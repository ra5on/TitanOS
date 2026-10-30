import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import {
	folderRestoreRequest,
	folderRestoreProgress,
	needsReplaceConsent,
	restoredFolderLink,
} from '../src/lib/snapshot-restore.ts';
import { taskItems } from '../src/lib/operations/tasks.ts';
import { noticeLink } from '../src/lib/notice-link.ts';
import { bellGroup } from '../src/lib/notify-groups.ts';
import { activityLabel } from '../src/lib/activity.ts';

const job = (over) => ({
	id: 'a1',
	volume: 'data',
	snapshot: 'manual',
	path: 'photos',
	mode: 'original',
	conflict: 'skip',
	target: 'photos',
	state: 'running',
	phase: 'copying',
	totalFiles: 4,
	totalBytes: 200,
	files: 1,
	bytes: 50,
	copied: 1,
	cloned: 1,
	skipped: 0,
	replaced: 0,
	renamed: 0,
	special: 0,
	started: '2026-10-27T10:00:00Z',
	...over,
});

test('a new folder sends no conflict rule and the start carries the preview identity', () => {
	assert.deepEqual(folderRestoreRequest('photos', 'copy', 'replace'), {
		path: 'photos',
		mode: 'copy',
	});
	assert.deepEqual(folderRestoreRequest('photos', 'original', 'rename', 'u-1'), {
		path: 'photos',
		mode: 'original',
		conflict: 'rename',
		uuid: 'u-1',
	});
});

test('progress is unknown while counting and follows bytes, else files', () => {
	assert.equal(folderRestoreProgress(job({ phase: 'counting' })), undefined);
	assert.equal(folderRestoreProgress(job({})), 25);
	assert.equal(folderRestoreProgress(job({ totalBytes: 0, bytes: 0 })), 25);
	assert.equal(folderRestoreProgress(job({ totalBytes: 0, totalFiles: 0, files: 0 })), undefined);
	assert.equal(folderRestoreProgress(job({ state: 'done' })), 100);
});

test('replacing existing files needs consent only when something is replaced', () => {
	const preview = { mode: 'original', conflict: 'replace', conflicts: 2 };
	assert.equal(needsReplaceConsent(preview), true);
	assert.equal(needsReplaceConsent({ ...preview, conflicts: 0 }), false);
	assert.equal(needsReplaceConsent({ ...preview, conflict: 'rename' }), false);
	assert.equal(needsReplaceConsent({ ...preview, mode: 'copy' }), false);
	assert.equal(needsReplaceConsent(null), false);
});

test('folder restores appear as tasks with cancel, and failures need attention', () => {
	const tasks = taskItems({
		transfers: [],
		snapshotRestores: [
			job({}),
			job({ id: 'b2', state: 'failed', error: 'permission denied' }),
			job({ id: 'c3', state: 'done', mode: 'copy', target: 'photos (manual)' }),
		],
	});
	const running = tasks.find((t) => t.id === 'snapshot-restore:a1');
	assert.equal(running.kind, 'restore');
	assert.equal(running.progress, 25);
	assert.deepEqual(running.actions, [
		{ label: 'stop', path: '/storage/snapshot-restores/a1/cancel', body: {} },
	]);
	assert.equal(tasks.find((t) => t.id === 'snapshot-restore:b2').state, 'failed');
	assert.equal(tasks.find((t) => t.id === 'snapshot-restore:c3').detail, 'data/photos (manual)');
	assert.equal(
		restoredFolderLink(job({ target: 'photos (manual)' })),
		'/files?root=volume%2Fdata&path=photos+%28manual%29',
	);
});

test('notifications, bell group, activity and texts exist in both languages', () => {
	assert.equal(
		noticeLink('snapshot_restore_done', 'data/photos'),
		'/storage/manage?mode=snapshots',
	);
	assert.equal(bellGroup('snapshot_restore_failed'), 'storage');
	assert.deepEqual(
		activityLabel('POST /api/v1/storage/volumes/{name}/snapshots/{snapshot}/folders/restore'),
		{ kind: 'named', key: 'snapshotFolderRestore' },
	);
	const de = JSON.parse(readFileSync(new URL('../src/lib/i18n/de.json', import.meta.url), 'utf8'));
	const en = JSON.parse(readFileSync(new URL('../src/lib/i18n/en.json', import.meta.url), 'utf8'));
	for (const key of Object.keys(de).filter(
		(k) => k.startsWith('snapshotRestore.') || k.includes('snapshot_restore_'),
	))
		assert.ok(en[key], key);
	for (const kind of ['done', 'failed'])
		for (const part of ['title', 'text'])
			assert.ok(de[`notify.kind.snapshot_restore_${kind}.${part}`]);
	assert.ok(de['tasks.kind.restore'] && en['tasks.kind.restore']);
});
