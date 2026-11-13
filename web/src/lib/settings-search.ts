import type { MessageKey } from '#lib/i18n/index.svelte.ts';
import type { IconName } from '#lib/components/Icon.svelte';

const keywords: Record<string, string> = {
	users:
		'nutzer benutzer konto konten account accounts passwort password rolle role administrator besitzer owner 2fa sperren lock anlegen apps app programme programs docker rechte rights erlauben allow freigaben shares kamera kameras camera cameras nvr live aufnahmen recordings',
	appearance:
		'navigation reiter tabs leiste kopfzeile header reihenfolge order theme design farbe color font schrift hintergrund background app symbol icon home bildschirm screen menü menu fenster window dialog transparenz transparency glas glass glaseffekt deckkraft opacity durchsichtig deckend ecken corners farbstärke saturation dock anmeldeseite login',
	network:
		'ip dns gateway bridge vlan bonding netzwerk network hostname name nas-name rechnername umbenennen rename fernzugriff remote vpn tailscale netbird',
	firewall:
		'firewall paketfilter packet filter nftables port ports zone zonen heimnetz home vpn internet regel rule sperren block blockieren',
	notifications:
		'email e-mail ntfy telegram discord push meldungen mitteilungen notifications glocke bell',
	updates: 'version update rollback aktualisieren rückwechsel kernel',
	access:
		'ssh https tls ssl zertifikat certificate schlüssel key zugang access angemeldet bleiben stay logged in anmeldung login sitzung session dauerhaft eingeloggt abmelden ausgeloggt logout sperren sperre leerlauf lock idle',
	backups:
		'backup sicherung datensicherung restic usb smb s3 kopie copy stick config konfiguration wiederherstellung restore',
	ups: 'usv ups batterie battery strom power',
	homeassistant:
		'home assistant hass smart home licht light schalter switch mqtt sensoren sensors automation verbindungen connections intervall interval kamera kameras camera cameras nvr',
	hardware: 'gpu npu pci usb passthrough hardware',
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
	about: 'lizenz license version titanos fair source',
	timezone: 'zeitzone timezone time zone uhrzeit zeit clock',
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
	viewer:
		'fernansicht remote view docker app apps fenster window browser chromium gpu grafikkarte graphics vaapi nvenc qualität quality bildrate frame rate fps ton sound audio lautstärke volume profil profile',
};

function normalize(text: string): string {
	return text.normalize('NFKD').replace(/\p{M}/gu, '').toLowerCase();
}

/** The words of a settings page beyond its title (both languages). */
export function settingsKeywords(id: string): string {
	return keywords[id] ?? '';
}

export function matchesSetting(query: string, title: string, id: string): boolean {
	const haystack = normalize(`${title} ${keywords[id] ?? ''}`);
	return normalize(query)
		.trim()
		.split(/\s+/)
		.every((word) => haystack.includes(word));
}

/** The areas of the control panel's menu, in the order they are shown. */
// PLAN 86 (owner 13.11.2026, "logischer aufbau", draft 1 confirmed): eight
// areas by what one looks for. Every page belongs to exactly one; the menu
// (an area opens its pages right there, PLAN 86 variant A), the path title
// of a page ("Hardware und Energie › Energie"), the phone's list and the
// search read it from here. Within an area the pages keep the order of the
// confirmed draft (the same in both languages), not A–Z.
export const settingsGroups = [
	'system',
	'hardware',
	'network',
	'security',
	'notifications',
	'data',
	'cluster',
	'smarthome',
] as const;
export type SettingsGroup = (typeof settingsGroups)[number];

/**
 * Areas of links made before PLAN 86 (`/settings?area=…`) and where they
 * lead now: another area, or a page (its area is open in the menu).
 */
export const legacySettingsGroups: Record<string, SettingsGroup | { page: string }> = {
	accounts: 'security',
	ai: 'smarthome',
	services: { page: 'services' },
	updates: { page: 'updates' },
};

/** The symbol of an area in the menu. */
export const settingsGroupIcons: Record<SettingsGroup, IconName> = {
	system: 'gear',
	hardware: 'chip',
	network: 'globe',
	security: 'shield',
	notifications: 'bell',
	data: 'archive',
	cluster: 'tailnet',
	smarthome: 'home',
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

// One list for the settings navigation (menu and overview), the search, the
// command palette and the route guard. Within an area the pages appear in
// exactly this order (PLAN 86, draft 1); adding a page is one line in the
// right place.
export const settingsAreas: SettingsArea[] = [
	{ id: 'about', label: 'about.title', group: 'system', icon: 'info' },
	{ id: 'updates', label: 'update.title', group: 'system', icon: 'download', admin: true },
	{ id: 'appearance', label: 'menu.appearance', group: 'system', icon: 'palette' },
	{ id: 'timezone', label: 'settings.datetime', group: 'system', icon: 'clock', admin: true },
	{
		id: 'services',
		label: 'services.title',
		group: 'system',
		icon: 'box',
		admin: true,
		newSince: { version: '0.41.0', date: '2026-10-31' },
	},
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
	{ id: 'power-button', label: 'powerButton.title', group: 'hardware', icon: 'power', admin: true },
	{ id: 'ups', label: 'ups.title', group: 'hardware', icon: 'battery', admin: true },
	{ id: 'memory', label: 'memoryPolicy.title', group: 'hardware', icon: 'memory', admin: true },
	{ id: 'network', label: 'netcfg.title', group: 'network', icon: 'globe', admin: true },
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
		group: 'security',
		icon: 'users',
		admin: true,
		newSince: { version: '0.70.1', date: '2026-11-08' },
	},
	{ id: 'access', label: 'access.title', group: 'security', icon: 'lock', admin: true },
	{
		id: 'firewall',
		label: 'firewall.title',
		group: 'security',
		icon: 'shield',
		admin: true,
		newSince: { version: '0.31.0', date: '2026-10-28' },
	},
	{ id: 'api', label: 'api.title', group: 'security', icon: 'terminal' },
	{
		id: 'notifications',
		label: 'notify.settingsTitle',
		group: 'notifications',
		icon: 'bell',
		admin: true,
	},
	{ id: 'backups', label: 'safeguard.title', group: 'data', icon: 'archive', admin: true },
	{
		id: 'systemdata',
		label: 'sysdata.title',
		group: 'data',
		icon: 'disk',
		admin: true,
		newSince: { version: '0.77.0', date: '2026-11-12' },
	},
	{ id: 'files', label: 'fileIndex.title', group: 'data', icon: 'search', admin: true },
	{
		id: 'cluster',
		label: 'cluster.title',
		group: 'cluster',
		icon: 'tailnet',
		admin: true,
		newSince: { version: '0.38.0', date: '2026-10-30' },
	},
	{ id: 'homeassistant', label: 'ha.title', group: 'smarthome', icon: 'home', admin: true },
	{
		id: 'agent',
		label: 'agent.settingsTitle',
		group: 'smarthome',
		icon: 'sparkle',
		admin: true,
		newSince: { version: '0.48.0', date: '2026-11-05' },
	},
	{
		id: 'mcp',
		label: 'mcp.pageTitle',
		group: 'smarthome',
		icon: 'chat',
		newSince: { version: '0.41.0', date: '2026-10-31' },
	},
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

/** One page of the settings navigation (menu, phone list). */
export type SettingsEntry = {
	href: string;
	id: string;
	label: string;
	group: SettingsGroup;
	icon: IconName;
	newSince?: NewSince;
};

/** The pages an account may open, in the fixed order. */
export function settingsPages(admin: boolean): SettingsArea[] {
	return settingsAreas.filter((area) => !area.admin || admin);
}

/**
 * The pages of the settings navigation for an account, grouped and in the
 * fixed order (PLAN 86). A query keeps the pages whose name or keywords
 * match; the search over every row is `findSettings` (settings-find.ts).
 */
export function settingsEntries(
	query: string,
	admin: boolean,
	translate: (key: MessageKey) => string,
): { group: SettingsGroup; entries: SettingsEntry[] }[] {
	const searching = query.trim() !== '';
	const entries = settingsPages(admin)
		.filter((area) => !searching || matchesSetting(query, translate(area.label), area.id))
		.map((area) => ({
			id: area.id,
			group: area.group,
			icon: area.icon,
			newSince: area.newSince,
			href: `/settings/${area.id}`,
			label: translate(area.label),
		}));
	return settingsGroups
		.map((group) => ({ group, entries: entries.filter((entry) => entry.group === group) }))
		.filter((group) => group.entries.length);
}

/** Whether an account may open a settings page (the server decides again). */
export function settingsSideAllowed(href: string, admin: boolean): boolean {
	if (admin) return true;
	const path = href.split(/[?#]/)[0].replace(/\/$/, '');
	return !path.startsWith('/settings') || userSettingsPaths.includes(path);
}

/**
 * The address of an area: the control panel opens its first page with the
 * area open in the menu (PLAN 86: there is no page of its own any more;
 * links from before keep working).
 */
export function settingsGroupHref(group: SettingsGroup): string {
	return `/settings?area=${group}`;
}

/** The area an address stands for: the page's own, else "?area=" (also of old links). */
export function settingsGroupOf(href: string): SettingsGroup | undefined {
	const [path, query = ''] = href.split('#')[0].split('?');
	const id = settingsAreaOf(path);
	if (id) return settingsAreas.find((area) => area.id === id)?.group;
	const wanted = new URLSearchParams(query).get('area') ?? '';
	const legacy = legacySettingsGroups[wanted];
	if (legacy)
		return typeof legacy === 'string'
			? legacy
			: settingsAreas.find((area) => area.id === legacy.page)?.group;
	return settingsGroups.find((group) => group === wanted);
}

/**
 * Where "/settings?area=<area>" leads an account (PLAN 86): the area's first
 * page it may open (an old link to a page of the area that the account may
 * not open, too), or undefined when it may open none – then the overview.
 */
export function settingsAreaTarget(search: string, admin: boolean): string | undefined {
	const wanted = new URLSearchParams(search).get('area') ?? '';
	const legacy = legacySettingsGroups[wanted];
	if (legacy && typeof legacy !== 'string') {
		const page = settingsAreas.find((area) => area.id === legacy.page);
		if (page && (!page.admin || admin)) return `/settings/${page.id}`;
	}
	const group = settingsGroupOf(`/settings?area=${wanted}`);
	const first = settingsPages(admin).find((area) => area.group === group);
	return first ? `/settings/${first.id}` : undefined;
}

/** One entry of the control panel's menu: an area, its pages and where it leads. */
export type SettingsMenuEntry = {
	group: SettingsGroup;
	label: MessageKey;
	icon: IconName;
	/** The area's first page, or its one page (then the menu has no expander). */
	href: string;
	pages: SettingsArea[];
};

/**
 * The areas of the menu for an account: only areas with a page the account
 * may open (PLAN 78: "Ein Benutzer sieht nur, was er öffnen darf"). An area
 * with one page for this account opens it directly (PLAN 86).
 */
export function settingsMenu(admin: boolean): SettingsMenuEntry[] {
	return settingsGroups.flatMap((group) => {
		const pages = settingsPages(admin).filter((area) => area.group === group);
		if (!pages.length) return [];
		return [
			{
				group,
				label: settingsGroupLabel(group),
				icon: settingsGroupIcons[group],
				href: `/settings/${pages[0].id}`,
				pages,
			},
		];
	});
}

/**
 * The path above a settings page's title (PLAN 78-1): its area as text (the
 * menu shows the area open, PLAN 86), and for a page below a settings page
 * (Sicherungen › Abdeckung) that page as a link.
 */
export function settingsCrumbs(
	pathname: string,
	admin: boolean,
): { label: MessageKey; href?: string }[] {
	const parts = pathname.replace(/\/$/, '').split('/');
	if (parts[1] !== 'settings' || !parts[2]) return [];
	const area = settingsAreas.find((a) => a.id === parts[2]);
	if (!area) return [];
	// An area that is one page (Benachrichtigungen, Cluster) has no path above its name.
	const alone = settingsPages(admin).filter((a) => a.group === area.group).length <= 1;
	if (alone && parts.length <= 3) return [];
	const crumbs: { label: MessageKey; href?: string }[] = alone
		? []
		: [{ label: settingsGroupLabel(area.group) }];
	if (parts.length > 3) crumbs.push({ label: area.label, href: `/settings/${area.id}` });
	return crumbs;
}
