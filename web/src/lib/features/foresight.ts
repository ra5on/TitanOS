// Gruppe „Vorausschau“ (PLAN 54-V): Was-wäre-wenn and Hardware-Berater,
// NAS-wide, owners and administrators, both off by default.
import { api } from '../api.ts';

export type WhatIf = { enabled?: boolean };
/** predict and bottlenecks are on unless switched off (the server fills them). */
export type Advisor = {
	enabled?: boolean;
	predict?: boolean;
	bottlenecks?: boolean;
	prices?: boolean;
	monthly?: boolean;
	at?: string;
};

export type ScenarioKind = 'disk' | 'full' | 'level';
export type Scenario = { kind: ScenarioKind; disk?: string; volume?: string; level?: string };

export type Reason = { code: string; disk?: string; value?: number };
export type Gap = {
	share: string;
	never?: boolean;
	unknown?: boolean;
	notYet?: boolean;
	days?: number;
	job?: string;
	bytes?: number;
};
export type Safe = { share: string; job: string; at: string };
export type Advice = {
	code: string;
	share?: string;
	job?: string;
	volume?: string;
	disk?: string;
	bytes?: number;
	kind?: string;
	count?: number;
	level?: string;
};
export type LevelFacts = { level: string; disks: number; tolerates: number; usableBytes: number };
export type WhatIfResult = {
	scenario: Scenario;
	volume?: string;
	level?: string;
	filesystem?: string;
	disks?: number;
	disk?: string;
	serial?: string;
	outcome?: string;
	failed?: number;
	dataAtRiskBytes: number;
	rebuild?: { seconds: number; bytes: number; bps: number; measured: boolean; auto?: boolean };
	risk?: { level: 'normal' | 'elevated' | 'high'; reasons: Reason[] };
	gaps: Gap[];
	safe: Safe[];
	advice: Advice[];
	full?: {
		freeBytes: number;
		usedPct: number;
		dailyBytes?: number;
		daysToFull?: number;
		effects: string[];
	};
	compare?: {
		from: LevelFacts;
		to: LevelFacts;
		needDisks: number;
		usedBytes: number;
		fits: boolean;
		way: 'profile' | 'newVolume';
	};
};

export type Finding = {
	area: 'disk' | 'memory' | 'volume' | 'cpu' | 'gpu' | 'temperature';
	subject: string;
	level: 'ok' | 'watch' | 'tight' | 'fail';
	code: string;
	also?: string[];
	from?: number;
	to?: number;
	weeks?: number;
	hours?: number;
	percent?: number;
	days?: number;
	bytes?: number;
	totalBytes?: number;
	suggestBytes?: number;
	celsius?: number;
	model?: string;
	kind?: string;
	sizeBytes?: number;
	volume?: string;
	label?: string;
};
export type Offer = { title: string; url: string; price?: string };
export type AdvisorView = {
	settings: Required<Advisor>;
	saved?: {
		report: { at: string; findings: Finding[] };
		offers?: Record<string, Offer[]>;
		history: boolean;
		lastRun: string;
	};
	running: boolean;
	web: boolean;
};

export const whatIfState = () => api.get<{ enabled: boolean; levels: string[] }>('/agent/whatif');
export const playWhatIf = (s: Scenario) => api.post<WhatIfResult>('/agent/whatif', s);
export const loadAdvisor = () => api.get<AdvisorView>('/agent/advisor');
export const runAdvisor = () => api.post<AdvisorView>('/agent/advisor/run', {});

let state: Promise<{ enabled: boolean; levels: string[] }> | null = null;
/** Whether "Durchspielen" is offered; asked once per page load, off on any error (403 for users). */
export function whatIfOn(): Promise<{ enabled: boolean; levels: string[] }> {
	state ??= whatIfState().catch(() => ({ enabled: false, levels: [] }));
	return state;
}
/** Forget the cached answer (after switching the function). */
export function forgetWhatIf() {
	state = null;
}

/** Badge of a finding: the text key and the tone (never only the colour). */
export function findingTone(level: Finding['level']): 'ok' | 'warn' | 'bad' {
	return level === 'ok' ? 'ok' : level === 'fail' ? 'bad' : 'warn';
}

/** Years in service from power-on hours, one decimal. */
export function yearsOf(hours: number): number {
	return Math.round((hours / (24 * 365)) * 10) / 10;
}

/** The tone of a what-if outcome for the headline note. */
export function outcomeTone(r: WhatIfResult): 'ok' | 'warn' | 'bad' {
	switch (r.outcome) {
		case 'lost':
		case 'possiblyLost':
		case 'cacheLost':
			return 'bad';
		case 'noRedundancy':
		case 'alreadyFailed':
			return 'warn';
	}
	if (r.full) return (r.full.daysToFull ?? Infinity) < 60 || r.full.usedPct >= 90 ? 'warn' : 'ok';
	if (r.compare) return r.compare.fits ? 'ok' : 'warn';
	return 'ok';
}

/** Scenarios offered for a volume: each member fails, full, and the levels that differ. */
export function scenariosFor(
	volume: { name: string; level: string; filesystem: string; members: { disk: string }[] },
	levels: string[],
): Scenario[] {
	const out: Scenario[] = volume.members
		.filter((m) => m.disk)
		.map((m) => ({ kind: 'disk', disk: m.disk, volume: volume.name }));
	out.push({ kind: 'full', volume: volume.name });
	for (const level of levels) {
		if (level === volume.level) continue;
		// Btrfs has no RAID 5/6 in TitanOS; mdadm no raid1c3.
		if (volume.filesystem !== 'btrfs' && level === 'raid1c3') continue;
		out.push({ kind: 'level', volume: volume.name, level });
	}
	return out;
}

export const scenarioKey = (s: Scenario) =>
	`${s.kind}:${s.disk ?? ''}:${s.volume ?? ''}:${s.level ?? ''}`;
