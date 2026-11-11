<script lang="ts">
	// The menus of the tool bar: ⋯ (everything at once, settings, LAN, auto
	// updates) and the arrow next to "Install app".
	import ContextMenu from '#lib/components/ContextMenu.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import type { DockerPage } from './docker-page.svelte.ts';

	let { p }: { p: DockerPage } = $props();
</script>

{#if p.pageMenu && p.pageMenuAt}
	<ContextMenu
		label={t(p.pageMenu === 'more' ? 'docker.table.more' : 'docker.installMenu')}
		x={p.pageMenuAt.x}
		y={p.pageMenuAt.y}
		onclose={() => (p.pageMenu = null)}
	>
		{#if p.pageMenu === 'more'}
			{#if p.status?.running && p.containers.length > 0}
				<button
					type="button"
					role="menuitem"
					disabled={p.globalBusy || p.runCount === p.containers.length}
					onclick={() => p.pick(p.startAll)}
					><Icon name="play" size={16} /><span>{t('docker.startAll')}</span></button
				>
				<button
					type="button"
					role="menuitem"
					disabled={p.globalBusy || p.runCount === 0}
					onclick={() => p.pick(() => (p.confirmStopAll = true))}
					><Icon name="stop" size={16} /><span>{t('docker.stopAll')} …</span></button
				>
				<button
					type="button"
					role="menuitem"
					disabled={p.globalBusy}
					onclick={() => p.pick(p.checkUpdates)}
					><Icon name="refresh" size={16} /><span>{t('docker.checkUpdates')}</span></button
				>
				<button
					type="button"
					role="menuitem"
					disabled={p.globalBusy || !p.updatable}
					onclick={() => p.pick(p.askUpdates)}
					><Icon name="download" size={16} /><span
						>{t('docker.updateAllCount', { count: p.updateApps.length })} …</span
					></button
				>
				<hr />
			{/if}
			{#if p.status?.settings.enabled}
				<button type="button" role="menuitem" onclick={() => p.pick(() => p.open('settings'))}
					><Icon name="gear" size={16} /><span
						>{t('docker.settings')}<small>{t('docker.settingsHint')}</small></span
					></button
				>
			{/if}
			<a role="menuitem" href="/docker/networks" onclick={() => (p.pageMenu = null)}
				><Icon name="globe" size={16} /><span
					>{t('lan.title')}<small>{t('docker.lanHint')}</small></span
				></a
			>
			{#if p.status?.settings.enabled}
				<button
					type="button"
					role="menuitem"
					disabled={!p.status.running}
					onclick={() => p.pick(() => (p.autoUpdates = true))}
					><Icon name="clock" size={16} /><span
						>{t('autoUpdate.button')}<small>{t('docker.autoUpdateHint')}</small></span
					></button
				>
			{/if}
		{:else}
			<a role="menuitem" href="/apps" onclick={() => (p.pageMenu = null)}
				><Icon name="grid" size={16} /><span
					>{t('docker.fromCatalog')}<small>{t('docker.fromCatalogHint')}</small></span
				></a
			>
			<button
				type="button"
				role="menuitem"
				disabled={!p.status?.running}
				onclick={() => p.pick(() => p.open('import'))}
				><Icon name="fileText" size={16} /><span
					>{t('docker.import')}<small>{t('docker.importHint')}</small></span
				></button
			>
			<a role="menuitem" href="/apps/new" onclick={() => (p.pageMenu = null)}
				><Icon name="box" size={16} /><span
					>{t('docker.ownContainer')}<small>{t('docker.ownContainerHint')}</small></span
				></a
			>
		{/if}
	</ContextMenu>
{/if}

<style>
	:global(.dropdown-panel) [role^='menuitem'] small {
		display: block;
		color: var(--muted);
		font-size: var(--text-sm);
		white-space: normal;
	}
	small {
		font-size: var(--text-sm);
		color: var(--muted);
	}
</style>
