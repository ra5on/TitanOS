import type { MessageKey } from '#lib/i18n/index.svelte.ts';
import type { IconName } from '#lib/components/Icon.svelte';

const keywords: Record<string, string> = {
	appearance: 'theme design farbe color font schrift hintergrund background',
	network: 'ip dns gateway bridge vlan bonding netzwerk network',
	notifications: 'email e-mail ntfy telegram discord push meldungen notifications',
	updates: 'version update rollback aktualisieren rückwechsel',
	access: 'ssh https tls ssl zertifikat certificate schlüssel key zugang access',
	assistant:
		'ki ai assistent assistant chat ollama modell model llm sprachmodell websuche web search duckduckgo',
	backups:
		'backup sicherung datensicherung restic usb smb s3 kopie copy stick config konfiguration wiederherstellung restore',
	stick: 'stick usb config konfiguration configuration wiederherstellen restore',
	ups: 'usv ups batterie battery strom power',
	homeassistant:
		'home assistant hass smart home licht light schalter switch mqtt sensoren sensors automation',
	hardware: 'gpu npu pci usb passthrough hardware',
	'power-button': 'power button taste knopf ausschalten shutdown',
	memory: 'ram memory speicher cache',
	files:
		'dateisuche volltext index inhalt full text search content ocr vorschaubilder thumbnails miniaturen',
	energy:
		'energie energy governor cpu strom zeitplan schedule herunterfahren shutdown einschalten wake on lan wol rtc',
	temperatures: 'temperatur temperature sensor wärme heat',
	about: 'lizenz license version titanos fair source',
	timezone: 'zeitzone timezone time zone uhrzeit zeit clock',
	coverage: 'abdeckung coverage geschützt protected ungesichert unprotected',
	'application-restore': 'anwendung app application docker vm wiederherstellung restore',
	'data-backup': 'backup datensicherung dateien files restic usb smb s3 kopie copy',
	ssh: 'ssh schlüssel key remote',
	certificate: 'https tls ssl zertifikat certificate',
	dns: 'dns namensauflösung nameserver',
	restore: 'wiederherstellung wiederherstellen restore ersetzen replace',
	terminal: 'terminal shell konsole console bestätigung confirmation',
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
		{ id: 'terminal', label: 'settings.consoles', href: '/settings/access#access-terminal' },
	],
	network: [{ id: 'dns', label: 'netcfg.dns', href: '/settings/network#network-dns' }],
	homeassistant: [
		{ id: 'connection', label: 'ha.connection', href: '/settings/homeassistant#connection' },
		{ id: 'release', label: 'ha.release', href: '/settings/homeassistant#release' },
		{ id: 'mqtt', label: 'ha.mqtt', href: '/settings/homeassistant#mqtt' },
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
	{ id: 'proxy', label: 'proxy.settingsTitle', group: 'network', icon: 'external', admin: true },
	{ id: 'access', label: 'access.title', group: 'network', icon: 'lock', admin: true },
	{ id: 'backups', label: 'safeguard.title', group: 'data', icon: 'archive', admin: true },
	{ id: 'files', label: 'fileIndex.title', group: 'data', icon: 'search', admin: true },
	{ id: 'homeassistant', label: 'ha.title', group: 'integrations', icon: 'home', admin: true },
	{ id: 'assistant', label: 'assistant.settingsTitle', group: 'integrations', icon: 'chat', admin: true },
	{ id: 'api', label: 'api.title', group: 'integrations', icon: 'key' },
	{ id: 'appearance', label: 'menu.appearance', group: 'personal', icon: 'palette' },
];

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
