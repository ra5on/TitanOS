const keywords: Record<string, string> = {
	appearance: 'theme design farbe color font schrift hintergrund background',
	network: 'ip dns gateway bridge vlan bonding netzwerk network',
	notifications: 'email e-mail ntfy telegram discord push meldungen notifications',
	updates: 'version update rollback aktualisieren rückwechsel',
	access: 'ssh https tls ssl zertifikat certificate schlüssel key zugang access',
	backups: 'backup sicherung restic usb smb s3 kopie copy',
	stick: 'stick usb config konfiguration configuration wiederherstellen restore',
	ups: 'usv ups batterie battery strom power',
	hardware: 'gpu npu pci usb passthrough hardware',
	'power-button': 'power button taste knopf ausschalten shutdown',
	memory: 'ram memory speicher cache',
	energy: 'energie energy governor cpu strom',
	temperatures: 'temperatur temperature sensor wärme heat',
	about: 'lizenz license version titanos fair source',
	timezone: 'zeitzone timezone time zone uhrzeit zeit clock',
	terminal: 'terminal shell konsole console bestätigung confirmation'
};

function normalize(text: string): string {
	return text.normalize('NFKD').replace(/\p{M}/gu, '').toLowerCase();
}

export function matchesSetting(query: string, title: string, id: string): boolean {
	const haystack = normalize(`${title} ${keywords[id] ?? ''}`);
	return normalize(query).trim().split(/\s+/).every(word => haystack.includes(word));
}
