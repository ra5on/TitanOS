import { beforeEach, expect, test, vi } from 'vitest';

const calls = vi.hoisted(() => ({
	list: [] as [string, string, unknown][],
	handler: null as null | (() => void),
	prefs: {} as Record<string, unknown>,
	fail: null as null | Error,
}));

vi.mock('#lib/api.ts', () => {
	class ApiError extends Error {
		constructor(
			readonly status: number,
			readonly code: string,
			message = code,
			readonly retryAfter = 0,
		) {
			super(message);
		}
	}
	const record = (method: string) => async (path: string, body?: unknown) => {
		calls.list.push([method, path, body]);
		if (calls.fail) throw calls.fail;
		if (method === 'GET' && path === '/preferences') return calls.prefs;
		return undefined;
	};
	return {
		ApiError,
		api: { get: record('GET'), post: record('POST'), put: record('PUT') },
		setLockedHandler: (fn: () => void) => (calls.handler = fn),
	};
});
vi.mock('#lib/passkeys.ts', () => ({
	confirmWithPasskey: async () => ({ challengeId: 'c1', id: 'k1' }),
}));
vi.mock('#lib/session.svelte.ts', () => {
	// A plain object is enough: the tests read it right after changing it.
	const session: { data: Record<string, unknown> | null } = { data: null };
	return { session, refreshSession: vi.fn(async () => {}) };
});

const lockModule = await import('#lib/lock.svelte.ts');
const {
	lockState,
	isLocked,
	lock,
	unlock,
	unlockWithPasskey,
	readLockMinutes,
	idleExpired,
	isLockShortcut,
	saveLockMinutes,
	LOCK_DEFAULT_MINUTES,
} = lockModule;
const { session } = await import('#lib/session.svelte.ts');
const { ApiError } = await import('#lib/api.ts');

beforeEach(() => {
	calls.list = [];
	calls.fail = null;
	lockState.locked = false;
	session.data = { user: { name: 'alex', role: 'owner', twoFactor: false }, lockMinutes: 15 };
});

test('the minutes of the automatic lock come from the desktop preferences', () => {
	expect(readLockMinutes(null)).toBe(LOCK_DEFAULT_MINUTES);
	expect(readLockMinutes({ icons: [] })).toBe(LOCK_DEFAULT_MINUTES);
	expect(readLockMinutes({ lockMinutes: 0 })).toBe(0);
	expect(readLockMinutes({ lockMinutes: -5 })).toBe(0);
	expect(readLockMinutes({ lockMinutes: 7.4 })).toBe(7);
	expect(readLockMinutes({ lockMinutes: 99999 })).toBe(24 * 60);
	expect(readLockMinutes({ lockMinutes: '5' })).toBe(LOCK_DEFAULT_MINUTES);
});

test('idle time runs out only after the minutes, never with 0', () => {
	const t0 = 1_000_000;
	expect(idleExpired(t0, t0 + 14 * 60_000, 15)).toBe(false);
	expect(idleExpired(t0, t0 + 15 * 60_000, 15)).toBe(true);
	expect(idleExpired(t0, t0 + 999 * 60_000, 0)).toBe(false);
});

test('Strg/⌘+Alt+L is the shortcut, by key position', () => {
	const key = (over: Partial<KeyboardEvent>) => ({
		code: 'KeyL',
		altKey: true,
		ctrlKey: false,
		metaKey: false,
		shiftKey: false,
		...over,
	});
	expect(isLockShortcut(key({ ctrlKey: true }))).toBe(true);
	expect(isLockShortcut(key({ metaKey: true }))).toBe(true);
	expect(isLockShortcut(key({}))).toBe(false);
	expect(isLockShortcut(key({ ctrlKey: true, shiftKey: true }))).toBe(false);
	expect(isLockShortcut(key({ ctrlKey: true, code: 'KeyK' }))).toBe(false);
});

test('a 423 "locked" answer anywhere shows the lock screen', () => {
	expect(calls.handler).toBeTypeOf('function');
	expect(isLocked()).toBe(false);
	calls.handler!();
	expect(isLocked()).toBe(true);
});

test('what the NAS said at start counts as locked', () => {
	session.data = { ...session.data!, locked: true };
	expect(isLocked()).toBe(true);
	session.data = { ...session.data!, user: null };
	expect(isLocked()).toBe(false);
});

test('lock() asks the NAS once and shows the screen at once', async () => {
	await lock();
	expect(lockState.locked).toBe(true);
	await lock();
	expect(calls.list).toEqual([['POST', '/session/lock', undefined]]);
});

test('unlock() sends password and code and clears the lock', async () => {
	lockState.locked = true;
	await unlock({ password: 'secret', code: ' 123456 '.trim() });
	expect(calls.list[0]).toEqual([
		'POST',
		'/session/unlock',
		{ password: 'secret', code: '123456' },
	]);
	expect(lockState.locked).toBe(false);
	expect(lockState.busy).toBe(false);
});

test('a refused unlock keeps the lock and throws', async () => {
	lockState.locked = true;
	calls.fail = new ApiError(403, 'wrong_password');
	await expect(unlock({ password: 'nope' })).rejects.toMatchObject({ code: 'wrong_password' });
	expect(lockState.locked).toBe(true);
	expect(lockState.busy).toBe(false);
});

test('unlocking with a passkey sends its answer', async () => {
	lockState.locked = true;
	await unlockWithPasskey();
	expect(calls.list[0]).toEqual([
		'POST',
		'/session/unlock',
		{ passkey: { challengeId: 'c1', id: 'k1' } },
	]);
	expect(lockState.locked).toBe(false);
});

test('saving the minutes keeps the other fields of the desktop', async () => {
	calls.prefs = { desktop: { icons: ['files'], wallpaper: 'dunes' } };
	await saveLockMinutes(30);
	expect(calls.list.at(-1)).toEqual([
		'PUT',
		'/preferences/desktop',
		{ icons: ['files'], wallpaper: 'dunes', lockMinutes: 30 },
	]);
	expect(session.data?.lockMinutes).toBe(30);
	calls.prefs = { desktop: null };
	await saveLockMinutes(-1);
	expect(calls.list.at(-1)).toEqual(['PUT', '/preferences/desktop', { lockMinutes: 0 }]);
});
