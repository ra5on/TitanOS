// Shared pieces of the VM form (VmForm.svelte) and its sections.
import type { Volume, VMHost } from '#lib/api.ts';
import { t } from '#lib/i18n/index.svelte.ts';

import { GB } from '#lib/format.ts';

export { GB };
/** The tag a shared folder is mounted by inside the VM. */
const TAG = /^[A-Za-z0-9_-]{1,36}$/;

export type DiskDraft = {
	/** A new empty disk, a copy of an image, an image as it is, or a whole disk. */
	mode: 'new' | 'copy' | 'direct' | 'block';
	format: 'qcow2' | 'raw';
	/** Decimal gigabytes as typed; sent to titand as bytes. */
	sizeGB: number;
	volume: string;
	/** The image or the whole disk, for everything but `new`. */
	path: string;
};

export type Folder = { path: string; tag: string; readOnly: boolean };

/** Free bytes on a mounted volume, 0 for one that is not among them. */
export function freeOn(volumes: Volume[], volume: string): number {
	const found = volumes.find((candidate) => candidate.name === volume);
	return found ? found.sizeBytes - found.usedBytes : 0;
}

export const imageOf = (images: VMHost['images'], disk: DiskDraft) =>
	images.find((image) => image.path === disk.path);

/** Whether a new disk cannot be made as it is set up. */
export function diskBlocked(
	disk: DiskDraft,
	images: VMHost['images'],
	free: (volume: string) => number,
): boolean {
	if (disk.mode === 'direct' || disk.mode === 'block') return disk.path === '';
	const image = imageOf(images, disk);
	return (
		!(disk.sizeGB >= 1) ||
		(disk.mode === 'copy' && (!image || disk.sizeGB * GB < image.virtualBytes)) ||
		(disk.format === 'raw' && disk.sizeGB * GB > free(disk.volume))
	);
}

/** What is wrong with a shared folder, or '' if nothing is. */
export const folderProblem = (folders: Folder[], folder: Folder, index: number) =>
	!folder.path.startsWith('/mnt/')
		? t('vms.form.folderPath')
		: !TAG.test(folder.tag)
			? t('vms.form.folderTag')
			: folders.some((other, at) => at !== index && other.tag === folder.tag)
				? t('vms.form.folderTwice')
				: '';
