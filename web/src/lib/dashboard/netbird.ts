// Peers of the NetBird network as GET /plugins/netbird/peers reports them.
// Unknown values stay null or empty; the ID is the peer's WireGuard public key.

export type NetbirdPeer = {
	id: string;
	name: string;
	fqdn: string;
	ipv4: string;
	ipv6: string;
	online: boolean | null;
	lastSeen: string | null;
	connection?: 'p2p' | 'relayed';
	networks: string[];
	self: boolean;
};
export type NetbirdPeers = {
	active: boolean;
	state: string;
	peers: NetbirdPeer[];
	updated: string;
};

export const NETBIRD_FIELDS = [
	'status',
	'self',
	'ipv4',
	'ipv6',
	'fqdn',
	'lastSeen',
	'connection',
	'networks',
] as const;
export type NetbirdField = (typeof NETBIRD_FIELDS)[number];
export const DEFAULT_NETBIRD_FIELDS: NetbirdField[] = [
	'status',
	'self',
	'ipv4',
	'connection',
	'lastSeen',
];
/** Shown as a marker next to the name instead of a labelled row. */
export const NETBIRD_MARKER_FIELDS: NetbirdField[] = ['status', 'self'];

export function netbirdFields(value: unknown): NetbirdField[] {
	return Array.isArray(value)
		? [...new Set(value.filter((v): v is NetbirdField => NETBIRD_FIELDS.includes(v)))]
		: [...DEFAULT_NETBIRD_FIELDS];
}

/** Peers of a tile: the chosen ones (null = all, including new) and optionally only connected ones. */
export function visiblePeers(
	peers: NetbirdPeer[],
	items: string[] | null,
	onlineOnly: boolean,
): NetbirdPeer[] {
	return peers.filter(
		(p) => (items === null || items.includes(p.id)) && (!onlineOnly || p.online === true),
	);
}
