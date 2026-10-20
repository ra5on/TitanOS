import { test } from 'node:test';
import assert from 'node:assert/strict';
import { noticeLink } from '../src/lib/notice-link.ts';
import { matchesSetting } from '../src/lib/settings-search.ts';
import { taskPercent } from '../src/lib/task-progress.ts';

test('notifications link to existing local destinations and encode untrusted subjects', () => {
 assert.equal(noticeLink('volume_full', 'data'), '/storage#volume-data');
 assert.equal(noticeLink('backup_failed', 'daily'), '/settings/backups#backup-daily');
 assert.equal(noticeLink('config_backup_failed', 'data'), '/settings/stick');
 assert.equal(noticeLink('update_available', '0.6.0'), '/settings/updates');
 assert.equal(noticeLink('disk_test_failed', 'sda'), '/tools');
 assert.equal(noticeLink('volume_failed', 'x/#?"'), '/storage#volume-x%2F%23%3F%22');
 assert.equal(noticeLink('unknown', 'https://evil.example'), null);
});
test('settings search matches translated titles and synonyms, case and accent independently', () => {
 for (const q of ['SSH', 'certificate', 'Zertifikat', 'schlussel']) assert.ok(matchesSetting(q, 'Zugang', 'access'));
 assert.ok(matchesSetting('dns', 'Netzwerk', 'network'));
 assert.ok(matchesSetting('time zone', 'Zeitzone', 'timezone'));
 assert.ok(matchesSetting('  ', 'Darstellung', 'appearance'));
 assert.ok(!matchesSetting('ssh', 'Darstellung', 'appearance'));
 assert.ok(!matchesSetting('ssh unknown', 'Zugang', 'access'));
});
test('task percentages preserve unknown totals and reject invalid measurements', () => {
 assert.equal(taskPercent(0,0),undefined);
 assert.equal(taskPercent(7,0),undefined);
 assert.equal(taskPercent(NaN,100),undefined);
 assert.equal(taskPercent(1,Infinity),undefined);
 assert.equal(taskPercent(25,100),25);
 assert.equal(taskPercent(-10,100),0);
 assert.equal(taskPercent(200,100),100);
});
