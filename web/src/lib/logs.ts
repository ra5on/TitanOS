import { ApiError } from './api.ts';

export type LogEntry = { time: string; priority: number; source: string; message: string };
export type Logs = { entries: LogEntry[]; cursor: string };

// A type annotation on fetch does not validate a response. In particular, an
// HTML fallback or an unrelated JSON page must never become journal entries.
export function parseLogs(value: unknown): Logs {
	const fail = () => {
		throw new ApiError(200, 'invalid_response', 'Invalid journal response');
	};
	if (!value || typeof value !== 'object') return fail();
	const logs = value as Partial<Logs>;
	if (!Array.isArray(logs.entries) || typeof logs.cursor !== 'string') return fail();
	for (const entry of logs.entries) {
		if (
			!entry ||
			typeof entry !== 'object' ||
			typeof entry.time !== 'string' ||
			!Number.isFinite(Date.parse(entry.time)) ||
			!Number.isInteger(entry.priority) ||
			entry.priority < 0 ||
			entry.priority > 7 ||
			typeof entry.source !== 'string' ||
			typeof entry.message !== 'string'
		)
			return fail();
	}
	return logs as Logs;
}
