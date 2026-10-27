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

export type SettingsGroup = 'system' | 'network' | 'storage' | 'integrations' | 'personal';
export type SettingsArea = {
	id: string;
	label: MessageKey;
	group: SettingsGroup;
	icon: IconName;
	admin?: boolean;
};

// One list for the settings overview, the command palette and the route guard.
export const settingsAreas: SettingsArea[] = [
	{ id: 'appearance', label: 'menu.appearance', group: 'personal', icon: 'palette' },
	{ id: 'api', label: 'api.title', group: 'personal', icon: 'key' },
	{ id: 'network', label: 'netcfg.title', group: 'network', icon: 'globe', admin: true },
	{ id: 'notifications', label: 'notify.settingsTitle', group: 'system', icon: 'bell', admin: true },
	{ id: 'updates', label: 'update.title', group: 'system', icon: 'download', admin: true },
	{ id: 'access', label: 'access.title', group: 'network', icon: 'lock', admin: true },
	{ id: 'assistant', label: 'assistant.settingsTitle', group: 'integrations', icon: 'chat', admin: true },
	{ id: 'backups', label: 'safeguard.title', group: 'storage', icon: 'archive', admin: true },
	{ id: 'timezone', label: 'settings.datetime', group: 'system', icon: 'clock', admin: true },
	{ id: 'ups', label: 'ups.title', group: 'system', icon: 'battery', admin: true },
	{ id: 'homeassistant', label: 'ha.title', group: 'integrations', icon: 'home', admin: true },
	{ id: 'hardware', label: 'hardware.title', group: 'system', icon: 'monitor', admin: true },
	{ id: 'power-button', label: 'powerButton.title', group: 'system', icon: 'power', admin: true },
	{ id: 'memory', label: 'memoryPolicy.title', group: 'storage', icon: 'grid', admin: true },
	{ id: 'files', label: 'fileIndex.title', group: 'storage', icon: 'search', admin: true },
	{ id: 'energy', label: 'energy.title', group: 'system', icon: 'zap', admin: true },
	{ id: 'temperatures', label: 'temperatures.title', group: 'personal', icon: 'activity' },
	{ id: 'about', label: 'about.title', group: 'system', icon: 'fileText' },
];

/** Settings paths that accounts with the role "user" may open. */
export const userSettingsPaths = [
	'/settings',
	...settingsAreas.filter((area) => !area.admin).map((area) => `/settings/${area.id}`),
];
