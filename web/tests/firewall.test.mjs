import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import {
	appPorts,
	appZones,
	countdown,
	ruleProblem,
	sameRules,
	servicePorts,
	validPorts,
	validSource,
} from '../src/lib/firewall.ts';
import { settingsAreas, settingSections, matchesSetting } from '../src/lib/settings-search.ts';
import { activityLabel } from '../src/lib/activity.ts';

const config = () => ({
	enabled: false,
	services: { webui: { home: true, vpn: true, internet: false }, ssh: { home: true, vpn: true, internet: false } },
	apps: {},
	newApps: { home: true, vpn: true, internet: false },
	rules: [],
	autoBlock: { enabled: false, attempts: 10, within: 5, minutes: 60, permanent: false, exemptLocal: true, exempt: [] },
});

test('ports of services and apps are readable', () => {
	assert.equal(
		servicePorts({ id: 'smb', ports: [{ protocol: 'tcp', ports: ['139', '445'] }, { protocol: 'udp', ports: ['137-138'] }], default: {} }),
		'TCP 139, 445 · UDP 137-138',
	);
	assert.equal(servicePorts({ id: 'ping', ports: [{ protocol: 'icmp', ports: null }], default: {} }), 'ICMP');
	assert.equal(
		appPorts({ key: 'a', name: 'a', ports: [{ port: 8096, protocol: 'tcp' }, { port: 53, protocol: 'tcp' }, { port: 53, protocol: 'udp' }, { port: 53, protocol: 'tcp' }] }),
		'TCP 53, 8096 · UDP 53',
	);
});

test('apps without a setting follow the one for new apps', () => {
	const cfg = config();
	assert.deepEqual(appZones(cfg, 'immich'), cfg.newApps);
	cfg.apps.immich = { home: true, vpn: false, internet: true };
	assert.equal(appZones(cfg, 'immich').internet, true);
});

test('changes are detected without the automatic blocking and key order', () => {
	const a = config();
	const b = config();
	b.autoBlock.enabled = true;
	b.services = { ssh: b.services.ssh, webui: b.services.webui };
	assert.ok(sameRules(a, b));
	b.services.ssh = { home: false, vpn: true, internet: false };
	assert.ok(!sameRules(a, b));
});

test('rules are checked like the backend does', () => {
	for (const ok of ['22', '1-65535', '8000-8100']) assert.ok(validPorts(ok), ok);
	for (const bad of ['0', '70000', '90-80', '22,23', 'ssh']) assert.ok(!validPorts(bad), bad);
	for (const ok of ['192.168.1.5', '10.0.0.0/8', '2001:db8::/32', 'fd7a:115c:a1e0::5']) assert.ok(validSource(ok), ok);
	for (const bad of ['example.org', '300.1.1.1', '10.0.0.0/33', '2001:db8::/129', '1.2.3.4/8/1']) assert.ok(!validSource(bad), bad);
	assert.equal(ruleProblem({ action: 'allow', protocol: 'tcp', ports: '22' }), '');
	assert.equal(ruleProblem({ action: 'allow', protocol: 'tcp', ports: 'x' }), 'firewall.rulePorts');
	assert.equal(ruleProblem({ action: 'deny', protocol: 'any', source: 'nas' }), 'firewall.ruleSource');
	assert.equal(ruleProblem({ action: 'deny', protocol: 'any', comment: 'say "hi"' }), 'firewall.ruleComment');
	assert.equal(countdown(119), '1:59');
	assert.equal(countdown(-3), '0:00');
});

test('the firewall page is found and named in both languages', () => {
	assert.ok(settingsAreas.some((area) => area.id === 'firewall' && area.admin && area.group === 'network'));
	assert.ok(matchesSetting('nftables', 'Firewall', 'firewall'));
	assert.ok(settingSections.firewall.some((s) => s.href === '/settings/firewall#firewall-autoblock'));
	assert.deepEqual(activityLabel('firewall_block'), { kind: 'named', key: 'firewallBlock' });
	const page = readFileSync(new URL('../src/routes/settings/firewall/+page.svelte', import.meta.url), 'utf8');
	const used = new Set([...page.matchAll(/t\('((?:firewall|common|ui|nav)\.[A-Za-z.]+)'/g)].map((m) => m[1]));
	for (const id of ['webui', 'apps', 'smb', 'nfs', 'ssh', 'discovery', 'ping', 'ups', 'backuptarget', 'vpn'])
		for (const prefix of ['firewall.svc.', 'firewall.svcText.']) used.add(prefix + id);
	for (const zone of ['home', 'vpn', 'internet']) used.add(`firewall.zone.${zone}`);
	for (const source of ['web', 'ssh', 'smb']) used.add(`firewall.source.${source}`);
	for (const code of ['invalid_firewall', 'firewall_excludes_you', 'firewall_pending', 'firewall_not_pending', 'firewall_not_blocked', 'firewall_failed'])
		used.add(`error.${code}`);
	for (const kind of ['firewall_blocked', 'firewall_reverted', 'firewall_failed'])
		for (const part of ['title', 'text']) used.add(`notify.kind.${kind}.${part}`);
	for (const action of ['firewallBlock', 'firewallReverted', 'firewallChange', 'firewallConfirm', 'firewallRevert', 'firewallAutoBlock', 'firewallUnblock'])
		used.add(`activity.do.${action}`);
	for (const lang of ['de', 'en']) {
		const texts = JSON.parse(readFileSync(new URL(`../src/lib/i18n/${lang}.json`, import.meta.url), 'utf8'));
		for (const key of used) assert.ok(texts[key], `${lang}: ${key}`);
	}
});
