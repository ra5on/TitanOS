// PLAN 67-1: web apps and the remote view live in the desktop itself, so
// their commands for the menu bar are registered by their window (not by a
// frame); Systemsteuerung registers the search of its side bar.
import { untrack } from 'svelte';
import { api } from '../api.ts';
import { registerWindowCommands } from '../menu-commands.svelte.ts';
import type { WebApp } from '../webapps/protocol.ts';
import type WebAppView from '../webapps/WebAppView.svelte';
import { openProgram } from './programs.svelte.ts';
import type RemoteViewType from './RemoteView.svelte';
import { sources } from './sources.svelte.ts';
import type { Win } from './wm.svelte.ts';

/** What the commands need from the window. */
export type WindowMenuHost = {
	win: () => Win;
	external: () => boolean;
	webapp: () => WebApp | undefined;
	view: () => WebAppView | undefined;
	remote: () => RemoteViewType | undefined;
	restartApp: () => void;
	openTab: () => void;
	focusSearch: () => Promise<void> | undefined;
};

/** Registers the window's commands; call while the window's component starts. */
export function registerWindowMenu(host: WindowMenuHost) {
	async function restartDockerApp() {
		const app = host.win().app.slice('docker:'.length);
		for (const c of sources.containers.filter((x) => (x.app || x.name) === app))
			await api.post(`/docker/containers/${encodeURIComponent(c.id)}/restart`).catch(() => {});
	}
	if (untrack(() => host.win().app.startsWith('webapp:')))
		registerWindowCommands(
			untrack(() => host.win().id),
			{
				'webapp.open': { run: () => host.view()?.openPicker(), enabled: () => !!host.webapp() },
				'webapp.saveAs': {
					run: () => host.view()?.saveAs(),
					enabled: () => !!host.webapp() && host.webapp()?.bridge === 'host',
				},
				'webapp.reload': { run: host.restartApp, enabled: () => !!host.webapp() },
				// A new window of the app with no file (the app starts empty).
				'webapp.new': {
					run: () => void openProgram(host.win().app, { fresh: true }),
					enabled: () => !!host.webapp(),
				},
				...Object.fromEntries(
					(
						[
							['webapp.save', 'save'],
							['webapp.undo', 'undo'],
							['webapp.redo', 'redo'],
							['webapp.find', 'find'],
							['webapp.selectAll', 'select-all'],
						] as const
					).map(([id, name]) => [
						id,
						{
							run: () => host.view()?.command(name),
							enabled: () => !!host.webapp() && host.webapp()?.bridge === 'host',
						},
					]),
				),
				'window.openTab': host.openTab,
			},
		);
	else if (untrack(() => host.win().app === 'app:settings'))
		registerWindowCommands(
			untrack(() => host.win().id),
			{ 'settings.search': () => void host.focusSearch() },
		);
	else if (untrack(() => host.external()))
		registerWindowCommands(
			untrack(() => host.win().id),
			{
				'remote.upload': {
					run: () => host.remote()?.menu.upload(),
					enabled: () => !!host.remote()?.menu.live(),
				},
				'remote.clipSend': {
					run: () => void host.remote()?.menu.clipSend(),
					enabled: () => !!host.remote()?.menu.live(),
				},
				'remote.clipGet': {
					run: () => void host.remote()?.menu.clipGet(),
					enabled: () => !!host.remote()?.menu.remoteClip(),
				},
				...Object.fromEntries(
					(['auto', 'high', 'balanced', 'low'] as const).map((q) => [
						'remote.quality.' + q,
						{
							run: () => host.remote()?.menu.setQuality(q),
							checked: () => host.remote()?.menu.quality() === q,
						},
					]),
				),
				'remote.mute': {
					run: () => host.remote()?.menu.toggleMute(),
					enabled: () => !!host.remote()?.menu.audio(),
					checked: () => !!host.remote()?.menu.muted(),
				},
				'remote.fullscreen': {
					run: () => host.remote()?.menu.fullscreen(),
					enabled: () => !!host.remote()?.menu.live(),
				},
				'remote.reconnect': {
					run: () => host.remote()?.menu.reconnect(),
					enabled: () => !!host.remote(),
				},
				'remote.downloads': {
					items: () => host.remote()?.menu.downloads() ?? [],
					run: (id) => id && host.remote()?.menu.download(id),
				},
				'remote.stats': {
					run: () => host.remote()?.menu.toggleStats(),
					enabled: () => !!host.remote()?.menu.live(),
					checked: () => !!host.remote()?.menu.stats(),
				},
				'remote.restart': restartDockerApp,
				'window.openTab': host.openTab,
			},
		);
}
