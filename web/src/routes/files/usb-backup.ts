export type BackupState = {
	running: boolean;
	percent: number;
	bytesDone: number;
	bytesTotal: number;
	lastOK: string | null;
	lastError: string;
	warning: string;
	/** Links and special files left out by the last copy (0.38.1). */
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
