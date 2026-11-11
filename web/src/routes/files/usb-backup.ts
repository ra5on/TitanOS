import type { BackupNote } from '#lib/backup-notes.ts';

export type BackupState = {
	running: boolean;
	percent: number;
	bytesDone: number;
	bytesTotal: number;
	lastOK: string | null;
	lastError: string;
	warning: string;
	/** What the last copy could not save and what else it tells, as codes (0.77). */
	problems?: BackupNote[] | null;
	notices?: BackupNote[] | null;
	/** Entries the last copy could not save. */
	skipped?: number;
};
export type USBBackup = {
	state?: BackupState;
	job: { id: string; destination: { type: string; deviceId?: string; folder?: string } };
};

/** Folder of a USB backup on its drive, relative to the drive's root. */
export function backupFolder(backup: USBBackup) {
	return backup.job.destination.folder || 'backups/' + backup.job.id;
}
