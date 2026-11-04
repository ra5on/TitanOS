import { cronOfPreset, describe, type Translate } from '#lib/schedule.ts';

// Types and helpers of Tools › Scripts (package scripts in titand).

export type Trigger = 'manual' | 'schedule' | 'start' | 'shutdown';
export type Every = 'hourly' | 'daily' | 'weekly' | 'monthly' | 'custom';

export type Schedule = {
	every: Every;
	minute?: number;
	time?: string;
	weekday?: number;
	monthDay?: number;
	cron?: string;
};

export type Outcome =
	'running' | 'ok' | 'failed' | 'timeout' | 'canceled' | 'error' | 'interrupted';

export type ScriptRun = {
	trigger: Trigger;
	started: string;
	durationMs: number;
	exit?: number;
	outcome: Outcome;
	error?: string;
	output?: string;
	truncated?: boolean;
};

export type Script = {
	id: string;
	name: string;
	description: string;
	trigger: Trigger;
	schedule?: Schedule;
	changed: string;
};

export type ScriptView = Script & { running: boolean; last?: ScriptRun; next?: string };

export type ScriptsStatus = {
	volume: string;
	chosen: boolean;
	available: boolean;
	disabled: boolean;
	error?: string;
	/** Zone of the NAS: times are shown in it, as the schedules run in it. */
	timezone?: string;
	volumes: string[];
	scripts: ScriptView[];
};

export type ScriptOutput = {
	running: boolean;
	text: string;
	next: number;
	skipped: boolean;
	run?: ScriptRun;
};

export type Draft = {
	name: string;
	description: string;
	trigger: Trigger;
	/** The schedule as cron expression (lib/schedule.ts, ScheduleField). */
	cron: string;
	content: string;
};

export const TRIGGERS: Trigger[] = ['manual', 'schedule', 'start', 'shutdown'];

export function newDraft(): Draft {
	return { name: '', description: '', trigger: 'manual', cron: '', content: '#!/bin/bash\n' };
}

export function draftOf(script: Script, content: string): Draft {
	return {
		name: script.name,
		description: script.description,
		trigger: script.trigger,
		cron: script.schedule ? cronOfPreset(script.schedule) : '',
		content,
	};
}

/** The body for POST/PUT. The schedule always goes as cron expression. */
export function draftBody(draft: Draft) {
	return {
		name: draft.name.trim(),
		description: draft.description,
		trigger: draft.trigger,
		schedule:
			draft.trigger === 'schedule'
				? ({ every: 'custom', cron: draft.cron.trim() } satisfies Schedule)
				: undefined,
		content: draft.content,
	};
}

/** What the list says about when a script runs. */
export function whenText(
	script: Pick<Script, 'trigger' | 'schedule'>,
	tr: Translate,
	join: (items: string[]) => string,
): string {
	if (script.trigger !== 'schedule' || !script.schedule)
		return tr(`scripts.trigger.${script.trigger}`);
	return describe(cronOfPreset(script.schedule), tr, join);
}

/** The chip class of an outcome (always shown with its text). */
export function outcomeTone(outcome: Outcome): 'ok' | 'error' | 'warn' | 'info' {
	if (outcome === 'ok') return 'ok';
	if (outcome === 'running') return 'info';
	if (outcome === 'canceled' || outcome === 'interrupted') return 'warn';
	return 'error';
}

/** Duration in seconds or minutes, short. */
export function shortDuration(ms: number): string {
	if (ms < 1000) return `${ms} ms`;
	const s = Math.round(ms / 1000);
	if (s < 120) return `${s} s`;
	const m = Math.floor(s / 60);
	return `${m} min ${s % 60} s`;
}

/** The result of a run as one line: outcome, exit code of a failure, duration. */
export function runLabel(
	run: Pick<ScriptRun, 'outcome' | 'exit' | 'durationMs'>,
	tr: Translate,
	withDuration = false,
): string {
	const parts = [tr(`scripts.outcome.${run.outcome}`)];
	if (run.exit !== undefined && run.outcome === 'failed')
		parts[0] += ` (${tr('scripts.exit', { code: run.exit })})`;
	if (withDuration && run.durationMs) parts.push(shortDuration(run.durationMs));
	return parts.join(' · ');
}
