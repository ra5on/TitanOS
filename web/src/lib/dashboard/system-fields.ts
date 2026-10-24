export const SYSTEM_FIELDS = [
	'hostname',
	'version',
	'uptime',
	'mainboard',
	'bios',
	'cpu',
	'memory',
	'kernel',
	'slot',
	'ipv4',
	'ipv6',
	'link',
	'drivers',
	'update',
	'image',
	'overlay',
	'stick',
] as const;
export type SystemField = (typeof SYSTEM_FIELDS)[number];
export const DEFAULT_SYSTEM_FIELDS: SystemField[] = [
	'hostname',
	'version',
	'uptime',
	'mainboard',
	'bios',
	'cpu',
	'memory',
	'kernel',
	'slot',
];
export function systemFields(value: unknown): SystemField[] {
	return Array.isArray(value)
		? [...new Set(value.filter((v): v is SystemField => SYSTEM_FIELDS.includes(v)))]
		: [...DEFAULT_SYSTEM_FIELDS];
}
