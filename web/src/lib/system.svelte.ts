import { api, ApiError, type SystemInfo } from './api.ts';
import { fromDesktop, shareWithWindows } from './embedded.ts';
import { poll, type Poller } from './poll.ts';
import { sessionExpired } from './session.svelte.ts';

const INTERVAL_MS = 2000;
const TIMEOUT_MS = 10000;

/** Live state of the NAS, refreshed while the shell is shown. */
export const system = $state<{ info: SystemInfo | null; reachable: boolean; lastUpdated: number }>({
	info: null,
	reachable: true,
	lastUpdated: 0,
});

let poller: Poller | null = null;

// The desktop asks every 2 s; its windows take that answer instead of asking
// again each (PLAN 65-6). An explicit refresh still asks the NAS.
shareWithWindows('titanosSystem', system);
let forced = false;

/** The desktop's answer when it is newer than ours and fresh; undefined: ask the NAS. */
function desktopInfo(): SystemInfo | null | undefined {
	const shared = fromDesktop<typeof system>('titanosSystem');
	if (!shared?.info || !shared.reachable || Date.now() - shared.lastUpdated > INTERVAL_MS * 2.5)
		return undefined;
	if (shared.lastUpdated <= system.lastUpdated) return null;
	system.lastUpdated = shared.lastUpdated;
	return JSON.parse(JSON.stringify(shared.info)) as SystemInfo;
}

async function read(signal?: AbortSignal) {
	const timeout = AbortSignal.timeout(TIMEOUT_MS);
	const fresh = forced;
	forced = false;
	try {
		const shared = fresh ? undefined : desktopInfo();
		if (shared === null) return;
		if (shared) system.info = shared;
		else {
			system.info = await api.get<SystemInfo>(
				'/system',
				signal ? AbortSignal.any([signal, timeout]) : timeout,
			);
			system.lastUpdated = Date.now();
		}
		system.reachable = true;
	} catch (err) {
		if (signal?.aborted) return;
		if (err instanceof ApiError && err.status === 401) sessionExpired();
		else system.reachable = false;
	}
}

/** Reads the state now; while the shell watches, through its poller (never two reads at once). */
export function refreshSystem(): Promise<void> {
	forced = true;
	return poller ? poller.now() : read();
}

/** Starts polling (the shell does this once); returns the function that stops it. */
export function watchSystem(): () => void {
	const current = poll(read, { interval: INTERVAL_MS });
	poller = current;
	return () => {
		current();
		if (poller === current) poller = null;
	};
}
