// Restoring whole folders from a Btrfs snapshot: request, preview and the
// background job as the API reports them.

export type FolderRestoreMode = 'original' | 'copy';
export type FolderConflict = 'skip' | 'replace' | 'rename';

export type FolderRestoreRequest = {
	uuid?: string;
	path: string;
	mode: FolderRestoreMode;
	conflict?: FolderConflict;
};

export type FolderRestorePreview = {
	uuid: string;
	path: string;
	mode: FolderRestoreMode;
	conflict: FolderConflict;
	target: string;
	files: number;
	folders: number;
	links: number;
	special: number;
	bytes: number;
	conflicts: number;
	partial: boolean;
};

export type FolderRestoreJob = {
	id: string;
	user: string;
	volume: string;
	snapshot: string;
	path: string;
	mode: FolderRestoreMode;
	conflict: FolderConflict;
	target: string;
	state: 'running' | 'done' | 'failed' | 'cancelled';
	phase: 'counting' | 'copying';
	totalFiles: number;
	totalBytes: number;
	files: number;
	bytes: number;
	copied: number;
	cloned: number;
	skipped: number;
	replaced: number;
	renamed: number;
	special: number;
	error?: string;
	started: string;
	finished?: string;
};

/** The request a preview and the start share; a new folder has no conflicts. */
export function folderRestoreRequest(
	path: string,
	mode: FolderRestoreMode,
	conflict: FolderConflict,
	uuid?: string,
): FolderRestoreRequest {
	const request: FolderRestoreRequest = { path, mode };
	if (mode === 'original') request.conflict = conflict;
	if (uuid) request.uuid = uuid;
	return request;
}

/** Percent done by bytes, else by files; unknown while counting. */
export function folderRestoreProgress(job: FolderRestoreJob): number | undefined {
	if (job.state === 'done') return 100;
	if (job.phase !== 'copying') return undefined;
	const [done, total] =
		job.totalBytes > 0 ? [job.bytes, job.totalBytes] : [job.files, job.totalFiles];
	if (total <= 0) return undefined;
	return Math.max(0, Math.min(100, (done / total) * 100));
}

/** Replacing existing files needs its own consent before the start. */
export function needsReplaceConsent(preview: FolderRestorePreview | null): boolean {
	return (
		!!preview &&
		preview.mode === 'original' &&
		preview.conflict === 'replace' &&
		preview.conflicts > 0
	);
}

/** Link into the file manager for the restored folder. */
export function restoredFolderLink(job: FolderRestoreJob): string {
	return '/files?' + new URLSearchParams({ root: `volume/${job.volume}`, path: job.target });
}
