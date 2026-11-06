// PLAN 61: the desktop's stored settings, its programs and their pages.
import { readFileSync, readdirSync, statSync } from 'node:fs';
import { join } from 'node:path';
import { expect, test } from 'vitest';
import { appAllowed, appForPath, sideActive, SYSTEM_APPS } from '#lib/desktop/apps.ts';
import { arrangeIcons, defaultDesktop, placeWidget, sanitizeDesktop } from '#lib/desktop/prefs.ts';
import de from '#lib/i18n/features/desktop.de.json';
import en from '#lib/i18n/features/desktop.en.json';

test('broken or foreign settings fall back to the standard', () => {
	expect(sanitizeDesktop(null)).toEqual(defaultDesktop());
	const v = sanitizeDesktop({
		icons: ['app:docker', '../x', 42, 'app:docker'],
		widgets: [
			{ id: 'a', kind: 'clock', x: 3, y: 2, w: 999, h: 1 },
			{ id: 'b', kind: 'evil', x: 0, y: 0, w: 5, h: 5 },
		],
		wallpaper: { source: 'builtin', id: 'nope' },
		lockMinutes: -5,
		layouts: [
			{
				id: 'l1',
				name: 'x',
				windows: [{ app: 'app:docker', href: '//evil.example', x: 0, y: 0, w: 1, h: 1 }],
			},
		],
	});
	expect(v.icons).toEqual(['app:docker']);
	expect(v.widgets).toEqual([{ id: 'a', kind: 'clock', x: 3, y: 2, w: 60, h: 3 }]);
	expect(v.wallpaper).toEqual({ source: 'builtin', id: 'wave', live: true });
	expect(v.lockMinutes).toBe(0);
	expect(v.layouts[0].windows).toEqual([]);
});

test('icons: the standard set, folders take their programs, own order wins', () => {
	const available = [
		{ key: 'app:files', label: 'Dateien', kind: 'system' },
		{ key: 'docker:immich', label: 'immich', kind: 'docker' },
		{ key: 'app:docker', label: 'Docker', kind: 'system' },
	];
	const prefs = defaultDesktop();
	expect(arrangeIcons(prefs, available)).toEqual(['app:files', 'app:docker', 'docker:immich']);
	prefs.folders = [{ id: 'f', name: 'Medien', apps: ['docker:immich'] }];
	expect(arrangeIcons(prefs, available)).toEqual(['app:files', 'app:docker', 'folder:f']);
	prefs.sort = 'free';
	prefs.customIcons = true;
	prefs.icons = ['docker:immich', 'app:gone', 'app:files'];
	expect(arrangeIcons(prefs, available)).toEqual(['docker:immich', 'app:files']);
});

test('widgets are placed where nothing else is, beside the icons', () => {
	const at = placeWidget([{ id: 'a', kind: 'clock', x: 10, y: 0, w: 10, h: 5 }], 10, 5, 40, 10);
	expect(at).toEqual({ x: 20, y: 0 });
});

test('every page of TitanOS belongs to a program', () => {
	const routes: string[] = [];
	const walk = (dir: string, base: string) => {
		for (const name of readdirSync(dir)) {
			const path = join(dir, name);
			if (statSync(path).isDirectory()) walk(path, `${base}/${name}`);
			else if (name === '+page.svelte') routes.push(base || '/');
		}
	};
	walk('src/routes', '');
	const unowned = routes
		.filter((r) => !r.includes('[') && !['/welcome', '/'].includes(r))
		.filter((r) => !appForPath(r));
	expect(unowned).toEqual([]);
	expect(appForPath('/docker/networks')?.id).toBe('docker');
	expect(appForPath('/settings/backups/data')?.id).toBe('backup');
	expect(appForPath('/settings/updates')?.id).toBe('settings');
});

test('side bar marks the most specific page', () => {
	const docker = SYSTEM_APPS.find((a) => a.id === 'docker')!;
	const active = docker.side.filter((i) => sideActive(i, '/docker/networks', docker.side));
	expect(active.map((i) => ('href' in i ? i.href : ''))).toEqual(['/docker/networks']);
});

test('roles and services decide which programs an account sees', () => {
	const docker = SYSTEM_APPS.find((a) => a.id === 'docker')!;
	const agent = SYSTEM_APPS.find((a) => a.id === 'agent')!;
	expect(appAllowed(docker, 'user', {})).toBe(false);
	expect(appAllowed(docker, 'admin', { docker: false })).toBe(false);
	expect(appAllowed(agent, 'user', {})).toBe(false);
	expect(appAllowed(agent, 'user', { agent: true })).toBe(true);
});

test('German and English texts have the same keys', () => {
	expect(Object.keys(en).sort()).toEqual(Object.keys(de).sort());
	void readFileSync;
});
