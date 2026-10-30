// Settings › WireGuard: the VPN server of the NAS (backend package wgserver).

export type WireGuardPeer = {
	name: string;
	publicKey: string;
	address: string;
	created: string;
	lastHandshake?: string;
	received: number;
	sent: number;
	endpoint?: string;
};

export type WireGuardStatus = {
	enabled: boolean;
	running: boolean;
	endpoint: string;
	port: number;
	network: string;
	fullTunnel: boolean;
	dns: string;
	publicKey?: string;
	peers: WireGuardPeer[];
	error?: string;
	disabled?: boolean;
};

/** A device counts as connected after a handshake in the last three minutes (WireGuard renews every two). */
export function connected(peer: WireGuardPeer, now: number): boolean {
	return !!peer.lastHandshake && now - Date.parse(peer.lastHandshake) < 3 * 60 * 1000;
}

/** A file name for the configuration of a device: wg-quick takes the interface name from it. */
export function configFileName(name: string): string {
	const base = name
		.normalize('NFKD')
		.replace(/\p{M}/gu, '')
		.replace(/[^A-Za-z0-9_=+.-]+/g, '-')
		.replace(/^-+|-+$/g, '')
		.slice(0, 15);
	return `${base || 'titanos'}.conf`;
}

/** A rough check of the address devices dial; the NAS checks again. */
export function validEndpoint(host: string): boolean {
	const value = host.trim();
	if (!value) return true;
	if (value.includes(':')) return /^[0-9a-fA-F:]+$/.test(value);
	return (
		/^[A-Za-z0-9.-]{1,253}$/.test(value) &&
		!value.startsWith('.') &&
		!value.endsWith('.') &&
		!value.includes('..')
	);
}
