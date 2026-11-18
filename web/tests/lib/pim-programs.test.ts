// 1.11 (step 11, TECHNIK 7.5): T-Kalender and Kontakte as programs – shown
// when /services/state says so, their pages, the event as a task window,
// the frames' states, and an old offline snapshot without the keys.
import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import { ApiError } from '#lib/api.ts';
import type { IconName } from '#lib/components/Icon.svelte';
import { appAllowed, appForPath, pageDrawsSide, SYSTEM_APPS, taskPage } from '#lib/desktop/apps.ts';
import { clearOffline, noteForOffline, offlineSnapshot } from '#lib/offline.svelte.ts';
import { calendarShell, sanitizeBooks, shellOfError } from '#lib/pim/shell.ts';

const calendar = SYSTEM_APPS.find((a) => a.id === 'calendar')!;
const contacts = SYSTEM_APPS.find((a) => a.id === 'contacts')!;

test('both programs show only when the NAS says so, for every role', () => {
	for (const app of [calendar, contacts]) {
		expect(app.optIn).toBe(true);
		expect(app.roles).toBeUndefined();
		for (const role of ['owner', 'admin', 'user'] as const) {
			expect(appAllowed(app, role, {})).toBe(false);
			expect(appAllowed(app, role, { [app.id]: false })).toBe(false);
			expect(appAllowed(app, role, { [app.id]: true })).toBe(true);
		}
	}
	expect(calendar.glyph).toBe('calendar');
	expect(calendar.gradient).toBe('violet');
	expect(contacts.glyph).toBe('contacts');
	expect(contacts.gradient).toBe('green');
	// Next to T-Mail in the menu's order.
	const ids = SYSTEM_APPS.map((a) => a.id);
	expect(ids.indexOf('calendar')).toBe(ids.indexOf('mail') + 1);
	expect(ids.indexOf('contacts')).toBe(ids.indexOf('mail') + 2);
});

test('the pages belong to their programs; the iPhone page to both', () => {
	expect(appForPath('/calendar')?.id).toBe('calendar');
	expect(appForPath('/calendar/iphone')?.id).toBe('calendar');
	expect(appForPath('/calendar/event')?.id).toBe('calendar');
	expect(appForPath('/contacts')?.id).toBe('contacts');
	expect(appForPath('/contacts/iphone')?.id).toBe('contacts');
	// No longer a page of "Mein Konto" (until step 11 it was).
	expect(appForPath('/account')?.id).toBe('account');
	expect(SYSTEM_APPS.find((a) => a.id === 'account')!.paths).toEqual(['/account']);
	// The side bars of the drafts.
	const hrefs = (app: typeof calendar) => app.side.map((i) => ('href' in i ? i.href : ''));
	expect(hrefs(calendar)).toEqual(['/calendar', '/calendar?view=list', '/calendar/iphone']);
	expect(hrefs(contacts)).toEqual(['/contacts', '/contacts?view=suggestions', '/contacts/iphone']);
	// The views draw their own column; the iPhone page has the window's.
	expect(pageDrawsSide(calendar.side, '/calendar')).toBe(true);
	expect(pageDrawsSide(calendar.side, '/calendar/iphone')).toBe(false);
	expect(pageDrawsSide(contacts.side, '/contacts')).toBe(true);
	expect(pageDrawsSide(contacts.side, '/contacts/iphone')).toBe(false);
});

test('a new event is a window of its own, like writing an e-mail', () => {
	expect(taskPage('/calendar/event')).toBe(true);
	expect(taskPage('/calendar/event?date=2026-11-18')).toBe(true);
	expect(taskPage('/calendar')).toBe(false);
	expect(taskPage('/calendar/events')).toBe(false);
	expect(taskPage('/mail/compose')).toBe(true);
});

test('the line icons of the drafts are there', () => {
	// npm run check fails if one of these names is missing in Icon.svelte.
	const names: IconName[] = [
		'calendar',
		'person',
		'phone',
		'place',
		'cake',
		'repeat',
		'note',
		'device',
		'book',
	];
	expect(names.length).toBe(9);
});

test('the frame: no volume is "not available", never an empty list', () => {
	expect(calendarShell({ enabled: true, volume: true })).toEqual({ kind: 'ready' });
	expect(calendarShell({ enabled: true, volume: false })).toEqual({ kind: 'unavailable' });
	expect(calendarShell(null)).toEqual({ kind: 'unavailable' });
	expect(shellOfError(new ApiError(503, 'contacts_unavailable', 'x'))).toEqual({
		kind: 'unavailable',
	});
	expect(shellOfError(new ApiError(503, 'calendar_unavailable', 'x'))).toEqual({
		kind: 'unavailable',
	});
	expect(shellOfError(new ApiError(500, 'internal', 'x')).kind).toBe('error');
	// Another 503 (the stick) is not the volume: its own text.
	expect(shellOfError(new ApiError(503, 'config_unavailable', 'x')).kind).toBe('error');
	expect(shellOfError(new ApiError(404, 'not_found', 'x')).kind).toBe('error');
	expect(sanitizeBooks([{ id: 'kontakte', name: 'Kontakte', count: 3 }])).toEqual([
		{ id: 'kontakte', name: 'Kontakte', count: 3 },
	]);
	expect(sanitizeBooks({})).toBeNull();
	expect(sanitizeBooks([{ id: 'x', count: -2 }, { name: 'no id' }])).toEqual([
		{ id: 'x', name: '', count: 0 },
	]);
});

// TECHNIK 7.5, last point: the snapshot of /services/state kept for the view
// without the NAS. A snapshot from before 1.11 has no "calendar"/"contacts":
// the programs stay hidden until the next answer brings the keys. (The
// desktop itself starts from {} and fills services from the live answer.)
beforeEach(() => {
	clearOffline();
	localStorage.clear();
	vi.useFakeTimers();
});
afterEach(() => vi.useRealTimers());
test('an old offline snapshot without the keys hides the programs until the next answer', () => {
	noteForOffline('/session', {
		user: { name: 'bea', role: 'user' },
		hostname: 'titan',
		lockMinutes: 0,
	});
	// What 1.10 answered.
	noteForOffline('/services/state', {
		docker: true,
		vms: true,
		lxc: true,
		mail: false,
		agent: false,
	});
	const old = offlineSnapshot()!.services!;
	expect(appAllowed(calendar, 'user', old)).toBe(false);
	expect(appAllowed(contacts, 'user', old)).toBe(false);
	// The next answer of a 1.11 NAS (a part is written at most every 20 s).
	vi.advanceTimersByTime(21_000);
	noteForOffline('/services/state', {
		docker: true,
		vms: true,
		lxc: true,
		mail: false,
		agent: false,
		calendar: true,
		contacts: true,
	});
	const now = offlineSnapshot()!.services!;
	expect(now).toMatchObject({ calendar: true, contacts: true });
	expect(appAllowed(calendar, 'user', now)).toBe(true);
	expect(appAllowed(contacts, 'user', now)).toBe(true);
});
