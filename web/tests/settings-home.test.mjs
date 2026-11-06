// PLAN 61: the settings overview ("Alle Einstellungen"): every area in one of
// the four groups with a short line in both languages, and "Zuletzt geändert"
// only from settings routes of the activity log.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, readdirSync, statSync } from 'node:fs';
import { settingsAreas, settingsGroups } from '../src/lib/settings-search.ts';
import { settingsAreaOfAction, recentSettingsChanges } from '../src/lib/settings-recent.ts';

const read = (name) =>
	JSON.parse(readFileSync(new URL(`../src/lib/i18n/features/${name}`, import.meta.url), 'utf8'));
const de = read('settingshome.de.json');
const en = read('settingshome.en.json');

test('the texts of the overview exist in both languages', () => {
	assert.deepEqual(Object.keys(de).sort(), Object.keys(en).sort());
	for (const key of Object.keys(de)) assert.ok(key.startsWith('settingshome.'), key);
	for (const group of settingsGroups) assert.ok(de[`settingshome.group.${group}`], group);
	for (const area of settingsAreas) assert.ok(de[`settingshome.desc.${area.id}`], area.id);
});

test('every settings route has exactly one area (redirects excepted)', () => {
	const dir = new URL('../src/routes/settings/', import.meta.url);
	const routes = readdirSync(dir).filter((name) => statSync(new URL(name, dir)).isDirectory());
	const redirects = { ipv64: 'ddns', stick: 'backups' };
	const ids = settingsAreas.map((a) => a.id);
	assert.equal(new Set(ids).size, ids.length);
	for (const route of routes) assert.ok(ids.includes(redirects[route] ?? route), route);
	assert.ok(ids.includes('programs'));
	for (const area of settingsAreas) assert.ok(settingsGroups.includes(area.group), area.id);
});

test('only settings changes count as "recently changed"', () => {
	assert.equal(settingsAreaOfAction('PUT /api/v1/system/timezone'), 'timezone');
	assert.equal(settingsAreaOfAction('PUT /api/v1/network'), 'network');
	assert.equal(settingsAreaOfAction('PUT /api/v1/network/wireguard'), 'wireguard');
	assert.equal(settingsAreaOfAction('POST /api/v1/network/ipv64/records/add'), 'ddns');
	assert.equal(settingsAreaOfAction('PUT /api/v1/firewall'), 'firewall');
	assert.equal(settingsAreaOfAction('PUT /api/v1/backups/{id}'), 'backups');
	assert.equal(settingsAreaOfAction('POST /api/v1/account/tokens'), 'api');
	assert.equal(settingsAreaOfAction('PUT /api/v1/agent/settings'), 'agent');
	for (const action of [
		'GET /api/v1/system/timezone',
		'POST /api/v1/files/delete',
		'POST /api/v1/docker/containers/{id}/{action}',
		'POST /api/v1/agent/chats',
		'POST /api/v1/backups/{id}/{action}',
		'POST /api/v1/backups/{id}/check',
		'POST /api/v1/network/confirm',
		'POST /api/v1/ups/scan',
		'login',
		'login_failed',
	])
		assert.equal(settingsAreaOfAction(action), null, action);
	const entries = [
		{ at: '2026-11-01T10:00:00Z', user: 'a', via: 'web', action: 'PUT /api/v1/ups', ok: true },
		{
			at: '2026-11-03T10:00:00Z',
			user: 'a',
			via: 'web',
			action: 'PUT /api/v1/firewall',
			ok: false,
		},
		{
			at: '2026-11-02T10:00:00Z',
			user: 'b',
			via: 'web',
			action: 'POST /api/v1/files/delete',
			ok: true,
		},
		{
			at: '2026-11-04T10:00:00Z',
			user: 'b',
			via: 'web',
			action: 'PUT /api/v1/system/timezone',
			ok: true,
		},
	];
	assert.deepEqual(
		recentSettingsChanges(entries).map((e) => e.area),
		['timezone', 'ups'],
	);
	assert.equal(recentSettingsChanges(entries, 1).length, 1);
});
