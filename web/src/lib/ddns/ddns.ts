// Dynamic DNS (Settings › Dynamic DNS): the types titand sends and the form
// of the updater settings (/config/ddns/settings.json).
import { api } from '#lib/api.ts';
import type { DnsProvider, SecretName } from '#lib/dns-providers.ts';
import type { MessageKey } from '#lib/i18n/index.svelte.ts';

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
	};
	secrets: Partial<Record<SecretName, boolean>>;
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
};

/** The intervals offered besides the default, in minutes. */
export const INTERVALS = [5, 10, 15, 30, 60, 360, 1440];

/** At most this many names are kept up to date. */
export const MAX_HOSTS = 8;

/** The updater settings of the form. */
export type UpdaterForm = { ipv4: boolean; ipv6: boolean; interval: number; lookupURL: string; iface: string };

export function updaterForm(s: DdnsStatus['settings']): UpdaterForm {
	return { ipv4: s.ipv4, ipv6: s.ipv6, interval: s.interval ?? 0, lookupURL: s.lookupURL ?? '', iface: s.interface ?? '' };
}

/** The updater part of PUT /network/ddns. */
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
