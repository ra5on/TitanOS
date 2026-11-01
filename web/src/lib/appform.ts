// Helpers of the app form (install and edit share it): CPUs and memory as
// the form shows them, the volume a folder lies on, and the progress of an
// installation read from the output of its job.

/** The CPUs of a cpuset ("0,2,8-11"), or null if it cannot be read. */
export function cpusetCpus(cpuset: string): number[] | null {
	const value = cpuset.trim();
	if (value === '') return [];
	const cpus = new Set<number>();
	for (const part of value.split(',')) {
		const match = /^(\d+)(?:-(\d+))?$/.exec(part);
		if (!match) return null;
		const from = Number(match[1]);
		const to = match[2] === undefined ? from : Number(match[2]);
		if (to < from || to - from > 4096) return null;
		for (let cpu = from; cpu <= to; cpu++) cpus.add(cpu);
	}
	return [...cpus].sort((a, b) => a - b);
}

/** A cpuset with ranges where CPUs follow each other: [0, 2, 8, 9, 10] → "0,2,8-10". */
export function cpusetText(cpus: number[]): string {
	const sorted = [...new Set(cpus)].sort((a, b) => a - b);
	const parts: string[] = [];
	for (let i = 0; i < sorted.length;) {
		let j = i;
		while (j + 1 < sorted.length && sorted[j + 1] === sorted[j] + 1) j++;
		parts.push(j > i + 1 ? `${sorted[i]}-${sorted[j]}` : sorted.slice(i, j + 1).join(','));
		i = j + 1;
	}
	return parts.join(',');
}

/** The memory limits the form offers, as Compose writes them. */
export const MEMORY_LIMITS = ['512m', '1g', '2g', '4g', '6g', '8g', '12g', '16g', '32g'];

/** Bytes of a Compose memory value ("4g", "512m", "1073741824"); NaN if unreadable. */
export function memoryBytes(value: string): number {
	const match = /^(\d+(?:\.\d+)?)([bkmg]?)$/i.exec(value.trim());
	if (!match) return NaN;
	const unit = { '': 1, b: 1, k: 1024, m: 1024 ** 2, g: 1024 ** 3 }[match[2].toLowerCase()] ?? 1;
	return Math.round(Number(match[1]) * unit);
}

export type VolumeSpace = {
	name: string;
	mountPoint: string;
	sizeBytes: number;
	usedBytes: number;
};

/** The volume a folder of the NAS lies on, if any. */
export function volumeOf<T extends VolumeSpace>(path: string, volumes: T[]): T | null {
	const clean = path.trim().replace(/\/+$/, '');
	let best: T | null = null;
	for (const volume of volumes) {
		const root = volume.mountPoint.replace(/\/+$/, '');
		if (!root) continue;
		if (
			(clean === root || clean.startsWith(root + '/')) &&
			root.length > (best?.mountPoint.length ?? 0)
		)
			best = volume;
	}
	return best;
}

export type PullImage = { name: string; state: 'waiting' | 'pulling' | 'done' | 'failed' };
export type PullProgress = {
	images: PullImage[];
	/** Bytes of all layers seen so far; done counts finished layers in full. */
	doneBytes: number;
	totalBytes: number;
	/** Lines that are no progress of an image or layer (Compose's container messages, errors). */
	other: string[];
};

const SIZE = /([\d.]+)\s*([kMG]?B)\s*\/\s*([\d.]+)\s*([kMG]?B)/;
const UNITS: Record<string, number> = { B: 1, kB: 1e3, MB: 1e6, GB: 1e9 };
const bytes = (value: string, unit: string) => Number(value) * (UNITS[unit] ?? 1);
const LAYER = /^[0-9a-f]{12}$/;

/**
 * Reads the output of an installation job: Compose's plain progress, one
 * line per image ("web Pulling") and layer ("034d6572bf28 Downloading
 * [=>  ] 1.7MB/2.2MB"), as titand keeps it (the latest line of each).
 */
export function pullProgress(output: string): PullProgress {
	const progress: PullProgress = { images: [], doneBytes: 0, totalBytes: 0, other: [] };
	for (const line of output.split('\n')) {
		const text = line.trim();
		if (!text) continue;
		const [id, ...rest] = text.split(/\s+/);
		const state = rest.join(' ');
		if (LAYER.test(id)) {
			const size = SIZE.exec(state);
			if (size) {
				const total = bytes(size[3], size[4]);
				progress.totalBytes += total;
				// Extracting counts as downloaded.
				progress.doneBytes += state.startsWith('Extracting') ? total : bytes(size[1], size[2]);
			}
			continue;
		}
		const image = /^(Pulling|Pulled|Waiting|Skipped|Error|Interrupted)\b/.exec(state);
		if (image && rest.length <= 3) {
			progress.images.push({
				name: id,
				state:
					image[1] === 'Pulled' || image[1] === 'Skipped'
						? 'done'
						: image[1] === 'Error' || image[1] === 'Interrupted'
							? 'failed'
							: image[1] === 'Waiting'
								? 'waiting'
								: 'pulling',
			});
			continue;
		}
		progress.other.push(text);
	}
	return progress;
}
