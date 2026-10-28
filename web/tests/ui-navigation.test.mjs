import { test } from 'node:test';
import assert from 'node:assert/strict';
import { noticeLink } from '../src/lib/notice-link.ts';
import { matchesSetting } from '../src/lib/settings-search.ts';
import { taskPercent } from '../src/lib/task-progress.ts';

test('notifications link to existing local destinations and encode untrusted subjects', () => {
 assert.equal(noticeLink('volume_full', 'data'), '/storage#volume-data');
 assert.equal(noticeLink('backup_failed', 'daily'), '/settings/backups/data#backup-daily');
 assert.equal(noticeLink('config_backup_failed', 'data'), '/settings/backups/configuration');
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

test('settings sections resolve specific search terms to distinct destinations', async () => {
 const { settingSections } = await import('../src/lib/settings-search.ts');
 const cases = [['SSH','access','/settings/access#access-ssh'], ['Zertifikat','access','/settings/access#access-cert'], ['Terminal','access','/settings/access#access-terminal'], ['DNS','network','/settings/network#network-dns'], ['restic','backups','/settings/backups/data'], ['config','backups','/settings/backups/configuration'], ['ersetzen','backups','/settings/backups/restore']];
 for (const [query, area, href] of cases) {
  assert.deepEqual(settingSections[area].filter(s => matchesSetting(query, '', s.id)).map(s => s.href), [href]);
 }
});

test('the HTTPS names of apps have their own admin-only settings page', async () => {
 const { settingsAreas, userSettingsPaths } = await import('../src/lib/settings-search.ts');
 const area = settingsAreas.find(a => a.id === 'proxy');
 assert.ok(area?.admin);
 assert.equal(area.group, 'network');
 assert.ok(!userSettingsPaths.includes('/settings/proxy'));
 for (const q of ['proxy', 'Reverse Proxy', 'mdns', 'domain']) assert.ok(matchesSetting(q, 'HTTPS-Namen für Apps', 'proxy'), q);
});
