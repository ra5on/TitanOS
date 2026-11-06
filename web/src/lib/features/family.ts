// Gruppe „Gemeinschaft und Familie“ (PLAN 54-G): Backup-Buddy, Skill-Katalog,
// Familien-Tagebuch. Types of the API and small helpers (no runes, so they
// can be tested with node --test).
import { api } from '../api.ts';

export type Buddy = {
	enabled?: boolean;
	address?: string;
	port?: number;
	volume?: string;
	protectDays?: number;
	every?: 'daily' | 'weekly' | 'monthly';
	weekday?: number;
	time?: string;
	probe?: 'monthly' | 'off';
	probeFiles?: number;
};
/** The skills come from the App-Store sources (folder skills/); only a switch. */
export type Catalog = { enabled?: boolean };
export type DiaryNAS = { enabled?: boolean };
export type DiarySource = 'chats' | 'reminders' | 'home' | 'photos' | 'immich' | 'frigate';
export type DiaryMine = {
	off?: DiarySource[];
	immichUrl?: string;
	/** Only sent when changed; the page never gets it back. */
	immichKey?: string;
	immichKeySet?: boolean;
	clearImmichKey?: boolean;
	frigateUrl?: string;
	photos?: { root: string; path: string } | null;
};

export const DIARY_SOURCES: DiarySource[] = [
	'chats',
	'reminders',
	'home',
	'photos',
	'immich',
	'frigate',
];

export type BuddyProbe = {
	at: string;
	files: number;
	bytes: number;
	error: string;
	truncated?: boolean;
};
export type BuddyJob = {
	lastRun: string | null;
	lastOK: string | null;
	lastError?: string;
	bytes: number;
	running: boolean;
	next: string | null;
	probe?: BuddyProbe;
};
export type BuddyPartner = {
	id: string;
	name: string;
	address: string;
	quota: number;
	used: number;
	usedKnown: boolean;
	theirQuota: number;
	shares: string[];
	paired: string;
	job: BuddyJob | null;
	/** Why the job to the partner could not be saved. */
	jobError?: string;
};
export type BuddyView = {
	settings: Buddy;
	name: string;
	partners: BuddyPartner[];
	code: { code: string; expires: string } | null;
	volumes: string[];
	shares: string[];
	listening: string[];
	problem: '' | 'off' | 'volume' | 'address' | 'tailscale';
};

export type CatalogSkill = {
	/** "<source>/<id>", what install names. */
	key: string;
	id: string;
	source: string;
	/** "TitanOS-Apps" or the name of an own source. */
	sourceName: string;
	name: string;
	description?: string;
	when: string;
	steps: string;
	tools: string[];
	author?: string;
	digest: string;
	unknown: string[];
	installed: boolean;
	secret?: boolean;
};

export const GiB = 1 << 30;

export const loadBuddy = () => api.get<BuddyView>('/agent/buddy');
export const buddyCode = (quota: number, shares: string[]) =>
	api.post<{ code: string; expires: string }>('/agent/buddy/code', { quota, shares });
export const buddyCodeCancel = () => api.delete('/agent/buddy/code');
export const buddyPair = (address: string, code: string, quota: number, shares: string[]) =>
	api.post<{ id: string; name: string; jobError?: string }>('/agent/buddy/pair', {
		address,
		code,
		quota,
		shares,
	});
export const buddyPartnerSet = (id: string, quota: number, shares: string[]) =>
	api.put(`/agent/buddy/partners/${encodeURIComponent(id)}`, { quota, shares });
export const buddyPartnerDelete = (id: string, data: boolean) =>
	api.delete(`/agent/buddy/partners/${encodeURIComponent(id)}${data ? '?data=1' : ''}`);

export const loadCatalog = () => api.get<{ skills: CatalogSkill[] }>('/agent/catalog');
export const installSkill = (s: CatalogSkill) =>
	api.post('/agent/catalog/install', { key: s.key, digest: s.digest });

/** Share of a quota in percent, 0–100. */
export function quotaPercent(used: number, quota: number): number {
	if (!quota || quota <= 0) return 0;
	return Math.max(0, Math.min(100, Math.round((used / quota) * 100)));
}

/** The first steps of a skill, for the question before installing. */
export function stepsPreview(steps: string, lines = 3): string[] {
	return steps
		.split('\n')
		.map((l) => l.trim())
		.filter(Boolean)
		.slice(0, lines);
}

/** A code typed by a person: upper case, groups of three. */
export function formatCode(raw: string): string {
	const c = raw
		.toUpperCase()
		.replace(/[^0-9A-Z]/g, '')
		.slice(0, 9);
	return c.replace(/(.{3})(?=.)/g, '$1-');
}

/** The diary sources a tool step carries (titanos:diary/<id>). */
export function diarySources(
	sources: { title?: string; url: string }[] | undefined,
): { title: string; id: string }[] {
	return (sources ?? [])
		.filter((s) => s.url.startsWith('titanos:diary/'))
		.map((s) => ({ title: s.title ?? '', id: s.url.slice('titanos:diary/'.length) }));
}
