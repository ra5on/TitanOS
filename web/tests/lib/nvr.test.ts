// PLAN 79-7: the NVR's pure parts – the live view's arrangement, the time
// line's arithmetic, the friendly groups of objects and the bell group.
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { expect, test } from 'vitest';
import de from '../../src/lib/i18n/de.json';
import en from '../../src/lib/i18n/en.json';
import { appForPath, sideFor, SYSTEM_APPS } from '../../src/lib/desktop/apps.ts';
import { sanitizeDesktop } from '../../src/lib/desktop/prefs.ts';
import {
	defaultNvrPrefs,
	dragTile,
	gridTiles,
	layoutRows,
	LIVE_COLS,
	phoneOrderFor,
	sanitizeNvrPrefs,
	settle,
	tilesFor,
} from '../../src/lib/nvr/layout.ts';
import { supportedCodecs } from '../../src/lib/nvr/mse.ts';
import { nvrSideFor, rightsOf } from '../../src/lib/nvr/side.svelte.ts';
import { talkCloseProblem } from '../../src/lib/nvr/talk.ts';
import { faceNameProblem, groupsOf, plateOk, toggleGroup } from '../../src/lib/nvr/texts.ts';
import {
	dayOf,
	dayStart,
	gaps,
	mediaToWall,
	nextRecorded,
	playWindow,
	runs,
	wallToMedia,
} from '../../src/lib/nvr/timeline.ts';
import { bellGroup, sanitizeBell } from '../../src/lib/notify-groups.ts';

test('the standard arrangement is an even grid', () => {
	expect(gridTiles(['a'])).toEqual([{ id: 'a', x: 0, y: 0, w: 24, h: 14 }]);
	const four = gridTiles(['a', 'b', 'c', 'd']);
	expect(four.map((t) => [t.x, t.y])).toEqual([
		[0, 0],
		[12, 0],
		[0, 7],
		[12, 7],
	]);
	expect(layoutRows(four)).toBe(14);
	expect(gridTiles(['a', 'b', 'c', 'd', 'e'])[4]).toMatchObject({ x: 8, y: 5, w: 8 });
});

test('an own arrangement keeps its tiles, drops gone cameras and places new ones freely', () => {
	const prefs = {
		...defaultNvrPrefs(),
		custom: true,
		tiles: [
			{ id: 'a', x: 0, y: 0, w: 16, h: 9 },
			{ id: 'gone', x: 16, y: 0, w: 8, h: 5 },
		],
	};
	const tiles = tilesFor(prefs, ['a', 'b']);
	expect(tiles[0]).toEqual({ id: 'a', x: 0, y: 0, w: 16, h: 9 });
	expect(tiles[1]).toEqual({ id: 'b', x: 16, y: 0, w: 8, h: 5 });
	expect(tilesFor(defaultNvrPrefs(), ['a', 'b'])).toEqual(gridTiles(['a', 'b']));
});

test('dragging stays inside the grid and above the smallest size', () => {
	const tile = { id: 'a', x: 2, y: 1, w: 8, h: 5 };
	expect(dragTile(tile, 'move', -9, -9)).toMatchObject({ x: 0, y: 0 });
	expect(dragTile(tile, 'move', 99, 2)).toMatchObject({ x: LIVE_COLS - 8, y: 3 });
	expect(dragTile(tile, 'size', -99, -99)).toMatchObject({ x: 2, w: 4, h: 3 });
	expect(dragTile(tile, 'size', 99, 1)).toMatchObject({ w: LIVE_COLS - 2, h: 6 });
});

test('stored settings are checked: broken ones fall back, the desktop keeps them', () => {
	expect(sanitizeNvrPrefs(null)).toEqual(defaultNvrPrefs());
	const clean = sanitizeNvrPrefs({
		custom: true,
		events: true,
		names: false,
		tiles: [
			{ id: 'haustuer', x: 30, y: -4, w: 2, h: 999 },
			{ id: 'haustuer', x: 0, y: 0, w: 8, h: 5 },
			{ id: 'Bad Id', x: 0, y: 0, w: 8, h: 5 },
		],
		phoneOrder: ['b', 'b', 'x y', 'a'],
	});
	expect(clean.tiles).toEqual([{ id: 'haustuer', x: 20, y: 0, w: 4, h: 60 }]);
	expect(clean.phoneOrder).toEqual(['b', 'a']);
	expect(clean).toMatchObject({ custom: true, events: true, names: false, marks: true });
	expect(sanitizeDesktop({ nvr: { events: true } }).nvr.events).toBe(true);
	expect(sanitizeDesktop({}).nvr).toEqual(defaultNvrPrefs());
	expect(phoneOrderFor(clean, ['a', 'b', 'c'])).toEqual(['b', 'a', 'c']);
});

test('the camera widget keeps its camera', () => {
	const prefs = sanitizeDesktop({
		widgets: [
			{ id: 'w1', kind: 'camera', x: 0, y: 0, w: 14, h: 10, camera: 'garten' },
			{ id: 'w2', kind: 'camera', x: 0, y: 0, w: 14, h: 10, camera: '../x' },
		],
	});
	expect(prefs.widgets.map((w) => w.camera)).toEqual(['garten', undefined]);
});

const seg = (start: number, end: number, motion = 0, objects = 0) => ({
	start,
	end,
	motion,
	objects,
});

test('the time line joins segments of one kind and finds the gaps', () => {
	const segments = [seg(0, 10), seg(10, 20), seg(20, 30, 5), seg(30, 40, 5, 1), seg(100, 110)];
	expect(runs(segments)).toEqual([
		{ start: 0, end: 20, kind: 'plain' },
		{ start: 20, end: 30, kind: 'motion' },
		{ start: 30, end: 40, kind: 'objects' },
		{ start: 100, end: 110, kind: 'plain' },
	]);
	expect(gaps(segments, 0, 200).map((g) => [g.start, g.end])).toEqual([
		[40, 100],
		[110, 200],
	]);
});

test('positions in the video and clock times translate across gaps', () => {
	const segments = [seg(100, 110), seg(200, 230)];
	expect(mediaToWall(segments, 0, 3600, 5)).toBe(105);
	expect(mediaToWall(segments, 0, 3600, 15)).toBe(205);
	expect(wallToMedia(segments, 0, 3600, 105)).toBe(5);
	expect(wallToMedia(segments, 0, 3600, 150)).toBe(10);
	expect(wallToMedia(segments, 0, 3600, 210)).toBe(20);
	expect(nextRecorded(segments, 150)).toBe(200);
	expect(nextRecorded(segments, 205)).toBe(205);
	expect(nextRecorded(segments, 300)).toBe(null);
	expect(playWindow(7300, 0, 100000)).toEqual({ from: 7200, to: 10800 });
	expect(playWindow(7300, 0, 8000)).toEqual({ from: 7200, to: 8000 });
	expect(dayOf(dayStart('2026-11-12') + 3600)).toBe('2026-11-12');
});

test('friendly groups switch whole sets of objects in the order of the NAS', () => {
	const allowed = ['person', 'bicycle', 'car', 'motorcycle', 'bus', 'bird', 'cat', 'dog'];
	expect(groupsOf(['person', 'dog'])).toEqual(['persons', 'animals']);
	expect(toggleGroup(['person'], 'vehicles', true, allowed)).toEqual([
		'person',
		'bicycle',
		'car',
		'motorcycle',
		'bus',
	]);
	expect(toggleGroup(['person', 'car', 'dog'], 'vehicles', false, allowed)).toEqual([
		'person',
		'dog',
	]);
});

test('the browser is asked only for codecs it can play', () => {
	expect(supportedCodecs((type) => type.includes('avc1'))).toBe(
		'avc1.640029,avc1.64002A,avc1.640033',
	);
});

test('the NVR is a program; only administrators get its cameras, settings and diagnosis', () => {
	const nvr = SYSTEM_APPS.find((a) => a.id === 'nvr')!;
	expect(appForPath('/nvr/recordings')?.id).toBe('nvr');
	const hrefs = (role: 'owner' | 'user') =>
		sideFor(nvr.side, role).map((i) => ('href' in i ? i.href : ''));
	expect(hrefs('user')).toEqual(['/nvr', '/nvr/recordings', '/nvr/events']);
	expect(hrefs('owner')).toHaveLength(6);
});

test("the NVR's notifications have a bell group of their own", () => {
	expect(bellGroup('nvr_camera_offline')).toBe('cameras');
	expect(bellGroup('nvr_failover')).toBe('cameras');
	expect(sanitizeBell({ groups: { cameras: false } }).groups.cameras).toBe(false);
	expect(sanitizeBell({}).groups.cameras).toBe(true);
});

test('names of known faces follow the NAS: letters and digits, - and _, no spaces, not train', () => {
	for (const ok of ['Anna', 'Jürgen_2', 'Øystein-B', '2nd', 'a'.repeat(40), 'Ägidius'])
		expect(faceNameProblem(ok), ok).toBe('');
	expect(faceNameProblem('')).toBe('empty');
	expect(faceNameProblem('   ')).toBe('empty');
	expect(faceNameProblem('Oma Erna')).toBe('space');
	expect(faceNameProblem('a'.repeat(41))).toBe('long');
	expect(faceNameProblem('train')).toBe('train');
	expect(faceNameProblem('_anna')).toBe('start');
	expect(faceNameProblem('-anna')).toBe('start');
	expect(faceNameProblem('anna.b')).toBe('chars');
	expect(faceNameProblem('anna/b')).toBe('chars');
});

test('talking says why the NAS closed the connection', () => {
	expect(talkCloseProblem('nvr_camera_not_found')).toBe('camera_gone');
	expect(talkCloseProblem('nvr_not_running')).toBe('not_running');
	expect(talkCloseProblem('nvr_no_picture')).toBe('no_picture');
	expect(talkCloseProblem('nvr_unavailable')).toBe('unavailable');
	expect(talkCloseProblem('logout')).toBe('logout');
	expect(talkCloseProblem('forbidden')).toBe('forbidden');
	expect(talkCloseProblem('')).toBe('failed');
	expect(talkCloseProblem('something else')).toBe('failed');
});

test("every NVR notification of titand has the UI's own title and text in both languages", () => {
	const go = readFileSync(resolve(process.cwd(), '../backend/internal/notify/texts.go'), 'utf8');
	const kinds = [...new Set([...go.matchAll(/"(nvr_[a-z_]+)":\s*\{/g)].map((m) => m[1]))];
	expect(kinds).toContain('nvr_event_face_known');
	// PLAN 88: unknown faces have their own message (from Frigate's tries
	// of the face recognition, routing.go).
	expect(kinds).toContain('nvr_event_face_unknown');
	for (const [lang, all] of [
		['de', de],
		['en', en],
	] as const) {
		const texts = all as Record<string, string>;
		for (const kind of kinds)
			for (const part of ['title', 'text']) {
				const key = `notify.kind.${kind}.${part}`;
				expect(texts[key], `${lang} ${key}`).toBeTruthy();
				expect(texts[key], `${lang} ${key}`).not.toContain('{s}');
			}
		expect(texts['ha.group.cameras'], `${lang} ha.group.cameras`).toBeTruthy();
		expect(texts['bell.group.cameras'], `${lang} bell.group.cameras`).toBeTruthy();
	}
});

test('an account without the admin role sees the pages of the NVR it has a right for', () => {
	const nvrApp = SYSTEM_APPS.find((a) => a.id === 'nvr')!;
	const side = sideFor(nvrApp.side, 'user');
	const hrefs = (rights: { live: boolean; recordings: boolean } | null) =>
		nvrSideFor(side, rights).map((i) => ('href' in i ? i.href : ''));
	expect(hrefs(null)).toEqual(['/nvr', '/nvr/recordings', '/nvr/events']);
	expect(hrefs({ live: true, recordings: false })).toEqual(['/nvr']);
	expect(hrefs({ live: false, recordings: true })).toEqual(['/nvr/recordings', '/nvr/events']);
	expect(hrefs({ live: true, recordings: true })).toEqual([
		'/nvr',
		'/nvr/recordings',
		'/nvr/events',
	]);
	// Nothing shared: "Live" stays and says so.
	expect(hrefs({ live: false, recordings: false })).toEqual(['/nvr']);
	expect(
		rightsOf([
			{ rights: { live: true, recordings: false } },
			{ rights: { live: false, recordings: true } },
		]),
	).toEqual({ live: true, recordings: true });
	expect(rightsOf([])).toEqual({ live: false, recordings: false });
});

test('known plates are what the NAS takes: letters, digits, spaces, - and * only', () => {
	for (const ok of ['M-AB 1234', 'MAB12*', 'K-TI 2026', 'ÖHR 12', '*', 'a'.repeat(64)])
		expect(plateOk(ok), ok).toBe(true);
	for (const bad of ['', 'M/AB 12', 'MAB.12', 'M(AB)', '[A-Z]+', 'a'.repeat(65)])
		expect(plateOk(bad), bad).toBe(false);
});

test('a moved tile stays where it was put; tiles it covers move down', () => {
	const tiles = gridTiles(['a', 'b', 'c', 'd']);
	// b one row down now covers d: d moves below it, a and c stay.
	const moved = tiles.map((t) => (t.id === 'b' ? { ...t, y: t.y + 1 } : t));
	const done = settle(moved, 'b');
	const by = Object.fromEntries(done.map((t) => [t.id, t]));
	expect(by.b).toEqual({ id: 'b', x: 12, y: 1, w: 12, h: 7 });
	expect(by.d.y).toBe(8);
	expect(by.a).toEqual(tiles[0]);
	expect(by.c).toEqual(tiles[2]);
	for (const x of done)
		for (const y of done)
			if (x !== y)
				expect(
					x.x < y.x + y.w && y.x < x.x + x.w && x.y < y.y + y.h && y.y < x.y + x.h,
					`${x.id}/${y.id}`,
				).toBe(false);
});
