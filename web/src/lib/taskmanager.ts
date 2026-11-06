// Task manager (PLAN 61, Monitor app): types of GET /system/processes and
// the pure logic of the page (sorting, search, histories, volume sums).
import type { SystemInfo, Volume } from './api.ts';

export type ProcessOwner = {
	kind: 'docker' | 'vm' | 'lxc' | 'service' | '';
	id?: string;
	name?: string;
};
export type ProcessRow = {
	pid: number;
	ppid: number;
	name: string;
	command: string;
	user: string;
	state: string;
	cpuPercent: number;
	memoryBytes: number;
	/** Start time in clock ticks; a signal must name it. */
	start: number;
	owner: ProcessOwner;
	/** init, titand and its parent: cannot be ended here. */
	protected?: boolean;
};
export type ProcessList = {
	processes: ProcessRow[];
	cpus: number;
	memoryTotalBytes: number;
	sampledSeconds: number;
};
/** Throughput of a block device (GET /system, field disks). */
export type DiskIO = {
	name: string;
	readBytesPerSecond: number;
	writeBytesPerSecond: number;
	busyPercent: number;
};
export type TaskSystem = SystemInfo & { disks?: DiskIO[] };

export type ProcessSort = 'cpu' | 'memory' | 'name';

/** Rows matching the search (name, command, user, owner, pid), sorted. */
export function sortProcesses(
	rows: readonly ProcessRow[],
	sort: ProcessSort,
	query = '',
): ProcessRow[] {
	const q = query.trim().toLowerCase();
	const found = q
		? rows.filter((p) =>
				[p.name, p.command, p.user, p.owner.name ?? '', String(p.pid)].some((v) =>
					v.toLowerCase().includes(q),
				),
			)
		: [...rows];
	const byName = (a: ProcessRow, b: ProcessRow) =>
		a.name.localeCompare(b.name, undefined, { sensitivity: 'base' }) || a.pid - b.pid;
	return found.sort((a, b) =>
		sort === 'cpu'
			? b.cpuPercent - a.cpuPercent || b.memoryBytes - a.memoryBytes || a.pid - b.pid
			: sort === 'memory'
				? b.memoryBytes - a.memoryBytes || a.pid - b.pid
				: byName(a, b),
	);
}

/** A command line cut to `most` characters with an ellipsis. */
export function shortCommand(command: string, most = 90): string {
	const text = command.trim();
	return text.length > most ? `${text.slice(0, most - 1).trimEnd()}…` : text;
}

/** The history with a new value at the end, at most `capacity` long. */
export function pushHistory(history: readonly number[], value: number, capacity = 40): number[] {
	const next = [...history, Number.isFinite(value) ? Math.max(0, value) : 0];
	return next.slice(-capacity);
}

/** Values scaled to 0–100 of their own peak, for a sparkline of rates. */
export function scaleToPeak(values: readonly number[]): number[] {
	const peak = Math.max(0, ...values);
	return peak > 0 ? values.map((v) => (v / peak) * 100) : values.map(() => 0);
}

/** The whole disk of a member name: "/dev/sda1" → "sda", "nvme0n1p2" → "nvme0n1". */
export function memberDisk(member: string, disks: readonly string[]): string {
	const name = member.replace(/^\/dev\//, '');
	if (disks.includes(name)) return name;
	return disks
		.filter((d) => name.startsWith(d))
		.reduce((best, d) => (d.length > best.length ? d : best), '');
}

/**
 * Throughput per volume as the sum of its member disks. Mirrors (RAID 1,
 * Btrfs) write each block to several disks, so writes count once per
 * member: what the disks do, not what the programs wrote.
 */
export function volumeRates(
	volumes: readonly Pick<Volume, 'name' | 'members'>[],
	io: readonly DiskIO[],
): { name: string; read: number; write: number; known: boolean }[] {
	const names = io.map((d) => d.name);
	return volumes.map((v) => {
		let read = 0,
			write = 0,
			known = false;
		const seen = new Set<string>();
		for (const m of v.members ?? []) {
			const disk = memberDisk(m.disk, names);
			if (!disk || seen.has(disk)) continue;
			seen.add(disk);
			const d = io.find((x) => x.name === disk)!;
			read += d.readBytesPerSecond;
			write += d.writeBytesPerSecond;
			known = true;
		}
		return { name: v.name, read, write, known };
	});
}
