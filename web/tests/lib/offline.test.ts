import { beforeEach, describe, expect, it } from 'vitest';
import {
	clearOffline,
	noteForOffline,
	offlineSnapshot,
	snapshotLocked,
} from '#lib/offline.svelte.ts';

// 1.07: the snapshot for the view without the NAS keeps an allow-list per
// account and nothing else.
describe('offline snapshot', () => {
	beforeEach(() => {
		clearOffline();
		localStorage.clear();
	});
	const login = (name: string, extra: Record<string, unknown> = {}) =>
		noteForOffline('/session', {
			user: { name, role: 'owner' },
			hostname: 'titan',
			lockMinutes: 0,
			...extra,
		});

	it('keeps only the allow-list', () => {
		login('alex');
		noteForOffline('/system', {
			cpu: { usagePercent: 12, model: 'x' },
			memory: { usedBytes: 1, totalBytes: 4 },
			uptimeSeconds: 60,
			network: { address: '192.168.1.2' },
		});
		noteForOffline('/storage', {
			volumes: [
				{
					name: 'Data',
					state: 'ok',
					usedBytes: 5,
					sizeBytes: 10,
					mounted: true,
					members: [{ disk: 'sda' }],
				},
			],
			disks: [{ serial: 'SECRET-SERIAL' }],
		});
		noteForOffline('/agent/chats', {
			chats: [
				{ id: 'c1', title: 'Backups', updated: 'u', state: 'idle', preview: 'Mein Passwort ist …' },
				{ id: 'c2', title: 'Fremd', author: 'maria' },
			],
		});
		noteForOffline('/files/list', { entries: [{ name: 'Steuer.pdf' }] });
		noteForOffline('/mail/messages', { items: [{ subject: 'Bank' }] });
		const snap = offlineSnapshot();
		expect(snap?.user).toBe('alex');
		expect(snap?.hostname).toBe('titan');
		const stored = localStorage.getItem('titanos.offline.alex') ?? '';
		for (const secret of [
			'192.168.1.2',
			'SECRET-SERIAL',
			'Passwort',
			'Steuer.pdf',
			'Bank',
			'Fremd',
			'sda',
		])
			expect(stored).not.toContain(secret);
		expect(snap?.chats?.list.map((c) => c.title)).toEqual(['Backups']);
		expect(snap?.volumes?.list[0]).toEqual({
			name: 'Data',
			state: 'ok',
			used: 5,
			size: 10,
			mounted: true,
		});
	});

	it('goes on logout and with another account', () => {
		login('alex');
		expect(offlineSnapshot()?.user).toBe('alex');
		login('maria');
		expect(localStorage.getItem('titanos.offline.alex')).toBeNull();
		expect(offlineSnapshot()?.user).toBe('maria');
		noteForOffline('/session', { user: null, hostname: 'titan' });
		expect(offlineSnapshot()).toBeNull();
		login('maria');
		clearOffline();
		expect(offlineSnapshot()).toBeNull();
	});

	it('shows only the lock when locked or when the lock time passed', () => {
		login('alex', { lockMinutes: 5 });
		noteForOffline('/agent/chats', { chats: [{ id: 'c1', title: 'Backups' }] });
		const snap = offlineSnapshot()!;
		expect(snapshotLocked(snap, snap.savedAt + 60_000)).toBe(false);
		expect(snapshotLocked(snap, snap.savedAt + 6 * 60_000)).toBe(true);
		login('alex', { locked: true });
		const locked = offlineSnapshot()!;
		expect(locked.locked).toBe(true);
		expect(locked.chats).toBeUndefined();
		expect(snapshotLocked(locked)).toBe(true);
	});
});
