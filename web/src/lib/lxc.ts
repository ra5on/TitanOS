// System containers (LXC): types of the API and the checks the form shares
// with the backend (backend/internal/lxc).
import { newVMIdentifier } from './vm-name.ts';

export type LXCMount = { path: string; target: string; readOnly: boolean };

export type LXCContainer = {
	name: string;
	displayName?: string;
	description?: string;
	volume: string;
	distribution: string;
	release: string;
	arch: string;
	cpus?: number;
	memoryMiB?: number;
	/** "bridge" (br0), "network:<id>" or "none". */
	network: string;
	mac: string;
	mounts?: LXCMount[];
	autostart: boolean;
	privileged?: boolean;
	btrfs?: boolean;
	created: string;
};

export type LXCJob = {
	state: 'running' | 'failed';
	phase: 'prepare' | 'download' | 'configure';
	text?: string;
	started: string;
};

export type LXCStatus = LXCContainer & {
	state: 'running' | 'stopped';
	available: boolean;
	cpuPercent: number;
	memoryBytes: number;
	job?: LXCJob;
};

export type LXCNetwork = { id: string; name: string; bridge: boolean; vlan?: number };

export type LXCList = {
	installed: boolean;
	dev: boolean;
	host: { cpus: number; memoryBytes: number };
	bridge: boolean;
	networks: LXCNetwork[] | null;
	containers: LXCStatus[];
};

export type LXCImage = { distribution: string; release: string; arch: string; build: string };
export type LXCSnapshot = { id: string; created: string };

export function containerLabel(c: { name: string; displayName?: string }): string {
	return c.displayName || c.name;
}

/** The identifier that goes into paths, URLs and LXC commands. */
export function newContainerIdentifier(label: string, taken: string[]): string {
	// A label without any letter or digit gives "ct", not the "vm" of VMs.
	return newVMIdentifier(/[a-z0-9]/i.test(label.normalize('NFKD')) ? label : 'ct', taken);
}

const systemTargets = [
	'/bin',
	'/boot',
	'/dev',
	'/etc',
	'/lib',
	'/lib64',
	'/proc',
	'/run',
	'/sbin',
	'/sys',
	'/usr',
	'/var',
];
const coveredBelow = ['/dev', '/proc', '/run', '/sys'];

/** Whether a place inside the container may hold a folder of the NAS (same rules as the backend). */
export function validMountTarget(target: string): boolean {
	if (
		!/^\/[A-Za-z0-9._/-]{1,200}$/.test(target) ||
		target === '/' ||
		target.endsWith('/') ||
		target.includes('//')
	)
		return false;
	if (target.split('/').some((part) => part === '.' || part === '..')) return false;
	if (systemTargets.includes(target)) return false;
	return !coveredBelow.some((dir) => target.startsWith(dir + '/'));
}

/** A unicast MAC address, or empty for one TitanOS picks. */
export function validMAC(mac: string): boolean {
	const value = mac.trim().toLowerCase();
	if (value === '') return true;
	if (!/^[0-9a-f]{2}(:[0-9a-f]{2}){5}$/.test(value) || value === '00:00:00:00:00:00') return false;
	return (parseInt(value.slice(0, 2), 16) & 1) === 0;
}

/** Images grouped by distribution, newest release names first where they are numbers. */
export function groupImages(images: LXCImage[]): { distribution: string; releases: string[] }[] {
	const groups = new Map<string, string[]>();
	for (const image of images)
		groups.set(image.distribution, [...(groups.get(image.distribution) ?? []), image.release]);
	return [...groups.entries()]
		.sort(([a], [b]) => a.localeCompare(b))
		.map(([distribution, releases]) => ({
			distribution,
			releases: releases.sort((a, b) => b.localeCompare(a, undefined, { numeric: true })),
		}));
}

/** Debian (the base of TitanOS) as a start, else the first image there is. */
export function defaultImage(images: LXCImage[]): { distribution: string; release: string } {
	const groups = groupImages(images);
	const debian = groups.find((group) => group.distribution === 'debian');
	if (debian) {
		const release =
			['trixie', 'bookworm'].find((name) => debian.releases.includes(name)) ?? debian.releases[0];
		return { distribution: 'debian', release };
	}
	return groups[0]
		? { distribution: groups[0].distribution, release: groups[0].releases[0] }
		: { distribution: '', release: '' };
}
