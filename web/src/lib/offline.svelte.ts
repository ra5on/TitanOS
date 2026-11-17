// 1.07: TitanOS without a connection to the NAS. Two parts:
//
// - `connection`: whether the NAS answers right now. A request that does not
//   reach it (no network, NAS off, phone away from home) marks it lost; the
//   next answer marks it back. While lost, changes are not sent: api.ts
//   refuses them at once with "nas_unreachable", nothing is queued.
// - the snapshot: what this browser saw last, so that the installed app
//   opens with something instead of an error when the NAS cannot be
//   reached. Only an allow-list is kept (host name, the account and its
//   role, the state of system, volumes and backups, the switched-on
//   services, the titles of the agent's chats) – never file names, mails,
//   camera pictures, messages or what a chat says. It lives in this
//   browser's localStorage per account and goes on logout, when the session
//   ends and when another account logs in. Written by the top window only
//   (windows of the desktop are frames of it), at most every 20 seconds
//   per part. A home-screen app on the iPhone may have storage of its own:
//   the agent app keeps its own snapshot.

export const connection = $state({ lost: false, since: 0 });

/** A request did not reach the NAS. */
export function markLost() {
	if (!connection.lost) {
		connection.lost = true;
		connection.since = Date.now();
	}
}

/** The NAS answered. */
export function markBack() {
	if (connection.lost) {
		connection.lost = false;
		connection.since = 0;
	}
}

const KEY = 'titanos.offline';
const WRITE_EVERY = 20_000;

export type OfflineVolume = {
	name: string;
	state: string;
	used: number;
	size: number;
	mounted: boolean;
};
export type OfflineBackup = { name: string; lastRun?: string; lastOk?: boolean; running: boolean };
export type OfflineChat = { id: string; title: string; updated: string; state: string };

export type OfflineSnapshot = {
	v: 1;
	user: string;
	role: string;
	hostname: string;
	version?: string;
	/** When the page last saw the NAS. */
	savedAt: number;
	/** The session was locked when it was seen last. */
	locked: boolean;
	lockMinutes: number;
	system?: { at: number; cpu: number; memUsed: number; memTotal: number; uptime: number };
	volumes?: { at: number; list: OfflineVolume[] };
	backups?: { at: number; list: OfflineBackup[] };
	services?: Record<string, boolean>;
	chats?: { at: number; list: OfflineChat[] };
};

function top(): boolean {
	try {
		return typeof window !== 'undefined' && window.top === window;
	} catch {
		return false;
	}
}

function read(key: string): unknown {
	try {
		const text = localStorage.getItem(key);
		return text ? JSON.parse(text) : null;
	} catch {
		return null;
	}
}

function write(key: string, value: unknown) {
	try {
		localStorage.setItem(key, JSON.stringify(value));
	} catch {
		// Full or blocked storage: there is just no snapshot.
	}
}

function remove(key: string) {
	try {
		localStorage.removeItem(key);
	} catch {
		// Nothing to do.
	}
}

const userKey = (user: string) => `${KEY}.${user}`;

/** The snapshot of the account last seen in this browser, if any. */
export function offlineSnapshot(): OfflineSnapshot | null {
	const user = read(KEY);
	if (typeof user !== 'string' || !user) return null;
	const snap = read(userKey(user)) as OfflineSnapshot | null;
	return snap && snap.v === 1 && snap.user === user ? snap : null;
}

/** Whether the snapshot may only show the lock: locked when seen, or the
 *  account's automatic lock would have locked it since. */
export function snapshotLocked(snap: OfflineSnapshot, now = Date.now()): boolean {
	return snap.locked || (snap.lockMinutes > 0 && now - snap.savedAt > snap.lockMinutes * 60_000);
}

/** Removes every snapshot of this browser (logout, session gone). */
export function clearOffline() {
	const user = read(KEY);
	if (typeof user === 'string' && user) remove(userKey(user));
	remove(KEY);
	current = null;
	for (const part of Object.keys(written)) delete written[part];
}

let current: OfflineSnapshot | null = null;
const written: Record<string, number> = {};

// What was seen in the last seconds is kept when the page goes away.
if (typeof window !== 'undefined' && top()) {
	const flush = () => {
		if (current) write(userKey(current.user), current);
	};
	addEventListener('pagehide', flush);
	document.addEventListener('visibilitychange', () => {
		if (document.visibilityState === 'hidden') flush();
	});
}

function save(part: string, change: (s: OfflineSnapshot) => void, force = false) {
	if (!top() || !current) return;
	change(current);
	current.savedAt = Date.now();
	if (!force && Date.now() - (written[part] ?? 0) < WRITE_EVERY) return;
	written[part] = Date.now();
	write(userKey(current.user), current);
}

type SessionAnswer = {
	user?: { name: string; role: string } | null;
	hostname?: string;
	version?: string;
	locked?: boolean;
	lockMinutes?: number;
};

function noteSession(s: SessionAnswer) {
	if (!top()) return;
	const name = s.user?.name;
	if (!name) {
		// Logged out: nothing of the account stays.
		clearOffline();
		return;
	}
	const before = read(KEY);
	if (typeof before === 'string' && before && before !== name) remove(userKey(before));
	if (!current || current.user !== name) {
		for (const part of Object.keys(written)) delete written[part];
		const kept = read(userKey(name)) as OfflineSnapshot | null;
		current =
			kept && kept.v === 1 && kept.user === name
				? kept
				: { v: 1, user: name, role: '', hostname: '', savedAt: 0, locked: false, lockMinutes: 0 };
	}
	write(KEY, name);
	save(
		'session',
		(c) => {
			c.role = s.user?.role ?? '';
			c.hostname = s.hostname ?? '';
			c.version = s.version;
			c.locked = !!s.locked;
			c.lockMinutes = Math.max(0, Number(s.lockMinutes) || 0);
			// Locked: what the account sees is not kept while it is locked.
			if (c.locked) {
				delete c.system;
				delete c.volumes;
				delete c.backups;
				delete c.chats;
			}
		},
		true,
	);
}

const num = (v: unknown) => (typeof v === 'number' && Number.isFinite(v) ? v : 0);
const str = (v: unknown) => (typeof v === 'string' ? v : '');

/**
 * Called by api.ts with every answer to a GET: the answers of the
 * allow-list become the snapshot, everything else is ignored.
 */
export function noteForOffline(path: string, data: unknown) {
	if (!data || typeof data !== 'object') return;
	const d = data as Record<string, unknown>;
	switch (path) {
		case '/session':
			noteSession(d as SessionAnswer);
			return;
	}
	if (!current || current.locked) return;
	const at = Date.now();
	switch (path) {
		case '/system': {
			const cpu = d.cpu as Record<string, unknown> | undefined;
			const mem = d.memory as Record<string, unknown> | undefined;
			save('system', (c) => {
				c.system = {
					at,
					cpu: num(cpu?.usagePercent),
					memUsed: num(mem?.usedBytes),
					memTotal: num(mem?.totalBytes),
					uptime: num(d.uptimeSeconds),
				};
			});
			return;
		}
		case '/storage': {
			const list = Array.isArray(d.volumes) ? (d.volumes as Record<string, unknown>[]) : [];
			save('volumes', (c) => {
				c.volumes = {
					at,
					list: list.slice(0, 16).map((v) => ({
						name: str(v.name),
						state: str(v.state),
						used: num(v.usedBytes),
						size: num(v.sizeBytes),
						mounted: v.mounted === true,
					})),
				};
			});
			return;
		}
		case '/backups': {
			const list = Array.isArray(d.jobs) ? (d.jobs as Record<string, unknown>[]) : [];
			save('backups', (c) => {
				c.backups = {
					at,
					list: list.slice(0, 16).map((j) => {
						const job = (j.job ?? {}) as Record<string, unknown>;
						const state = (j.state ?? {}) as Record<string, unknown>;
						return {
							name: str(job.name) || str(job.id),
							lastRun: str(state.lastRun) || undefined,
							lastOk: typeof state.lastOk === 'boolean' ? state.lastOk : undefined,
							running: state.running === true,
						};
					}),
				};
			});
			return;
		}
		case '/services/state':
			save('services', (c) => {
				c.services = Object.fromEntries(
					Object.entries(d).filter(([, v]) => typeof v === 'boolean'),
				) as Record<string, boolean>;
			});
			return;
		case '/agent/chats': {
			// Titles only: no preview of what a chat says.
			const list = Array.isArray(d.chats) ? (d.chats as Record<string, unknown>[]) : [];
			save('chats', (c) => {
				c.chats = {
					at,
					list: list
						.filter((x) => !x.archived && !x.author)
						.slice(0, 15)
						.map((x) => ({
							id: str(x.id),
							title: str(x.title),
							updated: str(x.updated),
							state: str(x.state),
						})),
				};
			});
			return;
		}
	}
}
