import { api, type StorageStatus } from './api.ts';

const INTERVAL_MS = 4000;

/** Disks and volumes, refreshed while the shell is shown. */
export const storage = $state<{ status: StorageStatus | null }>({ status: null });

/** Reads the state now, e.g. right after an action changed it. */
export async function refreshStorage() {
	if (document.hidden) return;
	try {
		storage.status = await api.get<StorageStatus>('/storage');
	} catch {
		// The system poller reports a lost connection or session.
	}
}

/** Starts polling; returns the function that stops it. */
export function watchStorage(): () => void {
	refreshStorage();
	const timer = setInterval(refreshStorage, INTERVAL_MS);
	return () => clearInterval(timer);
}
