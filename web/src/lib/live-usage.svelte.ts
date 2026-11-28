// §121 (owner 28.11.2026, drafts Entwuerfe/121-handy-start D and
// 121-desktop 2): "Live-Nutzung" – the four values of the NAS and what each
// app, VM and system container uses right now. One module for the phone's
// sheet, its start page and the popover at the desktop's top bar.
//
// The NAS reports CPU on three scales: a Docker container like `docker
// stats` (100 % = one thread), a VM in percent of its own vCPUs, a system
// container in percent of the CPUs it may use. Everything here is a share of
// the whole NAS (all threads = 100 %). "System" is what the NAS uses beyond
// those, never below 0.
import {
	api,
	type DockerContainer,
	type DockerStatus,
	type StorageStatus,
	type SystemInfo,
	type VM,
} from './api.ts';
import { containerLabel, type LXCList } from './lxc.ts';
import { poll } from './poll.ts';
import { vmLabel } from './vm-name.ts';

export type UsageKind = 'vm' | 'docker' | 'lxc' | 'system';
export type UsageRow = {
	id: string;
	kind: UsageKind;
	name: string;
	/** Containers of an app, vCPUs of a VM. */
	count: number;
	/** Share of the whole NAS, 0–100. */
	cpu: number;
	memoryBytes: number;
	href: string;
};
export type UsageSort = 'cpu' | 'memory';

export type LiveUsage = {
	cpu: number;
	threads: number;
	memoryUsed: number;
	memoryTotal: number;
	storageUsed: number;
	storageTotal: number;
	/** The busiest accelerator with a load, null without one. */
	gpu: { percent: number; memoryUsed: number | null } | null;
	rx: number;
	tx: number;
	temperature: number | null;
	rows: UsageRow[];
};

const clamp = (v: number) => (Number.isFinite(v) ? Math.min(100, Math.max(0, v)) : 0);

/** A Docker container's CPU (100 % = one thread) as share of the NAS. */
export const dockerShare = (percent: number, threads: number) =>
	clamp(percent / Math.max(1, threads));
/** A VM's CPU (percent of its vCPUs) as share of the NAS. */
export const vmShare = (percent: number, vcpus: number, threads: number) =>
	clamp((percent * Math.max(1, vcpus)) / Math.max(1, threads));
/** A system container's CPU (percent of the CPUs it may use) as share of the NAS. */
export const lxcShare = (percent: number, cpus: number | undefined, threads: number) =>
	clamp((percent * Math.max(1, cpus || threads)) / Math.max(1, threads));

/** The rows of the apps, VMs and system containers plus "System", sorted. */
export function usageRows(
	system: Pick<SystemInfo, 'cpu' | 'memory'>,
	containers: DockerContainer[],
	vms: VM[],
	lxc: LXCList['containers'],
	sort: UsageSort = 'cpu',
): UsageRow[] {
	const threads = Math.max(1, system.cpu.threads);
	const apps = new Map<string, UsageRow>();
	for (const c of containers) {
		if (c.state !== 'running') continue;
		const app = c.app || c.name;
		const row = apps.get(app) ?? {
			id: 'docker:' + app,
			kind: 'docker' as const,
			name: app,
			count: 0,
			cpu: 0,
			memoryBytes: 0,
			href: '/docker?group=' + encodeURIComponent(app),
		};
		row.count++;
		row.cpu += dockerShare(c.cpuPercent, threads);
		row.memoryBytes += c.memoryBytes || 0;
		apps.set(app, row);
	}
	const rows: UsageRow[] = [...apps.values()];
	for (const v of vms)
		if (v.state === 'running')
			rows.push({
				id: 'vm:' + v.name,
				kind: 'vm',
				name: vmLabel(v),
				count: v.vcpus,
				cpu: vmShare(v.cpuPercent, v.vcpus, threads),
				memoryBytes: v.memoryBytes || 0,
				href: '/vms?vm=' + encodeURIComponent(v.name),
			});
	for (const c of lxc)
		if (c.state === 'running')
			rows.push({
				id: 'lxc:' + c.name,
				kind: 'lxc',
				name: containerLabel(c),
				count: c.cpus || 0,
				cpu: lxcShare(c.cpuPercent, c.cpus, threads),
				memoryBytes: c.memoryBytes || 0,
				href: '/lxc?ct=' + encodeURIComponent(c.name),
			});
	const guests = rows.reduce((sum, r) => sum + r.cpu, 0);
	const guestMemory = rows.reduce((sum, r) => sum + r.memoryBytes, 0);
	rows.push({
		id: 'system',
		kind: 'system',
		name: '',
		count: 0,
		cpu: clamp(system.cpu.usagePercent - guests),
		memoryBytes: Math.max(0, system.memory.totalBytes - system.memory.availableBytes - guestMemory),
		href: '/tools/taskmanager?tab=processes',
	});
	return rows.sort((a, b) =>
		sort === 'cpu'
			? b.cpu - a.cpu || b.memoryBytes - a.memoryBytes || a.name.localeCompare(b.name)
			: b.memoryBytes - a.memoryBytes || b.cpu - a.cpu || a.name.localeCompare(b.name),
	);
}

export const live = $state<{ value: LiveUsage | null; error: boolean }>({
	value: null,
	error: false,
});

let watchers = 0;
let stop: (() => void) | null = null;
let storage: StorageStatus | null = null;
let storageAt = 0;
let guests: { containers: DockerContainer[]; vms: VM[]; lxc: LXCList['containers'] } = {
	containers: [],
	vms: [],
	lxc: [],
};
let guestsAt = 0;

async function refresh(admin: boolean) {
	try {
		const system = await api.get<SystemInfo>('/system');
		const now = Date.now();
		if (!storage || now - storageAt > 30000) {
			storageAt = now;
			storage = await api.get<StorageStatus>('/storage').catch(() => storage);
		}
		// The guests every 4 s: Docker answers from its own cache, VMs and
		// containers from the loop of their managers.
		if (admin && now - guestsAt > 3500) {
			guestsAt = now;
			const [docker, vms, lxc] = await Promise.all([
				api.get<DockerStatus>('/docker').catch(() => null),
				api.get<{ vms: VM[] }>('/vms').catch(() => null),
				api.get<LXCList>('/lxc').catch(() => null),
			]);
			guests = {
				containers: docker?.containers ?? [],
				vms: vms?.vms ?? [],
				lxc: lxc?.containers ?? [],
			};
		}
		const volumes = storage?.volumes ?? [];
		const gpus = (system.accelerators ?? []).filter((a) => a.usagePercent !== null);
		const gpu = gpus.sort((a, b) => (b.usagePercent ?? 0) - (a.usagePercent ?? 0))[0];
		const temps = (system.temperatures ?? [])
			.map((t) => t.celsius)
			.filter((c): c is number => typeof c === 'number' && Number.isFinite(c));
		live.value = {
			cpu: clamp(system.cpu.usagePercent),
			threads: system.cpu.threads,
			memoryUsed: Math.max(0, system.memory.totalBytes - system.memory.availableBytes),
			memoryTotal: system.memory.totalBytes,
			storageUsed: volumes.reduce((s, v) => s + (v.usedBytes || 0), 0),
			storageTotal: volumes.reduce((s, v) => s + (v.sizeBytes || 0), 0),
			gpu: gpu ? { percent: clamp(gpu.usagePercent ?? 0), memoryUsed: gpu.memoryUsedBytes } : null,
			rx: (system.networks ?? []).reduce((s, n) => s + (n.rxBytesPerSecond || 0), 0),
			tx: (system.networks ?? []).reduce((s, n) => s + (n.txBytesPerSecond || 0), 0),
			temperature: temps.length ? Math.max(...temps) : null,
			rows: admin ? usageRows(system, guests.containers, guests.vms, guests.lxc) : [],
		};
		live.error = false;
	} catch {
		live.error = true;
	}
}

/** Follows the values every 2 s while someone shows them; returns how to stop. */
export function watchLive(admin: boolean): () => void {
	watchers++;
	if (!stop) stop = poll(() => refresh(admin), { interval: 2000 });
	return () => {
		watchers--;
		if (watchers <= 0) {
			watchers = 0;
			stop?.();
			stop = null;
		}
	};
}
