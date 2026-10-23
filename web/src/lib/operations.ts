import type { CopyJob, VM } from '#lib/api.ts';
export type MaintenanceJob = {
	dependsOn?: string;
	id: string;
	name: string;
	kind: 'raid-check' | 'smart-long' | 'backup-check' | 'restore-probe' | 'snapshot';
	target: string;
	enabled: boolean;
	every: 'daily' | 'weekly' | 'monthly';
	weekdays: number[];
	monthDay: number;
	time: string;
	missed: 'window' | 'skip';
	readData: number;
	maxBytes: number;
	maxFiles: number;
	keep: number;
	allowPrune: boolean;
};
export type MaintenanceSettings = {
	backupsFirst?: boolean;
	enabled: boolean;
	volume: string;
	timezone: string;
	windowFrom: string;
	windowTo: string;
	jobs: MaintenanceJob[];
};
export type MaintenanceState = {
	running: boolean;
	blocked: boolean;
	lastAttempt: string | null;
	lastOK: string | null;
	error: string;
	phase: string;
};
export type MaintenanceResult = {
	id: string;
	name: string;
	kind: string;
	target: string;
	started: string;
	finished: string;
	error: string;
};
export type MaintenanceStatus = {
	settings: MaintenanceSettings;
	jobs: { job: MaintenanceJob; state: MaintenanceState; wait?: string; next: string | null }[];
	history: MaintenanceResult[];
	available: boolean;
	error: string;
};
export type MonitorSettings = { enabled: boolean; volume: string; days: number; maxMiB: number };
export type MonitorStatus = { settings: MonitorSettings; last: string | null; error: string };
export type MonitorSample = {
	at: string;
	gap?: boolean;
	cpu: number;
	ramTotal: number;
	ramAvailable: number;
	disks: { name: string; readBps: number; writeBps: number; latencyMs: number | null }[];
	volumes: { name: string; used: number; total: number }[];
	networks: { name: string; receive: number; send: number }[];
};
export type Growth = {
	id: string;
	volume: string;
	recoveryVolume: string;
	arrayUUID: string;
	diskSerial: string;
	currentBytes?: number;
	plannedBytes?: number;
	originalDisks: number;
	targetDisks: number;
	phase: string;
	error: string;
	started: string;
};
export type GrowthStatus = { pending: boolean; active?: boolean; operation: Growth; error: string };
export type BackupView = {
	stateKnown?: boolean;
	job: { id: string; destination: { type: string } };
	state: {
		running: boolean;
		phase: string;
		lastOK: string | null;
		lastError: string;
		checkError: string;
		statusError?: string;
		applicationRestore?: {
			at: string;
			snapshot: string;
			target: string;
			verified: boolean;
			error: string;
		};
		lastRun?: string | null;
		lastCheck?: string | null;
		warning?: string;
		probe?: {
			at: string;
			snapshot: string;
			files: number;
			bytes: number;
			truncated: boolean;
			error: string;
		};
	};
	next: string | null;
};
export type Operations = {
	storageActive?: boolean;
	clones?: {
		id: string;
		phase: string;
		source: string;
		error?: string;
		vm: { name: string; volume: string };
	}[];
	unavailable?: string[];
	growthError?: string;
	uploads?: { id: string; name: string; size: number; offset: number; busy?: boolean }[];
	applicationRecovery?: { operation: { id: string; job: string } | null; active: boolean };
	assistant?: {
		id: string;
		title: string;
		updated: string;
		run?: { state: string; detail?: string };
	}[];
	automations?: {
		id: string;
		name: string;
		enabled: boolean;
		next?: string;
		kind?: string;
		pending?: boolean;
		detail?: string;
	}[];
	maintenance?: MaintenanceStatus;
	transfers: CopyJob[];
	backups?: BackupView[];
	docker?: Record<string, { state: string; output: string }>;
	vms?: VM[];
	growth?: GrowthStatus;
	diskTests?: { disk: string; phase: string; result: string; percent: number }[];
};
import type { MessageKey } from '#lib/i18n/index.svelte.ts';
export const operationError = (error: unknown, translate?: (key: MessageKey) => string) =>
	`${translate ? translate('ops.failed') + ' ' : ''}${error instanceof Error ? error.message : String(error)}`;
export function newMaintenanceJob(): MaintenanceJob {
	return {
		id: '',
		dependsOn: '',
		name: '',
		kind: 'raid-check',
		target: '',
		enabled: true,
		every: 'monthly',
		weekdays: [0],
		monthDay: 1,
		time: '03:00',
		missed: 'window',
		readData: 5,
		maxBytes: 64 * 1048576,
		maxFiles: 20,
		keep: 7,
		allowPrune: false,
	};
}
export function chartSegments(
	samples: MonitorSample[],
	value: (sample: MonitorSample) => number | null,
): { at: string; value: number }[][] {
	const segments: { at: string; value: number }[][] = [];
	let current: { at: string; value: number }[] = [];
	let previous = 0;
	for (const sample of samples) {
		const at = Date.parse(sample.at),
			v = value(sample);
		if (
			v === null ||
			!Number.isFinite(v) ||
			sample.gap ||
			(sample.gap === undefined && previous && at - previous > 150000)
		) {
			if (current.length) segments.push(current);
			current = [];
		}
		if (v !== null && Number.isFinite(v)) current.push({ at: sample.at, value: v });
		previous = at;
	}
	if (current.length) segments.push(current);
	return segments;
}
