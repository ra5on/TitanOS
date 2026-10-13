import type { Disk, Volume } from '#lib/api.ts';
import { t } from '#lib/i18n/index.svelte.ts';

/** "HDD · 7200 U/min", "NVMe-SSD", … */
export function diskType(disk: Disk): string {
	if (disk.kind === 'hdd' && disk.smart && disk.smart.rotationRate > 0) {
		return t('disk.hddRpm', { rpm: disk.smart.rotationRate });
	}
	return t(`disk.${disk.kind}`);
}

/** The status column: what the disk is doing right now. */
export function diskStatus(disk: Disk): { text: string; dot: '' | 'ok' | 'warn' | 'error' } {
	switch (disk.role) {
		case 'stick':
			return { text: t('disk.stick'), dot: 'ok' };
		case 'free':
			return { text: t('disk.free'), dot: '' };
		case 'used':
			return { text: t('disk.usedRole'), dot: '' };
	}
	switch (disk.memberState) {
		case 'faulty':
			return { text: t('member.faulty'), dot: 'error' };
		case 'rebuilding':
			return { text: t('member.rebuilding'), dot: 'warn' };
		case 'spare':
			return { text: t('member.spare'), dot: '' };
		default:
			return { text: t('member.active'), dot: 'ok' };
	}
}

export function smartText(disk: Disk): string {
	if (!disk.smart) return t('disk.smartNone');
	if (!disk.smart.healthy) return t('disk.smartBad');
	if (disk.smart.wearPercent >= 0) return t('disk.smartWear', { percent: disk.smart.wearPercent });
	return t('disk.smartOk');
}

/** The color of a volume's state chip. */
export function stateTone(volume: Volume): 'ok' | 'warn' | 'error' {
	switch (volume.state) {
		case 'ok':
			return 'ok';
		case 'degraded':
		case 'failed':
		case 'missing':
			return 'error';
		default:
			return 'warn';
	}
}

/** Disks a volume may take: not the stick, not in a volume, nothing mounted. */
export function isUsable(disk: Disk): boolean {
	return (disk.role === 'free' || disk.role === 'used') && !disk.mounted;
}
