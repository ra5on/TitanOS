// The assistant for swapping a disk (Storage › Swap a disk,
// docs/DISK-SWAP.md): which way a disk of a volume can be swapped.
import type { Disk, Volume } from './api.ts';

/**
 * rebuild: the disk failed or is missing; the new disk takes its place and
 *   is rebuilt (AddDisk), the old one is removed.
 * live: the disk still works; the new disk copies it while the volume stays
 *   redundant (mdadm --replace), then the old one is removed.
 * addRemove: Btrfs with a working disk: add the new disk, then remove the old.
 * impossible: RAID 0 or a single-disk Btrfs has no copy to rebuild from.
 */
export type SwapMode = 'rebuild' | 'live' | 'addRemove' | 'impossible';

/** A member of the volume and how it can be swapped. */
export type SwapCandidate = {
	disk: string;
	state: string;
	present: boolean;
	warn: boolean;
	mode: SwapMode;
};

/** The members of a volume, those that need swapping first. */
export function swapCandidates(volume: Volume, disks: Disk[]): SwapCandidate[] {
	const noCopy = volume.level === 'raid0' && volume.filesystem !== 'btrfs';
	const list: SwapCandidate[] = volume.members
		.map((member) => {
			const disk = disks.find((d) => d.name === member.disk);
			const failed = member.state === 'faulty' || !disk;
			const warn = !!disk?.smart && (!disk.smart.healthy || disk.smart.errors > 0);
			let mode: SwapMode;
			if (noCopy) mode = 'impossible';
			else if (failed) mode = 'rebuild';
			else if (volume.filesystem === 'btrfs')
				mode = volume.members.length > 1 ? 'addRemove' : 'impossible';
			else if (volume.state !== 'ok' || member.state !== 'active') mode = 'impossible';
			else mode = 'live';
			return { disk: member.disk, state: member.state, present: !!disk, warn, mode };
		})
		.sort(
			(a, b) =>
				Number(b.mode === 'rebuild') - Number(a.mode === 'rebuild') ||
				Number(b.warn) - Number(a.warn) ||
				a.disk.localeCompare(b.disk),
		);
	// mdadm does not list a disk that is gone: offer its empty place.
	if (!noCopy && volume.state === 'degraded' && !list.some((c) => c.mode === 'rebuild'))
		list.unshift({ disk: '', state: 'missing', present: false, warn: false, mode: 'rebuild' });
	return list;
}

/** Volumes missing a disk come first: the assistant offers them at once. */
export function needsSwap(volume: Volume): boolean {
	return (
		volume.state === 'degraded' ||
		volume.state === 'failed' ||
		volume.members.some((m) => m.state === 'faulty')
	);
}

/** Disks that can take the place of old: free or used, not in a volume, at least as large. */
export function replacementDisks(disks: Disk[], old: Disk | undefined, smallest: number): Disk[] {
	const need = Math.max(old?.sizeBytes ?? 0, smallest);
	return disks.filter(
		(d) =>
			d.name !== old?.name &&
			!d.unavailableReason &&
			(d.role === 'free' || d.role === 'used') &&
			!d.mounted &&
			!d.testing &&
			d.sizeBytes >= need,
	);
}

/** Disks that showed up since the assistant started looking. */
export function newlyAttached(before: string[], now: Disk[]): Disk[] {
	return now.filter((d) => !before.includes(d.identity ?? d.name));
}
