// Codes a copy or move job reports for single entries (problems) and for the
// job as a whole (errorCode). titand sets them in backend/internal/files/
// copy_problems.go; the texts are files.copyProblem.* and files.copyError.*.
import type { MessageKey } from '#lib/i18n/index.svelte.ts';

const problemCodes = [
	'too_large',
	'name_too_long',
	'name_not_allowed',
	'read_error',
	'write_error',
	'io_error',
	'read_only',
	'permission',
	'missing',
	'changed',
	'changed_after_copy',
	'changed_target',
	'exists',
	'into_itself',
	'same_folder',
	'protected',
	'type_mismatch',
	'unreadable_folder',
	'recycle_failed',
	'remove_failed',
	'kept_skipped',
	'kept_link',
	'kept_special',
	'kept_internal',
	'kept_new',
	'kept_changed',
	'kept_target_changed',
	'kept_identical',
	'other',
] as const;

const errorCodes = [
	'no_space',
	'unavailable',
	'store',
	'into_itself',
	'same_folder',
	'rebooted',
	'missing',
	'permission',
	'changed',
	'interrupted',
	'unknown_outcome',
	'other',
] as const;

/** The text key for the problem of one entry. */
export function problemKey(code: string): MessageKey {
	const known = (problemCodes as readonly string[]).includes(code) ? code : 'other';
	return `files.copyProblem.${known}` as MessageKey;
}

/** The text key for why a job stopped, or "" when the code is unknown. */
export function copyErrorKey(code: string | undefined): MessageKey | '' {
	if (!code || !(errorCodes as readonly string[]).includes(code)) return '';
	return `files.copyError.${code}` as MessageKey;
}

/** Entries a move left at the source on purpose, rather than failures. */
export const isKept = (code: string) => code.startsWith('kept_');
