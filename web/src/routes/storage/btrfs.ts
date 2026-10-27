import type { BtrfsProfile, Volume } from '#lib/api.ts';

const COPIES: Record<BtrfsProfile, number> = { single: 1, raid1: 2, raid1c3: 3 };

/**
 * Space a Btrfs profile gives on disks of these sizes, the same estimate as
 * btrfsUsable in internal/storage: every copy of a block lies on another
 * disk, so the largest disks fill only as far as the others hold the copies.
 */
export function btrfsUsable(profile: BtrfsProfile, sizes: number[]): number {
	if (sizes.length === 0) return 0;
	const sorted = [...sizes].sort((a, b) => a - b);
	const sum = sorted.reduce((total, size) => total + size, 0);
	const copies = COPIES[profile];
	if (copies <= 1) return sum;
	const rest = sorted.slice(0, Math.max(0, sorted.length - (copies - 1))).reduce((total, size) => total + size, 0);
	return Math.min(Math.floor(sum / copies), rest);
}

export const BTRFS_MIN: Record<BtrfsProfile, number> = { single: 1, raid1: 2, raid1c3: 3 };

/** The profile of a Btrfs volume; volumes from before 0.29.0 are RAID 1. */
export function profileOf(volume: Volume): BtrfsProfile {
	return volume.level in BTRFS_MIN ? (volume.level as BtrfsProfile) : 'raid1';
}

/**
 * Whether a volume takes another disk: a missing member is replaced, a
 * complete Btrfs volume grows, mdadm arrays take a spare. The same rule
 * as internal/storage AddDisk.
 */
export function takesDisk(volume: Volume): boolean {
	if (volume.filesystem === 'btrfs') return volume.state === 'ok' || volume.state === 'degraded';
	return volume.level !== 'raid0' && volume.state !== 'failed' && volume.state !== 'missing';
}

/** Whether a disk can be taken out of a complete Btrfs volume. */
export function canShrink(volume: Volume): boolean {
	return volume.filesystem === 'btrfs' && volume.state === 'ok' && volume.members.length > BTRFS_MIN[profileOf(volume)];
}
