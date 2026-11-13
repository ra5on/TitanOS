import type { MessageKey } from '#lib/i18n/index.svelte.ts';
import type { IconName } from '#lib/components/Icon.svelte';

const keywords: Record<string, string> = {
	users:
		'nutzer benutzer konto konten account accounts passwort password rolle role administrator besitzer owner 2fa sperren lock anlegen apps app programme programs docker rechte rights erlauben allow freigaben shares kamera kameras camera cameras nvr live aufnahmen recordings',
	appearance:
		'navigation reiter tabs leiste kopfzeile header reihenfolge order theme design farbe color font schrift hintergrund background app symbol icon home bildschirm screen menü menu fenster window dialog transparenz transparency glas glass glaseffekt deckkraft opacity durchsichtig deckend ecken corners farbstärke saturation dock anmeldeseite login',
	network:
		'ip dns gateway bridge vlan bonding netzwerk network hostname name nas-name rechnername umbenennen rename',
	firewall:
		'firewall paketfilter packet filter nftables port ports zone zonen heimnetz home vpn internet regel rule sperren block blockieren',
	'firewall-zones': 'zone zonen matrix vlan eigene own docker netz network ziel destination',
	'firewall-networks':
		'netz netze network networks vlan bridge docker br0 docker0 virbr subnetz subnet',
	'firewall-policies':
		'richtlinie richtlinien policy policies regel rule regeln rules erlauben allow verbieten deny docker apps port ports',
	'firewall-autoblock':
		'auto block automatisch sperren blockieren fehlgeschlagen failed login anmeldung brute force',
	'firewall-recent': 'gesperrt blocked verworfen dropped pakete packets zuletzt recent',
	notifications: 'email e-mail ntfy telegram discord push meldungen notifications',
	updates: 'version update rollback aktualisieren rückwechsel',
	access:
		'ssh https tls ssl zertifikat certificate schlüssel key zugang access angemeldet bleiben stay logged in anmeldung login sitzung session dauerhaft eingeloggt abmelden ausgeloggt logout sperren sperre leerlauf lock idle',
	backups:
		'backup sicherung datensicherung restic usb smb s3 kopie copy stick config konfiguration wiederherstellung restore',
	stick: 'stick usb config konfiguration configuration wiederherstellen restore',
	ups: 'usv ups batterie battery strom power',
	homeassistant:
		'home assistant hass smart home licht light schalter switch mqtt sensoren sensors automation verbindungen connections intervall interval kamera kameras camera cameras nvr',
	hardware: 'gpu npu pci usb passthrough hardware',
	'intel-guc': 'intel igpu grafik graphics guc huc firmware quick sync qsv i915 xe',
	'power-button': 'power button taste knopf ausschalten shutdown',
	memory: 'ram memory speicher cache schreibcache write cache arbeitsspeicher writeback dirty',
	files:
		'dateisuche volltext index inhalt full text search content ocr vorschaubilder thumbnails miniaturen',
	energy:
		'energie energy governor cpu strom zeitplan schedule herunterfahren shutdown einschalten wake on lan wol rtc ruhemodus standby festplatten platten-ruhe plattenruhe disk sleep spindown',
	temperatures:
		'temperatur temperature sensor wärme heat lüfter fan pwm drehzahl kurve curve überhitzung overheating notabschaltung herunterfahren shutdown',
	systemdata:
		'systemdaten system data ablage ablage-volume storage volume speicherort location konfigurationssicherung configuration backup stick protokoll protokolle logs journal systemprotokoll aktivitätsprotokoll activity meldungen notifications smart verlauf history monitoring ruhemodus schlafen sleep platten disks wecken wake .titanos',
	'systemdata-store': 'ablage volume speicherort storage location ruhemodus sleep wecken wake',
	'systemdata-config':
		'konfigurationssicherung configuration backup stick einstellungen settings passwort password stände copies täglich daily',
	'systemdata-logs':
		'protokoll protokolle logs journal aktivität activity meldungen notifications smart monitoring verlauf history',
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
	ddns: 'dyndns ddns dynamisches dynamic dns ip adresse address ipv4 ipv6 cloudflare desec dedyn hetzner ionos netcup inwx duckdns ipv64 ipv64.net strato no-ip dyndns2 healthcheck erreichbarkeit',
	wireguard:
		'wireguard vpn tunnel fernzugriff remote access qr handy phone smartphone laptop server',
	cluster:
		'cluster knoten nodes verbund proxmox mehrere nas andere titanos beitreten join konten synchronisieren backup',
	api: 'api token tokens cli titanosctl openapi rest automatisierung automation',
	services:
		'dienste services docker vm vms virtuelle maschinen lxc container ausschalten abschalten aus off',
	mcp: 'mcp model context protocol ki ai claude desktop code client clients agent llm werkzeuge tools',
	agent:
		'ki agent assistent ai ollama chat projekte modell denken kontext rechte ablageorte gedächtnis skills zeitplan home assistant assist',
	proxy:
		'proxy reverse https name namen domain mdns avahi app apps adresse address docker zertifikat certificate automatisch automatic webui zertifizierungsstelle certificate authority ca installieren install',
	'proxy-ca':
		'zertifizierungsstelle certificate authority ca root stammzertifikat installieren install iphone ipad android windows macos linux',
	viewer:
		'fernansicht remote view docker app apps fenster window browser chromium gpu grafikkarte graphics vaapi nvenc qualität quality bildrate frame rate fps ton sound audio lautstärke volume profil profile',
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
		{ id: 'idle-lock', label: 'idleLock.title', href: '/settings/access#access-idle-lock' },
		{ id: 'terminal', label: 'settings.consoles', href: '/settings/access#access-terminal' },
		{ id: 'webdav', label: 'webdav.title', href: '/settings/access#access-webdav' },
	],
	hardware: [{ id: 'intel-guc', label: 'intelGuc.title', href: '/settings/hardware#hw-intel' }],
	network: [
		{ id: 'hostname', label: 'netcfg.hostname', href: '/settings/network#net-system' },
		{ id: 'dns', label: 'netcfg.dns', href: '/settings/network#network-dns' },
	],
	proxy: [{ id: 'proxy-ca', label: 'appaccess.caTitle', href: '/settings/proxy#proxy-ca' }],
	letsencrypt: [
		{
			id: 'letsencrypt-status',
			label: 'letsencrypt.statusTitle',
			href: '/settings/letsencrypt#le-status',
		},
		{
			id: 'letsencrypt-settings',
			label: 'letsencrypt.settingsTitle',
			href: '/settings/letsencrypt#le-settings',
		},
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
	],
	firewall: [
		{
			id: 'firewall-zones',
			label: 'firewall.zones',
			href: '/settings/firewall?tab=zones#firewall-zones',
		},
		{
			id: 'firewall-networks',
			label: 'firewall.tab.networks',
			href: '/settings/firewall?tab=networks#firewall-networks',
		},
		{
			id: 'firewall-policies',
			label: 'firewall.tab.policies',
			href: '/settings/firewall?tab=policies#firewall-policies',
		},
		{
			id: 'firewall-autoblock',
			label: 'firewall.autoBlock',
			href: '/settings/firewall?tab=autoblock#firewall-autoblock',
		},
		{
			id: 'firewall-recent',
			label: 'firewall.recent',
			href: '/settings/firewall?tab=recent#firewall-recent',
		},
	],
	homeassistant: [
		{ id: 'connections', label: 'ha.conn.title', href: '/settings/homeassistant#connections' },
		{ id: 'connection', label: 'ha.connection', href: '/settings/homeassistant#connection' },
		{ id: 'release', label: 'ha.release', href: '/settings/homeassistant#release' },
		{ id: 'mqtt', label: 'ha.mqtt', href: '/settings/homeassistant#mqtt' },
		{ id: 'storage', label: 'hastore.title', href: '/settings/homeassistant#storage' },
	],
	systemdata: [
		{ id: 'systemdata-store', label: 'sysdata.common', href: '/settings/systemdata#sysdata-store' },
		{
			id: 'systemdata-config',
			label: 'sysdata.group.config',
			href: '/settings/systemdata#config',
		},
		{
			id: 'systemdata-logs',
			label: 'sysdata.group.logs',
			href: '/settings/systemdata#sysdata-logs',
		},
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

/** The areas of the control panel's menu, in the order they are shown. */
// PLAN 78-1/78-2 (owner 10.11.2026, draft 78): eight areas like the menu of
// Windows 11's settings. Every page belongs to exactly one; the menu, the
// path title of a page ("Hardware und Energie › Energie"), the area pages
// and the command palette read it from here. Within an area the pages are
// sorted A–Z in the page language (PLAN 53).
export const settingsGroups = [
	'system',
	'hardware',
	'network',
	'accounts',
	'services',
	'ai',
	'data',
	'updates',
] as const;
export type SettingsGroup = (typeof settingsGroups)[number];

/** The symbol of an area in the menu. */
export const settingsGroupIcons: Record<SettingsGroup, IconName> = {
	system: 'monitor',
	hardware: 'chip',
	network: 'globe',
	accounts: 'users',
	services: 'bell',
	ai: 'sparkle',
	data: 'archive',
	updates: 'download',
};

/** The title of a settings group in the page language. */
export function settingsGroupLabel(group: SettingsGroup): MessageKey {
	return `settingshome.group.${group}` as MessageKey;
}
export type SettingsArea = {
	id: string;
	label: MessageKey;
	group: SettingsGroup;
	icon: IconName;
	admin?: boolean;
	/**
	 * Shows a small "new" badge next to the area in the navigation: the
	 * release the area first shipped in and that release's date. The badge
	 * goes away by itself, see stillNew.
	 */
	newSince?: NewSince;
};

/** The release an area first shipped in (version "x.y.z", date "YYYY-MM-DD"). */
export type NewSince = { version: string; date: string };

/** How long a "new" badge stays: two minor versions or 30 days, whichever ends first. */
export const NEW_FOR_VERSIONS = 2;
export const NEW_FOR_DAYS = 30;

function minorOf(version: string): [number, number] | null {
	const m = /^v?(\d+)\.(\d+)\./.exec(version.trim());
	return m ? [Number(m[1]), Number(m[2])] : null;
}

/**
 * Whether a "new" badge still shows: until the system runs the second minor
 * version after the one the area came with (0.41.x → gone from 0.43.0 on)
 * and for at most 30 days after that release. Patch versions do not count.
 * Without a readable version (dev builds, no data yet) only the date counts.
 */
export function stillNew(
	since: NewSince,
	currentVersion: string | undefined,
	now: number,
): boolean {
	const start = Date.parse(`${since.date}T00:00:00Z`);
	if (Number.isNaN(start) || now >= start + NEW_FOR_DAYS * 86_400_000) return false;
	const from = minorOf(since.version);
	const current = minorOf(currentVersion ?? '');
	if (!from || !current) return true;
	if (current[0] !== from[0]) return current[0] < from[0];
	return current[1] - from[1] < NEW_FOR_VERSIONS;
}

// One list for the settings navigation (sidebar and overview), the command
// palette and the route guard. Within a group the areas appear in exactly
// this order; adding an area is one line in the right place.
export const settingsAreas: SettingsArea[] = [
	{ id: 'about', label: 'about.title', group: 'system', icon: 'info' },
	{ id: 'timezone', label: 'settings.datetime', group: 'system', icon: 'clock', admin: true },
	{ id: 'appearance', label: 'menu.appearance', group: 'system', icon: 'palette' },
	{
		id: 'viewer',
		label: 'settingshome.area.viewer',
		group: 'system',
		icon: 'monitor',
		admin: true,
		newSince: { version: '0.61.0', date: '2026-11-07' },
	},
	{ id: 'hardware', label: 'hardware.title', group: 'hardware', icon: 'gpu', admin: true },
	{ id: 'temperatures', label: 'temperatures.title', group: 'hardware', icon: 'activity' },
	{ id: 'energy', label: 'energy.title', group: 'hardware', icon: 'zap', admin: true },
	{ id: 'ups', label: 'ups.title', group: 'hardware', icon: 'battery', admin: true },
	{ id: 'power-button', label: 'powerButton.title', group: 'hardware', icon: 'power', admin: true },
	{ id: 'memory', label: 'memoryPolicy.title', group: 'hardware', icon: 'memory', admin: true },
	{ id: 'network', label: 'netcfg.title', group: 'network', icon: 'globe', admin: true },
	{
		id: 'firewall',
		label: 'firewall.title',
		group: 'network',
		icon: 'shield',
		admin: true,
		newSince: { version: '0.31.0', date: '2026-10-28' },
	},
	{
		id: 'wireguard',
		label: 'wg.title',
		group: 'network',
		icon: 'key',
		admin: true,
		newSince: { version: '0.34.0', date: '2026-10-28' },
	},
	{
		id: 'ddns',
		label: 'ddns.title',
		group: 'network',
		icon: 'refresh',
		admin: true,
		newSince: { version: '0.31.0', date: '2026-10-28' },
	},
	{
		id: 'letsencrypt',
		label: 'letsencrypt.title',
		group: 'network',
		icon: 'check',
		admin: true,
		newSince: { version: '0.31.0', date: '2026-10-28' },
	},
	{ id: 'proxy', label: 'proxy.settingsTitle', group: 'network', icon: 'external', admin: true },
	{
		id: 'users',
		label: 'desktop.app.users',
		group: 'accounts',
		icon: 'users',
		admin: true,
		newSince: { version: '0.70.1', date: '2026-11-08' },
	},
	{ id: 'access', label: 'access.title', group: 'accounts', icon: 'lock', admin: true },
	{ id: 'api', label: 'api.title', group: 'accounts', icon: 'terminal' },
	{
		id: 'services',
		label: 'services.title',
		group: 'services',
		icon: 'box',
		admin: true,
		newSince: { version: '0.41.0', date: '2026-10-31' },
	},
	{
		id: 'notifications',
		label: 'notify.settingsTitle',
		group: 'services',
		icon: 'bell',
		admin: true,
	},
	{
		id: 'cluster',
		label: 'cluster.title',
		group: 'services',
		icon: 'tailnet',
		admin: true,
		newSince: { version: '0.38.0', date: '2026-10-30' },
	},
	{
		id: 'agent',
		label: 'agent.settingsTitle',
		group: 'ai',
		icon: 'sparkle',
		admin: true,
		newSince: { version: '0.48.0', date: '2026-11-05' },
	},
	{
		id: 'mcp',
		label: 'mcp.pageTitle',
		group: 'ai',
		icon: 'chat',
		newSince: { version: '0.41.0', date: '2026-10-31' },
	},
	{ id: 'homeassistant', label: 'ha.title', group: 'ai', icon: 'home', admin: true },
	{ id: 'backups', label: 'safeguard.title', group: 'data', icon: 'archive', admin: true },
	{ id: 'files', label: 'fileIndex.title', group: 'data', icon: 'search', admin: true },
	{
		id: 'systemdata',
		label: 'sysdata.title',
		group: 'data',
		icon: 'disk',
		admin: true,
		newSince: { version: '0.77.0', date: '2026-11-12' },
	},
	{ id: 'updates', label: 'update.title', group: 'updates', icon: 'download', admin: true },
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
	newSince?: NewSince;
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
			const base = { id: area.id, group: area.group, icon: area.icon, newSince: area.newSince };
			const sections = searching
				? (settingSections[area.id] ?? []).filter((section) =>
						matchesSetting(
							query,
							`${translate(area.label)} ${translate(section.label)}`,
							section.id,
						),
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
	// Entries in alphabetical order of the page language (PLAN 53), groups in
	// their fixed order (PLAN 61).
	const order = new Intl.Collator(undefined, { sensitivity: 'base', numeric: true });
	return settingsGroups
		.map((group) => ({
			group,
			entries: entries
				.filter((entry) => entry.group === group)
				.sort((a, b) => order.compare(a.label, b.label)),
		}))
		.filter((group) => group.entries.length);
}

/** Whether an account may open a settings page (the server decides again). */
export function settingsSideAllowed(href: string, admin: boolean): boolean {
	if (admin) return true;
	const path = href.split(/[?#]/)[0].replace(/\/$/, '');
	return !path.startsWith('/settings') || userSettingsPaths.includes(path);
}

/** The address of an area's own page: the list of its settings pages. */
export function settingsGroupHref(group: SettingsGroup): string {
	return `/settings?area=${group}`;
}

/** The area an address stands for: "?area=" of the start page, else the page's own. */
export function settingsGroupOf(href: string): SettingsGroup | undefined {
	const [path, query = ''] = href.split('#')[0].split('?');
	const id = settingsAreaOf(path);
	if (id) return settingsAreas.find((area) => area.id === id)?.group;
	const wanted = new URLSearchParams(query).get('area');
	return settingsGroups.find((group) => group === wanted);
}

/** One entry of the control panel's menu (PLAN 78): an area and where it leads. */
export type SettingsMenuEntry = {
	group: SettingsGroup;
	label: MessageKey;
	icon: IconName;
	/** The area's page, or its one page when the account sees only one. */
	href: string;
	pages: SettingsArea[];
};

/**
 * The areas of the menu for an account: only areas with a page the account
 * may open (PLAN 78: "Ein Benutzer sieht nur, was er öffnen darf").
 */
export function settingsMenu(admin: boolean): SettingsMenuEntry[] {
	return settingsGroups.flatMap((group) => {
		const pages = settingsAreas.filter((area) => area.group === group && (!area.admin || admin));
		if (!pages.length) return [];
		return [
			{
				group,
				label: settingsGroupLabel(group),
				icon: settingsGroupIcons[group],
				href: pages.length === 1 ? `/settings/${pages[0].id}` : settingsGroupHref(group),
				pages,
			},
		];
	});
}

/**
 * The path above a settings page's title (PLAN 78-1): its area, and for a
 * page below a settings page (Sicherungen › Abdeckung) that page too.
 */
export function settingsCrumbs(
	pathname: string,
	admin: boolean,
): { label: MessageKey; href: string }[] {
	const parts = pathname.replace(/\/$/, '').split('/');
	if (parts[1] !== 'settings' || !parts[2]) return [];
	const area = settingsAreas.find((a) => a.id === parts[2]);
	if (!area) return [];
	const entry = settingsMenu(admin).find((e) => e.group === area.group);
	// An area that is one page (Updates) has no path above its name.
	const alone = settingsAreas.filter((a) => a.group === area.group).length === 1;
	if (alone && parts.length <= 3) return [];
	const crumbs = alone
		? []
		: [
				{
					label: settingsGroupLabel(area.group),
					href: entry?.href ?? settingsGroupHref(area.group),
				},
			];
	if (parts.length > 3) crumbs.push({ label: area.label, href: `/settings/${area.id}` });
	return crumbs;
}
