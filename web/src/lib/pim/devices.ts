// T-Kalender and Kontakte: the device passwords of an account and what the
// iPhone needs to sync (GET/POST /account/devices, GET /account/devices/setup;
// TECHNIK 3.2, 3.5, 4.2).

/** A device as GET /account/devices lists it; never a password or hash. */
export type Device = {
	id: string;
	name: string;
	created: string;
	lastSync: string | null;
	lastKind: string;
	lastSyncBeforeRestart?: boolean;
};

/** POST /account/devices: shown once, never kept in the browser. */
export type CreatedDevice = { id: string; name: string; user: string; password: string };

export type SetupAddress = {
	kind: 'domain' | 'tailscale' | 'lan' | 'ddns' | 'netbird';
	server: string;
	caldav: string;
	carddav: string;
	certificate: 'valid' | 'selfSigned' | 'own';
	recommended?: boolean;
	hints: string[];
};

export type DeviceSetup = { user: string; port: number; addresses: SetupAddress[] };

/** 1.13, GET /account/devices/profile: whether the profile is signed, and by which name. */
export type ProfileInfo = { signed: boolean; signer: string; ttl: number; states: string[] };

/** A one-time link of a profile (GET /account/devices/profile/{id}); never its token. */
export type ProfileLink = {
	id: string;
	device: string;
	expires: string;
	state: 'pending' | 'fetched' | 'failed' | 'expired' | 'cancelled';
	code?: string;
};

/** POST /account/devices/profile: the link with its URL, shown once, never kept. */
export type ProfileMade = { link: ProfileLink; url: string; path: string; signed: boolean };

/** The states of the holidays (codes of titand, pim/holidays). */
export const HOLIDAY_STATES = [
	'bw',
	'by',
	'be',
	'bb',
	'hb',
	'hh',
	'he',
	'mv',
	'ni',
	'nw',
	'rp',
	'sl',
	'sn',
	'st',
	'sh',
	'th',
] as const;

/**
 * "Last synced" as TECHNIK 3.5 words it: never, before the restart (a date),
 * or today/yesterday/a date with the time and what was synced.
 */
export type LastSync =
	| { when: 'never' }
	| { when: 'beforeRestart'; at: Date }
	| { when: 'today' | 'yesterday' | 'date'; at: Date; kind: 'caldav' | 'carddav' | '' };

function sameDay(a: Date, b: Date): boolean {
	return (
		a.getFullYear() === b.getFullYear() &&
		a.getMonth() === b.getMonth() &&
		a.getDate() === b.getDate()
	);
}

export function lastSync(device: Device, now: Date): LastSync {
	if (!device.lastSync) return { when: 'never' };
	const at = new Date(device.lastSync);
	if (Number.isNaN(at.getTime())) return { when: 'never' };
	if (device.lastSyncBeforeRestart) return { when: 'beforeRestart', at };
	const kind = device.lastKind === 'caldav' || device.lastKind === 'carddav' ? device.lastKind : '';
	const yesterday = new Date(now);
	yesterday.setDate(now.getDate() - 1);
	const when = sameDay(at, now) ? 'today' : sameDay(at, yesterday) ? 'yesterday' : 'date';
	return { when, at, kind };
}

/** The password in its groups of four, each with its dash, so a line breaks only between groups. */
export function passwordGroups(password: string): string[] {
	const parts = password.split('-');
	return parts.map((p, i) => (i < parts.length - 1 ? p + '-' : p));
}
