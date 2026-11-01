// The cluster of NAS (backend package cluster, docs/CLUSTER.md): what the
// settings page and the node switcher show.
import { api } from './api.ts';
import { t, type MessageKey } from './i18n/index.svelte.ts';
import { activeNode, compareVersions } from './cluster-node.ts';

export { compareVersions };

export type ClusterNode = {
	id: string;
	name: string;
	addresses: string[];
	extra?: string[];
	cert: string;
	joined: string;
	updated: string;
	/** The way of each address (home, tailscale, …). */
	ways?: Record<string, string>;
	tunnelKey?: string;
	/** Allowed by the admin node to manage the others. */
	manage?: boolean;
};

/** The ways between nodes, as the backend names them; '' is automatic. */
export const WAYS = [
	'home',
	'tunnel',
	'wireguard',
	'tailscale',
	'netbird',
	'extra',
	'internet',
] as const;
export type Way = (typeof WAYS)[number] | '';

export type Ways = {
	manage?: Way;
	only?: boolean;
	backup?: Way;
	/** Minutes between tries of the preferred way; 0 is 1, -1 never. */
	return?: number;
	/** Minutes until a notice about a node that does not answer; 0 is 10, -1 never. */
	alert?: number;
	clockOff?: boolean;
};

export type Route = { manage?: Way; only?: boolean; backup?: Way };

export type TunnelView = {
	enabled?: boolean;
	network?: string;
	port?: number;
	address?: string;
	running: boolean;
	error?: string;
};

export type Probe = {
	way: Way;
	address: string;
	ok: boolean;
	millis: number;
	problem?: 'timeout' | 'refused' | 'blocked' | 'failed';
	used: boolean;
};

export type NodeProbes = { id: string; name: string; probes: Probe[] };

export type UpdateRunNode = {
	id: string;
	name: string;
	from?: string;
	to?: string;
	state:
		| 'waiting'
		| 'checking'
		| 'installing'
		| 'restarting'
		| 'done'
		| 'current'
		| 'failed'
		| 'skipped';
	error?: string;
};

export type UpdateRun = {
	parallel: boolean;
	running: boolean;
	started: string;
	nodes: UpdateRunNode[];
};

export type NodeStatus = {
	name: string;
	version: string;
	hostname: string;
	uptimeSeconds: number;
	cpuPercent: number | null;
	memory: { totalBytes: number; availableBytes: number };
	volumes: {
		name: string;
		state: string;
		mounted: boolean;
		sizeBytes: number;
		usedBytes: number;
	}[];
	warnings: number;
	errors: number;
};

export type NodeView = ClusterNode & {
	self: boolean;
	online: boolean;
	status?: NodeStatus;
	error?: string;
	address?: string;
	way?: Way;
	millis?: number;
	preferred?: Way;
	fallback?: boolean;
	/** Speaks the protocol of this node: managed fully. */
	compatible: boolean;
	/** Seconds the clock is ahead (negative: behind), set beyond a minute. */
	clockSkew?: number;
	downSince?: string;
};

export type ClusterView = {
	self: string;
	name?: string;
	internet: boolean;
	nodes: ClusterNode[];
	/** The admin node, and whether this node may manage the others. */
	admin?: string;
	manages: boolean;
	extra: string[];
	found: string[];
	foundWays: Record<string, Way>;
	ways: Ways;
	routes: Record<string, Route>;
	tunnel: TunnelView;
	version: string;
	protocol: number;
};

export type ClusterPage = { cluster: ClusterView; nodes: NodeView[] };

export type JoinPreview = {
	cluster: string;
	nodes: string[];
	replaced: string[];
	local: string[];
	taken: string[];
};

export type SwitchNode = {
	id: string;
	name: string;
	self: boolean;
	online: boolean;
	version?: string;
	compatible?: boolean;
};

/** The nodes for the switcher; empty outside a cluster. */
export const clusterNodes = $state<{ nodes: SwitchNode[]; version: string; loaded: boolean }>({
	nodes: [],
	version: '',
	loaded: false,
});

export async function loadClusterNodes() {
	try {
		const res = await api.get<{ nodes: SwitchNode[]; version: string; manages: boolean }>(
			'/cluster/nodes',
		);
		// A member manages nobody: no switcher, and a tab that still
		// manages another node goes home (ClusterBanner).
		clusterNodes.nodes = res.manages ? res.nodes : [];
		clusterNodes.version = res.version;
	} catch {
		clusterNodes.nodes = [];
	} finally {
		clusterNodes.loaded = true;
	}
}

/** The node this tab manages, or undefined for this NAS. */
export function currentNode(): SwitchNode | undefined {
	const id = activeNode();
	return id ? clusterNodes.nodes.find((n) => n.id === id) : undefined;
}

/** The node of this NAS. */
export function homeNode(): SwitchNode | undefined {
	return clusterNodes.nodes.find((n) => n.self);
}

/** Whether a node can be managed from here: online and speaking the same
 * protocol, whatever its version. A node of another protocol is managed for
 * updates, logs and power only (limited). */
export function manageable(node: SwitchNode | NodeView): boolean {
	return node.self || node.online;
}

/** Whether a node runs another version than this one. */
export function otherVersion(node: SwitchNode | NodeView, version: string): boolean {
	const v = 'status' in node ? node.status?.version : (node as SwitchNode).version;
	return !node.self && !!v && v !== version;
}

/** The name of a way; '' is automatic. */
export function wayLabel(way: Way | string | undefined): string {
	return t(`cluster.way.${way || 'auto'}` as MessageKey);
}

/** The update state of a node: its version and the newer release found,
 * if any. This node's own page is asked only while the tab manages it. */
export async function nodeUpdate(
	node: { id: string; self: boolean },
	activeNode: string | null,
): Promise<{ version: string; available: string; kind: string }> {
	if (node.self && activeNode) throw new Error('another node is chosen');
	const st = await api.get<{
		version: string;
		available: { version: string } | null;
		kind: string;
	}>(node.self ? '/update' : '/cluster/nodes/' + encodeURIComponent(node.id) + '/api/update');
	const available = st.available && st.available.version !== st.version ? st.available.version : '';
	return { version: st.version, available, kind: st.kind };
}
