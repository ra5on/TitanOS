// PLAN 67-1: the menu bar of the programs – every program has Fenster and
// Hilfe, ids are unique, roles are respected, and the frame protocol only
// takes well-formed commands the program knows.
import { expect, test } from 'vitest';
import { SYSTEM_APPS } from '#lib/desktop/apps.ts';
import {
	commandIds,
	desktopMenus,
	entries,
	goTarget,
	helpUrl,
	logoMenu,
	menusFor,
	programOf,
	PROGRAM_KEYS,
	type Menu,
} from '#lib/desktop/menubar/menus.ts';
import {
	MAX_COMMANDS,
	readCommandStates,
	readMenuRequest,
	validCommandId,
} from '#lib/desktop/menubar/protocol.ts';
import de from '#lib/i18n/features/menubar.de.json';
import en from '#lib/i18n/features/menubar.en.json';

const WINDOWS = [
	...SYSTEM_APPS.map((a) => 'app:' + a.id),
	'docker:immich',
	'webapp:wordcraft',
	'vm:win11',
];
const ROLES = ['owner', 'admin', 'user'] as const;

test('every program has its own menus, then Fenster and Hilfe', () => {
	for (const key of WINDOWS) {
		const menus = menusFor(key, 'owner');
		const ids = menus.map((m) => m.id);
		expect(ids.slice(-2), key).toEqual(['window', 'help']);
		expect(menus.length, key).toBeGreaterThan(2);
	}
	// Each system program has a menu description of its own.
	for (const app of SYSTEM_APPS) expect(PROGRAM_KEYS, app.id).toContain(app.id);
	// An unknown window still gets Fenster and Hilfe.
	expect(menusFor('app:nothing', 'user').map((m) => m.id)).toEqual(['window', 'help']);
});

test('ids are unique within a program and its submenus', () => {
	const check = (menus: Menu[], what: string) => {
		const ids = entries(menus).map((e) => e.id);
		expect(new Set(ids).size, what).toBe(ids.length);
		expect(new Set(menus.map((m) => m.id)).size, what).toBe(menus.length);
	};
	for (const key of WINDOWS) check(menusFor(key, 'owner'), key);
	check(desktopMenus('owner'), 'desktop');
	check([logoMenu('owner')], 'logo');
});

test('roles: users see no administration', () => {
	const logo = entries([logoMenu('user')]).map((e) => e.id);
	expect(logo).not.toContain('sys.restart');
	expect(logo).not.toContain('sys.shutdown');
	expect(logo).not.toContain('sys.appcenter');
	expect(logo).toContain('sys.lock');
	expect(logo).toContain('sys.logout');
	expect(entries([logoMenu('owner')]).map((e) => e.id)).toContain('sys.shutdown');
	const help = entries(menusFor('app:files', 'user')).map((e) => e.id);
	expect(help).not.toContain('help.diagnose');
	expect(entries(menusFor('app:files', 'admin')).map((e) => e.id)).toContain('help.diagnose');
	// No separator first, last or twice after leaving entries out.
	for (const role of ROLES)
		for (const key of WINDOWS)
			for (const m of menusFor(key, role)) {
				expect('sep' in m.items[0], key).toBe(false);
				expect('sep' in m.items[m.items.length - 1], key).toBe(false);
				m.items.forEach((item, i) => {
					if ('sep' in item) expect('sep' in m.items[i + 1], key).toBe(false);
				});
			}
});

test('every entry does something or opens a submenu; texts exist in both languages', () => {
	const keys = new Set([...Object.keys(de), ...Object.keys(en)]);
	expect(Object.keys(de).sort()).toEqual(Object.keys(en).sort());
	const all = [
		...WINDOWS.flatMap((k) => menusFor(k, 'owner')),
		...desktopMenus('owner'),
		logoMenu('owner'),
	];
	for (const e of entries(all)) {
		expect(!!e.do !== !!e.sub, e.id).toBe(true);
		if (e.label.startsWith('menubar.')) expect(keys.has(e.label), e.label).toBe(true);
		if (e.do && 'cmd' in e.do) expect(validCommandId(e.do.cmd), e.do.cmd).toBe(true);
		if (e.do && 'go' in e.do) expect(e.do.go.startsWith('/'), e.id).toBe(true);
	}
	for (const m of all) if (m.label.startsWith('menubar.')) expect(keys.has(m.label)).toBe(true);
});

test('programs of windows and their help', () => {
	expect(programOf('app:files')).toBe('files');
	expect(programOf('docker:immich')).toBe('remote');
	expect(programOf('webapp:wordcraft')).toBe('webapp');
	expect(programOf('vm:x')).toBe('vmscreen');
	expect(helpUrl('docker:immich')).toMatch(/FERNANSICHT\.md$/);
	expect(helpUrl('')).toMatch(/DESKTOP\.md$/);
	expect(commandIds('app:files').has('files.mkdir')).toBe(true);
	expect(commandIds('app:files').has('docker.startAll')).toBe(false);
});

test('go: own pages in the window, other programs open, forbidden ones not at all', () => {
	const services = {};
	expect(goTarget('/docker/networks', 'app:docker', 'owner', services)).toEqual({
		kind: 'here',
		app: 'app:docker',
		href: '/docker/networks',
	});
	// "App installieren" in Docker opens the App-Center, not the catalogue inside Docker.
	expect(goTarget('/apps', 'app:docker', 'owner', services)?.kind).toBe('program');
	expect(goTarget('/apps', 'app:docker', 'owner', services)?.app).toBe('app:appcenter');
	expect(goTarget('/docker/apps/{app}', 'docker:my app', 'owner', services)?.href).toBe(
		'/docker/apps/my%20app',
	);
	// The Systemsteuerung lists Zugang in its own side bar: it stays in its window.
	expect(goTarget('/settings/access', 'app:settings', 'owner', services)?.kind).toBe('here');
	expect(goTarget('/settings/access', 'app:files', 'owner', services)?.app).toBe('app:users');
	expect(goTarget('/docker', 'app:files', 'user', services)).toBeNull();
	expect(goTarget('/docker', 'app:files', 'owner', { docker: false })).toBeNull();
});

test('protocol: only known, well-formed commands count', () => {
	const known = commandIds('app:files');
	expect(readCommandStates({ type: 'other', commands: [] }, known)).toBeNull();
	expect(readCommandStates({ type: 'titanos-menu', commands: 'x' }, known)).toBeNull();
	expect(
		readCommandStates(
			{
				type: 'titanos-menu',
				commands: [
					{ id: 'files.mkdir', enabled: true },
					{ id: 'files.hidden', enabled: true, checked: false },
					{ id: 'docker.startAll', enabled: true },
					{ id: 'files.mkdir"><img', enabled: true },
					{ id: 'files.rename', enabled: 'yes' },
					{ id: 'files.upload', enabled: false, checked: 'no' },
					null,
					42,
				],
			},
			known,
		),
	).toEqual({
		'files.mkdir': { enabled: true },
		'files.hidden': { enabled: true, checked: false },
		'files.upload': { enabled: false },
	});
	const many = Array.from({ length: MAX_COMMANDS + 1 }, () => ({
		id: 'files.mkdir',
		enabled: true,
	}));
	expect(readCommandStates({ type: 'titanos-menu', commands: many }, known)).toBeNull();
	expect(readMenuRequest({ type: 'titanos-menu-action', id: 'files.mkdir' })).toEqual({
		type: 'titanos-menu-action',
		id: 'files.mkdir',
	});
	expect(readMenuRequest({ type: 'titanos-menu-action', id: '../x' })).toBeNull();
	expect(readMenuRequest({ type: 'titanos-menu-get' })).toEqual({ type: 'titanos-menu-get' });
	expect(readMenuRequest('titanos-menu-get')).toBeNull();
});

test('every page command of the menus is registered by some page (no entry disabled forever)', async () => {
	const { readdirSync, readFileSync, statSync } = await import('node:fs');
	const { join } = await import('node:path');
	const files: string[] = [];
	const walk = (dir: string) => {
		for (const name of readdirSync(dir)) {
			const path = join(dir, name);
			if (statSync(path).isDirectory()) walk(path);
			else if (/\.(svelte|ts)$/.test(name) && !path.includes('menubar')) files.push(path);
		}
	};
	walk('src');
	const source = files
		.map((f) => readFileSync(f, 'utf8'))
		.filter((text) => /register(Menu|Window)Commands/.test(text))
		.join('\n');
	const missing: string[] = [];
	for (const key of WINDOWS)
		for (const e of entries(menusFor(key, 'owner'))) {
			if (!e.do || !('cmd' in e.do)) continue;
			const id = e.do.cmd;
			const prefix = id.slice(0, id.lastIndexOf('.') + 1);
			const found =
				source.includes(`'${id}'`) ||
				source.includes(`'${prefix}' +`) ||
				source.includes(`\`${prefix}\${`);
			if (!found) missing.push(id);
		}
	expect([...new Set(missing)]).toEqual([]);
});
