// The KI-Agent full screen inside TitanOS (PLAN 47): without the TitanOS
// menu, like the installed app. Remembered per device; the installed app
// is always full screen (agent-app.ts).

const KEY = 'titanos.agent.full';

function read(): boolean {
	try {
		return localStorage.getItem(KEY) === '1';
	} catch {
		return false;
	}
}

export const agentFull = $state({ on: read() });

export function setAgentFull(on: boolean) {
	agentFull.on = on;
	try {
		localStorage.setItem(KEY, on ? '1' : '0');
	} catch {
		// Only for this page view.
	}
}
