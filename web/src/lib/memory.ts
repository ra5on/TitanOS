import type { SystemInfo } from './api.ts';

/** Physical occupancy partitions sum to total; available overlaps the cache. */
export function memoryParts(memory: SystemInfo['memory']) {
	const total = Math.max(0, memory.totalBytes);
	const used = Math.min(total, Math.max(0, memory.usedBytes));
	const cache = memory.cacheBytes == null ? null : Math.min(used, Math.max(0, memory.cacheBytes));
	return {
		total,
		used,
		cache,
		other: cache === null ? null : used - cache,
		free: total - used,
		available: Math.min(total, Math.max(0, memory.availableBytes)),
	};
}
