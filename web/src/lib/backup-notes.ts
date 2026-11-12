// What the last run of a backup job has to tell (PLAN 79-9, 79-13): the
// codes of state.problems and state.notices (backend/internal/backup/copy.go)
// and how the page groups them. The words are backup.note.<code>.*.
import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';

export type BackupNote = { code: string; count: number; paths: string[] | null };

/** What a readable copy could not save (state.problems). */
export const PROBLEM_CODES = [
	'source_unavailable',
	'folder_unavailable',
	'read_error',
	'case_conflict',
	'too_large',
	'name_not_allowed',
	'changed',
	'vm_disk_changed',
	'vm_disk_busy',
	'vm_disk_passthrough',
	'vm_disk_backing_chain',
	'vm_disk_no_space',
	'vm_disk_snapshot_failed',
	'vm_disk_pause_failed',
	'vm_disk_pause_timeout',
	'vm_disk_undo_failed',
	'vm_disk_raw_image',
	'vm_disk_snapshot_unproven',
	'link_unsupported',
	'mirror_failed',
	'other',
] as const;

/** No failure, nothing to repair (state.notices). */
export const NOTICE_CODES = [
	'vm_disk_skipped',
	'vm_disk_choice_needed',
	'vm_disk_crash_consistent',
	'vm_disk_paused',
] as const;

const KNOWN = new Set<string>([...PROBLEM_CODES, ...NOTICE_CODES]);

/** Codes whose entry leads to the job's choice for disks of running VMs. */
const CHOICE = new Set(['vm_disk_skipped', 'vm_disk_choice_needed', 'vm_disk_changed']);

export type NoteGroup = {
	kind: 'problem' | 'notice';
	/** The code the texts are looked up with; "other" for one this page does not know. */
	code: string;
	count: number;
	/** The entries the NAS listed (at most 20); count says how many there are. */
	paths: string[];
	/** count minus the listed entries. */
	more: number;
	/** The entry offers to change the job's choice for disks of running VMs. */
	choice: boolean;
};

export type NotesState = {
	warning?: string;
	problems?: BackupNote[] | null;
	notices?: BackupNote[] | null;
};

function group(kind: NoteGroup['kind'], note: BackupNote): NoteGroup {
	const paths = (note.paths ?? []).filter((path) => typeof path === 'string' && path !== '');
	const count = Math.max(Number(note.count) || 0, paths.length);
	return {
		kind,
		code: KNOWN.has(note.code) ? note.code : 'other',
		count,
		paths,
		more: count - paths.length,
		choice: CHOICE.has(note.code),
	};
}

/** Problems first, then notices, each in the order the NAS sent them. */
export function noteGroups(state: NotesState): NoteGroup[] {
	return [
		...(state.problems ?? []).filter((n) => n && n.code).map((n) => group('problem', n)),
		...(state.notices ?? []).filter((n) => n && n.code).map((n) => group('notice', n)),
	];
}

/**
 * The English sentence of a NAS before 0.77 (state.warning), only when the
 * state has no codes: with codes the page says the same in its language.
 */
export function legacyWarning(state: NotesState): string {
	return noteGroups(state).length === 0 ? (state.warning ?? '') : '';
}

/** Whether the last run has anything to tell besides "done". */
export function hasNotes(state: NotesState): boolean {
	return noteGroups(state).length > 0 || !!state.warning;
}

/** How many entries a group shows before "Alle anzeigen". */
export const NOTE_PREVIEW = 3;

/** The last part of a path: "Volume-nvme/vms/debian/vdisk1.qcow2" → "vdisk1.qcow2". */
export function baseName(path: string): string {
	return path.split('/').filter(Boolean).pop() ?? path;
}

/**
 * The notes of the last run in one line, in the language of the page:
 * "1 VM-Platte übersprungen, 2 Verknüpfungen nicht gesichert". The English
 * sentence of a NAS before 0.77 only where the state has no codes.
 */
export function notesLine(state: NotesState): string {
	const groups = noteGroups(state);
	if (!groups.length) return legacyWarning(state);
	return groups
		.map((group) =>
			t(`backup.note.${group.code}.${group.count === 1 ? 'one' : 'many'}` as MessageKey, {
				count: group.count,
			}),
		)
		.join(', ');
}
