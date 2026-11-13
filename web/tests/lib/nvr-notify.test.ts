// PLAN 82-3: the pure parts of whom a camera tells – who can be chosen,
// what a camera of 0.78 meant, the ways, days and the window.
import { beforeAll, expect, test } from 'vitest';
import { i18n, loadLocale } from '#lib/i18n/index.svelte.ts';
import {
	dayRuns,
	daysText,
	eligible,
	legacyRecipients,
	missesNames,
	pauseText,
	staleReason,
	toggleWay,
	waysFor,
	windowOn,
	windowText,
	type RecipientAccount,
} from '#lib/nvr/notify.ts';

beforeAll(async () => {
	await loadLocale('de');
	i18n.locale = 'de';
});

const account = (name: string, admin: boolean, cameras: string[] = []): RecipientAccount => ({
	name,
	admin,
	email: false,
	pushDevices: 0,
	pushDevicesOff: 0,
	cameras,
});
const accounts = [
	account('alex', true, ['haustuer', 'garten']),
	account('mara', true, ['haustuer', 'garten']),
	account('sam', false, ['haustuer']),
];

test('only accounts that may see the camera can be chosen; a new camera only administrators', () => {
	expect(accounts.filter((a) => eligible(a, 'haustuer')).map((a) => a.name)).toEqual([
		'alex',
		'mara',
		'sam',
	]);
	expect(accounts.filter((a) => eligible(a, 'garten')).map((a) => a.name)).toEqual([
		'alex',
		'mara',
	]);
	expect(accounts.filter((a) => eligible(a)).map((a) => a.name)).toEqual(['alex', 'mara']);
	expect(staleReason('sam', accounts, 'garten')).toBe('noRight');
	expect(staleReason('dora', accounts, 'garten')).toBe('gone');
	expect(staleReason('alex', accounts, 'garten')).toBe('');
	expect(waysFor(accounts[2])).toEqual(['push', 'email']);
	expect(waysFor(accounts[0])).toEqual(['bell', 'push', 'email']);
});

test('a camera of 0.78 means every administrator with its old bell and push, never the old e-mail', () => {
	expect(legacyRecipients({ titan: true, push: false }, accounts)).toEqual([
		{ user: 'alex', bell: true, push: false, email: false },
		{ user: 'mara', bell: true, push: false, email: false },
	]);
	expect(legacyRecipients({ titan: false, push: false }, accounts)).toEqual([]);
});

test('a way switches per account; without any way the account leaves the list', () => {
	let list = toggleWay([], 'sam', 'push', true);
	list = toggleWay(list, 'alex', 'bell', true);
	expect(list.map((r) => r.user)).toEqual(['alex', 'sam']);
	list = toggleWay(list, 'sam', 'email', true);
	expect(list[1]).toEqual({ user: 'sam', bell: false, push: true, email: true });
	list = toggleWay(list, 'sam', 'push', false);
	list = toggleWay(list, 'sam', 'email', false);
	expect(list.map((r) => r.user)).toEqual(['alex']);
	// A plain account chosen with the bell as an administrator loses it with
	// the next change; with nothing else ticked it leaves the list.
	const demoted = [{ user: 'bob', bell: true, push: true, email: false }];
	expect(toggleWay(demoted, 'bob', 'email', true, false)).toEqual([
		{ user: 'bob', bell: false, push: true, email: true },
	]);
	expect(toggleWay(demoted, 'bob', 'push', false, false)).toEqual([]);
});

test('days, pause and window as words', () => {
	expect(dayRuns([5, 1, 2, 3, 4, 7])).toEqual([
		[1, 5],
		[7, 7],
	]);
	expect(daysText([1, 2, 3, 4, 5])).toBe('Mo–Fr');
	expect(daysText([6, 7])).toBe('Sa, So');
	expect(daysText([1, 2, 3, 4, 5, 6, 7])).toBe('täglich');
	expect(pauseText(undefined)).toBe('1 Minute');
	expect(pauseText(0)).toBe('Keine');
	expect(windowText(undefined)).toBe('Meldet jederzeit');
	expect(windowText({ on: true, days: [1, 2, 3, 4, 5], from: '22:00', to: '06:00' })).toBe(
		'Nur Mo–Fr, 22:00–06:00',
	);
});

test('switching the window on gives every day and the night unless chosen', () => {
	expect(windowOn({ on: false, days: [], from: '', to: '' }, true)).toEqual({
		on: true,
		days: [1, 2, 3, 4, 5, 6, 7],
		from: '22:00',
		to: '06:00',
	});
	expect(windowOn({ on: false, days: [6], from: '08:00', to: '18:00' }, true)).toEqual({
		on: true,
		days: [6],
		from: '08:00',
		to: '18:00',
	});
	expect(windowOn({ on: true, days: [6], from: '08:00', to: '18:00' }, false).on).toBe(false);
});

test('plate and face messages need the right "Namen und Kennzeichen" of the camera', () => {
	const sam = { ...account('sam', false, ['haustuer', 'einfahrt']), names: ['einfahrt'] };
	expect(missesNames(sam, 'haustuer', ['person'])).toBe(false);
	expect(missesNames(sam, 'haustuer', ['person', 'face'])).toBe(true);
	expect(missesNames(sam, 'einfahrt', ['plate'])).toBe(false);
	expect(missesNames(accounts[0], 'haustuer', ['plate'])).toBe(false);
	expect(missesNames(sam, undefined, ['plate'])).toBe(true);
	// PLAN 88: unknown faces need the right too.
	expect(missesNames(sam, 'haustuer', ['person'], true)).toBe(true);
	expect(missesNames(sam, 'einfahrt', ['person'], true)).toBe(false);
	expect(missesNames(accounts[0], 'haustuer', ['person'], true)).toBe(false);
});
