import { test } from 'node:test';
import assert from 'node:assert/strict';
import { noticeLink } from '../src/lib/notice-link.ts';
import { matchesSetting, settingsAreaOf } from '../src/lib/settings-search.ts';
import { taskPercent } from '../src/lib/task-progress.ts';

test('notifications link to existing local destinations and encode untrusted subjects', () => {
	assert.equal(noticeLink('volume_full', 'data'), '/storage#volume-data');
	assert.equal(noticeLink('backup_failed', 'daily'), '/settings/backups/data#backup-daily');
	assert.equal(noticeLink('config_backup_failed', 'data'), '/settings/backups/configuration');
	assert.equal(noticeLink('update_available', '0.6.0'), '/settings/updates');
	assert.equal(noticeLink('disk_test_failed', 'sda'), '/tools');
	assert.equal(noticeLink('volume_failed', 'x/#?"'), '/storage#volume-x%2F%23%3F%22');
	assert.equal(noticeLink('unknown', 'https://evil.example'), null);
	assert.equal(
		noticeLink('ipv64_check_down', 'https://evil.example'),
		'/settings/ddns?tab=ipv64&sub=monitoring',
	);
	assert.equal(noticeLink('ipv64_failover', 'p'), '/settings/ddns?tab=ipv64&sub=failover');
	assert.equal(noticeLink('ipv64_cdn_traffic', 'x'), '/settings/ddns?tab=ipv64&sub=public');
});
test('settings search matches translated titles and synonyms, case and accent independently', () => {
	for (const q of ['SSH', 'certificate', 'Zertifikat', 'schlussel'])
		assert.ok(matchesSetting(q, 'Zugang', 'access'));
	assert.ok(matchesSetting('dns', 'Netzwerk', 'network'));
	assert.ok(matchesSetting('time zone', 'Zeitzone', 'timezone'));
	assert.ok(matchesSetting('  ', 'Darstellung', 'appearance'));
	assert.ok(!matchesSetting('ssh', 'Darstellung', 'appearance'));
	assert.ok(!matchesSetting('ssh unknown', 'Zugang', 'access'));
});
test('task percentages preserve unknown totals and reject invalid measurements', () => {
	assert.equal(taskPercent(0, 0), undefined);
	assert.equal(taskPercent(7, 0), undefined);
	assert.equal(taskPercent(NaN, 100), undefined);
	assert.equal(taskPercent(1, Infinity), undefined);
	assert.equal(taskPercent(25, 100), 25);
	assert.equal(taskPercent(-10, 100), 0);
	assert.equal(taskPercent(200, 100), 100);
});

test('settings sections resolve specific search terms to distinct destinations', async () => {
	const { settingSections } = await import('../src/lib/settings-search.ts');
	const cases = [
		['SSH', 'access', '/settings/access#access-ssh'],
		['Zertifikat', 'access', '/settings/access#access-cert'],
		['Terminal', 'access', '/settings/access#access-terminal'],
		['DNS', 'network', '/settings/network#network-dns'],
		['restic', 'backups', '/settings/backups/data'],
		['config', 'backups', '/settings/backups/configuration'],
		['ersetzen', 'backups', '/settings/backups/restore'],
	];
	for (const [query, area, href] of cases) {
		assert.deepEqual(
			settingSections[area].filter((s) => matchesSetting(query, '', s.id)).map((s) => s.href),
			[href],
		);
	}
});

test('the HTTPS names of apps have their own admin-only settings page', async () => {
	const { settingsAreas, userSettingsPaths } = await import('../src/lib/settings-search.ts');
	const area = settingsAreas.find((a) => a.id === 'proxy');
	assert.ok(area?.admin);
	assert.equal(area.group, 'network');
	assert.ok(!userSettingsPaths.includes('/settings/proxy'));
	for (const q of ['proxy', 'Reverse Proxy', 'mdns', 'domain'])
		assert.ok(matchesSetting(q, 'HTTPS-Namen für Apps', 'proxy'), q);
});

test('the settings navigation keeps its fixed groups, order and role filter', async () => {
	const { settingsEntries, settingsGroups } = await import('../src/lib/settings-search.ts');
	const label = (key) => key;
	const admin = settingsEntries('', true, label);
	assert.deepEqual(
		admin.map((g) => g.group),
		[...settingsGroups],
	);
	assert.deepEqual(
		admin.map((g) => [g.group, g.entries.map((e) => e.id)]),
		[
			['system', ['about', 'updates', 'timezone', 'notifications']],
			['network', ['network', 'ddns', 'proxy', 'cluster']],
			['security', ['access', 'firewall', 'letsencrypt', 'wireguard']],
			['hardware', ['hardware', 'temperatures', 'energy', 'ups', 'power-button']],
			['data', ['memory', 'backups', 'files']],
			['integrations', ['homeassistant', 'mcp', 'api']],
			['personal', ['appearance']],
		],
	);
	const fresh = admin
		.flatMap((g) => g.entries)
		.filter((e) => e.isNew)
		.map((e) => e.id);
	assert.deepEqual(fresh, ['ddns', 'cluster', 'firewall', 'letsencrypt', 'wireguard']);
	const user = settingsEntries('', false, label).flatMap((g) => g.entries.map((e) => e.id));
	assert.deepEqual(user, ['about', 'temperatures', 'api', 'appearance']);
	const ssh = settingsEntries('SSH', true, label).flatMap((g) => g.entries);
	assert.deepEqual(
		ssh.map((e) => [e.href, e.section]),
		[['/settings/access#access-ssh', true]],
	);
	assert.equal(settingsEntries('SSH', false, label).length, 0);
	assert.deepEqual(
		settingsEntries('zeitzone', true, label).flatMap((g) => g.entries.map((e) => e.href)),
		['/settings/timezone'],
	);
});
test('a settings path names the area whose "new" badge it takes away', () => {
	assert.equal(settingsAreaOf('/settings/ddns'), 'ddns');
	assert.equal(settingsAreaOf('/settings/backups/data/'), 'backups');
	assert.equal(settingsAreaOf('/settings'), '');
	assert.equal(settingsAreaOf('/settings/'), '');
});
