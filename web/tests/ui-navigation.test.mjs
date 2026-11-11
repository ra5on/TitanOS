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

test('the settings navigation keeps its eight areas in order, sorts entries A–Z and filters by role', async () => {
	const { settingsEntries } = await import('../src/lib/settings-search.ts');
	const { readFileSync } = await import('node:fs');
	const de = JSON.parse(readFileSync(new URL('../src/lib/i18n/de.json', import.meta.url), 'utf8'));
	const label = (key) => de[key] ?? key;
	const admin = settingsEntries('', true, label);
	// PLAN 78 (draft 78): the eight areas of the menu – in this order.
	assert.deepEqual(
		admin.map((g) => g.group),
		['system', 'hardware', 'network', 'accounts', 'services', 'ai', 'data', 'updates'],
	);
	const order = new Intl.Collator(undefined, { sensitivity: 'base', numeric: true });
	for (const g of admin) {
		const names = g.entries.map((e) => e.label);
		assert.deepEqual(names, [...names].sort(order.compare), g.group);
	}
	assert.deepEqual(admin.flatMap((g) => g.entries.map((e) => e.id)).sort(), [
		'about',
		'access',
		'agent',
		'api',
		'appearance',
		'backups',
		'cluster',
		'ddns',
		'energy',
		'files',
		'firewall',
		'hardware',
		'homeassistant',
		'letsencrypt',
		'mcp',
		'memory',
		'network',
		'notifications',
		'power-button',
		'proxy',
		'services',
		'temperatures',
		'timezone',
		'updates',
		'ups',
		'users',
		'viewer',
		'wireguard',
	]);
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
		'users',
		'viewer',
		'wireguard',
	]);
	const user = settingsEntries('', false, label).flatMap((g) => g.entries.map((e) => e.id));
	assert.deepEqual(user.sort(), ['about', 'api', 'appearance', 'mcp', 'temperatures']);
	// MCP is one page for everyone, no longer a section of API and CLI.
	assert.deepEqual(
		settingsEntries('mcp', false, label).flatMap((g) => g.entries.map((e) => e.href)),
		['/settings/mcp'],
	);
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

test('PLAN 78: the menu of Systemsteuerung lists the areas an account may open; its search finds pages and sections', async () => {
	const { settingsEntries, settingsMenu, settingsSideAllowed, settingsCrumbs, settingsGroupOf } =
		await import('../src/lib/settings-search.ts');
	const { SYSTEM_APPS, sideActive } = await import('../src/lib/desktop/apps.ts');
	const { readFileSync, readdirSync } = await import('node:fs');
	const dir = new URL('../src/lib/i18n/', import.meta.url);
	const de = JSON.parse(readFileSync(new URL('de.json', dir), 'utf8'));
	for (const f of readdirSync(new URL('features/', dir)).filter((n) => n.endsWith('.de.json')))
		Object.assign(de, JSON.parse(readFileSync(new URL('features/' + f, dir), 'utf8')));
	const label = (key) => de[key] ?? key;
	const find = (q, admin = true) =>
		settingsEntries(q, admin, label).flatMap((g) => g.entries.map((e) => e.href));
	// A section hit leads to the section's anchor.
	assert.deepEqual(find('ssh'), ['/settings/access#access-ssh']);
	assert.ok(find('Zeitzone').includes('/settings/timezone'));
	assert.ok(find('platten-ruhe').includes('/settings/energy'));
	assert.equal(find('xyzzy').length, 0);
	// Accounts with the role "user" only find what they may open.
	assert.equal(find('ssh', false).length, 0);
	assert.ok(find('darstellung', false).includes('/settings/appearance'));
	assert.ok(settingsSideAllowed('/settings', false));
	assert.ok(!settingsSideAllowed('/settings/network', false));

	// The menu: every area with its name; an account sees only areas with a page it may open.
	const menu = settingsMenu(true);
	assert.deepEqual(
		menu.map((e) => label(e.label)),
		[
			'System',
			'Hardware und Energie',
			'Netzwerk',
			'Konten und Zugang',
			'Dienste und Meldungen',
			'KI und Smart Home',
			'Daten und Sicherung',
			'Updates',
		],
	);
	// An area with one page leads straight to it.
	assert.equal(menu.at(-1).href, '/settings/updates');
	assert.equal(menu[1].href, '/settings?area=hardware');
	const user = settingsMenu(false);
	assert.deepEqual(
		user.map((e) => e.group),
		['system', 'hardware', 'accounts', 'ai'],
	);
	for (const entry of user) for (const page of entry.pages) assert.ok(!page.admin, page.id);
	// The path above a page's title and the area an address belongs to.
	assert.deepEqual(settingsCrumbs('/settings/energy', true), [
		{ label: 'settingshome.group.hardware', href: '/settings?area=hardware' },
	]);
	assert.deepEqual(
		settingsCrumbs('/settings/backups/coverage', true).map((c) => c.href),
		['/settings?area=data', '/settings/backups'],
	);
	assert.deepEqual(settingsCrumbs('/settings', true), []);
	assert.equal(settingsGroupOf('/settings?area=network'), 'network');
	assert.equal(settingsGroupOf('/settings/ups'), 'hardware');
	assert.equal(settingsGroupOf('/settings'), undefined);
	// The window's side list carries the same areas (phone menu, menu bar).
	const side = SYSTEM_APPS.find((a) => a.id === 'settings').side;
	assert.equal(side.length, 9);
	assert.ok(sideActive(side[2], '/settings/energy', side));
});
