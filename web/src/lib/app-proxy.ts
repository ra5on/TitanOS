// HTTPS names of Docker apps (backend package appproxy): the rules the
// editor checks live, the same as the NAS checks again.

type Ports = { webui: string; ports: { host: number; protocol: string }[] };

const LABEL = /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/;

/** A name is one DNS label: a-z, 0-9 and hyphens, not at the ends, at most 63 characters. */
export function validProxyName(name: string): boolean {
	return LABEL.test(name);
}

/** A domain of one's own: at least two labels, never .local (that one is mDNS). Empty is allowed. */
export function validProxyDomain(domain: string): boolean {
	const value = normalDomain(domain);
	if (!value) return true;
	const labels = value.split('.');
	return (
		value.length <= 200 &&
		labels.length >= 2 &&
		labels.every((label) => LABEL.test(label)) &&
		labels.at(-1) !== 'local'
	);
}

export function normalDomain(domain: string): string {
	return domain.trim().toLowerCase().replace(/\.$/, '');
}

/** The suggested name of an app: its own name, made into a DNS label. */
export function suggestedProxyName(app: string): string {
	return app
		.toLowerCase()
		.replace(/[^a-z0-9-]+/g, '-')
		.replace(/^-+|-+$/g, '')
		.slice(0, 63)
		.replace(/-+$/, '');
}

/** The port to suggest: the one of the WebUI label, else the first published TCP port. */
export function suggestedProxyPort(containers: Ports[]): number {
	for (const container of containers) {
		const port =
			/^(\d{1,5})(?:[/?#].*)?$/.exec(container.webui.trim())?.[1] ??
			/^https?:\/\/\[host\]:(\d{1,5})/i.exec(container.webui.trim())?.[1];
		if (port && +port > 0 && +port < 65536) return +port;
	}
	for (const container of containers) {
		const port = container.ports.find((entry) => entry.protocol !== 'udp' && entry.host > 0);
		if (port) return port.host;
	}
	return 0;
}

/** The addresses an app gets under a name, e.g. https://immich.nas.local. */
export function proxyAddresses(name: string, suffixes: string[]): string[] {
	return validProxyName(name) ? suffixes.map((suffix) => `https://${name}.${suffix}`) : [];
}
