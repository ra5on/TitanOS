import type { Volume } from '#lib/api.ts';

/** An encrypted volume that waits for its password (internal/storage crypt.go). */
export function isLocked(volume: Pick<Volume, 'encryption'>): boolean {
	return !!volume.encryption?.locked;
}

/** Locked is a waiting state: a healthy locked volume is shown as a warning. */
export function lockedTone(volume: Pick<Volume, 'encryption' | 'state'>): 'warn' | null {
	return isLocked(volume) && volume.state === 'ok' ? 'warn' : null;
}

/** What the volume card offers on a locked volume: an mdadm array still
 * takes disks and checks (below the encryption), a Btrfs volume does not. */
export function lockedBlocks(volume: Pick<Volume, 'encryption' | 'filesystem'>): boolean {
	return isLocked(volume) && volume.filesystem === 'btrfs';
}
