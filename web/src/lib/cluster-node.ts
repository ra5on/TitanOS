// The node of the cluster this tab manages (see docs/CLUSTER.md). With
// another node chosen, the requests of the web interface go to it through
// this NAS: /api/v1/cluster/nodes/<id>/api/… instead of /api/v1/….
// Login, the own account, appearance and the cluster itself stay here.

const KEY = 'titanos.cluster.node';

/** Paths that always mean this NAS, whichever node is chosen. */
const LOCAL = [
	/^\/session(\/|\?|$)/,
	/^\/login/,
	/^\/logout/,
	/^\/setup/,
	/^\/preferences/,
	/^\/account(\/|\?|$)/,
	/^\/cluster(\/|\?|$)/,
	/^\/background/,
	/^\/login-appearance/,
	/^\/app-icon/,
	/^\/restore/,
	/^\/welcome/,
];

/** The ID of the chosen other node, null for this NAS. */
export function activeNode(): string | null {
	try {
		return sessionStorage.getItem(KEY);
	} catch {
		return null;
	}
}

/** Chooses another node, or this NAS with null, and loads the page anew. */
export function switchNode(id: string | null) {
	try {
		if (id) sessionStorage.setItem(KEY, id);
		else sessionStorage.removeItem(KEY);
	} catch {
		// Without storage the choice cannot hold; this NAS it is.
	}
	location.reload();
}

/** The URL path of an API path (with query) on the chosen node. */
export function apiPath(path: string, node = activeNode()): string {
	if (!node || LOCAL.some((re) => re.test(path))) return '/api/v1' + path;
	return `/api/v1/cluster/nodes/${encodeURIComponent(node)}/api${path}`;
}

/** The WebSocket URL of an API path on the chosen node. */
export function apiSocket(path: string): string {
	return `${location.protocol === 'https:' ? 'wss' : 'ws'}://${location.host}${apiPath(path)}`;
}
