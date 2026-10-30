import type { MessageKey } from '#lib/i18n/index.svelte.ts';
import type { IconName } from '#lib/components/Icon.svelte';

const keywords: Record<string, string> = {
	appearance: 'navigation reiter tabs leiste kopfzeile header reihenfolge order theme design farbe color font schrift hintergrund background app symbol icon home bildschirm screen menü menu fenster window dialog transparenz transparency glas glass',
	network: 'ip dns gateway bridge vlan bonding netzwerk network',
	firewall:
		'firewall paketfilter packet filter nftables port ports zone zonen heimnetz home vpn internet regel rule sperren block blockieren',
	'firewall-zones': 'zone zonen matrix vlan eigene own docker netz network ziel destination',
	'firewall-networks': 'netz netze network networks vlan bridge docker br0 docker0 virbr subnetz subnet',
	'firewall-policies':
		'richtlinie richtlinien policy policies regel rule regeln rules erlauben allow verbieten deny docker apps port ports',
	'firewall-autoblock':
		'auto block automatisch sperren blockieren fehlgeschlagen failed login anmeldung brute force',
	'firewall-recent': 'gesperrt blocked verworfen dropped pakete packets zuletzt recent',
	notifications: 'email e-mail ntfy telegram discord push meldungen notifications',
	updates: 'version update rollback aktualisieren rückwechsel',
	access: 'ssh https tls ssl zertifikat certificate schlüssel key zugang access angemeldet bleiben stay logged in anmeldung login sitzung session',
	assistant:
		'ki ai assistent assistant chat ollama modell model llm sprachmodell websuche web search duckduckgo',
	backups:
		'backup sicherung datensicherung restic usb smb s3 kopie copy stick config konfiguration wiederherstellung restore',
	stick: 'stick usb config konfiguration configuration wiederherstellen restore',
	ups: 'usv ups batterie battery strom power',
	homeassistant:
		'home assistant hass smart home licht light schalter switch mqtt sensoren sensors automation',
	hardware: 'gpu npu pci usb passthrough hardware',
	'intel-guc': 'intel igpu grafik graphics guc huc firmware quick sync qsv i915 xe',
	'power-button': 'power button taste knopf ausschalten shutdown',
	memory: 'ram memory speicher cache schreibcache write cache arbeitsspeicher writeback dirty',
	files:
		'dateisuche volltext index inhalt full text search content ocr vorschaubilder thumbnails miniaturen',
	energy:
		'energie energy governor cpu strom zeitplan schedule herunterfahren shutdown einschalten wake on lan wol rtc',
	temperatures: 'temperatur temperature sensor wärme heat lüfter fan pwm drehzahl kurve curve',
	about: 'lizenz license version titanos fair source',
	timezone: 'zeitzone timezone time zone uhrzeit zeit clock',
	coverage: 'abdeckung coverage geschützt protected ungesichert unprotected',
	'application-restore': 'anwendung app application docker vm wiederherstellung restore',
	'data-backup': 'backup datensicherung dateien files restic usb smb s3 kopie copy',
	ssh: 'ssh schlüssel key remote',
	webdav: 'webdav netzlaufwerk network drive dateien files finder',
	certificate: 'https tls ssl zertifikat certificate',
	dns: 'dns namensauflösung nameserver',
	restore: 'wiederherstellung wiederherstellen restore ersetzen replace',
	terminal: 'terminal shell konsole console bestätigung confirmation',
	letsencrypt:
		"let's encrypt letsencrypt acme zertifikat certificate https tls ssl dns challenge wildcard cloudflare desec hetzner ionos netcup inwx duckdns ipv64 rfc2136",
	ddns:
		'dyndns ddns dynamisches dynamic dns ip adresse address ipv4 ipv6 cloudflare desec dedyn hetzner ionos netcup inwx duckdns ipv64 ipv64.net strato no-ip dyndns2 healthcheck erreichbarkeit',
	// The tab "IPv64" of Settings › Dynamic DNS (since 29 Oct 2026 no area of its own).
	'ipv64-overview': 'ipv64 ipv64.net konto account api key schlüssel paket plan nutzung usage protokoll log',
	'ipv64-domains': 'ipv64 domain domains dns record records eintrag einträge txt caa cname mx',
	'ipv64-dyndns': 'ipv64 dyndns ddns update token aktualisieren präfix prefix wildcard router',
	'ipv64-assignments': 'ipv64 zuweisung zuweisungen assignment assignments app apps https name domain eigene',
	'ipv64-monitoring': 'ipv64 healthcheck überwachung monitoring statusseite status page offline wartung maintenance',
	'ipv64-failover': 'ipv64 failover ausfallschutz ersatz fallback',
	'ipv64-public': 'ipv64 öffentlich public cdn reverse proxy portmapper internet freigeben',
	'ipv64-router': 'ipv64 cloud router wireguard tunnel vpn gateway peer ipv64wg',
	'ipv64-blocklist': 'ipv64 blocklist sperrliste honeypot v64 melden report firewall',
	wireguard: 'wireguard vpn tunnel fernzugriff remote access qr handy phone smartphone laptop server',
	cluster: 'cluster knoten nodes verbund proxmox mehrere nas andere titanos beitreten join konten synchronisieren backup',
	api: 'api token tokens cli titanosctl openapi rest automatisierung automation',
	mcp: 'mcp model context protocol ki ai claude desktop code client clients agent llm werkzeuge tools',
	proxy:
		'proxy reverse https name namen domain mdns avahi app apps adresse address docker zertifikat certificate',
};

function normalize(text: string): string {
	return text.normalize('NFKD').replace(/\p{M}/gu, '').toLowerCase();
}

export function matchesSetting(query: string, title: string, id: string): boolean {
	const haystack = normalize(`${title} ${keywords[id] ?? ''}`);
	return normalize(query)
		.trim()
		.split(/\s+/)
		.every((word) => haystack.includes(word));
}

export const settingSections: Record<string, { id: string; label: MessageKey; href: string }[]> = {
	access: [
		{ id: 'ssh', label: 'access.ssh', href: '/settings/access#access-ssh' },
		{ id: 'certificate', label: 'access.cert', href: '/settings/access#access-cert' },
		{ id: 'stay', label: 'stay.title', href: '/settings/access#access-stay' },
		{ id: 'terminal', label: 'settings.consoles', href: '/settings/access#access-terminal' },
		{ id: 'webdav', label: 'webdav.title', href: '/settings/access#access-webdav' },
	],
	api: [{ id: 'mcp', label: 'mcp.title', href: '/settings/api#api-mcp' }],
	hardware: [{ id: 'intel-guc', label: 'intelGuc.title', href: '/settings/hardware#hw-intel' }],
	network: [{ id: 'dns', label: 'netcfg.dns', href: '/settings/network#network-dns' }],
	letsencrypt: [
		{ id: 'letsencrypt-status', label: 'letsencrypt.statusTitle', href: '/settings/letsencrypt#le-status' },
		{ id: 'letsencrypt-settings', label: 'letsencrypt.settingsTitle', href: '/settings/letsencrypt#le-settings' },
	],
	cluster: [
		{ id: 'cluster-join', label: 'cluster.joinInfo', href: '/settings/cluster#cluster-join' },
		{ id: 'cluster-nodes', label: 'cluster.nodes', href: '/settings/cluster#cluster-nodes' },
	],
	wireguard: [
		{ id: 'wg-server', label: 'wg.server', href: '/settings/wireguard#wg-server' },
		{ id: 'wg-devices', label: 'wg.devices', href: '/settings/wireguard#wg-devices' },
	],
	ddns: [
		{ id: 'ddns-status', label: 'ddns.statusTitle', href: '/settings/ddns#ddns-status' },
		{ id: 'ddns-settings', label: 'ddns.settingsTitle', href: '/settings/ddns#ddns-settings' },
		{ id: 'ipv64-overview', label: 'ipv64x.search.overview', href: '/settings/ddns?tab=ipv64&sub=overview' },
		{ id: 'ipv64-domains', label: 'ipv64x.search.domains', href: '/settings/ddns?tab=ipv64&sub=domains' },
		{ id: 'ipv64-dyndns', label: 'ipv64x.search.dyndns', href: '/settings/ddns?tab=ipv64&sub=dyndns' },
		{ id: 'ipv64-assignments', label: 'ipv64x.search.assignments', href: '/settings/ddns?tab=ipv64&sub=assignments' },
		{ id: 'ipv64-monitoring', label: 'ipv64x.search.monitoring', href: '/settings/ddns?tab=ipv64&sub=monitoring' },
		{ id: 'ipv64-failover', label: 'ipv64x.search.failover', href: '/settings/ddns?tab=ipv64&sub=failover' },
		{ id: 'ipv64-public', label: 'ipv64x.search.public', href: '/settings/ddns?tab=ipv64&sub=public' },
		{ id: 'ipv64-router', label: 'ipv64x.search.router', href: '/settings/ddns?tab=ipv64&sub=router' },
		{ id: 'ipv64-blocklist', label: 'ipv64x.search.blocklist', href: '/settings/ddns?tab=ipv64&sub=blocklist' },
	],
	firewall: [
		{ id: 'firewall-zones', label: 'firewall.zones', href: '/settings/firewall?tab=zones#firewall-zones' },
		{ id: 'firewall-networks', label: 'firewall.tab.networks', href: '/settings/firewall?tab=networks#firewall-networks' },
		{ id: 'firewall-policies', label: 'firewall.tab.policies', href: '/settings/firewall?tab=policies#firewall-policies' },
		{ id: 'firewall-autoblock', label: 'firewall.autoBlock', href: '/settings/firewall?tab=autoblock#firewall-autoblock' },
		{ id: 'firewall-recent', label: 'firewall.recent', href: '/settings/firewall?tab=recent#firewall-recent' },
	],
	homeassistant: [
		{ id: 'connection', label: 'ha.connection', href: '/settings/homeassistant#connection' },
		{ id: 'release', label: 'ha.release', href: '/settings/homeassistant#release' },
		{ id: 'mqtt', label: 'ha.mqtt', href: '/settings/homeassistant#mqtt' },
		{ id: 'storage', label: 'hastore.title', href: '/settings/homeassistant#storage' },
	],
	backups: [
		{ id: 'coverage', label: 'coverage.title', href: '/settings/backups/coverage' },
		{
			id: 'application-restore',
			label: 'applications.title',
			href: '/settings/backups/applications',
		},
		{ id: 'data-backup', label: 'safeguard.data', href: '/settings/backups/data' },
		{ id: 'stick', label: 'safeguard.config', href: '/settings/backups/configuration' },
		{ id: 'restore', label: 'safeguard.restore', href: '/settings/backups/restore' },
	],
};

/** The groups of the settings navigation, in the order they are shown. */
export const settingsGroups = ['system', 'hardware', 'network', 'data', 'integrations', 'personal'] as const;
export type SettingsGroup = (typeof settingsGroups)[number];
export type SettingsArea = {
	id: string;
	label: MessageKey;
	group: SettingsGroup;
	icon: IconName;
	admin?: boolean;
	/** Shows a small "new" badge next to the area in the navigation. */
	isNew?: boolean;
};

// One list for the settings navigation (sidebar and overview), the command
// palette and the route guard. Within a group the areas appear in exactly
// this order; adding an area is one line in the right place.
export const settingsAreas: SettingsArea[] = [
	{ id: 'updates', label: 'update.title', group: 'system', icon: 'download', admin: true },
	{ id: 'timezone', label: 'settings.datetime', group: 'system', icon: 'clock', admin: true },
	{ id: 'notifications', label: 'notify.settingsTitle', group: 'system', icon: 'bell', admin: true },
	{ id: 'about', label: 'about.title', group: 'system', icon: 'fileText' },
	{ id: 'hardware', label: 'hardware.title', group: 'hardware', icon: 'monitor', admin: true },
	{ id: 'temperatures', label: 'temperatures.title', group: 'hardware', icon: 'activity' },
	{ id: 'memory', label: 'memoryPolicy.title', group: 'hardware', icon: 'grid', admin: true },
	{ id: 'energy', label: 'energy.title', group: 'hardware', icon: 'zap', admin: true },
	{ id: 'ups', label: 'ups.title', group: 'hardware', icon: 'battery', admin: true },
	{ id: 'power-button', label: 'powerButton.title', group: 'hardware', icon: 'power', admin: true },
	{ id: 'network', label: 'netcfg.title', group: 'network', icon: 'globe', admin: true },
	{ id: 'firewall', label: 'firewall.title', group: 'network', icon: 'shield', admin: true, isNew: true },
	{ id: 'letsencrypt', label: 'letsencrypt.title', group: 'network', icon: 'check', admin: true, isNew: true },
	{ id: 'ddns', label: 'ddns.title', group: 'network', icon: 'refresh', admin: true, isNew: true },
	{ id: 'wireguard', label: 'wg.title', group: 'network', icon: 'key', admin: true, isNew: true },
	{ id: 'cluster', label: 'cluster.title', group: 'network', icon: 'tailnet', admin: true, isNew: true },
	{ id: 'proxy', label: 'proxy.settingsTitle', group: 'network', icon: 'external', admin: true },
	{ id: 'access', label: 'access.title', group: 'network', icon: 'lock', admin: true },
	{ id: 'backups', label: 'safeguard.title', group: 'data', icon: 'archive', admin: true },
	{ id: 'files', label: 'fileIndex.title', group: 'data', icon: 'search', admin: true },
	{ id: 'homeassistant', label: 'ha.title', group: 'integrations', icon: 'home', admin: true },
	{ id: 'assistant', label: 'assistant.settingsTitle', group: 'integrations', icon: 'chat', admin: true },
	{ id: 'api', label: 'api.title', group: 'integrations', icon: 'key' },
	{ id: 'appearance', label: 'menu.appearance', group: 'personal', icon: 'palette' },
];

/** The area a settings path belongs to ("/settings/ddns/x" → "ddns"), or "" for the overview. */
export function settingsAreaOf(pathname: string): string {
	return pathname.replace(/\/$/, '').split('/')[2] ?? '';
}

/** Settings paths that accounts with the role "user" may open. */
export const userSettingsPaths = [
	'/settings',
	...settingsAreas.filter((area) => !area.admin).map((area) => `/settings/${area.id}`),
];

/** One row of the settings navigation: an area, or a section of it while searching. */
export type SettingsEntry = {
	href: string;
	id: string;
	label: string;
	group: SettingsGroup;
	icon: IconName;
	isNew?: boolean;
	section?: boolean;
};

/**
 * The rows of the settings navigation for an account, grouped and in the fixed
 * order. With a search, matching sections replace their area (they lead
 * straight to the anchor); areas without matching sections show if they match.
 */
export function settingsEntries(
	query: string,
	admin: boolean,
	translate: (key: MessageKey) => string,
): { group: SettingsGroup; entries: SettingsEntry[] }[] {
	const searching = query.trim() !== '';
	const entries = settingsAreas
		.filter((area) => !area.admin || admin)
		.flatMap((area): SettingsEntry[] => {
			const base = { id: area.id, group: area.group, icon: area.icon, isNew: area.isNew };
			const sections = searching
				? (settingSections[area.id] ?? []).filter((section) =>
						matchesSetting(query, `${translate(area.label)} ${translate(section.label)}`, section.id),
					)
				: [];
			if (sections.length)
				return sections.map((section) => ({
					...base,
					href: section.href,
					label: `${translate(area.label)} › ${translate(section.label)}`,
					section: true,
				}));
			return !searching || matchesSetting(query, translate(area.label), area.id)
				? [{ ...base, href: `/settings/${area.id}`, label: translate(area.label) }]
				: [];
		});
	return settingsGroups
		.map((group) => ({ group, entries: entries.filter((entry) => entry.group === group) }))
		.filter((group) => group.entries.length);
}
