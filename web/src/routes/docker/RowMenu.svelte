<script lang="ts">
	// The menu of one row (app or stack) of the Docker page, split off the
	// page (PLAN 75-5). The page does the actions; this only offers them.
	import ContextMenu from '#lib/components/ContextMenu.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import { dockerName } from '#lib/docker-name.ts';
	import { stackPrimary, type DockerGroup } from '#lib/docker-layout.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import type { DockerContainer } from '#lib/api.ts';
	import { isRunning, webLinks } from './container.ts';

	let {
		group,
		x,
		y,
		app,
		busy,
		update,
		inTailnet,
		primaryServices,
		host,
		tailscaleHost,
		inWindow,
		proxy,
		savingLayout,
		onclose,
		on,
	}: {
		group: DockerGroup;
		x: number;
		y: number;
		/** The catalog app of the row, '' for loose containers. */
		app: string;
		busy: boolean;
		/** An update waits for one of the containers. */
		update: boolean;
		inTailnet: boolean;
		primaryServices: Record<string, string>;
		host: string;
		tailscaleHost: string;
		inWindow: boolean;
		/** The app proxy is there (Tailnet publishing). */
		proxy: boolean;
		savingLayout: boolean;
		onclose: () => void;
		on: {
			restart: () => void;
			openRemote: (app: string) => void;
			logs: (c: DockerContainer) => void;
			console: (c: DockerContainer) => void;
			update: (app: string) => void;
			editLinks: () => void;
			tailnet: (app: string) => void;
			remove: () => void;
		};
	} = $props();

	const primary = $derived(stackPrimary(group, primaryServices) ?? group.members[0]);
	const links = $derived(primary ? webLinks(primary, host, tailscaleHost) : []);
	const stack = $derived(group.members.length > 1);
</script>

<ContextMenu label={t('docker.actionsFor', { name: group.name })} {x} {y} {onclose}>
	<div class="menu-title">{group.name}</div>
	{#if primaryServices[group.id] && !stackPrimary(group, primaryServices)}<p class="menu-title">
			{t('docker.primaryMissing', { service: primaryServices[group.id] })}
		</p>{/if}
	<button type="button" role="menuitem" disabled={busy} onclick={() => on.restart()}
		><Icon name="restart" size={16} /><span>{t('docker.restart')}</span></button
	>
	{#if links.length}<p class="menu-title">{t('docker.table.open')}</p>{/if}
	{#each links as link, index (`${index}:${link.href}`)}<a
			role="menuitem"
			href={link.href}
			target="_blank"
			rel="noreferrer"
			title={link.label}
			onclick={onclose}><Icon name={link.icon} size={16} /><span>{link.text}</span></a
		>{/each}
	{#if inWindow && app}
		<button type="button" role="menuitem" onclick={() => on.openRemote(app)}
			><Icon name="monitor" size={16} /><span>{t('docker.table.openRemote')}</span></button
		>
	{/if}
	<hr />
	{#each group.members as c (c.id)}
		{@const label = c.service || dockerName(c, 'distinct')}
		<button type="button" role="menuitem" onclick={() => on.logs(c)}
			><Icon name="fileText" size={16} /><span
				>{stack ? t('docker.table.logsOf', { name: label }) : t('docker.logs')}</span
			></button
		>
		<button type="button" role="menuitem" disabled={!isRunning(c)} onclick={() => on.console(c)}
			><Icon name="terminal" size={16} /><span
				>{stack ? t('docker.table.consoleOf', { name: label }) : t('docker.console')}</span
			></button
		>
	{/each}
	<hr />
	{#if app}
		<a role="menuitem" href="/docker/apps/{encodeURIComponent(app)}" onclick={onclose}
			><Icon name="edit" size={16} /><span>{t('docker.edit')}</span></a
		>
	{/if}
	{#if app && update}
		<button type="button" role="menuitem" disabled={busy} onclick={() => on.update(app)}
			><Icon name="download" size={16} /><span>{t('docker.updateNow')}</span></button
		>
	{/if}
	{#if stack}
		<button type="button" role="menuitem" disabled={savingLayout} onclick={() => on.editLinks()}
			><Icon name="globe" size={16} /><span>{t('docker.stackLinks')}</span></button
		>
	{/if}
	{#if proxy && app}
		<button type="button" role="menuitem" onclick={() => on.tailnet(app)}
			><Icon name="tailnet" size={16} /><span
				>{t(inTailnet ? 'docker.tailnetUnpublish' : 'docker.tailnetPublish')}</span
			></button
		>
	{/if}
	<hr />
	<button type="button" role="menuitem" class="danger" onclick={() => on.remove()}
		><Icon name="trash" size={16} /><span>{t('docker.remove')}</span></button
	>
</ContextMenu>
