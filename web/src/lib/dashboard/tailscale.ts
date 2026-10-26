// Devices of the tailnet as GET /plugins/tailscale/devices reports them.
// Unknown values stay null or empty; the ID is Tailscale's stable node ID.

export type TailnetDevice = {
	id: string;
	name: string;
	dnsName: string;
	hostName: string;
	ipv4: string[];
	ipv6: string[];
	online: boolean | null;
	lastSeen: string | null;
	os?: string;
	exitNodeOption: boolean;
	exitNode: boolean;
	routes: string[];
	self: boolean;
};
export type TailnetDevices = {
	active: boolean;
	state: string;
	devices: TailnetDevice[];
	updated: string;
};

export const TAILSCALE_FIELDS = [
	'status',
	'self',
	'ipv4',
	'ipv6',
	'hostName',
	'dnsName',
	'lastSeen',
	'os',
	'exitNode',
	'routes',
] as const;
export type TailscaleField = (typeof TAILSCALE_FIELDS)[number];
export const DEFAULT_TAILSCALE_FIELDS: TailscaleField[] = [
	'status',
	'self',
	'ipv4',
	'lastSeen',
	'os',
];
/** Shown as a marker next to the name instead of a labelled row. */
export const MARKER_FIELDS: TailscaleField[] = ['status', 'self'];

export function tailscaleFields(value: unknown): TailscaleField[] {
	return Array.isArray(value)
		? [...new Set(value.filter((v): v is TailscaleField => TAILSCALE_FIELDS.includes(v)))]
		: [...DEFAULT_TAILSCALE_FIELDS];
}

/** Devices of a tile: the chosen ones (null = all, including new) and optionally only online ones. */
export function visibleDevices(
	devices: TailnetDevice[],
	items: string[] | null,
	onlineOnly: boolean,
): TailnetDevice[] {
	return devices.filter(
		(d) => (items === null || items.includes(d.id)) && (!onlineOnly || d.online === true),
	);
}
