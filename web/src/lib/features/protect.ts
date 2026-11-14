// Gruppe „Schutz“ (PLAN 54-S): Ransomware-Wächter, Notfallhandbuch,
// Notfallzugang. NAS-wide settings (owners and administrators) and the
// views of the three functions.
import { api } from '../api.ts';

export type GuardMode = 'report' | 'lock' | 'auto';

export type Guard = {
	enabled?: boolean;
	mode?: GuardMode;
	known?: number;
	renames?: number;
	rewrites?: number;
	notes?: number;
	windowSec?: number;
	quietSec?: number;
	exclude?: string[];
	noPreSnapshots?: boolean;
};

export type Contact = { name: string; phone?: string; note?: string };
export type Handbook = {
	enabled?: boolean;
	probes?: boolean;
	withContacts?: boolean;
	contacts?: Contact[];
};

export type Person = { account: string; waitDays: number; shares: string[] };
export type Emergency = { enabled?: boolean; persons?: Person[]; code?: boolean };

export type Affected = { was: string; now: string; kind: number };
export type GuardClient = {
	user?: string;
	ip?: string;
	machine?: string;
	process?: string;
	pid?: number;
};
export type RestoreResult = {
	restored: number;
	recycled: number;
	missing: number;
	failed?: string[];
};
export type GuardAlert = {
	id: string;
	share: string;
	volume: string;
	client: GuardClient;
	reason: 'known-extension' | 'unknown-extension' | 'rewrite' | 'notes';
	mode: GuardMode;
	first: string;
	last: string;
	count: number;
	more?: number;
	files: Affected[];
	state: 'active' | 'stopped' | 'restoring' | 'restored' | 'reopened' | 'dismissed';
	locked: boolean;
	btrfs: boolean;
	snapshot?: string;
	snapshotAt?: string;
	held?: string;
	restored?: RestoreResult;
	/** How many files a restore puts back (the stored ones). */
	restorable?: number;
	error?: string;
	by?: string;
};
export type GuardStatus = {
	enabled: boolean;
	mode: GuardMode;
	watching: boolean;
	error: string;
	overflows: number;
	alerts: GuardAlert[];
};

export type Section = { id: string; title: string; sub: string; steps: string[] };
export type NamedFact = { name: string; detail?: string };
export type HandbookView = {
	made: string;
	volumes: { name: string; line: string }[] | null;
	sections: Section[];
	facts: {
		host: string;
		shares: NamedFact[] | null;
		apps: NamedFact[] | null;
		vms: NamedFact[] | null;
		network: NamedFact[] | null;
		remote: NamedFact[] | null;
		backups: NamedFact[] | null;
		probes?: NamedFact[] | null;
		contacts?: Contact[] | null;
	};
};

export type EmergencyRequest = {
	id: string;
	account: string;
	reason: string;
	created: string;
	due: string;
	state: 'waiting' | 'declined' | 'granted' | 'ended' | 'canceled';
	shares: string[];
	by?: string;
	decided?: string;
	ends?: string;
	added?: string[];
	code?: boolean;
};
export type EmergencyView = {
	enabled: boolean;
	code: boolean;
	persons: (Person & { codeSet: boolean })[];
	requests: EmergencyRequest[];
	now: string;
};

/** What of the group is there for the signed-in account. */
export type ProtectMe = {
	guard: boolean;
	handbook: boolean;
	emergency: boolean;
	alerts?: number;
	requests?: number;
	person?: Person;
	codes?: boolean;
	request?: EmergencyRequest;
};

export const loadProtectMe = () => api.get<ProtectMe>('/agent/protect/me');
/** The guard's state. An older titand (cluster member) answers `alerts: null`
 * without alarms and `files: null` in an alarm without files: always lists here. */
export async function loadGuard(): Promise<GuardStatus> {
	const status = await api.get<GuardStatus>('/agent/protect/guard');
	const alerts = (status.alerts ?? []).map((a) => ({ ...a, files: a.files ?? [] }));
	return { ...status, alerts };
}
export const guardAction = (id: string, action: 'restore' | 'reopen' | 'dismiss') =>
	api.post<GuardAlert>(`/agent/protect/guard/${encodeURIComponent(id)}/${action}`);
export const loadHandbook = (lang: string) =>
	api.get<HandbookView>(`/agent/protect/handbook?lang=${lang === 'en' ? 'en' : 'de'}`);
export const refreshHandbook = () => api.post<{ made: string }>('/agent/protect/handbook/refresh');
export const downloadHandbook = (lang: string, format: 'pdf' | 'md') =>
	api.download(
		`/agent/protect/handbook/download?lang=${lang === 'en' ? 'en' : 'de'}&format=${format}`,
		`Notfallhandbuch.${format}`,
		{ serverName: true },
	);
export const loadEmergency = () => api.get<EmergencyView>('/agent/protect/emergency');
export const askEmergency = (reason: string, code = '') =>
	api.post<EmergencyRequest>('/agent/protect/emergency/request', { reason, code });
export const emergencyAction = (id: string, action: 'decline' | 'grant' | 'end' | 'cancel') =>
	api.post<EmergencyRequest>(`/agent/protect/emergency/${encodeURIComponent(id)}/${action}`);
export const emergencyCode = (account: string) =>
	api.post<{ code: string }>(`/agent/protect/emergency/code/${encodeURIComponent(account)}`);

/** Share of the waiting time that has passed, 0–1. */
export function waited(req: EmergencyRequest, now: Date): number {
	const from = new Date(req.created).getTime();
	const to = new Date(req.due).getTime();
	if (to <= from) return 1;
	return Math.min(1, Math.max(0, (now.getTime() - from) / (to - from)));
}

/** The base name and folder of a path in a share. */
export function splitPath(p: string): { dir: string; base: string } {
	const i = p.lastIndexOf('/');
	return i < 0 ? { dir: '', base: p } : { dir: p.slice(0, i), base: p.slice(i + 1) };
}
