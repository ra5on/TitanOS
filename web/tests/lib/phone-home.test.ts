// PLAN 87: the phone's home screen – pages, folders, widgets and dock.
import { expect, test } from 'vitest';
import {
	addWidget,
	combine,
	defaultPhone,
	dissolveFolder,
	dropEmptyPages,
	moveTo,
	moveToDock,
	outOfFolder,
	ownLayout,
	phoneLayout,
	removeWidget,
	renameFolder,
	resizeWidget,
	sanitizePhone,
} from '#lib/desktop/phone-home.ts';
import { defaultDesktop, sanitizeDesktop } from '#lib/desktop/prefs.ts';

const programs = [
	'app:files',
	'app:shares',
	'app:storage',
	'app:docker',
	'app:vms',
	'app:monitor',
	'app:settings',
	'docker:immich',
];

test('standard: status widget, programs not in the dock, pages by cells', () => {
	const shown = phoneLayout(defaultPhone(), programs, 12);
	expect(shown.dock).toEqual(['app:files', 'app:docker', 'app:monitor', 'app:settings']);
	// The status widget takes 8 cells, so 4 programs fit on the first page.
	expect(shown.pages).toEqual([
		['widget:p-status', 'app:shares', 'app:storage', 'app:vms', 'docker:immich'],
	]);
	expect(phoneLayout(defaultPhone(), programs, 10).pages).toEqual([
		['widget:p-status', 'app:shares', 'app:storage'],
		['app:vms', 'docker:immich'],
	]);
});

test('dock off: its programs come at the end of the pages', () => {
	const p = { ...defaultPhone(), dock: { show: false, apps: [], custom: false } };
	const shown = phoneLayout(p, programs, 40);
	expect(shown.dock).toEqual([]);
	expect(shown.pages.flat()).toEqual([
		'widget:p-status',
		'app:files',
		'app:shares',
		'app:storage',
		'app:docker',
		'app:vms',
		'app:monitor',
		'app:settings',
		'docker:immich',
	]);
});

test('own arrangement: new programs at the end, gone ones drop out, each once', () => {
	const p = ownLayout(defaultPhone(), phoneLayout(defaultPhone(), programs, 40));
	const moved = moveTo(p, 'docker:immich', 0, 1);
	expect(phoneLayout(moved, programs, 40).pages[0].slice(0, 3)).toEqual([
		'widget:p-status',
		'docker:immich',
		'app:shares',
	]);
	const later = phoneLayout(
		moved,
		[...programs.filter((k) => k !== 'app:vms'), 'docker:jellyfin', 'app:shares'],
		40,
	);
	expect(later.pages.flat()).toEqual([
		'widget:p-status',
		'docker:immich',
		'app:shares',
		'app:storage',
		'docker:jellyfin',
	]);
});

test('moving to another page makes the page; empty pages drop on leaving', () => {
	const p = ownLayout(defaultPhone(), phoneLayout(defaultPhone(), programs, 40));
	const moved = moveTo(p, 'app:vms', 2, 0);
	expect(moved.pages.length).toBe(3);
	expect(moved.pages[2]).toEqual(['app:vms']);
	expect(dropEmptyPages(moved).pages.length).toBe(2);
	expect(phoneLayout(moved, programs, 40).pages.length).toBe(2);
});

test('a program on another makes a folder; on a folder it goes in; the last one dissolves it', () => {
	const p = ownLayout(defaultPhone(), phoneLayout(defaultPhone(), programs, 40));
	const one = combine(p, 'app:vms', 'app:storage', 'Ordner', 'f1');
	expect(one.folders).toEqual([{ id: 'f1', name: 'Ordner', apps: ['app:storage', 'app:vms'] }]);
	expect(one.pages[0]).toEqual(['widget:p-status', 'app:shares', 'folder:f1', 'docker:immich']);
	const two = combine(one, 'docker:immich', 'folder:f1', 'x', 'f2');
	expect(two.folders[0].apps).toEqual(['app:storage', 'app:vms', 'docker:immich']);
	expect(two.folders.length).toBe(1);
	// Not into a widget, no folder into a folder.
	expect(combine(two, 'app:shares', 'widget:p-status', 'x', 'f3')).toBe(two);
	expect(combine(two, 'folder:f1', 'app:shares', 'x', 'f3')).toBe(two);
	const renamed = renameFolder(two, 'f1', '  Werkzeuge ');
	expect(renamed.folders[0].name).toBe('Werkzeuge');
	expect(renameFolder(two, 'f1', '   ')).toBe(two);
	const out = outOfFolder(renamed, 'app:vms', 'f1');
	expect(out.pages[0]).toEqual(['widget:p-status', 'app:shares', 'folder:f1', 'app:vms']);
	const last = outOfFolder(outOfFolder(out, 'docker:immich', 'f1'), 'app:storage', 'f1');
	expect(last.folders).toEqual([]);
	expect(last.pages[0]).toContain('app:storage');
	const gone = dissolveFolder(renamed, 'f1');
	expect(gone.pages[0]).toEqual([
		'widget:p-status',
		'app:shares',
		'app:storage',
		'app:vms',
		'docker:immich',
	]);
});

test('dock: holds four, takes no folders or widgets', () => {
	const p = ownLayout(defaultPhone(), phoneLayout(defaultPhone(), programs, 40));
	expect(moveToDock(p, 'app:vms', 0)).toBe(p);
	const freed = moveTo(p, 'app:files', 0, 1);
	const docked = moveToDock(freed, 'app:vms', 0);
	expect(docked.dock.apps).toEqual(['app:vms', 'app:docker', 'app:monitor', 'app:settings']);
	expect(docked.pages.flat()).not.toContain('app:vms');
	expect(moveToDock(docked, 'widget:p-status', 0)).toBe(docked);
});

test('widgets: added at the start of the page, resized, removed', () => {
	const p = ownLayout(defaultPhone(), phoneLayout(defaultPhone(), programs, 40));
	const added = addWidget(p, 'cpuram', 0, 'p-cpu');
	expect(added.pages[0][0]).toBe('widget:p-cpu');
	expect(resizeWidget(added, 'p-cpu').widgets.find((w) => w.id === 'p-cpu')?.size).toBe('s');
	const removed = removeWidget(added, 'p-status');
	expect(removed.pages[0]).not.toContain('widget:p-status');
	expect(removed.widgets.map((w) => w.id)).toEqual(['p-cpu']);
});

test('a page holding too much shows the rest on the next page', () => {
	const p = ownLayout(defaultPhone(), phoneLayout(defaultPhone(), programs, 40));
	const full = addWidget(addWidget(p, 'cpuram', 0, 'a'), 'clock', 0, 'b');
	// 8 + 4 + 8 cells of widgets and 4 programs in 16 cells.
	const shown = phoneLayout(full, programs, 16);
	expect(shown.pages[0]).toEqual(['widget:b', 'widget:a']);
	expect(shown.pages[1]).toEqual([
		'widget:p-status',
		'app:shares',
		'app:storage',
		'app:vms',
		'docker:immich',
	]);
	expect(shown.pages.flat().length).toBe(7);
});

test('sanitize: broken input falls back, the desktop keeps the field', () => {
	expect(sanitizePhone(null)).toEqual(defaultPhone());
	const p = sanitizePhone({
		custom: true,
		pages: [['app:files', 'bad key', 'app:files'], 'x'],
		folders: [{ id: 'f1', name: '\u0007 Name ', apps: ['app:vms', 7] }, { id: '' }],
		widgets: [
			{ id: 'w1', kind: 'nope' },
			{ id: 'w2', kind: 'camera', size: 's', camera: 'front_door' },
		],
		dock: { show: false, apps: ['a:1', 'a:2', 'a:3', 'a:4', 'a:5'] },
	});
	expect(p.pages).toEqual([['app:files'], []]);
	expect(p.folders).toEqual([{ id: 'f1', name: 'Name', apps: ['app:vms'] }]);
	expect(p.widgets).toEqual([{ id: 'w2', kind: 'camera', size: 's', camera: 'front_door' }]);
	expect(p.dock).toEqual({ show: false, apps: ['a:1', 'a:2', 'a:3', 'a:4'], custom: false });
	// The desktop's preference keeps the phone; one without it (0.80) gets the standard.
	expect(sanitizeDesktop({ phone: { custom: true, pages: [['app:files']] } }).phone.pages).toEqual([
		['app:files'],
	]);
	expect(sanitizeDesktop({}).phone).toEqual(defaultDesktop().phone);
});
