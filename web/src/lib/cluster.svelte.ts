// The cluster of NAS (backend package cluster, docs/CLUSTER.md): what the
// settings page and the node switcher show.
import { api } from './api.ts';
import { activeNode } from './cluster-node.ts';

export type ClusterNode = {
	id: string;
	name: string;
	addresses: string[];
	extra?: string[];
	cert: string;
	joined: string;
	updated: string;
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
};

export type ClusterView = {
	self: string;
	name?: string;
	internet: boolean;
	nodes: ClusterNode[];
	joinInfo?: string;
	extra: string[];
	found: string[];
};

export type ClusterPage = { cluster: ClusterView; nodes: NodeView[] };

export type JoinPreview = {
	cluster: string;
	nodes: string[];
	replaced: string[];
	added: string[];
	taken: string[];
};

export type SwitchNode = {
	id: string;
	name: string;
	self: boolean;
	online: boolean;
	version?: string;
};

/** The nodes for the switcher; empty outside a cluster. */
export const clusterNodes = $state<{ nodes: SwitchNode[]; version: string; loaded: boolean }>({
	nodes: [],
	version: '',
	loaded: false,
});

export async function loadClusterNodes() {
	try {
		const res = await api.get<{ nodes: SwitchNode[]; version: string }>('/cluster/nodes');
		clusterNodes.nodes = res.nodes;
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

/** Whether a node can be managed from here: online with the same version. */
export function manageable(node: SwitchNode | NodeView, version: string): boolean {
	const v = 'status' in node ? node.status?.version : (node as SwitchNode).version;
	return node.self || (node.online && v === version);
}
