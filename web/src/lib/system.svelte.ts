import { api, ApiError, type SystemInfo } from './api.ts';
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

export const systemHistory = $state<{ cpu: number[]; memory: number[] }>({ cpu: [], memory: [] });
let lastSample = 0;
let poller: Poller | null = null;

async function read(signal?: AbortSignal) {
	const timeout = AbortSignal.timeout(TIMEOUT_MS);
	try {
		system.info = await api.get<SystemInfo>(
			'/system',
			signal ? AbortSignal.any([signal, timeout]) : timeout,
		);
		system.reachable = true;
		system.lastUpdated = Date.now();
		const now = Date.now();
		if (now - lastSample > INTERVAL_MS * 3) {
			systemHistory.cpu = [];
			systemHistory.memory = [];
		}
		if (now - lastSample >= INTERVAL_MS * 0.8) {
			const { cpu, memory } = system.info;
			systemHistory.cpu = [...systemHistory.cpu.slice(-89), cpu.usagePercent];
			systemHistory.memory = [
				...systemHistory.memory.slice(-89),
				memory.totalBytes > 0 ? (memory.availableBytes / memory.totalBytes) * 100 : 0,
			];
			lastSample = now;
		}
	} catch (err) {
		if (signal?.aborted) return;
		if (err instanceof ApiError && err.status === 401) sessionExpired();
		else system.reachable = false;
	}
}

/** Reads the state now; while the shell watches, through its poller (never two reads at once). */
export function refreshSystem(): Promise<void> {
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
