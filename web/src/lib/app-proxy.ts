// HTTPS names of Docker apps (backend package appproxy): the rules the
// editor checks live, the same as the NAS checks again.

type Ports = { webui: string; ports: { host: number; protocol: string }[] };

const LABEL = /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/;

/** A name is one DNS label: a-z, 0-9 and hyphens, not at the ends, at most 63 characters. */
export function validProxyName(name: string): boolean {
	return LABEL.test(name);
}

/** A domain of one's own: at least two labels, never .local (that one is mDNS). Empty is allowed. */
export function validProxyDomain(domain: string): boolean {
	const value = normalDomain(domain);
	if (!value) return true;
	const labels = value.split('.');
	return (
		value.length <= 200 &&
		labels.length >= 2 &&
		labels.every((label) => LABEL.test(label)) &&
		labels.at(-1) !== 'local'
	);
}

export function normalDomain(domain: string): string {
	return domain.trim().toLowerCase().replace(/\.$/, '');
}

/** The suggested name of an app: its own name, made into a DNS label. */
export function suggestedProxyName(app: string): string {
	return app
		.toLowerCase()
		.replace(/[^a-z0-9-]+/g, '-')
		.replace(/^-+|-+$/g, '')
		.slice(0, 63)
		.replace(/-+$/, '');
}

/** The port to suggest: the one of the WebUI label, else the first published TCP port. */
export function suggestedProxyPort(containers: Ports[]): number {
	for (const container of containers) {
		const port =
			/^(\d{1,5})(?:[/?#].*)?$/.exec(container.webui.trim())?.[1] ??
			/^https?:\/\/\[host\]:(\d{1,5})/i.exec(container.webui.trim())?.[1];
		if (port && +port > 0 && +port < 65536) return +port;
	}
	for (const container of containers) {
		const port = container.ports.find((entry) => entry.protocol !== 'udp' && entry.host > 0);
		if (port) return port.host;
	}
	return 0;
}

/** The addresses an app gets under a name, e.g. https://immich.nas.local. */
export function proxyAddresses(name: string, suffixes: string[]): string[] {
	return validProxyName(name) ? suffixes.map((suffix) => `https://${name}.${suffix}`) : [];
}

/** "apps.local", " Apps " and "apps" mean the same local name. */
export function normalLocalName(name: string): string {
	return normalDomain(name)
		.replace(/\.local$/, '')
		.replace(/\.$/, '');
}

/** A local name: one to three labels, empty for the NAS's name. */
export function validLocalName(name: string): boolean {
	const value = normalLocalName(name);
	if (!value) return true;
	const labels = value.split('.');
	return labels.length <= 3 && labels.every((label) => LABEL.test(label) && label !== 'local');
}

/** Splits a list typed with commas, spaces or line breaks. */
export function splitList(text: string): string[] {
	return text
		.split(/[\s,;]+/)
		.map((item) => item.trim())
		.filter(Boolean);
}

const IPV4 = /^(25[0-5]|2[0-4]\d|1?\d?\d)(\.(25[0-5]|2[0-4]\d|1?\d?\d)){3}$/;

function validIPv6(text: string): boolean {
	if (!/^[0-9a-f:.]+$/i.test(text) || !text.includes(':')) return false;
	try {
		new URL(`http://[${text}]/`);
		return true;
	} catch {
		return false;
	}
}

/** A network as the NAS takes it: CIDR or a single IPv4/IPv6 address. */
export function validNetwork(text: string): boolean {
	const [address, bits, extra] = text.trim().split('/');
	if (extra !== undefined || !address) return false;
	const v4 = IPV4.test(address);
	if (!v4 && !validIPv6(address)) return false;
	if (bits === undefined) return true;
	return /^\d{1,3}$/.test(bits) && +bits <= (v4 ? 32 : 128);
}

/** The address of a target: an IP or a host name (the NAS checks the network). */
export function validTargetHost(host: string): boolean {
	const value = normalDomain(host);
	if (!value) return false;
	if (IPV4.test(value) || validIPv6(value)) return true;
	return value.length <= 253 && value.split('.').every((label) => LABEL.test(label));
}

/** Who may reach a name. */
export type ProxyAccess = 'local' | 'networks' | 'all';

/** A route as the form edits it. */
export type ProxyForm = {
	enabled: boolean;
	name: string;
	aliases: string;
	/** Full host names of their own (photos.example.org), comma separated. */
	domains: string;
	short: boolean;
	host: string;
	port: number;
	scheme: 'http' | 'https';
	verifyTLS: boolean;
	access: ProxyAccess;
	networks: string;
	tailscale: boolean;
	tailscaleName: string;
};

type Stored = {
	enabled: boolean;
	name: string;
	aliases?: string[];
	domains?: string[];
	short?: boolean;
	host?: string;
	port: number;
	scheme: 'http' | 'https';
	verifyTLS?: boolean;
	localOnly?: boolean;
	networks?: string[];
	tailscale?: boolean;
	tailscaleName?: string;
};

export function proxyForm(route: Stored | null, name = ''): ProxyForm {
	return {
		enabled: route?.enabled ?? false,
		name: route?.name ?? name,
		aliases: (route?.aliases ?? []).join(', '),
		domains: (route?.domains ?? []).join(', '),
		short: !!route?.short,
		host: route?.host ?? '',
		port: route?.port ?? 0,
		scheme: route?.scheme ?? 'http',
		verifyTLS: !!route?.verifyTLS,
		access: route?.networks?.length
			? 'networks'
			: route === null || route.localOnly
				? 'local'
				: 'all',
		networks: (route?.networks ?? []).join('\n'),
		tailscale: !!route?.tailscale,
		tailscaleName: route?.tailscaleName ?? '',
	};
}

/** A route's device name in the tailnet: its own, else the main name. */
export function tailnetName(route: { name: string; tailscale?: boolean; tailscaleName?: string }) {
	return route.tailscaleName || route.name;
}

/** What PUT /docker/proxy/apps|targets/{id} takes. */
export function proxyBody(form: ProxyForm, target: boolean) {
	return {
		enabled: form.enabled,
		name: form.name.trim().toLowerCase(),
		aliases: splitList(form.aliases.toLowerCase()),
		domains: splitList(form.domains).map(normalDomain),
		short: form.short,
		...(target ? { host: normalDomain(form.host) } : {}),
		port: form.port,
		scheme: form.scheme,
		verifyTLS: form.scheme === 'https' && form.verifyTLS,
		localOnly: form.access !== 'all',
		networks: form.access === 'networks' ? splitList(form.networks) : [],
		tailscale: form.tailscale,
		tailscaleName: form.tailscale ? form.tailscaleName.trim().toLowerCase() : '',
	};
}

/** Every name a route answers to. */
export function routeNames(route: { name: string; aliases?: string[] }): string[] {
	return [route.name, ...(route.aliases ?? [])];
}

export type ProxyProblem =
	| { key: 'proxy.nameInvalid' }
	| { key: 'proxy.aliasInvalid'; alias: string }
	| { key: 'proxy.aliasTooMany' }
	| { key: 'proxy.domainInvalid'; domain: string }
	| { key: 'proxy.domainTooMany' }
	| { key: 'proxy.nameTaken'; app: string }
	| { key: 'proxy.portInvalid' }
	| { key: 'proxy.hostInvalid' }
	| { key: 'proxy.networksInvalid'; network: string }
	| { key: 'proxy.networksEmpty' }
	| { key: 'proxy.shortIsNas'; name: string }
	| { key: 'proxy.tailnetNameInvalid' }
	| { key: 'proxy.tailnetNameTaken'; app: string };

/**
 * What is wrong with a form before saving; the NAS checks the same again.
 * others are the other routes (apps and targets), localLabel what stands
 * before ".local".
 */
export function proxyProblems(
	form: ProxyForm,
	target: boolean,
	others: {
		app: string;
		name: string;
		aliases?: string[];
		tailscale?: boolean;
		tailscaleName?: string;
	}[],
	localLabel: string,
): ProxyProblem[] {
	const out: ProxyProblem[] = [];
	const body = proxyBody(form, target);
	if (!validProxyName(body.name)) out.push({ key: 'proxy.nameInvalid' });
	const bad = body.aliases.find((alias) => !validProxyName(alias));
	if (bad) out.push({ key: 'proxy.aliasInvalid', alias: bad });
	if (body.aliases.length > 8) out.push({ key: 'proxy.aliasTooMany' });
	const wrongDomain = body.domains.find((domain) => !validProxyDomain(domain));
	if (wrongDomain) out.push({ key: 'proxy.domainInvalid', domain: wrongDomain });
	if (body.domains.length > 8) out.push({ key: 'proxy.domainTooMany' });
	const mine = [body.name, ...body.aliases];
	const owner = others.find((other) => routeNames(other).some((name) => mine.includes(name)));
	if (owner) out.push({ key: 'proxy.nameTaken', app: owner.app });
	if (!Number.isInteger(body.port) || body.port < 1 || body.port > 65535)
		out.push({ key: 'proxy.portInvalid' });
	if (target && !validTargetHost(form.host)) out.push({ key: 'proxy.hostInvalid' });
	if (form.access === 'networks') {
		const wrong = body.networks.find((network) => !validNetwork(network));
		if (wrong) out.push({ key: 'proxy.networksInvalid', network: wrong });
		else if (!body.networks.length) out.push({ key: 'proxy.networksEmpty' });
	}
	if (body.short && localLabel && mine.includes(localLabel))
		out.push({ key: 'proxy.shortIsNas', name: localLabel });
	if (body.tailscale) {
		if (body.tailscaleName && !validProxyName(body.tailscaleName))
			out.push({ key: 'proxy.tailnetNameInvalid' });
		const device = body.tailscaleName || body.name;
		const holder = others.find((other) => other.tailscale && tailnetName(other) === device);
		if (holder) out.push({ key: 'proxy.tailnetNameTaken', app: holder.app });
	}
	return out;
}

// "Im Tailnet veröffentlichen" (POST /docker/proxy/apps/{app}/tailnet).

/** Where auth keys are made in the Tailscale admin console. */
export const TAILSCALE_KEYS_URL = 'https://login.tailscale.com/admin/settings/keys';
/** Where MagicDNS and HTTPS certificates are switched on. */
export const TAILSCALE_DNS_URL = 'https://login.tailscale.com/admin/dns';

/** The port to publish: the main service's WebUI first, then the other services. */
export function tailnetPort(members: Ports[], primary?: Ports | null): number {
	return suggestedProxyPort(primary ? [primary, ...members] : members);
}

/** Where a published app stands, as the dialog shows it. */
export type TailnetStep =
	'off' | 'start' | 'login' | 'https-off' | 'error' | 'check' | 'ready' | 'unreachable';

type TailnetRoute = {
	enabled: boolean;
	tailscale?: boolean;
	tailnet?: { state: string; url?: string; error?: string };
};

/**
 * The step of a publication: the device's state, and once it runs, what
 * asking its address gave (null: not asked yet; failed: answers without
 * success so far, `limit` of them mean it is not reachable from the NAS).
 */
export function tailnetStep(
	route: TailnetRoute | null | undefined,
	check: { reachable: boolean } | null,
	failed = 0,
	limit = 12,
): TailnetStep {
	if (!inTailnet(route)) return 'off';
	switch (route?.tailnet?.state) {
		case undefined:
		case 'starting':
			return 'start';
		case 'login':
			return 'login';
		case 'https-off':
			return 'https-off';
		case 'error':
			return 'error';
		case 'running':
			if (check?.reachable) return 'ready';
			return failed >= limit ? 'unreachable' : 'check';
		default:
			return 'off';
	}
}

/** Whether an app is in the tailnet (or on its way there). */
export function inTailnet(route: TailnetRoute | null | undefined): boolean {
	return !!route?.enabled && !!route.tailscale;
}
