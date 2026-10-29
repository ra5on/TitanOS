// Types and helpers of Settings › IPv64. The field names are those of the
// OpenAPI description of IPv64 (https://ipv64.net/api/v2/openapi.json), as
// titand passes them on; secrets (tokens, push URLs, update tokens, keys of
// integrations, WireGuard keys) are never part of them.

export const TABS = ['overview', 'domains', 'dyndns', 'assignments', 'monitoring', 'failover', 'public', 'router', 'blocklist'] as const;
export type Tab = (typeof TABS)[number];

export function tabFrom(value: string | null): Tab {
	return (TABS as readonly string[]).includes(value ?? '') ? (value as Tab) : 'overview';
}

/** Where a sub-tab of IPv64 lives: the tab "IPv64" of Settings › Dynamic DNS. */
export function ipv64Href(sub?: string | null): string {
	return sub && (TABS as readonly string[]).includes(sub) ? `/settings/ddns?tab=ipv64&sub=${sub}` : '/settings/ddns?tab=ipv64';
}

/** The two tabs of Settings › Dynamic DNS. */
export const DDNS_TABS = ['ddns', 'ipv64'] as const;
export type DdnsTab = (typeof DDNS_TABS)[number];

export function ddnsTabFrom(value: string | null): DdnsTab {
	return value === 'ipv64' ? 'ipv64' : 'ddns';
}

export type Usage = { used: number; limit: number | null; limited?: boolean };
export type Me = {
	username: string | null;
	registered_at: string | null;
	last_login_at: string | null;
	plan: { id: number; name: string; valid_until: string | null };
	usage: Record<string, Usage>;
	log_retention_days: number;
	auth: { type: string; key_id: number | null; key_name: string };
};
export type LogEntry = { id: number; domain_id: number | null; title: string; message: string; time: string };
export type LogPage = { entries: LogEntry[]; next_before: number | null; retention_days: number };
export type ApiKey = {
	id: number;
	name: string;
	token_hint: string;
	created_at: string | null;
	expires_at: string | null;
	expired: boolean;
	uses: number;
	usedBy: string[];
};

export type Domain = { id: number; name: string; kind: string; zone: string; wildcard: boolean; ignore: string; ipv6Prefix?: string; active: boolean; updateCount: number };
export type DnsRecord = {
	id: number;
	domain: string;
	prefix: string;
	fqdn: string;
	type: string;
	content: string;
	ttl: number;
	state: string;
	pending_dns: boolean;
	token: string;
};
export type DomainDetail = Domain & { records: DnsRecord[] };

export type CheckEvent = { id: number; time: string; status: string; message: string; latency_ms: number | null };
export type Check = {
	id: string;
	name: string;
	type: string;
	status: string;
	notify: { on_down: boolean; on_up: boolean; integrations: number[] };
	created_at: string | null;
	last_seen_at: string | null;
	last_alarm_at: string | null;
	pings_total: number;
	failed_in_row: number;
	push?: { period_seconds: number; grace_seconds: number; expected_by: string | null };
	check?: {
		target: string;
		prefer_ipv4: boolean;
		port: number;
		url: string;
		verify_tls: boolean;
		keyword: string;
		interval_seconds: number;
		retry_interval_seconds: number;
		failures_before_alarm: number;
		next_check_at: string | null;
	} | null;
	recent?: CheckEvent[];
};
export type Integration = { id: number; type: string; name: string; status: string; default: boolean | null; options: { [key: string]: unknown }; uses: number };
export type Incident = { id: number; status: string; created_at: string | null; closed_at: string | null; events: { time: string | null; kind: string; message: string }[] };
export type Stats = {
	range: string;
	measurements: number;
	availability_percent: number | null;
	latency_ms: { avg: number | null; min: number | null; max: number | null };
	bucket_seconds: number;
	series: { time: string; measurements: number; availability_percent: number; latency_avg_ms: number | null; latency_max_ms: number | null }[];
};
export type StatusPage = {
	id: number;
	name: string;
	slug: string;
	url: string;
	description: string;
	password_protected: boolean;
	design: number;
	autorefresh: boolean;
	healthchecks: string[];
};
export type OfflinePage = {
	id: string;
	name: string;
	domain: string;
	email: string;
	phone: string;
	alternative_url: string;
	text: string;
	icon_url: string | null;
	views: number;
	in_use_by_policy: string | null;
	active: boolean;
	preview_url: string;
};
export type Policy = {
	id: string;
	name: string;
	status: string;
	manual: boolean;
	mode: string;
	fallback_requires_all: boolean;
	linking: string;
	notifications_enabled: boolean;
	integrations: number[];
	monitors_down: number;
	monitors: { healthcheck: string; name: string; negate: boolean; healthcheck_status: string | null }[];
	rules: { record: string; fqdn: string | null; record_type: string; state: string; target: { kind: string; ip?: string; record?: string; offline_page?: string } }[];
};
export type Maintenance = { id: string; domain: string; check: string; checkName: string; createdCheck?: boolean; offlinePage: string; policy: string; records: string[]; at: string };

export type Mapping = { id: string; name: string; listen_port: number; transport: string; target_port: number; record: string; fqdn: string | null };
export type CDN = {
	id: string;
	domain: string | null;
	domain_id: number;
	active: boolean;
	pending: string[];
	dns_records_enabled: boolean;
	server: { name: string; hostname: string; ipv4: string; ipv6: string } | null;
	proxy: {
		state: string;
		frontend: string | null;
		backend: { scheme: string; port: number };
		websocket: boolean;
		certificate: string;
		caching: { enabled: boolean; time_minutes: number; size_mb: number };
		access_list: { enabled: boolean; mode: string };
	};
	portmapper: { state: string };
	traffic_mb: { in: number; out: number };
	mappings?: Mapping[];
};
export type Published = { app: string; port: number; domain: string; prefix?: string; cdn: string; createdCDN?: boolean; records: string[]; firewall?: boolean; at: string };

export type Peer = {
	id: string;
	name: string;
	state: string;
	disabled: boolean;
	ipv4: string | null;
	ipv6: string | null;
	public_key: string | null;
	last_handshake: string | null;
	traffic_bytes: { in: number; out: number };
};
export type Gateway = {
	id: string;
	name: string;
	state: string;
	disabled: boolean;
	traffic_limited: boolean;
	server: { name: string; hostname: string; location: string } | null;
	listen_port: number | null;
	ipv4: string | null;
	ipv6: string | null;
	public_key: string | null;
	traffic_bytes: { in: number; out: number };
	peers?: Peer[];
};
export type Router = { id: string; name: string; vrf: string; traffic_limited: boolean; gateways: Gateway[] };
export type Route = { id: number; destination: string; gateway: string; distance: number; peer: string | null; system: boolean; disabled: boolean; state: string };
export type RouterRule = { id: number; chain: string; action: string; family: string; source: string | null; destination: string | null; disabled: boolean; state: string };
export type PublicIP = { id: number; ip: string; type: string; disabled: boolean; state: string };

export type Status = {
	status: {
		settings: {
			notify: { enabled: boolean; failover: boolean; cdnTraffic: boolean; healthchecks: boolean };
			blocklist: { enabled: boolean };
			wireguard: { enabled: boolean; router?: string; gateway?: string; peer?: string; name?: string; publicKey?: string; addresses?: string[]; endpoint?: string; allowedIPs?: string[] };
			published: Published[];
			maintenance: Maintenance[];
		};
		keySet: boolean;
		keySource: '' | 'own' | 'ddns' | 'letsencrypt';
		poll: { last?: string; error?: string };
		blocklistStatus: { entries: number; truncated?: boolean; live: boolean; fetched?: string; next?: string; error?: string };
		tunnel: { up: boolean; error?: string; routes: string[] };
		readOnly: boolean;
	};
	firewall: boolean;
	nasCheck: string;
	hostname: string;
	zones: string[];
	recordTypes: string[];
};

// --- enums of the description ---
export const CHECK_TYPES = ['push', 'icmp', 'tcp', 'http', 'keyword'] as const;
export const STATS_RANGES = ['1h', '24h', '7d', '30d'] as const;
export const POLICY_MODES = ['auto_failover', 'auto_failover_and_fallback', 'manual'] as const;
export const FRONTENDS = ['http', 'https_redirect', 'http_and_https'] as const;
export const TRANSPORTS = ['tcp', 'udp'] as const;
export const RULE_CHAINS = ['forward', 'input', 'output'] as const;
export const RULE_ACTIONS = ['accept', 'drop'] as const;
/** The usage entries of /me in the order shown. */
export const USAGE = [
	'dyndns_domains',
	'own_domains',
	'dyndns_updates_24h',
	'healthchecks',
	'healthcheck_pings_24h',
	'sms_24h',
	'failover_policies',
	'failover_monitors',
	'failover_rules',
	'cdn_domains',
	'cdn_traffic_gb',
	'vpn_traffic_gb',
	'dns64_clients',
] as const;

/** Lifetimes of new API keys, in seconds (0 = unlimited). */
export const KEY_LIFETIMES = [86400 * 7, 86400 * 30, 86400 * 90, 86400 * 365, 0] as const;

/** Fields of an integration per type, after POST /integrations. Secret
 * fields are write-only: TitanOS never reads them back. */
export const INTEGRATION_FIELDS: { [type: string]: { name: string; secret?: boolean; kind?: 'number' | 'url'; required?: boolean; min?: number; max?: number }[] } = {
	telegram: [
		{ name: 'chat_id', required: true },
		{ name: 'api_token', secret: true, required: true },
	],
	gotify: [
		{ name: 'server_url', kind: 'url', secret: true, required: true },
		{ name: 'app_token', secret: true, required: true },
		{ name: 'priority', kind: 'number', min: 0, max: 10 },
	],
	ntfy: [
		{ name: 'server_url', kind: 'url', secret: true, required: true },
		{ name: 'priority_down', kind: 'number', min: 1, max: 5 },
		{ name: 'priority_up', kind: 'number', min: 1, max: 5 },
	],
	pushover: [
		{ name: 'user_token', secret: true, required: true },
		{ name: 'app_token', secret: true, required: true },
		{ name: 'device' },
		{ name: 'priority', kind: 'number', min: -2, max: 1 },
	],
	discord: [{ name: 'webhook_url', kind: 'url', secret: true, required: true }, { name: 'ping_user' }, { name: 'ping_group' }],
	webhook: [
		{ name: 'url_down', kind: 'url', secret: true },
		{ name: 'url_up', kind: 'url', secret: true },
	],
	email: [{ name: 'email', required: true }],
	sms: [{ name: 'number', required: true }],
	app: [{ name: 'device_token', secret: true, required: true }],
};

/** What is missing or wrong in a new integration ([] = fine). */
export function integrationProblems(type: string, values: { [field: string]: string }): string[] {
	const fields = INTEGRATION_FIELDS[type];
	if (!fields) return ['type'];
	const out: string[] = [];
	for (const f of fields) {
		const v = (values[f.name] ?? '').trim();
		if (f.required && !v) out.push(f.name);
		if (v && f.kind === 'url' && !/^https?:\/\/[^\s/]+/.test(v)) out.push(f.name);
		if (v && f.kind === 'number') {
			const n = Number(v);
			if (!Number.isInteger(n) || (f.min !== undefined && n < f.min) || (f.max !== undefined && n > f.max)) out.push(f.name);
		}
	}
	if (type === 'webhook' && !(values.url_down ?? '').trim() && !(values.url_up ?? '').trim()) out.push('url_down');
	return out;
}

/** The body of a new integration: only its own fields, numbers as numbers. */
export function integrationBody(type: string, name: string, preselect: boolean, values: { [field: string]: string }) {
	const body: { [key: string]: unknown } = { type, name: name.trim(), default: preselect };
	for (const f of INTEGRATION_FIELDS[type] ?? []) {
		const v = (values[f.name] ?? '').trim();
		if (!v) continue;
		body[f.name] = f.kind === 'number' ? Number(v) : v;
	}
	if (type === 'sms') body.country_code = '0049';
	return body;
}

export type CheckForm = {
	name: string;
	type: string;
	period: number; // minutes
	grace: number; // minutes
	target: string;
	port: number;
	url: string;
	keyword: string;
	verifyTLS: boolean;
	preferIPv4: boolean;
	interval: number; // seconds
	retry: number;
	failures: number;
	integrations: number[];
	onDown: boolean;
	onUp: boolean;
};

export function checkForm(c?: Check): CheckForm {
	return {
		name: c?.name ?? '',
		type: c?.type ?? 'push',
		period: c?.push ? Math.round(c.push.period_seconds / 60) : 60,
		grace: c?.push ? Math.round(c.push.grace_seconds / 60) : 10,
		target: c?.check?.target ?? '',
		port: c?.check?.port || 443,
		url: c?.check?.url ?? '',
		keyword: c?.check?.keyword ?? '',
		verifyTLS: c?.check?.verify_tls ?? true,
		preferIPv4: c?.check?.prefer_ipv4 ?? false,
		interval: c?.check?.interval_seconds || 300,
		retry: c?.check?.retry_interval_seconds || 60,
		failures: c?.check?.failures_before_alarm || 2,
		integrations: c?.notify.integrations ?? [0],
		onDown: c?.notify.on_down ?? true,
		onUp: c?.notify.on_up ?? true,
	};
}

/** What is wrong with a check form ([] = fine), after the description. */
export function checkProblems(f: CheckForm): string[] {
	const out: string[] = [];
	if (!f.name.trim()) out.push('name');
	if (!(CHECK_TYPES as readonly string[]).includes(f.type)) out.push('type');
	if (f.type === 'push') {
		if (!Number.isInteger(f.period) || f.period < 1) out.push('period');
		if (!Number.isInteger(f.grace) || f.grace < 1) out.push('grace');
	} else {
		if ((f.type === 'icmp' || f.type === 'tcp') && !f.target.trim()) out.push('target');
		if (f.type === 'tcp' && !(f.port >= 1 && f.port <= 65535)) out.push('port');
		if ((f.type === 'http' || f.type === 'keyword') && !/^https?:\/\/\S+$/.test(f.url.trim())) out.push('url');
		if (f.type === 'keyword' && !f.keyword.trim()) out.push('keyword');
		if (!(f.interval >= 1 && f.interval <= 86400)) out.push('interval');
		if (!(f.retry >= 10 && f.retry <= 86400)) out.push('retry');
		if (!(f.failures >= 1 && f.failures <= 60)) out.push('failures');
	}
	return out;
}

/** The body of a check: only the fields of its type. */
export function checkBody(f: CheckForm) {
	const body: { [key: string]: unknown } = { name: f.name.trim(), type: f.type, integrations: f.integrations, notify_on_down: f.onDown, notify_on_up: f.onUp };
	if (f.type === 'push') {
		body.period_seconds = f.period * 60;
		body.grace_seconds = f.grace * 60;
		return body;
	}
	body.interval_seconds = f.interval;
	body.retry_interval_seconds = f.retry;
	body.failures_before_alarm = f.failures;
	if (f.type === 'icmp' || f.type === 'tcp') {
		body.target = f.target.trim();
		body.prefer_ipv4 = f.preferIPv4;
	}
	if (f.type === 'tcp') body.port = f.port;
	if (f.type === 'http' || f.type === 'keyword') {
		body.url = f.url.trim();
		body.verify_tls = f.verifyTLS;
	}
	if (f.type === 'keyword') body.keyword = f.keyword.trim();
	return body;
}

/** "3 / 48", "3" without a limit. */
export function usageText(u: Usage | undefined): string {
	if (!u) return '–';
	const used = Number.isInteger(u.used) ? String(u.used) : u.used.toFixed(1);
	return u.limit === null || u.limit === undefined ? used : `${used} / ${u.limit}`;
}

/** Share of a limit 0–100, null without one. */
export function usagePercent(u: Usage | undefined): number | null {
	if (!u || !u.limit) return null;
	return Math.min(100, Math.max(0, Math.round((u.used / u.limit) * 100)));
}

/** Tone of a state for chips. */
export function tone(state: string): 'ok' | 'warn' | 'error' | '' {
	if (['up', 'active', 'ready', 'on'].includes(state)) return 'ok';
	if (['down', 'failed_over', 'suspended_traffic_limit'].includes(state)) return 'error';
	if (['warning', 'stabilizing', 'pending', 'applying', 'failover_requested', 'fallback_requested', 'disabling', 'creating', 'paused', 'pending_verification'].includes(state)) return 'warn';
	return '';
}

/** Points of the availability chart: x 0–width, y 0–height (100 % at the top). */
export function chartPoints(series: Stats['series'], width: number, height: number): string {
	if (!series.length) return '';
	const step = series.length > 1 ? width / (series.length - 1) : 0;
	return series
		.map((s, i) => {
			const y = height - (Math.min(100, Math.max(0, s.availability_percent)) / 100) * height;
			return `${Math.round(i * step * 10) / 10},${Math.round(y * 10) / 10}`;
		})
		.join(' ');
}

/** The public address of a status page. */
export function statusPageURL(slug: string): string {
	return `https://ipv64.net/status/${encodeURIComponent(slug)}`;
}

export function validSlug(slug: string): boolean {
	return /^[A-Za-z0-9]{3,128}$/.test(slug);
}

/** A listen port of the portmapper: 0 lets IPv64 choose. */
export function validListenPort(port: number): boolean {
	return port === 0 || (Number.isInteger(port) && port >= 50000 && port <= 62000);
}

export function validPort(port: number): boolean {
	return Number.isInteger(port) && port >= 1 && port <= 65535;
}

/** An IP address or a network in CIDR notation (rough check). */
export function validCIDR(text: string): boolean {
	const v = text.trim();
	const [addr, bits, more] = v.split('/');
	if (more !== undefined || !addr) return false;
	const v4 = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/.exec(addr);
	if (v4) {
		if (v4.slice(1).some((x) => Number(x) > 255)) return false;
		return bits === undefined || (/^\d+$/.test(bits) && Number(bits) <= 32);
	}
	if (!/^[0-9a-fA-F:]+$/.test(addr) || !addr.includes(':')) return false;
	return bits === undefined || (/^\d+$/.test(bits) && Number(bits) <= 128);
}

/** Megabytes and bytes for people. */
export function size(bytes: number): string {
	const units = ['B', 'KB', 'MB', 'GB', 'TB'];
	let v = bytes;
	let i = 0;
	while (v >= 1000 && i < units.length - 1) {
		v /= 1000;
		i++;
	}
	return `${i === 0 ? v : v.toFixed(1)} ${units[i]}`;
}

/** Names of the offline page logo check: JPEG, PNG or WebP, ≤ 1 MB. */
export function iconProblem(file: { type: string; size: number }): '' | 'type' | 'size' {
	if (!['image/jpeg', 'image/png', 'image/webp'].includes(file.type)) return 'type';
	if (file.size > 1024 * 1024) return 'size';
	return '';
}

/** A prefix of a record: empty or one DNS label. */
export function validPrefix(prefix: string): boolean {
	return prefix === '' || /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/.test(prefix);
}

// --- Assignments: one domain of the account → NAS, app, target or CDN ---

export type AssignTarget = 'nas' | 'app' | 'target' | 'public';
export type Assignment = {
	domain: string;
	zone: string;
	prefix?: string;
	target: AssignTarget;
	app?: string;
	port?: number;
	scheme?: string;
	/** How the name points at the NAS: a name of dynamic DNS, a wildcard above it, or "" (records of its own). */
	dns?: '' | 'ddns' | 'wildcard';
	url?: string;
};
export type Assignments = {
	domains: Domain[];
	assignments: Assignment[];
	zones: string[];
	ddns: { enabled: boolean; provider: string; hostnames: string[]; max: number };
	firewall: boolean;
};

/** One row of the table: a domain of the account or an assigned name below it. */
export type AssignRow = { name: string; zone: string; assignment: Assignment | null };

/** The rows: every domain of the account, each followed by the assigned names below it. */
export function assignRows(data: Pick<Assignments, 'domains' | 'assignments'>): AssignRow[] {
	const byName = new Map(data.assignments.map((a) => [a.domain, a]));
	const rows: AssignRow[] = [];
	for (const d of [...data.domains].sort((a, b) => a.name.localeCompare(b.name))) {
		rows.push({ name: d.name, zone: d.name, assignment: byName.get(d.name) ?? null });
		for (const a of data.assignments.filter((x) => x.zone === d.name && x.domain !== d.name).sort((x, y) => x.domain.localeCompare(y.domain)))
			rows.push({ name: a.domain, zone: d.name, assignment: a });
	}
	return rows;
}

/** The full name of a prefix under a domain ("" is the domain itself). */
export function assignName(prefix: string, domain: string): string {
	const p = prefix.trim().toLowerCase().replace(/\.$/, '');
	return p ? `${p}.${domain}` : domain;
}

/** A prefix before a domain: labels of a-z, 0-9 and hyphens; through the CDN only one. */
export function validAssignPrefix(prefix: string, publicName = false): boolean {
	const p = prefix.trim().toLowerCase();
	if (p === '') return true;
	const labels = p.split('.');
	if (publicName && labels.length > 1) return false;
	return labels.every((l) => /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/.test(l));
}

export type AssignForm = {
	kind: 'nas' | 'app' | 'target';
	app: string;
	/** For an app: through the HTTPS name of the NAS or over the internet through the CDN. */
	via: 'local' | 'public';
	port: number;
	scheme: 'http' | 'https';
	localOnly: boolean;
	targetId: string;
	firewall: boolean;
	ownLogin: boolean;
};

export function assignForm(current: Assignment | null): AssignForm {
	return {
		kind: current ? (current.target === 'public' ? 'app' : current.target) : 'nas',
		app: current && (current.target === 'app' || current.target === 'public') ? (current.app ?? '') : '',
		via: current?.target === 'public' ? 'public' : 'local',
		port: current?.port ?? 0,
		scheme: current?.scheme === 'https' ? 'https' : 'http',
		localOnly: true,
		targetId: current?.target === 'target' ? (current.app ?? '') : '',
		firewall: true,
		ownLogin: false,
	};
}

/** What PUT /ipv64/assignments/{domain} takes. */
export function assignBody(form: AssignForm, firewall: boolean) {
	switch (form.kind) {
		case 'nas':
			return { target: 'nas' };
		case 'target':
			return { target: 'target', targetId: form.targetId };
		default:
			return form.via === 'public'
				? { target: 'app', app: form.app, public: true, port: form.port, scheme: form.scheme, firewall: firewall && form.firewall, confirm: true }
				: { target: 'app', app: form.app, port: form.port, scheme: form.scheme, localOnly: form.localOnly };
	}
}

/** What is still missing before the form can be sent; hasRoute says the app has an HTTPS name already. */
export function assignProblems(form: AssignForm, hasRoute: boolean): string[] {
	const out: string[] = [];
	if (form.kind === 'target' && !form.targetId) out.push('target');
	if (form.kind === 'app') {
		if (!form.app) out.push('app');
		else if ((form.via === 'public' || !hasRoute) && !(form.port > 0 && form.port < 65536)) out.push('port');
		if (form.via === 'public' && !form.ownLogin) out.push('ownLogin');
	}
	return out;
}
