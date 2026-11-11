// Backups of shares to another volume, another server or the cloud: the
// shapes the API sends and the texts and payloads the page builds from them.
import { bandwidthParts, type Bandwidth } from '#lib/backup-bandwidth.ts';
import type { BackupNote } from '#lib/backup-notes.ts';
import type { Keep } from '#lib/components/KeepVersions.svelte';
import { t } from '#lib/i18n/index.svelte.ts';
import type { Application } from '#lib/operations/ApplicationSelection.svelte';
import type { Chosen } from './selection.ts';
import type { SmbTarget } from './SmbTargets.svelte';

export type Kind =
	| 'smb-copy'
	| 'usb-copy'
	| 'local-copy'
	| 'local'
	| 'usb'
	| 'smb'
	| 's3'
	| 'sftp'
	| 'rest'
	| 'titanos';
/** Where a backup goes, as the page offers it; the kind adds the format. */
export type Place = 'local' | 'usb' | 'smb' | 'titanos' | 's3' | 'sftp' | 'rest';
export const PLACES: Place[] = ['local', 'usb', 'smb', 'titanos', 's3', 'sftp', 'rest'];
export type Destination = {
	type: Kind;
	deviceId?: string;
	/** The drive's name when the job was saved, only for showing. */
	deviceName?: string;
	targetId?: string;
	volume?: string;
	folder?: string;
	endpoint?: string;
	bucket?: string;
	prefix?: string;
	accessKey?: string;
	secretKey?: string;
	host?: string;
	port?: number;
	user?: string;
	path?: string;
	url?: string;
	username?: string;
	password?: string;
};
export type Job = {
	applications?: Application[];
	allowStop?: boolean;
	id: string;
	sources: string[];
	folders?: Record<string, string[]>;
	skip?: string[];
	patterns?: string[];
	extras?: string[];
	destination: Destination;
	enabled: boolean;
	every: 'hourly' | 'daily' | 'weekly' | 'monthly';
	interval?: number;
	weekday: number;
	weekdays?: number[];
	monthDay?: number;
	time: string;
	times?: string[];
	windowFrom?: string;
	windowTo?: string;
	keep: Keep;
	/** Options of a readable copy (0.41.0). */
	copy?: { dated?: boolean; mirror?: boolean; mirrorDays?: number };
	/** Whole volumes and folders outside shares ("volume/folder"). */
	volumes?: string[];
	places?: string[];
	bandwidth?: Bandwidth;
	/** How a readable copy treats disks of running VMs (0.77); none: no choice yet. */
	vmDisks?: VMDisks;
};
/** The job's choice for the image files of running VMs. */
export type VMDisks = { mode: 'consistent' | 'skip'; raw?: 'skip' | 'pause' };
/** One image file of a VM in the sources of a job (GET /backups/vm-disks). */
export type VMDisk = {
	vm: string;
	path: string;
	file: string;
	format: string;
	sizeBytes: number;
	state: 'running' | 'stopped';
	way: 'copy' | 'btrfs_snapshot' | 'libvirt_snapshot' | 'pause' | 'as_is' | 'skip' | 'none';
	agent: boolean;
	reason?: string;
	needBytes?: number;
	freeBytes?: number;
};
export type VMDisksAnswer = {
	choice?: VMDisks | null;
	choiceNeeded: boolean;
	disks: VMDisk[] | null;
};

/**
 * The query of GET /backups/vm-disks for the sources chosen in the form and
 * the choice not stored yet; "" without a source (nothing to ask).
 */
export function vmDisksQuery(
	selection: { sources: string[]; volumes: string[]; places: string[] },
	choice: VMDisks | undefined,
): string {
	const query = new URLSearchParams();
	for (const source of selection.sources) query.append('source', source);
	for (const volume of selection.volumes) query.append('volume', volume);
	for (const place of selection.places) query.append('place', place);
	if ([...query].length === 0) return '';
	if (choice) {
		query.set('mode', choice.mode);
		if (choice.mode === 'consistent' && choice.raw) query.set('raw', choice.raw);
	}
	return query.toString();
}
export type State = {
	statusError?: string;
	probe?: {
		at: string;
		snapshot: string;
		files: number;
		bytes: number;
		truncated: boolean;
		error: string;
	};
	running: boolean;
	phase: string;
	/** Started, but waiting for another job to end. */
	queued?: boolean;
	percent: number;
	bytesDone: number;
	bytesTotal: number;
	bandwidthLimit?: number;
	lastRun: string | null;
	lastOK: string | null;
	lastError: string;
	/** A known cause of lastError, with a text under error.<code>. */
	lastErrorCode?: string;
	warning: string;
	/** What the last run could not save and what else it tells, as codes (0.77). */
	problems?: BackupNote[] | null;
	notices?: BackupNote[] | null;
	/** Entries the last run of a readable copy could not save. */
	skipped?: number;
	bytes: number;
	restoredTo: string;
	restoreError?: string;
	lastCheck?: string;
	checkError?: string;
};
export type View = {
	stateKnown?: boolean;
	job: Job;
	secretSet: Record<string, boolean>;
	state: State;
	next: string | null;
};
export type Overview = {
	smbTargets: SmbTarget[];
	jobs: View[];
	shares: { name: string; volume: string }[];
	volumes: string[] | null;
};

export type UsbDrive = { id: string; name: string; supported: boolean };

/** A job that is being created: the values the form starts with. */
export function newJob(): Job {
	return {
		id: '',
		sources: [],
		applications: [],
		allowStop: false,
		destination: { type: 'local', volume: '', folder: '', port: 22 },
		enabled: true,
		every: 'daily',
		interval: 6,
		weekday: 0,
		weekdays: [1],
		monthDay: 1,
		time: '02:30',
		times: ['02:30'],
		keep: { last: 10, daily: 0, weekly: 0, monthly: 0 },
	};
}

/** A stored job as the form edits it. */
export function editableJob(job: Job, lastOf: (keep: Keep) => number): Job {
	const draft = JSON.parse(JSON.stringify(job)) as Job;
	draft.destination = { port: 22, ...draft.destination, secretKey: '', password: '' };
	// Jobs from before the schedule grew carry one weekday and one time.
	draft.interval ||= 6;
	draft.monthDay ||= 1;
	// ApplicationSelection binds both; a bound prop may not be undefined.
	draft.applications ??= [];
	draft.allowStop ??= false;
	draft.keep = { ...draft.keep, last: lastOf(draft.keep) };
	if (!draft.weekdays?.length) draft.weekdays = [draft.weekday];
	if (!draft.times?.length) draft.times = [draft.time];
	return draft;
}

export const EXTRAS = ['appdata', 'vms', 'lxc', 'config'] as const;

/** What a job saves, as one list of folders (decided 31.10.2026). */
export function chosenOf(job: Job, shares: { name: string; volume: string }[]): Chosen[] {
	const list: Chosen[] = [];
	const volumeOf = (share: string) => shares.find((s) => s.name === share)?.volume ?? '';
	for (const name of job.sources) {
		const volume = volumeOf(name);
		const only = job.folders?.[name] ?? [];
		if (only.length) for (const f of only) list.push({ volume, path: `${name}/${f}` });
		else list.push({ volume, path: name });
	}
	for (const v of job.volumes ?? []) list.push({ volume: v, path: '' });
	for (const place of job.places ?? []) {
		const at = place.indexOf('/');
		list.push({ volume: place.slice(0, at), path: place.slice(at + 1) });
	}
	return list.length ? list : [{ volume: '', path: '' }];
}

/** A pairing code of another TitanOS: titanos-backup: and base64url JSON. */
export function decodeCode(text: string): { url: string; user: string; password: string } | null {
	try {
		const body = text
			.trim()
			.replace(/^titanos-backup:/, '')
			.replace(/-/g, '+')
			.replace(/_/g, '/');
		const data = JSON.parse(
			new TextDecoder().decode(Uint8Array.from(atob(body), (c) => c.charCodeAt(0))),
		);
		return data.v === 1 &&
			typeof data.url === 'string' &&
			typeof data.user === 'string' &&
			typeof data.password === 'string'
			? data
			: null;
	} catch {
		return null;
	}
}

export const plainCopy = (kind: string) =>
	kind === 'usb-copy' || kind === 'smb-copy' || kind === 'local-copy';
export const placeOf = (kind: Kind): Place =>
	kind === 'local-copy'
		? 'local'
		: kind === 'usb-copy'
			? 'usb'
			: kind === 'smb-copy'
				? 'smb'
				: kind;
/** Volume, drive and share offer a readable copy besides encrypted versions. */
export const twoFormats = (place: Place) => place === 'local' || place === 'usb' || place === 'smb';
export const kindOf = (place: Place, readable: boolean): Kind =>
	readable && twoFormats(place) ? (`${place}-copy` as Kind) : place;
export const fullTarget = (kind: Kind) =>
	placeOf(kind) === 'usb'
		? 'drive'
		: placeOf(kind) === 'local'
			? 'volume'
			: placeOf(kind) === 'smb'
				? 'share'
				: 'storage';

// Only what belongs to the chosen kind of destination is sent.
export function destinationPayload(
	d: Destination,
	usbDrives: UsbDrive[],
	pairCode: string,
): Destination {
	switch (d.type) {
		case 'smb-copy':
			return { type: 'smb-copy', targetId: d.targetId, folder: d.folder };
		case 'usb-copy':
			return {
				type: 'usb-copy',
				deviceId: d.deviceId,
				deviceName: usbDrives.find((drive) => drive.id === d.deviceId)?.name ?? d.deviceName,
				folder: d.folder,
			};
		case 'local':
		case 'local-copy':
			return { type: d.type, volume: d.volume, folder: d.folder };
		case 'usb':
			return {
				type: 'usb',
				deviceId: d.deviceId,
				deviceName: usbDrives.find((drive) => drive.id === d.deviceId)?.name ?? d.deviceName,
				folder: d.folder,
			};
		case 'smb':
			return { type: 'smb', targetId: d.targetId, folder: d.folder };
		case 's3':
			return {
				type: 's3',
				endpoint: d.endpoint?.trim(),
				bucket: d.bucket?.trim(),
				prefix: d.prefix?.trim(),
				accessKey: d.accessKey?.trim(),
				secretKey: d.secretKey,
			};
		case 'sftp':
			return {
				type: 'sftp',
				host: d.host?.trim(),
				port: Number(d.port) || 22,
				user: d.user?.trim(),
				path: d.path?.trim(),
			};
		case 'titanos': {
			const code = decodeCode(pairCode);
			return { type: 'rest', url: code?.url, username: code?.user, password: code?.password };
		}
		default:
			return {
				type: 'rest',
				url: d.url?.trim(),
				username: d.username?.trim(),
				password: d.password,
			};
	}
}

export function where(d: Destination): string {
	if (d.type === 'smb-copy') return `${d.targetId} / ${d.folder}`;
	if (d.type === 'usb-copy')
		return `${t('backup.kind.usb-copy')} ${d.deviceName || d.deviceId} / ${d.folder}`;
	switch (d.type) {
		case 'local':
		case 'local-copy':
			return `/mnt/${d.volume}/${d.folder}`;
		case 's3':
			return `${d.endpoint}/${d.bucket}${d.prefix ? '/' + d.prefix : ''}`;
		case 'sftp':
			return `${d.user}@${d.host}:${d.path}`;
		default:
			return d.url ?? '';
	}
}

export function stateChip(view: View): { text: string; tone: string } {
	const state = view.state;
	if (state.running && state.queued) return { text: t('backup.queued'), tone: '' };
	if (state.running)
		return { text: t(`backup.phase.${state.phase}` as 'backup.phase.backup'), tone: 'info' };
	if (view.stateKnown === false || state.statusError)
		return { text: t('tasks.state.unknown'), tone: 'warn' };
	if (state.lastError && state.lastError !== 'cancelled')
		return { text: t('backup.failed'), tone: 'error' };
	if (state.lastError === 'cancelled') return { text: t('backup.cancelled'), tone: 'warn' };
	if (state.warning || state.problems?.length)
		return { text: t('backup.withWarning'), tone: 'warn' };
	if (state.lastOK) return { text: t('backup.ok'), tone: 'ok' };
	return { text: t('backup.never'), tone: '' };
}

export function when(job: Job): string {
	if (!job.enabled) return t('backup.manual');
	const times = (job.times?.length ? job.times : [job.time]).join(', ');
	let text: string;
	switch (job.every) {
		case 'hourly':
			text = t('backup.whenHourly', { hours: job.interval ?? 1, minute: job.time.slice(3) });
			break;
		case 'weekly': {
			const days = (job.weekdays?.length ? job.weekdays : [job.weekday])
				.slice()
				.sort((a, b) => ((a + 6) % 7) - ((b + 6) % 7));
			text = t('backup.whenWeekly', {
				day: days.map((d) => t(`backup.dayShort.${d}` as 'backup.dayShort.0')).join(', '),
				time: times,
			});
			break;
		}
		case 'monthly':
			text = t('backup.whenMonthly', { day: job.monthDay ?? 1, time: times });
			break;
		default:
			text = t('backup.whenDaily', { time: times });
	}
	return job.windowFrom
		? `${text} · ${t('backup.whenWindow', { from: job.windowFrom, to: job.windowTo ?? '' })}`
		: text;
}

export const rate = (mib: number) =>
	mib > 0 ? t('backup.bandwidthRate', { value: mib }) : t('backup.bandwidthNone');
export function bandwidthText(b?: Bandwidth): string {
	const parts = bandwidthParts(b);
	if (!parts) return t('backup.bandwidthNone');
	return parts.window
		? `${rate(parts.limit)} · ${t('backup.bandwidthWindowText', { from: parts.window.from, to: parts.window.to, limit: rate(parts.window.limit) })}`
		: rate(parts.limit);
}
