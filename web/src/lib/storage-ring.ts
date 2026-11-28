// §121: the storage ring of the phone's start page – what the used space of
// the volumes is made of. Only values the NAS has measured anyway: Docker's
// disk usage from its cache (never a fresh count), the blocks of the VMs'
// folders and the last measured size of each kind of system data (disks of
// a sleeping volume are not woken for it). "Dateien" is the rest of what the
// volumes use. Owners and administrators only; the routes refuse others.
import { api, type StorageStatus } from './api.ts';

export type StoragePart = 'apps' | 'files' | 'vms' | 'system';
export type StorageRing = {
	used: number;
	total: number;
	parts: { id: StoragePart; bytes: number }[];
	/** Docker is still counting: its part is not known yet. */
	measuring: boolean;
};

type DockerDf = {
	images?: { size?: number };
	containers?: { size?: number };
	volumes?: { size?: number };
	buildCache?: { size?: number };
	measuring?: boolean;
};
type SystemData = { functions: { id: string; sizeBytes: number | null }[] };

/** Splits the used space; nothing goes below 0 and the parts add up to `used`. */
export function splitStorage(
	used: number,
	total: number,
	apps: number,
	vms: number,
	system: number,
	measuring = false,
): StorageRing {
	const clean = (v: number) => (Number.isFinite(v) && v > 0 ? v : 0);
	let left = clean(used);
	const take = (v: number) => {
		const part = Math.min(left, clean(v));
		left -= part;
		return part;
	};
	const sys = take(system);
	const vm = take(vms);
	const app = take(apps);
	return {
		used: clean(used),
		total: clean(total),
		parts: [
			{ id: 'apps', bytes: app },
			{ id: 'files', bytes: left },
			{ id: 'vms', bytes: vm },
			{ id: 'system', bytes: sys },
		],
		measuring,
	};
}

export async function loadStorageRing(docker: boolean): Promise<StorageRing> {
	const [storage, df, space, data] = await Promise.all([
		api.get<StorageStatus>('/storage'),
		docker ? api.get<DockerDf>('/docker/df').catch(() => null) : Promise.resolve(null),
		api.get<{ bytes: number }>('/vms/space').catch(() => null),
		api.get<SystemData>('/system/data').catch(() => null),
	]);
	const volumes = storage.volumes ?? [];
	const used = volumes.reduce((s, v) => s + (v.usedBytes || 0), 0);
	const total = volumes.reduce((s, v) => s + (v.sizeBytes || 0), 0);
	const size = (part?: { size?: number }) => (typeof part?.size === 'number' ? part.size : 0);
	const apps = df
		? size(df.images) + size(df.containers) + size(df.volumes) + size(df.buildCache)
		: 0;
	const functions = Array.isArray(data?.functions) ? data.functions : [];
	const system = functions.reduce((s, f) => s + (f.sizeBytes ?? 0), 0);
	return splitStorage(used, total, apps, space?.bytes ?? 0, system, !!df?.measuring);
}
