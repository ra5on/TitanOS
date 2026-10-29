import type { SystemInfo } from './api.ts';

/**
 * Occupied, write cache and free partition the total. The read cache counts as
 * free because Linux drops it on demand; dirty pages are carved out of the
 * available memory because they must reach the disk before they can be freed.
 */
export function memoryParts(memory: SystemInfo['memory']) {
	const total = Math.max(0, memory.totalBytes);
	const available = Math.min(total, Math.max(0, memory.availableBytes));
	const write = Math.min(
		available,
		Math.max(0, (memory.dirtyBytes ?? 0) + (memory.writebackBytes ?? 0)),
	);
	return { total, occupied: total - available, write, free: available - write, available };
}
