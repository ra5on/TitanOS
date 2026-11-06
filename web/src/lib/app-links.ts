/** Only navigable web URLs; credentials and control characters are never links. */
export function publicURL(value: string): string {
	const text = value.trim();
	if (!/^https?:\/\//i.test(text) || /[\x00-\x20\x7f]/.test(text)) return '';
	try {
		const url = new URL(text);
		return url.hostname && !url.username && !url.password ? url.href : '';
	} catch {
		return '';
	}
}
export function appURL(value: string, host: string, tailnet = false): string {
	const text = value.trim();
	// A fixed external domain must not be rewritten to a NAS address.
	const portable =
		/^\d+(?:[/?#].*)?$/.test(text) || /^https?:\/\/\[host\]:\d+(?:[/?#].*)?$/i.test(text);
	if (tailnet && (!host || !portable)) return '';
	const address = host.includes(':') && !host.startsWith('[') ? `[${host}]` : host;
	return publicURL(
		/^\d/.test(text) ? `http://${address}:${text}` : text.replace('[host]', address),
	);
}
export type TailnetStatus = { active: boolean; state: string; name: string; ips: string[] };
export function tailnetHost(status: TailnetStatus | null): string {
	if (!status?.active || status.state !== 'Running') return '';
	return (
		status.name.replace(/\.$/, '') ||
		status.ips.find((ip) => !ip.includes(':')) ||
		status.ips[0] ||
		''
	);
}

/**
 * A link to an app's HTTPS name through TitanOS (PLAN 61): the NAS first
 * hands the browser a short pass for the app's browser gate, then sends it
 * on to url. open turns an API path into an address (apiPath).
 */
export function gatedAppURL(app: string, url: string, open: (path: string) => string): string {
	const href = publicURL(url);
	if (!href || !/^[a-z0-9][a-z0-9_-]{0,31}$/.test(app)) return href;
	const host = new URL(href).hostname;
	return `${open(`/apps/${encodeURIComponent(app)}/open`)}?host=${encodeURIComponent(host)}`;
}
