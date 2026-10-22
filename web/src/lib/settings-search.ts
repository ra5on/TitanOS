import type { MessageKey } from '#lib/i18n/index.svelte.ts';

const keywords: Record<string, string> = {
	appearance: 'theme design farbe color font schrift hintergrund background',
	network: 'ip dns gateway bridge vlan bonding netzwerk network',
	notifications: 'email e-mail ntfy telegram discord push meldungen notifications',
	updates: 'version update rollback aktualisieren rückwechsel',
	access: 'ssh https tls ssl zertifikat certificate schlüssel key zugang access',
	assistant: 'ki ai assistent assistant chat ollama modell model llm sprachmodell websuche web search duckduckgo',
	backups: 'backup sicherung datensicherung restic usb smb s3 kopie copy stick config konfiguration wiederherstellung restore',
	stick: 'stick usb config konfiguration configuration wiederherstellen restore',
	ups: 'usv ups batterie battery strom power',
	hardware: 'gpu npu pci usb passthrough hardware',
	'power-button': 'power button taste knopf ausschalten shutdown',
	memory: 'ram memory speicher cache',
	energy: 'energie energy governor cpu strom',
	temperatures: 'temperatur temperature sensor wärme heat',
	about: 'lizenz license version titanos fair source',
	timezone: 'zeitzone timezone time zone uhrzeit zeit clock',
	'data-backup': 'backup datensicherung dateien files restic usb smb s3 kopie copy',
	ssh: 'ssh schlüssel key remote',
	certificate: 'https tls ssl zertifikat certificate',
	dns: 'dns namensauflösung nameserver',
	restore: 'wiederherstellung wiederherstellen restore ersetzen replace',
	terminal: 'terminal shell konsole console bestätigung confirmation'
};

function normalize(text: string): string {
	return text.normalize('NFKD').replace(/\p{M}/gu, '').toLowerCase();
}

export function matchesSetting(query: string, title: string, id: string): boolean {
	const haystack = normalize(`${title} ${keywords[id] ?? ''}`);
	return normalize(query).trim().split(/\s+/).every(word => haystack.includes(word));
}

export const settingSections: Record<string, { id: string; label: MessageKey; href: string }[]> = {
	access: [
		{ id: 'ssh', label: 'access.ssh', href: '/settings/access#access-ssh' },
		{ id: 'certificate', label: 'access.cert', href: '/settings/access#access-cert' },
		{ id: 'terminal', label: 'settings.consoles', href: '/settings/access#access-terminal' }
	],
	network: [{ id: 'dns', label: 'netcfg.dns', href: '/settings/network#network-dns' }],
	backups: [
		{ id: 'data-backup', label: 'safeguard.data', href: '/settings/backups/data' },
		{ id: 'stick', label: 'safeguard.config', href: '/settings/backups/configuration' },
		{ id: 'restore', label: 'safeguard.restore', href: '/settings/backups/restore' }
	]
};
