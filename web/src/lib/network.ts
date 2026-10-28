export type NetworkBond = { id: string; ports: string[]; mode: string };
export type NetworkRoute = { destination: string; gateway: string; metric: number };
export type NASNetwork = {
	id: string;
	name: string;
	purpose: 'management' | 'storage' | 'apps' | 'custom';
	parent: string;
	vlan: number;
	bridge: boolean;
	management: boolean;
	ipv4: 'off' | 'dhcp' | 'static';
	address: string;
	gateway: string;
	ipv6: 'off' | 'auto' | 'dhcp' | 'static';
	address6: string;
	gateway6: string;
	dns: string[];
	routes?: NetworkRoute[];
};
export type NetworkConfig = {
	hostname: string;
	// The flat fields of network.json. For older configurations (`migrated`)
	// the NAS derives networks[] from them in `topology`; the page only edits
	// networks[].
	ports: string[];
	bondMode: string;
	vlan: number;
	bridge: boolean;
	dhcp: boolean;
	address: string;
	gateway: string;
	dns: string[];
	networks?: NASNetwork[];
	bonds?: NetworkBond[];
};
export type NetworkPort = { name: string; mac: string; carrier: boolean; speedMbps: number };
export type NetworkLive = { device: string; up: boolean; addresses: string[] };
export type NetworkStatus = {
	config: NetworkConfig;
	pending: NetworkConfig | null;
	pendingSeconds: number;
	interfaces: NetworkPort[];
	device: string;
	addresses: string[];
	gateway: string;
	dns: string[];
	configured: boolean;
	bridgeReady: boolean;
	migrated?: boolean;
	// config and pending in the multi-network form (older configurations
	// converted, others unchanged); config and pending stay as stored.
	topology: NetworkConfig;
	pendingTopology: NetworkConfig | null;
	live?: Record<string, NetworkLive>;
	// Whether the page offers VLANs (switched on or in use); missing from
	// older titand, which always showed them.
	vlans?: boolean;
	vlansInUse?: boolean;
};

/** Translation keys of the bond modes the NAS offers. */
export const bondModeKeys = {
	'active-backup': 'netcfg.bond.active-backup',
	'802.3ad': 'netcfg.bond.802.3ad',
	'balance-alb': 'netcfg.bond.balance-alb',
} as const;

export const purposes = ['management', 'storage', 'apps', 'custom'] as const;

/** What the page sends: values typed as text become numbers, unused fields empty. */
export function networkPayload(cfg: NetworkConfig): NetworkConfig {
	const copy = structuredClone(cfg);
	for (const n of copy.networks ?? []) {
		n.vlan = Number(n.vlan) || 0;
		if (n.ipv4 !== 'static') {
			n.address = '';
			n.gateway = '';
		}
		if (n.ipv6 !== 'static') {
			n.address6 = '';
			n.gateway6 = '';
		}
		n.address = n.address.trim();
		n.gateway = n.gateway.trim();
		n.address6 = n.address6.trim();
		n.gateway6 = n.gateway6.trim();
		n.dns = parseDns(n.dns.join(' '));
		for (const r of n.routes ?? []) r.metric = Number(r.metric) || 0;
	}
	return copy;
}

/** Name servers as typed: separated by spaces, commas or semicolons, no repeats. */
export function parseDns(text: string): string[] {
	return [...new Set(text.split(/[\s,;]+/).filter(Boolean))];
}

/** A rough check before the NAS decides: an IPv4 or IPv6 address, nothing else. */
export function looksLikeIP(value: string): boolean {
	if (/^(25[0-5]|2[0-4]\d|1?\d?\d)(\.(25[0-5]|2[0-4]\d|1?\d?\d)){3}$/.test(value)) return true;
	return value.includes(':') && /^[0-9a-fA-F:.]+$/.test(value);
}

export function blankNetwork(cfg: NetworkConfig, ports: NetworkPort[]): NASNetwork {
	const used = new Set((cfg.bonds ?? []).flatMap((b) => b.ports));
	const parent =
		ports.find((p) => !used.has(p.name))?.name ?? (cfg.bonds?.[0] ? 'bond:' + cfg.bonds[0].id : '');
	return {
		id: freeId(cfg.networks?.map((n) => n.id) ?? [], 'net'),
		name: '',
		purpose: 'custom',
		parent,
		vlan: 0,
		bridge: false,
		management: !cfg.networks?.length,
		ipv4: 'dhcp',
		address: '',
		gateway: '',
		ipv6: 'auto',
		address6: '',
		gateway6: '',
		dns: [],
		routes: [],
	};
}

export function blankBond(cfg: NetworkConfig): NetworkBond {
	return {
		id: freeId(cfg.bonds?.map((b) => b.id) ?? [], 'bond'),
		ports: [],
		mode: 'active-backup',
	};
}

// IDs become device names (tv<id>, tn<id>, tb<id>), so they stay short and
// are never reused while the draft is open.
function freeId(taken: string[], base: string): string {
	for (let i = 1; ; i++) {
		const id = base + i;
		if (!taken.includes(id)) return id;
	}
}

/** The port or bond a network runs on, as people read it. */
export function parentLabel(parent: string): string {
	return parent.startsWith('bond:') ? parent.slice(5) : parent;
}

/** Who uses a physical port: its bond, or the networks directly on it. */
export function portUsers(
	cfg: NetworkConfig,
	port: string,
): { kind: 'bond' | 'network'; name: string }[] {
	const bond = cfg.bonds?.find((b) => b.ports.includes(port));
	if (bond) return [{ kind: 'bond', name: bond.id }];
	return (cfg.networks ?? [])
		.filter((n) => n.parent === port)
		.map((n) => ({ kind: 'network', name: n.name }));
}

/** The networks on a bond; a bond in use cannot be removed. */
export function bondUsers(cfg: NetworkConfig, id: string): string[] {
	return (cfg.networks ?? []).filter((n) => n.parent === 'bond:' + id).map((n) => n.name);
}

/** Ports a bond may take: free ones and its own. */
export function bondCandidates(
	cfg: NetworkConfig,
	ports: NetworkPort[],
	bond: NetworkBond,
): NetworkPort[] {
	const elsewhere = new Set(
		(cfg.bonds ?? []).filter((b) => b.id !== bond.id).flatMap((b) => b.ports),
	);
	return ports.filter((p) => !elsewhere.has(p.name));
}

export type LiveState = 'up' | 'noAddress' | 'down' | 'draft' | 'unknown';

/** The state chip of a network card: drafts have none yet, nor do networks the NAS did not report. */
export function liveState(status: NetworkStatus, n: NASNetwork, applied: boolean): LiveState {
	if (!applied) return 'draft';
	const live = status.live?.[n.id];
	if (!live) return 'unknown';
	if (!live.up) return 'down';
	return live.addresses.length ? 'up' : 'noAddress';
}

/** The address of the management network the page should be opened at after a change. */
export function managementURL(n: NASNetwork, here: string): string {
	const address = (n.address || n.address6).split('/')[0];
	if (!address) return '';
	const url = new URL(here);
	url.hostname = address.includes(':') ? `[${address}]` : address;
	return url.href;
}
