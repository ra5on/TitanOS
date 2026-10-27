import type { MessageKey } from '#lib/i18n/index.svelte.ts';

/** Something that uses a share's folder, or a reason it cannot be deleted. */
export type ShareUsage = { kind: string; name?: string; path?: string };

/** GET /shares/{name}/delete-data: what deleting the data would delete. */
export type ShareDataPreview = {
	share: string;
	volume: string;
	path: string;
	exists: boolean;
	files: number;
	dirs: number;
	bytes: number;
	complete: boolean;
	limit?: string;
	subvolume: boolean;
	blockers: ShareUsage[];
	warnings: ShareUsage[];
};

/** A deletion of share data as /operations lists it under shareDeletions. */
export type ShareDeletion = {
	id: string;
	share: string;
	volume: string;
	path: string;
	phase: 'checking' | 'removing' | 'waiting' | 'deleting' | 'done' | 'failed' | string;
	started: string;
	finished?: string;
	files: number;
	bytes: number;
	totalFiles: number;
	totalBytes: number;
	shareRemoved: boolean;
	dataKept: boolean;
	error?: string;
};

const kinds = [
	'vm',
	'docker',
	'docker-unknown',
	'backup-source',
	'backup-target',
	'share',
	'smb-session',
	'smb-unknown',
	'mount',
	'nested-subvolume',
	'subvolume-snapshots',
	'subvolume-unknown',
	'unsafe-path',
	'purge-running',
	'volume-snapshots',
	'count-limit',
] as const;

/** The text that explains a usage; unknown kinds get a generic one. */
export function usageKey(kind: string): MessageKey {
	return (
		(kinds as readonly string[]).includes(kind) ? `share.use.${kind}` : 'share.use.other'
	) as MessageKey;
}

/** Deleting may start only after a preview that found nothing in the way. */
export const canDeleteData = (preview: ShareDataPreview | null) =>
	!!preview && preview.blockers.length === 0;

/** Progress in percent while files are deleted; unknown before that. */
export function deletionPercent(job: ShareDeletion): number | undefined {
	if (job.phase !== 'deleting' || job.totalFiles <= 0) return undefined;
	return Math.max(0, Math.min(100, (job.files / job.totalFiles) * 100));
}
