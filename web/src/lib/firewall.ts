// Helpers of the firewall page (Settings › Firewall), settings version 2:
// zones × destinations as cells with ordered policies and a default. The
// backend checks everything again and fills in missing cells the same way
// (backend/internal/firewall/config.go: defaultCell); these only give
// early hints, labels and the matrix.
import type {
	FirewallApp,
	FirewallCell,
	FirewallConfig,
	FirewallNetwork,
	FirewallPolicy,
	FirewallService,
	FirewallZoneDef,
} from './api.ts';

export const builtinZones = ['home', 'vpn', 'internet', 'docker'] as const;
/** Destinations besides the zones. */
export const NAS = 'nas';
export const APPS = 'apps';

export const TABS = ['zones', 'networks', 'policies', 'autoblock', 'recent'] as const;
export type Tab = (typeof TABS)[number];

/** The tab of ?tab= or of an anchor of the settings search. */
export function tabFrom(param: string | null, hash = ''): Tab {
	if (TABS.includes(param as Tab)) return param as Tab;
	const anchor = hash.replace(/^#/, '');
	if (anchor === 'firewall-autoblock') return 'autoblock';
	if (anchor === 'firewall-recent') return 'recent';
	if (anchor === 'firewall-networks') return 'networks';
	if (anchor === 'firewall-apps' || anchor === 'firewall-rules' || anchor === 'firewall-policies') return 'policies';
	return 'zones';
}

/** "TCP 80, 443 · UDP 137-138"; ping is "ICMP". */
export function servicePorts(service: FirewallService): string {
	return service.ports
		.map((set) =>
			set.protocol === 'icmp' ? 'ICMP' : `${set.protocol.toUpperCase()} ${(set.ports ?? []).join(', ')}`,
		)
		.join(' · ');
}

/** The published ports of an app, grouped by protocol. */
export function appPorts(app: FirewallApp): string {
	const by = new Map<string, number[]>();
	for (const p of app.ports) {
		const list = by.get(p.protocol) ?? [];
		if (!list.includes(p.port)) list.push(p.port);
		by.set(p.protocol, list);
	}
	return [...by.entries()]
		.map(([proto, ports]) => `${proto.toUpperCase()} ${ports.sort((a, b) => a - b).join(', ')}`)
		.join(' · ');
}

/** A network behind the NAS (Docker bridge, VM/LXC NAT bridge). */
export function isHostedNetwork(id: string): boolean {
	return id.startsWith('docker:') || id.startsWith('vm:');
}

/** Zones with networks behind the NAS have cells to other zones. */
export function isHosted(cfg: FirewallConfig, zone: string): boolean {
	if (zone === 'docker') return true;
	const z = cfg.zones.find((x) => x.id === zone);
	return !!z && !z.builtin && z.networks.some(isHostedNetwork);
}

/** Whether traffic of a cell passes the NAS at all (else the matrix shows it grey). */
export function routable(cfg: FirewallConfig, from: string, to: string): boolean {
	return to === NAS || to === APPS || isHosted(cfg, from);
}

/** The cell nobody changed, as the backend makes it. */
export function defaultCell(cfg: FirewallConfig, services: FirewallService[], from: string, to: string): FirewallCell {
	const cell: FirewallCell = { from, to, policies: [], default: 'deny' };
	if (to === NAS) {
		if (from === 'home' || from === 'vpn' || from === 'internet') {
			for (const s of services)
				if (s.default[from]) cell.policies.push({ action: 'allow', protocol: 'any', service: s.id });
		} else if (from === 'docker') {
			cell.policies.push(
				{ action: 'allow', protocol: 'any', service: 'apps' },
				{ action: 'allow', protocol: 'any', ports: '53', comment: 'DNS' },
			);
		}
	} else if (to === APPS) {
		if (from === 'home' || from === 'vpn' || from === 'docker') cell.default = 'allow';
	} else if (!isHosted(cfg, to)) {
		cell.default = 'allow';
	}
	return cell;
}

/** The cell of a pair: the stored one or its default. */
export function cellOf(cfg: FirewallConfig, services: FirewallService[], from: string, to: string): FirewallCell {
	return cfg.cells.find((c) => c.from === from && c.to === to) ?? defaultCell(cfg, services, from, to);
}

/** The cell of a pair inside cfg, added with its default if missing, to edit it. */
export function editCell(cfg: FirewallConfig, services: FirewallService[], from: string, to: string): FirewallCell {
	let cell = cfg.cells.find((c) => c.from === from && c.to === to);
	if (!cell) {
		cfg.cells.push(defaultCell(cfg, services, from, to));
		cell = cfg.cells[cfg.cells.length - 1];
	}
	return cell;
}

/** Destinations of a zone in the order of the matrix. */
export function destinations(cfg: FirewallConfig, from: string): string[] {
	return isHosted(cfg, from) ? [NAS, APPS, ...cfg.zones.map((z) => z.id)] : [NAS, APPS];
}

export type CellState = 'allow' | 'deny' | 'partial';

/** Allowed, blocked or partly: by the default and the enabled policies against it. */
export function cellState(cell: FirewallCell): CellState {
	const active = cell.policies.filter((p) => !p.disabled);
	if (cell.default === 'allow') return active.some((p) => p.action === 'deny') ? 'partial' : 'allow';
	return active.some((p) => p.action === 'allow') ? 'partial' : 'deny';
}

/** Number of enabled policies with the other action than the default. */
export function exceptions(cell: FirewallCell): number {
	return cell.policies.filter((p) => !p.disabled && p.action !== cell.default).length;
}

/** The zone a network is in: an own zone, 'docker' for Docker networks, '' (by address) else. */
export function zoneOfNetwork(cfg: FirewallConfig, network: FirewallNetwork | string): string {
	const id = typeof network === 'string' ? network : network.id;
	const own = cfg.zones.find((z) => !z.builtin && z.networks.includes(id));
	if (own) return own.id;
	return id.startsWith('docker:') ? 'docker' : '';
}

/** Puts a network into an own zone, or back to its default ('' / 'docker'). */
export function assignNetwork(cfg: FirewallConfig, id: string, zone: string): void {
	for (const z of cfg.zones) if (!z.builtin) z.networks = z.networks.filter((n) => n !== id);
	const target = cfg.zones.find((z) => z.id === zone && !z.builtin);
	if (target) target.networks.push(id);
}

/** A fresh ID for an own zone. */
export function newZoneId(cfg: FirewallConfig, random = Math.random): string {
	for (;;) {
		const id = 'z' + Math.floor(random() * 36 ** 5).toString(36).padStart(5, '0');
		if (!cfg.zones.some((z) => z.id === id)) return id;
	}
}

/** Removes an own zone with its cells and the cells into it. */
export function removeZone(cfg: FirewallConfig, id: string): void {
	cfg.zones = cfg.zones.filter((z) => z.id !== id || z.builtin);
	cfg.cells = cfg.cells.filter((c) => c.from !== id && c.to !== id);
}

/** Zones in the order the backend keeps: built-in first, then own ones. */
export function ownZones(cfg: FirewallConfig): FirewallZoneDef[] {
	return cfg.zones.filter((z) => !z.builtin);
}

export function cloneConfig(cfg: FirewallConfig): FirewallConfig {
	return JSON.parse(JSON.stringify(cfg));
}

function tidy(p: FirewallPolicy): FirewallPolicy {
	const out: FirewallPolicy = { action: p.action, protocol: p.service || p.app ? 'any' : p.protocol || 'any' };
	for (const key of ['ports', 'service', 'app', 'source', 'destination', 'comment'] as const) {
		const value = (p[key] ?? '').trim();
		if (value && !((key === 'ports') && (p.service || p.app))) out[key] = value;
	}
	if (p.disabled) out.disabled = true;
	return out;
}

/** What goes on trial, in a form that does not depend on order of keys or missing default cells. */
function canonical(cfg: FirewallConfig, services: FirewallService[]): string {
	const zones = cfg.zones.map((z) => ({
		id: z.id,
		name: z.builtin ? '' : (z.name ?? '').trim(),
		networks: z.builtin ? [] : [...z.networks].sort(),
		sources: z.builtin ? [] : z.sources.map((s) => s.trim()).filter(Boolean),
	}));
	const cells = cfg.zones.flatMap((z) =>
		destinations(cfg, z.id).map((to) => {
			const c = cellOf(cfg, services, z.id, to);
			return { from: z.id, to, default: c.default, policies: c.policies.map(tidy) };
		}),
	);
	return JSON.stringify({ enabled: cfg.enabled, zones, cells });
}

/** Whether the parts that go on trial differ (the automatic blocking is saved on its own). */
export function samePolicies(a: FirewallConfig, b: FirewallConfig, services: FirewallService[]): boolean {
	return canonical(a, services) === canonical(b, services);
}

const ipv4 = /^(25[0-5]|2[0-4]\d|1?\d?\d)(\.(25[0-5]|2[0-4]\d|1?\d?\d)){3}$/;

/** A rough check of an address or network; the backend has the last word. */
export function validSource(text: string): boolean {
	const [addr, bits, extra] = text.trim().split('/');
	if (extra !== undefined || !addr) return false;
	const v4 = ipv4.test(addr);
	const v6 = !v4 && addr.includes(':') && /^[0-9a-fA-F:.]+$/.test(addr) && !addr.includes(':::');
	if (!v4 && !v6) return false;
	if (bits === undefined) return true;
	if (!/^\d{1,3}$/.test(bits)) return false;
	return Number(bits) <= (v4 ? 32 : 128);
}

/** "22" or "8000-8100". */
export function validPorts(text: string): boolean {
	const match = /^(\d{1,5})(?:-(\d{1,5}))?$/.exec(text.trim());
	if (!match) return false;
	const low = Number(match[1]);
	const high = match[2] === undefined ? low : Number(match[2]);
	return low >= 1 && high <= 65535 && low <= high;
}

export type PolicyProblem =
	| ''
	| 'firewall.rulePorts'
	| 'firewall.ruleSource'
	| 'firewall.policyDestination'
	| 'firewall.ruleComment';

/** The first problem of a policy as a message key, or '' if it looks fine. */
export function policyProblem(policy: FirewallPolicy): PolicyProblem {
	if (!policy.service && !policy.app && policy.ports && !validPorts(policy.ports)) return 'firewall.rulePorts';
	if (policy.source && !validSource(policy.source)) return 'firewall.ruleSource';
	if (policy.destination && !validSource(policy.destination)) return 'firewall.policyDestination';
	if (policy.comment && (policy.comment.length > 80 || /["\\\n\r\t]/.test(policy.comment))) return 'firewall.ruleComment';
	return '';
}

/** The first problem of an own zone as a message key, or ''. */
export function zoneProblem(zone: FirewallZoneDef): '' | 'firewall.zoneName' | 'firewall.ruleSource' {
	const name = (zone.name ?? '').trim();
	if (!name || name.length > 40 || /["\\\n\r\t]/.test(name)) return 'firewall.zoneName';
	if (zone.sources.some((s) => !validSource(s))) return 'firewall.ruleSource';
	return '';
}

/** m:ss for the time a change on trial has left. */
export { countdown } from './trial.ts';
