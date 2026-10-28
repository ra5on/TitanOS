// Dynamic DNS shared by Settings › Dynamic DNS and Settings › IPv64 ›
// DynDNS: the types titand sends, the form of the updater settings and the
// calls of the IPv64 healthcheck. Both pages work on the same settings
// (/config/ddns/settings.json).
import { api } from '#lib/api.ts';
import type { DnsProvider, SecretName } from '#lib/dns-providers.ts';
import type { MessageKey } from '#lib/i18n/index.svelte.ts';

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

/** The intervals offered besides the default, in minutes. */
export const INTERVALS = [5, 10, 15, 30, 60, 360, 1440];

/** At most this many names are kept up to date. */
export const MAX_HOSTS = 8;

/** The updater settings both pages edit alike. */
export type UpdaterForm = { ipv4: boolean; ipv6: boolean; interval: number; lookupURL: string; iface: string };

export function updaterForm(s: DdnsStatus['settings']): UpdaterForm {
	return { ipv4: s.ipv4, ipv6: s.ipv6, interval: s.interval ?? 0, lookupURL: s.lookupURL ?? '', iface: s.interface ?? '' };
}

/** The updater part of PUT /network/ddns and PUT /ipv64/ddns. */
export function updaterPayload(form: UpdaterForm) {
	return { ipv4: form.ipv4, ipv6: form.ipv6, interval: form.interval, lookupURL: form.lookupURL.trim(), interface: form.iface.trim() };
}

/** The state chip of dynamic DNS. */
export function ddnsBadge(status: DdnsStatus | null): { text: MessageKey; tone: string } {
	if (!status?.settings.enabled) return { text: 'ddns.state.off', tone: '' };
	if (status.lastError) return { text: 'ddns.state.error', tone: 'warn' };
	if (status.lastUpdate || status.publishedIPv4 || status.publishedIPv6) return { text: 'ddns.state.ok', tone: 'ok' };
	return { text: 'ddns.state.waiting', tone: '' };
}

/** Sends the addresses now, even if they did not change. */
export function updateNow(): Promise<DdnsStatus> {
	return api.post<DdnsStatus>('/network/ddns/update', {});
}

/** Adds a name to a list of names (lower case, no doubles, at most MAX_HOSTS). */
export function withHost(hosts: string[], name: string): string[] {
	const value = name.trim().toLowerCase().replace(/\.$/, '');
	return !value || hosts.includes(value) || hosts.length >= MAX_HOSTS ? hosts : [...hosts, value];
}

/** The address a router or other updater calls for a domain of IPv64 (the token stays on ipv64.net). */
export function ipv64UpdateURL(domain: string, token = '<update-token>'): string {
	return `https://ipv64.net/nic/update?key=${token}&domain=${domain}&ip=<ipaddr>&ip6=<ip6addr>`;
}

// --- the healthcheck "watch reachability" (POST /network/ipv64/heartbeat/…) ---

/** The body of the IPv64 calls: a key typed in but not saved yet goes along; else titand takes the stored one. */
export function keyBody(apiKey: string, extra: Record<string, unknown> = {}): Record<string, unknown> {
	return apiKey.trim() ? { apiKey: apiKey.trim(), ...extra } : extra;
}

export type HeartbeatChoice = { check: string; name: string; period: number; grace: number; integrations: number[]; autoPause: boolean };

export function heartbeatChecks(apiKey: string) {
	return api.post<{ healthchecks: Ipv64Healthcheck[]; integrations: Ipv64Integration[]; suggestedName: string }>('/network/ipv64/healthchecks', keyBody(apiKey));
}

export function heartbeatOn(apiKey: string, c: HeartbeatChoice): Promise<DdnsStatus> {
	return api.post<DdnsStatus>(
		'/network/ipv64/heartbeat/on',
		keyBody(
			apiKey,
			c.check === 'new'
				? { name: c.name.trim(), periodMinutes: c.period, graceMinutes: c.grace, integrations: c.integrations, autoPause: c.autoPause }
				: { id: c.check, autoPause: c.autoPause },
		),
	);
}

export function heartbeatOff(apiKey: string, action: '' | 'pause' | 'delete'): Promise<DdnsStatus> {
	return api.post<DdnsStatus>('/network/ipv64/heartbeat/off', keyBody(apiKey, { action }));
}

export function heartbeatRemote(apiKey: string): Promise<Ipv64Healthcheck> {
	return api.post<Ipv64Healthcheck>('/network/ipv64/heartbeat/status', keyBody(apiKey));
}

export function heartbeatTest(apiKey: string): Promise<unknown> {
	return api.post('/network/ipv64/heartbeat/test', keyBody(apiKey));
}

/** Whether a new healthcheck may be created with these values. */
export function heartbeatChoiceValid(c: HeartbeatChoice): boolean {
	return c.check !== 'new' || (c.name.trim().length > 0 && c.period >= 20 && c.period <= 1440 && c.grace >= 1 && c.grace <= 1440);
}
