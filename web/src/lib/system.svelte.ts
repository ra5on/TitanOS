import { api, ApiError, type SystemInfo } from './api.ts';
import { sessionExpired } from './session.svelte.ts';

const INTERVAL_MS = 2000;

/** Live state of the NAS, refreshed while the shell is shown. */
export const system = $state<{ info: SystemInfo | null; reachable: boolean; lastUpdated: number }>({
	info: null,
	reachable: true,
	lastUpdated: 0
});

export const systemHistory = $state<{ cpu: number[]; memory: number[] }>({ cpu: [], memory: [] });
let lastSample = 0;
let polling = false;
export async function refreshSystem() {
	if (document.hidden || polling) return;
	polling = true;
	try {
		system.info = await api.get<SystemInfo>('/system', AbortSignal.timeout(10000));
		system.reachable = true;
		system.lastUpdated = Date.now();
        const now = Date.now();
        if (now - lastSample > INTERVAL_MS * 3) { systemHistory.cpu = []; systemHistory.memory = []; }
        if (now - lastSample >= INTERVAL_MS * .8) {
            const { cpu, memory } = system.info;
            systemHistory.cpu = [...systemHistory.cpu.slice(-89), cpu.usagePercent];
            systemHistory.memory = [...systemHistory.memory.slice(-89), memory.totalBytes > 0 ? memory.availableBytes / memory.totalBytes * 100 : 0];
            lastSample = now;
        }
	} catch (err) {
		if (err instanceof ApiError && err.status === 401) sessionExpired();
		else system.reachable = false;
	} finally { polling = false; }
}

/** Starts polling; returns the function that stops it. */
export function watchSystem(): () => void {
	refreshSystem();
	const timer = setInterval(refreshSystem, INTERVAL_MS);
	document.addEventListener('visibilitychange', refreshSystem);
	return () => {
		clearInterval(timer);
		document.removeEventListener('visibilitychange', refreshSystem);
	};
}
