// The power schedule of a VM: start it and shut it down at set times on
// chosen weekdays. The backend carries it out; the page only edits it.

export type VMPowerRule = { action: 'start' | 'stop'; days: number[]; time: string };
export type VMPowerSchedule = {
	enabled: boolean;
	rules: VMPowerRule[];
	/** Seconds a scheduled shutdown waits for the guest. */
	stopTimeout?: number;
	/** Switch the VM off when the guest did not shut down in time. */
	forceOff?: boolean;
};
/** What the backend says about the schedule of a VM right now. */
export type VMScheduleView = {
	nextStart?: string;
	nextStop?: string;
	waiting?: 'start' | 'stop';
	waitReason?: 'volume' | 'busy';
};
export type ScheduleDraft = {
	enabled: boolean;
	rules: VMPowerRule[];
	stopTimeout: number;
	forceOff: boolean;
};
export type ScheduleProblem =
	| { key: 'noRule' }
	| { key: 'many' }
	| { key: 'clash' }
	| { key: 'days'; n: number }
	| { key: 'time'; n: number };

export const MAX_RULES = 16;
export const DEFAULT_STOP_TIMEOUT = 300;
/** Offered waiting times in seconds. */
export const STOP_TIMEOUTS = [60, 120, 300, 600, 900, 1800, 3600];
/** Weekdays as the week is shown: Monday first, Sunday = 0. */
export const WEEK = [1, 2, 3, 4, 5, 6, 0];

const CLOCK = /^([01]\d|2[0-3]):[0-5]\d$/;

/** A copy of a stored schedule to edit, with the defaults filled in. */
export function scheduleDraft(stored?: VMPowerSchedule | null): ScheduleDraft {
	return {
		enabled: stored?.enabled ?? false,
		rules: (stored?.rules ?? []).map((rule) => ({ ...rule, days: [...rule.days] })),
		stopTimeout: stored?.stopTimeout || DEFAULT_STOP_TIMEOUT,
		forceOff: stored?.forceOff ?? false,
	};
}

/** A new rule: the other action than the last one, on workdays. */
export function newRule(rules: VMPowerRule[]): VMPowerRule {
	const last = rules.at(-1);
	return last?.action === 'start'
		? { action: 'stop', days: [...last.days], time: '22:00' }
		: { action: 'start', days: [1, 2, 3, 4, 5], time: '07:00' };
}

/** What keeps a schedule from being saved, in the order of the rules. */
export function scheduleProblems(draft: ScheduleDraft): ScheduleProblem[] {
	const problems: ScheduleProblem[] = [];
	if (draft.enabled && draft.rules.length === 0) problems.push({ key: 'noRule' });
	if (draft.rules.length > MAX_RULES) problems.push({ key: 'many' });
	const at = new Map<string, string>();
	let clash = false;
	draft.rules.forEach((rule, index) => {
		if (!CLOCK.test(rule.time)) problems.push({ key: 'time', n: index + 1 });
		if (rule.days.length === 0) problems.push({ key: 'days', n: index + 1 });
		for (const day of rule.days) {
			const key = `${day} ${rule.time}`;
			const other = at.get(key);
			if (other && other !== rule.action) clash = true;
			at.set(key, rule.action);
		}
	});
	if (clash) problems.push({ key: 'clash' });
	return problems;
}

/** What is sent to the backend: weekdays sorted and once each. */
export function schedulePayload(draft: ScheduleDraft): VMPowerSchedule {
	return {
		enabled: draft.enabled,
		rules: draft.rules.map((rule) => ({
			action: rule.action,
			time: rule.time,
			days: [...new Set(rule.days)].sort((a, b) => a - b),
		})),
		stopTimeout: draft.stopTimeout,
		forceOff: draft.forceOff,
	};
}

/** The next scheduled start or stop, whichever comes first. */
export function nextScheduled(
	view: VMScheduleView | null | undefined,
): { action: 'start' | 'stop'; at: string } | null {
	const candidates: { action: 'start' | 'stop'; at: string }[] = [];
	if (view?.nextStart) candidates.push({ action: 'start', at: view.nextStart });
	if (view?.nextStop) candidates.push({ action: 'stop', at: view.nextStop });
	candidates.sort((a, b) => Date.parse(a.at) - Date.parse(b.at));
	return candidates[0] ?? null;
}

/** The waiting times to offer, with a stored value that is not among them. */
export function timeoutChoices(current: number): number[] {
	return STOP_TIMEOUTS.includes(current)
		? STOP_TIMEOUTS
		: [...STOP_TIMEOUTS, current].sort((a, b) => a - b);
}
