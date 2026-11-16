// The form of the morning report on the page "Geplant" (PLAN 49) and when
// it takes the saved report anew. 1.01: the form went back to the saved
// state whenever the list was read again – after switching another task,
// "Jetzt ausführen" or saving another one – and lost what was typed.
import type { Report, Schedule } from '#lib/agent.ts';

export type MorningForm = { report: Report; at: string; days: number[]; on: boolean };

/** The form for a saved morning report. */
export function morningForm(s: Schedule): MorningForm {
	return {
		report: { ...(s.report ?? {}) },
		at: s.at ?? '07:00',
		days:
			s.every === 'weekdays'
				? [...(s.weekdays ?? [])]
				: s.every === 'daily'
					? [0, 1, 2, 3, 4, 5, 6]
					: [1, 2, 3, 4, 5],
		on: s.enabled,
	};
}

/**
 * What of a saved morning report the form shows. The form is set anew only
 * when this changes, not when the list is read again with the next run,
 * the last run or the words of another task changed.
 */
export function morningKey(s: Schedule): string {
	return JSON.stringify(morningForm(s));
}
