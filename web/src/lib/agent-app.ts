// The KI-Agent as an app of its own (PLAN 44): installed from /agent with
// its own manifest, it starts at /agent?app=1. The flag holds for the tab,
// so the agent's pages keep showing without the TitanOS navigation.

const KEY = 'titanos.agent.app';

/**
 * Whether the agent runs as its own app. Started from the home screen it
 * has ?app=1; opened later from a notification (/agent?chat=…) or after
 * iOS ended it, the URL lacks it. Then the app counts as the agent's when
 * it runs standalone and was the agent's app before (PLAN 59: on iOS each
 * home-screen app has storage of its own, so the TitanOS app is not taken
 * for the agent's).
 */
export function agentAppMode(
	url: {
		pathname: string;
		searchParams: { get(name: string): string | null };
	},
	standalone: () => boolean = isStandalone,
): boolean {
	if (!url.pathname.startsWith('/agent')) return false;
	const flag = url.searchParams.get('app') === '1';
	try {
		if (flag) {
			sessionStorage.setItem(KEY, '1');
			if (standalone()) localStorage.setItem(KEY, '1');
		}
		if (sessionStorage.getItem(KEY) === '1') return true;
		return standalone() && localStorage.getItem(KEY) === '1';
	} catch {
		return flag;
	}
}

function isStandalone(): boolean {
	if (typeof window === 'undefined') return false;
	const nav = navigator as Navigator & { standalone?: boolean };
	return nav.standalone === true || matchMedia('(display-mode: standalone)').matches;
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
