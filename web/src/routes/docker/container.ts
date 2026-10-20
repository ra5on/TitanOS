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
export const monogramHue = (name: string) => [...name].reduce((sum, char) => (sum * 31 + char.charCodeAt(0)) % 360, 7);

/** The published ports as short labels, e.g. "8096" or "7359/udp". */
export const portLabels = (container: DockerContainer) =>
	[...new Set(container.ports.map((port) => `${port.host}${port.protocol === 'udp' ? '/udp' : ''}`))];

/** The web UI on the NAS address, and further addresses for the menu. */
export function webLinks(container: DockerContainer, host: string, tailscaleHost: string) {
	const extra = [
		{ label: t('docker.tailscaleWebui'), href: appURL(container.webui, tailscaleHost, true) },
		{ label: t('docker.publicWebui'), href: publicURL(container.publicUrl ?? '') }
	].filter((link) => link.href);
	return { main: appURL(container.webui, host), extra };
}
