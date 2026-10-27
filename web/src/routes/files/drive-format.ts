// Formatting a whole unmanaged drive. The rules mirror backend/internal/usb/
// disks.go; the server checks them again.

export type DrivePartition = {
	device: string;
	fsType: string;
	label: string;
	size: number;
	mounted: boolean;
};
export type Drive = {
	id: string;
	device: string;
	model: string;
	serial: string;
	size: number;
	transport: string;
	kind: 'hdd' | 'ssd' | 'nvme' | 'sd' | 'removable';
	readOnly: boolean;
	table: string;
	partitions: DrivePartition[];
	formattable: boolean;
	reason?: 'readonly' | 'mounted' | 'inuse' | 'claimed' | 'formatting';
	claimed?: string;
};
export type FormatJob = {
	disk: string;
	device: string;
	name: string;
	filesystem: string;
	label: string;
	scheme: string;
	/** Set for partition editor jobs: table, create, delete, format, label, grow. */
	action?: 'table' | 'create' | 'delete' | 'format' | 'label' | 'grow';
	partition?: string;
	phase:
		| 'check'
		| 'wipe'
		| 'discard'
		| 'partition'
		| 'format'
		| 'label'
		| 'resize'
		| 'mount'
		| 'done'
		| 'failed';
	error?: string;
	started: string;
	finished?: string;
	id?: string;
};
export type DriveFilesystem = 'exfat' | 'vfat' | 'ntfs' | 'ext4' | 'xfs' | 'btrfs';
export type Scheme = 'auto' | 'mbr' | 'gpt';

const MiB = 2 ** 20;
const GiB = 2 ** 30;
const TiB = 2 ** 40;
const mbrLimit = 2 * TiB;

type Rule = { limit: number; windows: boolean; min: number; max?: number };
export const driveFilesystems: Record<DriveFilesystem, Rule> = {
	exfat: { limit: 15, windows: true, min: 8 * MiB },
	vfat: { limit: 11, windows: true, min: 8 * MiB, max: mbrLimit },
	ntfs: { limit: 32, windows: true, min: 8 * MiB },
	ext4: { limit: 16, windows: false, min: 8 * MiB },
	xfs: { limit: 12, windows: false, min: 300 * MiB },
	btrfs: { limit: 255, windows: false, min: 256 * MiB },
};
const fatExtra = " !#$%&'()-@^_`{}~";

/** The length the filesystem counts: bytes, or UTF-16 units for exFAT/NTFS. */
function labelLength(fs: DriveFilesystem, label: string) {
	if (fs === 'exfat' || fs === 'ntfs') return label.length;
	return new TextEncoder().encode(label).length;
}

/** Why a label does not fit, as an i18n key, or '' when it does. */
export function labelProblem(fs: DriveFilesystem, label: string): string {
	const rule = driveFilesystems[fs];
	for (const ch of label) {
		if (/\p{Cc}/u.test(ch)) return 'files.formatLabelChars';
		if (rule.windows && '"*/:<>?\\|'.includes(ch)) return 'files.formatLabelChars';
		if (fs === 'vfat' && !/^[A-Za-z0-9]$/.test(ch) && !fatExtra.includes(ch))
			return 'files.formatLabelFat';
	}
	return labelLength(fs, label) > rule.limit ? 'files.formatLabelLong' : '';
}

/** Why a filesystem cannot go on a drive of this size, or ''. */
export function sizeProblem(fs: DriveFilesystem, size: number, scheme: Scheme): string {
	const rule = driveFilesystems[fs];
	if (size < rule.min) return 'files.formatTooSmall';
	if (rule.max && size > rule.max) return 'files.formatFatTooLarge';
	if (scheme === 'mbr' && size > mbrLimit) return 'files.formatMbrTooLarge';
	return '';
}

/** The partition table that "auto" picks, like the server. */
export function resolvedScheme(fs: DriveFilesystem, size: number, scheme: Scheme): 'mbr' | 'gpt' {
	if (scheme !== 'auto') return scheme;
	return driveFilesystems[fs].windows && size <= mbrLimit ? 'mbr' : 'gpt';
}

/** SD cards as the SD standard has it (FAT32 up to 32 GiB, exFAT above);
 * everything else exFAT, which Windows, macOS and Linux read and write. */
export function suggestedFilesystem(drive: Drive): DriveFilesystem {
	if (drive.kind === 'sd' && drive.size <= 32 * GiB) return 'vfat';
	return 'exfat';
}

/** Content that is not plain files: RAID, LVM, encryption, other systems. */
const foreign = [
	'linux_raid_member',
	'LVM2_member',
	'crypto_LUKS',
	'BitLocker',
	'zfs_member',
	'apfs',
	'ReFS',
];
export function hasForeignData(drive: Drive) {
	return drive.partitions.some((p) => foreign.includes(p.fsType));
}

export function formatRunning(job: FormatJob | null | undefined) {
	return !!job && !job.finished;
}
