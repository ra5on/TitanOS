import { isAlias, parseDocument, type Document } from 'yaml';
export type ContainerLAN = {
	name: string;
	parent: string;
	driver: 'ipvlan' | 'macvlan';
	subnet: string;
	gateway: string;
	range: string;
	hostAccess: boolean;
	hostAddress?: string;
	error?: string;
};
export function lanAssignment(text: string, service: string): { network: string; address: string } {
	const doc = parseDocument(text, { merge: true });
	if (doc.errors.length) return { network: '', address: '' };
	const data = doc.toJS();
	const attached = data?.services?.[service]?.networks ?? {};
	for (const [key, value] of Object.entries(attached)) {
		const name = data?.networks?.[key]?.name;
		if (typeof name === 'string' && name.startsWith('titanos-lan-'))
			return {
				network: name.slice(12),
				address: (value as { ipv4_address?: string })?.ipv4_address ?? '',
			};
	}
	return { network: '', address: '' };
}
export function assignContainerLAN(
	text: string,
	service: string,
	network: string,
	address: string,
): string {
	const doc: Document = parseDocument(text, { merge: true });
	if (
		doc.errors.length ||
		!doc.hasIn(['services', service]) ||
		isAlias(doc.getIn(['services', service]))
	)
		throw Error('invalid compose');
	const data = doc.toJS();
	if (data.services[service].network_mode) throw Error('network mode must be private');
	const current = data.services[service].networks;
	const attached: Record<string, unknown> = Array.isArray(current)
		? Object.fromEntries(current.map((n: string) => [n, null]))
		: current
			? { ...current }
			: { default: null };
	const previous: string[] = [];
	for (const key of Object.keys(attached)) {
		if (data.networks?.[key]?.name?.startsWith?.('titanos-lan-')) {
			previous.push(key);
			delete attached[key];
		}
	}
	if (network) {
		const key = 'titanos_lan_' + network.replaceAll('-', '_');
		const old = data.networks?.[key];
		if (old && old.name !== 'titanos-lan-' + network) throw Error('network key conflict');
		doc.setIn(['networks', key], { external: true, name: 'titanos-lan-' + network });
		attached[key] = address.trim() ? { ipv4_address: address.trim() } : {};
	}
	if (Object.hasOwn(attached, 'default') && !doc.hasIn(['networks', 'default']))
		doc.setIn(['networks', 'default'], {});
	doc.setIn(['services', service, 'networks'], attached);
	const updated = doc.toJS();
	for (const key of previous) {
		const used = Object.values(updated.services ?? {}).some((v: any) =>
			Array.isArray(v.networks) ? v.networks.includes(key) : Object.hasOwn(v.networks ?? {}, key),
		);
		if (!used) doc.deleteIn(['networks', key]);
	}
	return doc.toString();
}
