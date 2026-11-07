import { api, type StorageStatus } from './api.ts';
import { fromDesktop, shareWithWindows } from './embedded.ts';
import { poll, type Poller } from './poll.ts';

const INTERVAL_MS = 4000;
const TIMEOUT_MS = 10000;

/** Disks and volumes, refreshed while the shell is shown. */
export const storage = $state<{ status: StorageStatus | null }>({ status: null });

let poller: Poller | null = null;

// The desktop's answer serves its windows too (PLAN 65-6, see system.svelte.ts).
const shared = { status: null as StorageStatus | null, at: 0 };
shareWithWindows('titanosStorage', shared);
let at = 0;
let forced = false;

async function read(signal?: AbortSignal) {
	const timeout = AbortSignal.timeout(TIMEOUT_MS);
	const fresh = forced;
	forced = false;
	const top = fresh ? null : fromDesktop<typeof shared>('titanosStorage');
	if (top?.status && Date.now() - top.at < INTERVAL_MS * 2.5) {
		if (top.at > at) {
			at = top.at;
			storage.status = JSON.parse(JSON.stringify(top.status)) as StorageStatus;
		}
		return;
	}
	try {
		storage.status = await api.get<StorageStatus>(
			'/storage',
			signal ? AbortSignal.any([signal, timeout]) : timeout,
		);
		at = Date.now();
		shared.status = storage.status;
		shared.at = at;
	} catch {
		// The system poller reports a lost connection or session.
	}
}

/**
 * Reads the state now, e.g. right after an action changed it. While the shell
 * watches, this goes through its poller: reads never overlap and one asked for
 * during a read follows it, so an older answer cannot overwrite a newer one.
 */
export function refreshStorage(): Promise<void> {
	forced = true;
	return poller ? poller.now() : read();
}

/** Starts polling (the shell does this once); returns the function that stops it. */
export function watchStorage(): () => void {
	const current = poll(read, { interval: INTERVAL_MS });
	poller = current;
	return () => {
		current();
		if (poller === current) poller = null;
	};
}
