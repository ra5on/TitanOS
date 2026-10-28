// Types of the dynamic DNS page and of IPv64.net, as titand sends them.
import type { DnsProvider, SecretName } from '#lib/dns-providers.ts';

export type Heartbeat = {
	enabled: boolean;
	check?: string;
	interval?: number;
	autoPause?: boolean;
};

export type DdnsStatus = {
	settings: {
		enabled: boolean;
		hostnames: string[];
		provider: DnsProvider;
		ipv4: boolean;
		ipv6: boolean;
		interval?: number;
		lookupURL?: string;
		interface?: string;
		heartbeat: Heartbeat;
	};
	secrets: Partial<Record<SecretName | 'heartbeat', boolean>>;
	currentIPv4?: string;
	currentIPv6?: string;
	publishedIPv4?: string;
	publishedIPv6?: string;
	lastCheck?: string;
	lastUpdate?: string;
	lastError?: string;
	nextCheck?: string;
	running: boolean;
	disabled?: boolean;
	heartbeat: { lastPing?: string; lastError?: string; nextPing?: string; paused?: boolean };
};

export type Ipv64Usage = { used: number; limit: number | null };
export type Ipv64Account = { username: string; plan: string; validUntil?: string; usage: Record<string, Ipv64Usage>; keyName?: string };
export type Ipv64Domain = { id: number; name: string; kind: string; zone: string; wildcard: boolean; ignore: string; active: boolean; updateCount: number };
export type Ipv64Record = { id: number; prefix: string; fqdn: string; type: string; content: string; ttl: number; state: string; pendingDNS: boolean };
export type Ipv64Healthcheck = {
	id: string;
	name: string;
	status: string;
	lastSeenAt?: string;
	expectedBy?: string;
	periodSeconds: number;
	graceSeconds: number;
	integrations: number[];
};
export type Ipv64Integration = { id: number; type: string; name: string; status: string };

/** The usage entries worth showing, in this order. */
export const USAGE_KEYS = ['dyndns_updates_24h', 'dyndns_domains', 'own_domains', 'healthchecks', 'healthcheck_pings_24h'];

/** "3 of 48", "3" without a limit. */
export function usageText(usage: Ipv64Usage | undefined, of: (used: number, limit: number) => string): string {
	if (!usage) return '–';
	return usage.limit === null || usage.limit === undefined ? String(usage.used) : of(usage.used, usage.limit);
}

/** The CAA record that allows only Let's Encrypt to issue certificates. */
export const CAA_LETSENCRYPT = '0 issue "letsencrypt.org"';

/** A rough check of record contents before they are sent (titand checks again). */
export function recordProblem(type: string, content: string): boolean {
	const value = content.trim();
	if (!value || /[\r\n]/.test(value)) return true;
	switch (type) {
		case 'A':
			return !/^(\d{1,3}\.){3}\d{1,3}$/.test(value);
		case 'AAAA':
			return !value.includes(':');
		case 'MX':
			return !/^\d{1,5}\s+\S+$/.test(value);
		case 'SRV':
			return !/^\d+\s+\d+\s+\d+\s+\S+$/.test(value);
		case 'CAA':
			return !/^\d{1,3}\s+(issue|issuewild|iodef)\s+\S+/.test(value);
	}
	return false;
}
