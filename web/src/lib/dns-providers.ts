// DNS providers for Let's Encrypt (TXT records of the DNS challenge) and
// dynamic DNS (A/AAAA records), as backend package dnsprovider knows them.
// Secrets (token, password, secret) never come back from the NAS; the form
// sends one only when it was typed in, and names dropped ones in clearSecrets.

export type DnsProviderType =
	| 'cloudflare'
	| 'desec'
	| 'hetzner'
	| 'ionos'
	| 'netcup'
	| 'inwx'
	| 'duckdns'
	| 'ipv64'
	| 'rfc2136'
	| 'dyndns2';

export type DnsProvider = {
	type: DnsProviderType | '';
	customer?: string;
	user?: string;
	server?: string;
	zone?: string;
	keyName?: string;
	algorithm?: string;
	token?: string;
	password?: string;
	secret?: string;
	updateToken?: string;
	ipv6Prefix?: string;
};

export type SecretName = 'token' | 'password' | 'secret' | 'updateToken';
export type FieldName = Exclude<keyof DnsProvider, 'type'>;

export type ProviderField = {
	name: FieldName;
	/** Suffix of the label key: dnsProvider.field.<label>. */
	label: string;
	secret?: boolean;
	required?: boolean;
	placeholder?: string;
	/** The field is a choice from these values. */
	choices?: string[];
	/** Only for this purpose; both when missing. */
	only?: 'challenge' | 'address';
	/** Required for the DNS challenge only. */
	requiredForChallenge?: boolean;
};

export const TSIG_ALGORITHMS = [
	'hmac-sha256',
	'hmac-sha512',
	'hmac-sha384',
	'hmac-sha224',
	'hmac-sha1',
];

export const providerFields: Record<DnsProviderType, ProviderField[]> = {
	cloudflare: [{ name: 'token', label: 'apiToken', secret: true, required: true }],
	desec: [{ name: 'token', label: 'apiToken', secret: true, required: true }],
	hetzner: [{ name: 'token', label: 'apiToken', secret: true, required: true }],
	ionos: [
		{
			name: 'token',
			label: 'ionosKey',
			secret: true,
			required: true,
			placeholder: 'prefix.secret',
		},
	],
	netcup: [
		{ name: 'customer', label: 'customer', required: true, placeholder: '12345' },
		{ name: 'token', label: 'apiKey', secret: true, required: true },
		{ name: 'password', label: 'apiPassword', secret: true, required: true },
	],
	inwx: [
		{ name: 'user', label: 'user', required: true },
		{ name: 'password', label: 'password', secret: true, required: true },
	],
	duckdns: [{ name: 'token', label: 'apiToken', secret: true, required: true }],
	// IPv64.net: the API key of the account can do everything; the domain
	// update token only DynDNS. For dynamic DNS one of the two is enough.
	ipv64: [
		{ name: 'token', label: 'ipv64ApiKey', secret: true, requiredForChallenge: true },
		{ name: 'updateToken', label: 'ipv64UpdateToken', secret: true, only: 'address' },
		{ name: 'ipv6Prefix', label: 'ipv6Prefix', placeholder: 'auto', only: 'address' },
	],
	rfc2136: [
		{ name: 'server', label: 'nameServer', required: true, placeholder: 'ns1.example.org:53' },
		{ name: 'zone', label: 'zone', required: true, placeholder: 'example.org' },
		{ name: 'keyName', label: 'keyName', required: true, placeholder: 'titanos' },
		{ name: 'algorithm', label: 'algorithm', required: true, choices: TSIG_ALGORITHMS },
		{ name: 'secret', label: 'tsigSecret', secret: true, required: true },
	],
	dyndns2: [
		{
			name: 'server',
			label: 'updateUrl',
			required: true,
			placeholder: 'https://dynupdate.no-ip.com/nic/update',
		},
		{ name: 'user', label: 'user' },
		{ name: 'password', label: 'password', secret: true },
	],
};

/** The providers for a purpose; dyndns2 only changes addresses. */
export function providerTypes(purpose: 'challenge' | 'address'): DnsProviderType[] {
	const all: DnsProviderType[] = [
		'cloudflare',
		'desec',
		'hetzner',
		'ionos',
		'netcup',
		'inwx',
		'duckdns',
		'ipv64',
		'rfc2136',
	];
	return purpose === 'address' ? [...all, 'dyndns2'] : all;
}

/** The fields of a provider for a purpose. */
export function fieldsFor(
	type: DnsProviderType,
	purpose: 'challenge' | 'address',
): ProviderField[] {
	return providerFields[type]
		.filter((field) => !field.only || field.only === purpose)
		.map((field) =>
			field.requiredForChallenge && purpose === 'challenge' ? { ...field, required: true } : field,
		);
}

/** Fields the account of a provider is known by: secrets stay only with them. */
const accountFields: FieldName[] = ['customer', 'user', 'server', 'keyName'];

/** Whether the stored secrets still belong to the account in the form. */
export function sameAccount(form: DnsProvider, stored: DnsProvider): boolean {
	return (
		form.type === stored.type && accountFields.every((f) => (form[f] ?? '') === (stored[f] ?? ''))
	);
}

/**
 * Problems of the form: required fields left empty. A secret counts as
 * filled when the NAS has one for the same account and it is not dropped.
 */
export function missingFields(
	form: DnsProvider,
	stored: DnsProvider,
	set: Partial<Record<SecretName, boolean>>,
	cleared: SecretName[],
	purpose: 'challenge' | 'address' = 'challenge',
): FieldName[] {
	if (!form.type) return [];
	const keep = sameAccount(form, stored);
	const filled = (field: ProviderField) => {
		if ((form[field.name] ?? '').trim()) return true;
		return (
			!!field.secret &&
			keep &&
			!!set[field.name as SecretName] &&
			!cleared.includes(field.name as SecretName)
		);
	};
	const fields = fieldsFor(form.type, purpose);
	const missing = fields
		.filter((field) => field.required && !filled(field))
		.map((field) => field.name);
	// IPv64 for dynamic DNS: the API key or the update token.
	if (form.type === 'ipv64' && purpose === 'address' && !fields.some((f) => f.secret && filled(f)))
		missing.push('token', 'updateToken');
	return missing;
}

/** What is sent: only the fields of the chosen provider, secrets only when typed. */
export function providerPayload(
	form: DnsProvider,
	purpose: 'challenge' | 'address' = 'challenge',
): DnsProvider {
	if (!form.type) return { type: '' };
	const out: DnsProvider = { type: form.type };
	for (const field of fieldsFor(form.type, purpose)) {
		const value = (form[field.name] ?? '').trim();
		if (value) out[field.name] = value;
	}
	return out;
}

/** A form for the stored provider: secrets empty, a TSIG algorithm chosen. */
export function providerForm(stored: DnsProvider | undefined): DnsProvider {
	const form: DnsProvider = { type: '', ...(stored ?? {}) };
	form.token = form.password = form.secret = form.updateToken = '';
	if (form.type === 'rfc2136' && !form.algorithm) form.algorithm = 'hmac-sha256';
	return form;
}

/** Host names from a text field: one per line (or separated by spaces/commas), lower case, no trailing dot. */
export function parseNames(text: string): string[] {
	const seen = new Set<string>();
	for (const part of text.split(/[\s,;]+/)) {
		const name = part.trim().toLowerCase().replace(/\.$/, '');
		if (name) seen.add(name);
	}
	return [...seen];
}

const label = /^(?!-)[a-z0-9_-]{1,63}(?<!-)$/;

/** Whether a host name is acceptable; wildcards ("*.example.org") only where allowed. */
export function validHostName(name: string, wildcard = false): boolean {
	let host = name;
	if (wildcard && host.startsWith('*.')) host = host.slice(2);
	if (host.length > 253 || !host.includes('.') || host.endsWith('.local')) return false;
	return host.split('.').every((part) => label.test(part));
}

/** The zones IPv64 makes DynDNS names under, should the NAS not say. */
export const IPV64_ZONES = ['ipv64.net', 'ipv64.de', 'home64.de', 'nas64.de', 'dyndns64.de'];

/** A wished name for a new IPv64 DynDNS name: one label. */
export function validIpv64Wish(wish: string): boolean {
	return /^(?!-)[a-z0-9-]{1,63}(?<!-)$/.test(wish);
}
