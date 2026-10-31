// A node of the cluster that is starting again (an app update, a restart of
// titand) refuses connections for a few seconds. The proxy of this NAS then
// answers 503 "cluster_restarting" (backend/internal/cluster/proxy.go); the
// API client waits and asks again, and the cluster strip says
// "Knoten startet neu, gleich wieder da" while any request waits.

/** How long a request keeps waiting for a restarting node at most. */
export const RESTART_WAIT_MS = 120_000;

let waiting = 0;
const listeners = new Set<(restarting: boolean) => void>();

function tell() {
	for (const listener of listeners) listener(waiting > 0);
}

/** Marks a request as waiting for a restarting node; call the result when it stops. */
export function waitForNode(): () => void {
	waiting++;
	if (waiting === 1) tell();
	let done = false;
	return () => {
		if (done) return;
		done = true;
		waiting--;
		if (waiting === 0) tell();
	};
}

/** Follows whether some request waits for a restarting node; returns the unsubscribe. */
export function onNodeRestart(listener: (restarting: boolean) => void): () => void {
	listeners.add(listener);
	listener(waiting > 0);
	return () => listeners.delete(listener);
}

/** The pause before asking again, in milliseconds (Retry-After, 1 to 10 s). */
export function restartPause(retryAfter: number): number {
	return Math.min(10, Math.max(1, Number.isFinite(retryAfter) ? retryAfter : 3)) * 1000;
}
