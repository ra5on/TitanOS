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
	assert.equal(noticeLink('disk_test_failed', 'sda'), '/tools/disks');
	assert.equal(noticeLink('volume_failed', 'x/#?"'), '/storage#volume-x%2F%23%3F%22');
	assert.equal(noticeLink('unknown', 'https://evil.example'), null);
	// Notifications of the removed IPv64 account area that are still listed
	// lead to dynamic DNS, never to a tab that is gone.
	assert.equal(noticeLink('ipv64_check_down', 'https://evil.example'), '/settings/ddns');
	assert.equal(noticeLink('ipv64_failover', 'p'), '/settings/ddns');
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

test('the HTTPS names of apps have their own admin-only settings page', async () => {
	const { settingsAreas, userSettingsPaths } = await import('../src/lib/settings-search.ts');
	const area = settingsAreas.find((a) => a.id === 'proxy');
	assert.ok(area?.admin);
	assert.equal(area.group, 'network');
	assert.ok(!userSettingsPaths.includes('/settings/proxy'));
	for (const q of ['proxy', 'Reverse Proxy', 'mdns', 'domain'])
		assert.ok(matchesSetting(q, 'HTTPS-Namen für Apps', 'proxy'), q);
});

test('PLAN 86: the eight areas and their pages in the confirmed order, filtered by role', async () => {
	const { settingsEntries } = await import('../src/lib/settings-search.ts');
	const { readFileSync } = await import('node:fs');
	const de = JSON.parse(readFileSync(new URL('../src/lib/i18n/de.json', import.meta.url), 'utf8'));
	const label = (key) => de[key] ?? key;
	const admin = settingsEntries('', true, label);
	// Draft 1, confirmed by the owner on 13.11.2026: areas and pages in this
	// order (the same in both languages, not A–Z).
	assert.deepEqual(
		admin.map((g) => [g.group, g.entries.map((e) => e.id)]),
		[
			['system', ['about', 'updates', 'appearance', 'timezone', 'services', 'viewer']],
			['hardware', ['hardware', 'temperatures', 'energy', 'power-button', 'ups', 'memory']],
			['network', ['network', 'wireguard', 'ddns', 'letsencrypt', 'proxy']],
			['security', ['users', 'access', 'firewall', 'api']],
			['notifications', ['notifications']],
			['data', ['backups', 'systemdata', 'files']],
			['cluster', ['cluster']],
			['smarthome', ['homeassistant', 'agent', 'speech', 'mcp']],
		],
	);
	const fresh = admin
		.flatMap((g) => g.entries)
		.filter((e) => e.newSince)
		.map((e) => e.id);
	assert.deepEqual(fresh.sort(), [
		'agent',
		'cluster',
		'ddns',
		'firewall',
		'letsencrypt',
		'mcp',
		'services',
		'speech',
		'systemdata',
		'users',
		'viewer',
		'wireguard',
	]);
	// A user sees only its pages; areas without one disappear.
	assert.deepEqual(
		settingsEntries('', false, label).map((g) => [g.group, g.entries.map((e) => e.id)]),
		[
			['system', ['about', 'appearance']],
			['hardware', ['temperatures']],
			['security', ['api']],
			['smarthome', ['mcp']],
		],
	);
	// MCP is one page for everyone, no longer a section of API and CLI.
	assert.deepEqual(
		settingsEntries('mcp', false, label).flatMap((g) => g.entries.map((e) => e.href)),
		['/settings/mcp'],
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

test('"new" badges end by themselves after two minor versions or 30 days', async () => {
	const { stillNew } = await import('../src/lib/settings-search.ts');
	const since = { version: '0.41.0', date: '2026-10-31' };
	const day = (d) => Date.parse(`${d}T12:00:00Z`);
	assert.ok(stillNew(since, '0.41.0', day('2026-10-31')));
	assert.ok(stillNew(since, '0.42.3', day('2026-11-10')));
	assert.ok(!stillNew(since, '0.43.0', day('2026-11-10')), 'second minor version after');
	assert.ok(!stillNew(since, '1.0.0', day('2026-11-10')), 'new major version');
	assert.ok(!stillNew(since, '0.41.1', day('2026-11-30')), '30 days over');
	assert.ok(stillNew(since, '0.41.1', day('2026-11-29')));
	// Dev builds and missing data: only the date counts.
	assert.ok(stillNew(since, 'v0.40.0-12-gabc-dirty', day('2026-11-01')));
	assert.ok(stillNew(since, undefined, day('2026-11-01')));
	assert.ok(!stillNew(since, 'dev', day('2026-12-01')));
	// Areas from 0.31 are no longer new in 0.40.
	assert.ok(!stillNew({ version: '0.31.0', date: '2026-10-28' }, '0.40.0', day('2026-10-31')));
});

test('PLAN 86: the menu of Systemsteuerung – areas open in the column, old area links still lead somewhere', async () => {
	const {
		settingsMenu,
		settingsSideAllowed,
		settingsCrumbs,
		settingsGroupOf,
		settingsAreaTarget,
		settingsGroupHref,
	} = await import('../src/lib/settings-search.ts');
	const { SYSTEM_APPS, sideActive, sideFor } = await import('../src/lib/desktop/apps.ts');
	const { readFileSync, readdirSync } = await import('node:fs');
	const dir = new URL('../src/lib/i18n/', import.meta.url);
	const de = JSON.parse(readFileSync(new URL('de.json', dir), 'utf8'));
	const en = JSON.parse(readFileSync(new URL('en.json', dir), 'utf8'));
	for (const f of readdirSync(new URL('features/', dir)))
		Object.assign(
			f.endsWith('.de.json') ? de : en,
			JSON.parse(readFileSync(new URL('features/' + f, dir), 'utf8')),
		);
	const label = (key) => de[key] ?? key;
	assert.ok(settingsSideAllowed('/settings', false));
	assert.ok(!settingsSideAllowed('/settings/network', false));

	// The menu: every area with its name in both languages.
	const menu = settingsMenu(true);
	assert.deepEqual(
		menu.map((e) => label(e.label)),
		[
			'System',
			'Hardware und Energie',
			'Netzwerk und Fernzugriff',
			'Sicherheit und Konten',
			'Benachrichtigungen',
			'Daten und Sicherung',
			'Cluster',
			'Smart Home und KI',
		],
	);
	for (const entry of menu) assert.ok(en[entry.label], entry.label);
	// An area leads to its first page; one with a single page is that page
	// (the menu shows no expander for it).
	assert.equal(menu[1].href, '/settings/hardware');
	assert.deepEqual(
		menu.filter((e) => e.pages.length === 1).map((e) => e.href),
		['/settings/notifications', '/settings/cluster'],
	);
	// A user: System with two pages, three areas with one page each.
	const user = settingsMenu(false);
	assert.deepEqual(
		user.map((e) => [e.group, e.pages.length, e.href]),
		[
			['system', 2, '/settings/about'],
			['hardware', 1, '/settings/temperatures'],
			['security', 1, '/settings/api'],
			['smarthome', 1, '/settings/mcp'],
		],
	);
	for (const entry of user) for (const page of entry.pages) assert.ok(!page.admin, page.id);

	// The path above a page's title: the area as text (no area page any more).
	assert.deepEqual(settingsCrumbs('/settings/energy', true), [
		{ label: 'settingshome.group.hardware' },
	]);
	assert.deepEqual(settingsCrumbs('/settings/backups/coverage', true), [
		{ label: 'settingshome.group.data' },
		{ label: 'safeguard.title', href: '/settings/backups' },
	]);
	// One page alone in its area (for this account) has no path above it.
	assert.deepEqual(settingsCrumbs('/settings/cluster', true), []);
	assert.deepEqual(settingsCrumbs('/settings/temperatures', false), []);
	assert.deepEqual(settingsCrumbs('/settings', true), []);

	// The area an address belongs to, also for links made before PLAN 86.
	assert.equal(settingsGroupOf('/settings?area=network'), 'network');
	assert.equal(settingsGroupOf('/settings/ups'), 'hardware');
	assert.equal(settingsGroupOf('/settings/cluster'), 'cluster');
	assert.equal(settingsGroupOf('/settings?area=accounts'), 'security');
	assert.equal(settingsGroupOf('/settings?area=ai'), 'smarthome');
	assert.equal(settingsGroupOf('/settings?area=updates'), 'system');
	assert.equal(settingsGroupOf('/settings'), undefined);
	// Where "?area=" leads in a window: the first page the account may open.
	assert.equal(settingsAreaTarget('?area=hardware', true), '/settings/hardware');
	assert.equal(settingsAreaTarget('?area=hardware', false), '/settings/temperatures');
	assert.equal(settingsAreaTarget('?area=network', false), undefined);
	assert.equal(settingsAreaTarget('?area=accounts', true), '/settings/users');
	assert.equal(settingsAreaTarget('?area=updates', true), '/settings/updates');
	assert.equal(settingsAreaTarget('?area=services', true), '/settings/services');
	assert.equal(settingsAreaTarget('?area=services', false), '/settings/about');
	assert.equal(settingsAreaTarget('?area=ai', false), '/settings/mcp');
	assert.equal(settingsAreaTarget('?area=nonsense', true), undefined);

	// The program's side list (phone's ⋯ menu, search of the menu) carries
	// the same areas and hides the ones an account may not open.
	const app = SYSTEM_APPS.find((a) => a.id === 'settings');
	assert.equal(app.side.length, 9);
	assert.ok(sideActive(app.side[2], '/settings/energy', app.side));
	assert.ok(sideActive(app.side[4], '/settings/firewall', app.side));
	assert.equal(app.side[2].href, settingsGroupHref('hardware'));
	assert.deepEqual(
		sideFor(app.side, 'user').map((item) => item.href),
		[
			'/settings',
			'/settings?area=system',
			'/settings?area=hardware',
			'/settings?area=security',
			'/settings?area=smarthome',
		],
	);
	assert.equal(sideFor(app.side, 'admin').length, 9);
});
