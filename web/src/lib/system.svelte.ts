import { api, ApiError, type SystemInfo } from './api.ts';
import { sessionExpired } from './session.svelte.ts';

const INTERVAL_MS = 2000;

/** Live state of the NAS, refreshed while the shell is shown. */
export const system = $state<{ info: SystemInfo | null; reachable: boolean }>({
	info: null,
	reachable: true
});

export const systemHistory = $state<{ cpu: number[]; memory: number[] }>({ cpu: [], memory: [] });
let lastSample = 0;
async function poll() {
	if (document.hidden) return;
	try {
		system.info = await api.get<SystemInfo>('/system');
		system.reachable = true;
        const now = Date.now();
        if (now - lastSample > INTERVAL_MS * 3) { systemHistory.cpu = []; systemHistory.memory = []; }
        if (now - lastSample >= INTERVAL_MS * .8) {
            const { cpu, memory } = system.info;
            systemHistory.cpu = [...systemHistory.cpu.slice(-89), cpu.usagePercent];
            systemHistory.memory = [...systemHistory.memory.slice(-89), memory.totalBytes > 0 ? memory.usedBytes / memory.totalBytes * 100 : 0];
            lastSample = now;
        }
	} catch (err) {
		if (err instanceof ApiError && err.status === 401) sessionExpired();
		else system.reachable = false;
	}
}

/** Starts polling; returns the function that stops it. */
export function watchSystem(): () => void {
	poll();
	const timer = setInterval(poll, INTERVAL_MS);
	document.addEventListener('visibilitychange', poll);
	return () => {
		clearInterval(timer);
		document.removeEventListener('visibilitychange', poll);
	};
}
