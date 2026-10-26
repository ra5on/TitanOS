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
export type NetworkStatus = {
	config: NetworkConfig;
	pending: NetworkConfig | null;
	pendingSeconds: number;
	interfaces: { name: string; mac: string; carrier: boolean; speedMbps: number }[];
	device: string;
	addresses: string[];
	gateway: string;
	dns: string[];
	configured: boolean;
	bridgeReady: boolean;
};
export function topologyDraft(cfg: NetworkConfig, ports: string[]): NetworkConfig {
	if (cfg.networks?.length) return structuredClone(cfg);
	const bond = cfg.ports.length > 1;
	const parent = bond ? 'bond:uplink' : (cfg.ports[0] ?? ports[0] ?? '');
	return {
		...structuredClone(cfg),
		bonds: bond ? [{ id: 'uplink', ports: [...cfg.ports], mode: cfg.bondMode }] : [],
		networks: [
			{
				id: 'admin',
				name: cfg.hostname,
				purpose: 'management',
				parent,
				vlan: cfg.vlan,
				bridge: cfg.bridge,
				management: true,
				ipv4: cfg.dhcp ? 'dhcp' : 'static',
				address: cfg.dhcp ? '' : cfg.address,
				gateway: cfg.dhcp ? '' : cfg.gateway,
				ipv6: 'auto',
				address6: '',
				gateway6: '',
				dns: [...cfg.dns],
			},
		],
	};
}
export function networkPayload(cfg: NetworkConfig): NetworkConfig {
	const copy = structuredClone(cfg);
	for (const n of copy.networks ?? []) {
		n.vlan = Number(n.vlan);
		if (n.ipv4 !== 'static') {
			n.address = '';
			n.gateway = '';
		}
		if (n.ipv6 !== 'static') {
			n.address6 = '';
			n.gateway6 = '';
		}
		n.dns = n.dns.map((s) => s.trim()).filter(Boolean);
		for (const r of n.routes ?? []) r.metric = Number(r.metric);
	}
	return copy;
}
