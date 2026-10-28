import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, readdirSync } from 'node:fs';
import {
	CHECK_TYPES,
	INTEGRATION_FIELDS,
	POLICY_MODES,
	FRONTENDS,
	TABS,
	USAGE,
	chartPoints,
	checkBody,
	checkForm,
	checkProblems,
	iconProblem,
	integrationBody,
	integrationProblems,
	statusPageURL,
	tabFrom,
	tone,
	usagePercent,
	usageText,
	validCIDR,
	validListenPort,
	validPrefix,
	validSlug,
} from '../src/routes/settings/ipv64/ipv64.ts';
import { settingsAreas, settingSections, matchesSetting } from '../src/lib/settings-search.ts';

const de = JSON.parse(readFileSync(new URL('../src/lib/i18n/de.json', import.meta.url)));
const en = JSON.parse(readFileSync(new URL('../src/lib/i18n/en.json', import.meta.url)));

test('tabs come from the address and fall back to the overview', () => {
	assert.equal(tabFrom('router'), 'router');
	assert.equal(tabFrom('nonsense'), 'overview');
	assert.equal(tabFrom(null), 'overview');
});

test('usage shows used and limit, percent only with a limit', () => {
	assert.equal(usageText({ used: 3, limit: 48 }), '3 / 48');
	assert.equal(usageText({ used: 1.5, limit: null }), '1.5');
	assert.equal(usageText(undefined), '–');
	assert.equal(usagePercent({ used: 60, limit: 48 }), 100);
	assert.equal(usagePercent({ used: 12, limit: 48 }), 25);
	assert.equal(usagePercent({ used: 1, limit: null }), null);
});

test('a check sends only the fields of its type', () => {
	const push = checkForm();
	push.name = 'backup';
	assert.deepEqual(checkProblems(push), []);
	const body = checkBody(push);
	assert.equal(body.period_seconds, 3600);
	assert.equal(body.url, undefined);
	const tcp = { ...checkForm(), name: 'ssh', type: 'tcp', target: '', port: 0 };
	assert.deepEqual(checkProblems(tcp), ['target', 'port']);
	const web = { ...checkForm(), name: 'w', type: 'keyword', url: 'ftp://x', keyword: '' };
	assert.deepEqual(checkProblems(web), ['url', 'keyword']);
	const ok = { ...web, url: 'https://nas.example.org', keyword: 'TitanOS' };
	const sent = checkBody(ok);
	assert.equal(sent.keyword, 'TitanOS');
	assert.equal(sent.target, undefined);
	assert.equal(sent.period_seconds, undefined);
	assert.ok(checkProblems({ ...ok, retry: 5 }).includes('retry'));
});

test('integrations need the fields of their type and keep numbers as numbers', () => {
	assert.deepEqual(integrationProblems('telegram', { chat_id: '42' }), ['api_token']);
	assert.deepEqual(integrationProblems('gotify', { server_url: 'https://g.example', app_token: 'a', priority: '11' }), ['priority']);
	assert.deepEqual(integrationProblems('webhook', {}), ['url_down']);
	assert.deepEqual(integrationProblems('webhook', { url_up: 'https://hook.example/up' }), []);
	assert.deepEqual(integrationProblems('carrier', {}), ['type']);
	const body = integrationBody('ntfy', ' phone ', true, { server_url: 'https://ntfy.sh/topic', priority_down: '5', priority_up: '' });
	assert.deepEqual(body, { type: 'ntfy', name: 'phone', default: true, server_url: 'https://ntfy.sh/topic', priority_down: 5 });
	assert.equal(integrationBody('sms', 's', false, { number: '0171' }).country_code, '0049');
});

test('validators follow the description of IPv64', () => {
	assert.ok(validSlug('meinnas'));
	assert.ok(!validSlug('no'));
	assert.ok(!validSlug('mein-nas'));
	assert.ok(validListenPort(0) && validListenPort(50000) && validListenPort(62000));
	assert.ok(!validListenPort(49999) && !validListenPort(62001));
	assert.ok(validCIDR('198.51.100.0/24') && validCIDR('2001:db8::/48') && validCIDR('10.0.0.1'));
	assert.ok(!validCIDR('300.1.1.1') && !validCIDR('10.0.0.0/33') && !validCIDR('hello'));
	assert.ok(validPrefix('') && validPrefix('photos') && !validPrefix('a.b') && !validPrefix('-x'));
	assert.equal(iconProblem({ type: 'image/gif', size: 10 }), 'type');
	assert.equal(iconProblem({ type: 'image/png', size: 2 * 1024 * 1024 }), 'size');
	assert.equal(iconProblem({ type: 'image/webp', size: 1000 }), '');
	assert.equal(statusPageURL('meinnas'), 'https://ipv64.net/status/meinnas');
	assert.equal(tone('down'), 'error');
	assert.equal(tone('up'), 'ok');
	assert.equal(tone('paused'), 'warn');
});

test('the availability chart maps 100 % to the top', () => {
	const points = chartPoints(
		[
			{ time: 'a', measurements: 1, availability_percent: 100, latency_avg_ms: null, latency_max_ms: null },
			{ time: 'b', measurements: 1, availability_percent: 50, latency_avg_ms: null, latency_max_ms: null },
		],
		300,
		60,
	);
	assert.equal(points, '0,0 300,30');
	assert.equal(chartPoints([], 300, 60), '');
});

test('the settings area is in Network and access with its tabs', () => {
	const area = settingsAreas.find((a) => a.id === 'ipv64');
	assert.ok(area && area.group === 'network' && area.admin);
	assert.equal(settingSections.ipv64.length, TABS.length);
	assert.ok(matchesSetting('cloud router', 'IPv64', 'ipv64'));
	assert.ok(matchesSetting('failover', 'IPv64', 'ipv64'));
});

function keysIn(file) {
	const text = readFileSync(file, 'utf8');
	return [...text.matchAll(/'((?:ipv64x|ipv64)\.[A-Za-z0-9_.]+)'/g)].map((m) => m[1]).filter((k) => k !== 'ipv64.de');
}

test('every text of the IPv64 pages exists in German and English', () => {
	const dir = new URL('../src/routes/settings/ipv64/', import.meta.url);
	const files = readdirSync(dir).filter((f) => f.endsWith('.svelte')).map((f) => new URL(f, dir));
	files.push(new URL('../src/routes/docker/apps/[name]/AppIpv64.svelte', import.meta.url));
	files.push(new URL('../src/routes/settings/ddns/Ipv64Panel.svelte', import.meta.url));
	const keys = new Set(files.flatMap(keysIn));
	// Keys built from the enums of the description.
	for (const tab of TABS) keys.add(`ipv64x.tab.${tab}`);
	for (const u of USAGE) keys.add(`ipv64x.usage.${u}`);
	for (const ty of CHECK_TYPES) keys.add(`ipv64x.checkType.${ty}`);
	for (const st of ['new', 'up', 'paused', 'warning', 'down', 'stabilizing']) keys.add(`ipv64x.checkState.${st}`);
	for (const ty of [...Object.keys(INTEGRATION_FIELDS), 'account_email']) keys.add(`ipv64x.integrationType.${ty}`);
	for (const fields of Object.values(INTEGRATION_FIELDS)) for (const f of fields) keys.add(`ipv64x.field.${f.name}`);
	for (const m of POLICY_MODES) keys.add(`ipv64x.mode.${m}`);
	for (const st of ['pending', 'active', 'failed_over', 'paused', 'failover_requested', 'fallback_requested']) keys.add(`ipv64x.policyState.${st}`);
	for (const f of FRONTENDS) keys.add(`ipv64x.frontendKind.${f}`);
	for (const st of ['on', 'off', 'disabling', 'suspended_traffic_limit']) keys.add(`ipv64x.onOff.${st}`);
	for (const p of ['server_assignment', 'dns', 'config_deploy', 'certificate', 'server_move', 'deletion']) keys.add(`ipv64x.pendingKind.${p}`);
	for (const c of ['default', 'letsencrypt', 'issuing']) keys.add(`ipv64x.cert.${c}`);
	for (const st of ['active', 'inactive', 'cdn_record', 'cdn_proxied', 'cdn_partial', 'failover_record', 'failover_replaced']) keys.add(`ipv64x.recordState.${st}`);
	for (const s of ['own', 'ddns', 'letsencrypt']) keys.add(`ipv64x.keySource.${s}`), keys.add(`ipv64x.usedBy.${s}`);
	for (const kind of ['delete', 'reset', 'rotate', 'record', 'deleteCheck', 'rotateCheck', 'deleteIntegration', 'deletePage'])
		for (const part of ['title', 'text', 'button']) keys.add(`ipv64x.confirm.${kind}.${part}`);
	for (const kind of ['ipv64_failover', 'ipv64_fallback', 'ipv64_check_down', 'ipv64_cdn_traffic']) keys.add(`notify.kind.${kind}.title`), keys.add(`notify.kind.${kind}.text`);
	for (const code of ['ipv64_not_found', 'ipv64_conflict', 'ipv64_limit', 'ipv64_invalid', 'ipv64_exists', 'ipv64_reserved_port', 'ipv64_port_not_published', 'browser_session_required', 'ipv64_confirm_required'])
		keys.add(`error.${code}`);
	const missing = [...keys].filter((k) => !(k in de) || !(k in en) || !de[k] || !en[k]);
	assert.deepEqual(missing, []);
});
