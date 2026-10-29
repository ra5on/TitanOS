import type { DockerContainer } from '#lib/api.ts';
import { appURL, publicURL } from '#lib/app-links.ts';
import { formatDuration } from '#lib/format.ts';
import { t } from '#lib/i18n/index.svelte.ts';

/** What a row or card can ask the page to do with its container. */
export type ContainerActions = {
	onaction: (action: 'start' | 'stop' | 'restart') => void;
	onlogs: () => void;
	onremove: () => void;
	onconsole: () => void;
	onupdate: () => void;
	/** "Im Tailnet veröffentlichen" / "Aus dem Tailnet nehmen" of a managed app. */
	ontailnet?: () => void;
	/** Whether the app is in the tailnet (or on its way there). */
	tailnet?: boolean;
};

export const isRunning = (container: DockerContainer) => container.state === 'running' || container.state === 'restarting';

/** How long the container has been in its state, e.g. "3 d 4 h"; empty without a time. */
export function stateAge(container: DockerContainer): string {
	const time = new Date(container.since).getTime();
	if (!time || time < 0) return '';
	return formatDuration(Math.max(0, (Date.now() - time) / 1000));
}

// Without an icon from a template, an app gets a monogram in a color of
// its own, derived from its name so that it stays the same.

/** The published ports as short labels, e.g. "8096" or "7359/udp". */
export const portLabels = (container: DockerContainer) =>
	[...new Set(container.ports.map((port) => `${port.host}${port.protocol === 'udp' ? '/udp' : ''}`))];

export type WebLink = { icon: 'globe' | 'lock' | 'tailnet' | 'external'; text: string; label: string; href: string };

/** The addresses of the web UI: on the NAS, its HTTPS name, over Tailscale and the public one. */
export function webLinks(container: DockerContainer, host: string, tailscaleHost: string): WebLink[] {
	const links: WebLink[] = [
		{ icon: 'globe', text: t('docker.webui'), label: t('docker.webui'), href: appURL(container.webui, host) },
		{ icon: 'lock', text: t('docker.proxyWebui'), label: t('docker.proxyWebuiLabel'), href: publicURL(container.proxyUrl ?? '') },
		{ icon: 'tailnet', text: t('docker.tailnetWebui'), label: t('docker.tailnetWebuiLabel'), href: publicURL(container.tailnetUrl ?? '') },
		{ icon: 'tailnet', text: t('docker.tailscaleShort'), label: t('docker.tailscaleWebui'), href: appURL(container.webui, tailscaleHost, true) },
		{ icon: 'external', text: t('docker.publicWebui'), label: t('docker.publicWebui'), href: publicURL(container.publicUrl ?? '') }
	];
	return links.filter((link) => link.href);
}
