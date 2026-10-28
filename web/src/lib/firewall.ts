// Helpers of the firewall page (Settings › Firewall). The backend checks
// everything again; these only give early hints and readable labels.
import type {
	FirewallApp,
	FirewallConfig,
	FirewallRule,
	FirewallService,
	FirewallZone,
	FirewallZones,
} from './api.ts';

export const zones: FirewallZone[] = ['home', 'vpn', 'internet'];

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

/** The zones an app is allowed from: its own entry or the one for new apps. */
export function appZones(cfg: FirewallConfig, key: string): FirewallZones {
	return cfg.apps[key] ?? cfg.newApps;
}

export function cloneConfig(cfg: FirewallConfig): FirewallConfig {
	return JSON.parse(JSON.stringify(cfg));
}

/** Whether the parts that go on trial differ (the automatic blocking is saved on its own). */
export function sameRules(a: FirewallConfig, b: FirewallConfig): boolean {
	const strip = (cfg: FirewallConfig) => {
		const { autoBlock: _, ...rest } = cfg;
		const sorted = (o: Record<string, unknown>) =>
			Object.fromEntries(Object.entries(o).sort(([x], [y]) => x.localeCompare(y)));
		return JSON.stringify({ ...rest, services: sorted(rest.services), apps: sorted(rest.apps) });
	};
	return strip(a) === strip(b);
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

/** The first problem of a rule as a message key, or '' if it looks fine. */
export function ruleProblem(rule: FirewallRule): '' | 'firewall.rulePorts' | 'firewall.ruleSource' | 'firewall.ruleComment' {
	if (rule.ports && !validPorts(rule.ports)) return 'firewall.rulePorts';
	if (rule.source && !validSource(rule.source)) return 'firewall.ruleSource';
	if (rule.comment && (rule.comment.length > 80 || /["\\\n\r\t]/.test(rule.comment))) return 'firewall.ruleComment';
	return '';
}

/** m:ss for the time a change on trial has left. */
export function countdown(seconds: number): string {
	const s = Math.max(0, Math.floor(seconds));
	return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`;
}
