// The partition editor of a drive. The rules mirror backend/internal/usb/
// partitions.go; the server checks them again right before it writes.

import {
	driveFilesystems,
	type Drive,
	type DriveFilesystem,
	type FormatJob,
} from './drive-format.ts';

export type PartitionSlot = {
	number: number;
	device: string;
	start: number;
	size: number;
	type: string;
	name?: string;
	fsType: string;
	label: string;
	uuid: string;
	mounted: boolean;
	reason?: string;
	growBy: number;
	renamable: boolean;
};
export type PartitionGap = { start: number; size: number; after: number };
export type PartitionLayout = {
	disk: Drive;
	table: '' | 'gpt' | 'dos' | string;
	sectorSize: number;
	reason?: string;
	partitions: PartitionSlot[];
	gaps: PartitionGap[];
	job: FormatJob | null;
};

export const MiB = 2 ** 20;

/** A row of the bar and the table: a partition or free space, by position. */
export type Segment =
	| { kind: 'part'; start: number; size: number; part: PartitionSlot; percent: number }
	| { kind: 'gap'; start: number; size: number; gap: PartitionGap; percent: number };

/** Partitions and gaps in the order they lie on the drive. Each gets a
 * share of the bar; tiny ones stay visible with at least 2 %. */
export function segments(layout: PartitionLayout): Segment[] {
	const total = Math.max(1, layout.disk.size);
	const list: Segment[] = [
		...layout.partitions.map((part) => ({
			kind: 'part' as const,
			start: part.start,
			size: part.size,
			part,
			percent: 0,
		})),
		...layout.gaps.map((gap) => ({
			kind: 'gap' as const,
			start: gap.start,
			size: gap.size,
			gap,
			percent: 0,
		})),
	].sort((a, b) => a.start - b.start);
	for (const s of list) s.percent = Math.max(2, (s.size / total) * 100);
	return list;
}

const gptTypes: Record<string, string> = {
	'C12A7328-F81F-11D2-BA4B-00A0C93EC93B': 'EFI',
	'EBD0A0A2-B9E5-4433-87C0-68B6B72699C7': 'Microsoft basic data',
	'E3C9E316-0B5C-4DB8-817D-F92DF00215AE': 'Microsoft reserved',
	'DE94BBA4-06D1-4D40-A16A-BFD50179D6AC': 'Windows recovery',
	'0FC63DAF-8483-4772-8E79-3D69D8477DE4': 'Linux',
	'A19D880F-05FC-4D3B-A006-743F0F84911E': 'Linux RAID',
	'E6D6D379-F507-44C2-A23C-238F2A3DF928': 'Linux LVM',
	'0657FD6D-A4AB-43C4-84E5-0933C84B4F4F': 'Linux swap',
	'48465300-0000-11AA-AA11-00306543ECAC': 'Apple HFS+',
	'7C3457EF-0000-11AA-AA11-00306543ECAC': 'Apple APFS',
	'21686148-6449-6E6F-744E-656564454649': 'BIOS boot',
};
const mbrTypes: Record<string, string> = {
	'5': 'Extended',
	f: 'Extended',
	'85': 'Linux extended',
	'6': 'FAT16',
	e: 'FAT16',
	b: 'FAT32',
	c: 'FAT32',
	'7': 'NTFS/exFAT',
	'83': 'Linux',
	'82': 'Linux swap',
	fd: 'Linux RAID',
	'8e': 'Linux LVM',
	ef: 'EFI',
	ee: 'GPT protective',
};

/** A readable name of a partition type; unknown ones as they are. */
export function typeName(table: string, type: string): string {
	if (table === 'gpt') return gptTypes[type.toUpperCase()] ?? type;
	return mbrTypes[type.toLowerCase()] ?? `0x${type}`;
}

/** Filesystems that grow with their partition: FAT and exFAT do not. */
export const growableFilesystems = ['', 'ext2', 'ext3', 'ext4', 'xfs', 'btrfs', 'ntfs'];

/** Decimal megabyte: sizes are typed in MB like everywhere in the interface. */
export const MB = 1000 ** 2;

/** Bytes of a size typed in MB, rounded down to a whole MiB (partitions start and end there), or NaN. */
export function mbToBytes(text: string): number {
	const value = Number(text.trim().replace(',', '.'));
	if (!Number.isFinite(value) || value <= 0) return NaN;
	return Math.floor((value * MB) / MiB) * MiB;
}

/** Why a new partition does not fit, as an i18n key, or ''. */
export function createProblem(
	gap: PartitionGap,
	size: number,
	fs: DriveFilesystem | '',
	partitions: number,
	table: string,
): string {
	if (table === 'dos' && partitions >= 4) return 'files.partitions.mbrFull';
	if (Number.isNaN(size) || size < MiB) return 'files.partitions.sizeInvalid';
	if (size > gap.size) return 'files.partitions.sizeTooLarge';
	if (fs) {
		const rule = driveFilesystems[fs];
		if (size < rule.min) return 'files.formatTooSmall';
		if (rule.max && size > rule.max) return 'files.formatFatTooLarge';
	}
	return '';
}

/** Why a partition cannot grow to a new size, or ''. Nothing is shrunk. */
export function growProblem(part: PartitionSlot, size: number): string {
	if (!growableFilesystems.includes(part.fsType)) return 'files.partitions.growFs';
	if (part.growBy < MiB) return 'files.partitions.growNoSpace';
	if (Number.isNaN(size) || size <= part.size || size > part.size + part.growBy)
		return 'files.partitions.growSize';
	return '';
}

/** The largest size a partition can grow to, ending on a whole MiB. */
export function growLimit(part: PartitionSlot): number {
	return Math.floor((part.start + part.size + part.growBy) / MiB) * MiB - part.start;
}

/** The same physical drive after its ID changed with its content. */
export function sameDrive(a: Drive, b: Drive): boolean {
	return a.device === b.device && a.serial === b.serial && a.model === b.model && a.size === b.size;
}

/** Partition editor jobs; a whole-drive format has no action. */
export function partitionJob(job: FormatJob | null | undefined): boolean {
	return !!job && !!job.action;
}
