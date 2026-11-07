import type { DockerContainer } from '#lib/api.ts';
import { appURL, gatedAppURL, publicURL } from '#lib/app-links.ts';
import { apiPath } from '#lib/cluster-node.ts';
import { t } from '#lib/i18n/index.svelte.ts';

export const isRunning = (container: DockerContainer) =>
	container.state === 'running' || container.state === 'restarting';

/** The published ports as short labels, e.g. "8096" or "7359/udp". */
export const portLabels = (container: DockerContainer) => [
	...new Set(container.ports.map((port) => `${port.host}${port.protocol === 'udp' ? '/udp' : ''}`)),
];

export type WebLink = {
	icon: 'globe' | 'lock' | 'tailnet' | 'external';
	text: string;
	label: string;
	href: string;
};

/** The addresses of the web UI: on the NAS, its HTTPS name, over Tailscale and the public one. */
export function webLinks(
	container: DockerContainer,
	host: string,
	tailscaleHost: string,
): WebLink[] {
	const links: WebLink[] = [
		{
			icon: 'globe',
			text: t('docker.webui'),
			label: t('docker.webui'),
			href: appURL(container.webui, host),
		},
		{
			icon: 'lock',
			text: t('docker.proxyWebui'),
			label: t('docker.proxyWebuiLabel'),
			href: gatedAppURL(container.app, container.proxyUrl ?? '', apiPath),
		},
		{
			icon: 'tailnet',
			text: t('docker.tailnetWebui'),
			label: t('docker.tailnetWebuiLabel'),
			href: gatedAppURL(container.app, container.tailnetUrl ?? '', apiPath),
		},
		{
			icon: 'tailnet',
			text: t('docker.tailscaleShort'),
			label: t('docker.tailscaleWebui'),
			href: appURL(container.webui, tailscaleHost, true),
		},
		{
			icon: 'external',
			text: t('docker.publicWebui'),
			label: t('docker.publicWebui'),
			href: publicURL(container.publicUrl ?? ''),
		},
	];
	return links.filter((link) => link.href);
}
