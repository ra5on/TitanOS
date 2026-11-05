// The KI-Agent as an app of its own (PLAN 44): installed from /agent with
// its own manifest, it starts at /agent?app=1. The flag holds for the tab,
// so the agent's pages keep showing without the TitanOS navigation.

const KEY = 'titanos.agent.app';

export function agentAppMode(url: {
	pathname: string;
	searchParams: { get(name: string): string | null };
}): boolean {
	if (!url.pathname.startsWith('/agent')) return false;
	try {
		if (url.searchParams.get('app') === '1') sessionStorage.setItem(KEY, '1');
		return sessionStorage.getItem(KEY) === '1';
	} catch {
		return url.searchParams.get('app') === '1';
	}
}

/** Whether a path belongs to the KI-Agent. */
export function isAgentPath(path: string): boolean {
	return path === '/agent' || path.startsWith('/agent/');
}

/**
 * Opens a page of the KI-Agent from TitanOS with a full load, so Safari
 * takes the agent's manifest for "Add to Home Screen" (PLAN 48). Within
 * the agent it stays a normal step.
 */
export function openAgent(url: string, go: (url: string) => unknown): void {
	if (isAgentPath(location.pathname)) void go(url);
	else location.assign(url);
}
