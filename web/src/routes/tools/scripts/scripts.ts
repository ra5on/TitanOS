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
	schedule: Schedule;
	content: string;
};

export const TRIGGERS: Trigger[] = ['manual', 'schedule', 'start', 'shutdown'];
export const EVERY: Every[] = ['hourly', 'daily', 'weekly', 'monthly', 'custom'];

export function newDraft(): Draft {
	return {
		name: '',
		description: '',
		trigger: 'manual',
		schedule: { every: 'daily', minute: 0, time: '03:00', weekday: 1, monthDay: 1, cron: '' },
		content: '#!/bin/bash\n',
	};
}

export function draftOf(script: Script, content: string): Draft {
	const base = newDraft();
	return {
		name: script.name,
		description: script.description,
		trigger: script.trigger,
		schedule: { ...base.schedule, ...(script.schedule ?? {}) },
		content,
	};
}

/** The body for POST/PUT: only the fields of the chosen frequency. */
export function draftBody(draft: Draft) {
	const s = draft.schedule;
	const schedule: Schedule | undefined =
		draft.trigger !== 'schedule'
			? undefined
			: s.every === 'hourly'
				? { every: 'hourly', minute: Number(s.minute ?? 0) }
				: s.every === 'daily'
					? { every: 'daily', time: s.time }
					: s.every === 'weekly'
						? { every: 'weekly', time: s.time, weekday: Number(s.weekday ?? 0) }
						: s.every === 'monthly'
							? { every: 'monthly', time: s.time, monthDay: Number(s.monthDay ?? 1) }
							: { every: 'custom', cron: (s.cron ?? '').trim() };
	return {
		name: draft.name.trim(),
		description: draft.description,
		trigger: draft.trigger,
		schedule,
		content: draft.content,
	};
}

/** What the list says about when a script runs: a message key and its values. */
export function whenText(
	script: Pick<Script, 'trigger' | 'schedule'>,
	weekday: (day: number) => string = String,
): {
	key: string;
	values?: Record<string, string | number>;
} {
	if (script.trigger !== 'schedule' || !script.schedule)
		return { key: `scripts.trigger.${script.trigger}` };
	const s = script.schedule;
	switch (s.every) {
		case 'hourly':
			return {
				key: 'scripts.when.hourly',
				values: { minute: String(s.minute ?? 0).padStart(2, '0') },
			};
		case 'daily':
			return { key: 'scripts.when.daily', values: { time: s.time ?? '' } };
		case 'weekly':
			return {
				key: 'scripts.when.weekly',
				values: { time: s.time ?? '', day: weekday(s.weekday ?? 0) },
			};
		case 'monthly':
			return { key: 'scripts.when.monthly', values: { time: s.time ?? '', day: s.monthDay ?? 1 } };
		default:
			return { key: 'scripts.when.custom', values: { cron: s.cron ?? '' } };
	}
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

type Translate = (key: string, values?: Record<string, string | number>) => string;

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
