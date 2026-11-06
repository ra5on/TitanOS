<script lang="ts">
	// PLAN 61: the bar above a program on the phone – back to the home
	// screen, the program's name and its pages.
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import Icon from '../components/Icon.svelte';
	import { t } from '../i18n/index.svelte.ts';
	import DesktopIcon from './DesktopIcon.svelte';
	import { appForPath, sideActive } from './apps.ts';
	import { activeNode } from '../cluster-node.ts';
	import { clusterNodes } from '../cluster.svelte.ts';

	const app = $derived(appForPath(page.url.pathname));
	const items = $derived((app?.side ?? []).filter((i) => 'href' in i));
	let open = $state(false);
	// PLAN 64: while this tab manages another node of the cluster, its name stays in sight.
	const nodeId = activeNode();
	const node = $derived(nodeId ? clusterNodes.nodes.find((n) => n.id === nodeId) : undefined);
</script>

<header class="phone-bar">
	<button type="button" class="back" onclick={() => goto('/')} aria-label={t('desktop.phone.home')}
		><Icon name="chevronLeft" size={22} /></button
	>
	{#if app}<DesktopIcon
			glyph={app.glyph}
			gradient={app.gradient}
			size={26}
			name={t(app.label)}
		/>{/if}
	<b>{app ? t(app.label) : ''}</b>
	<span class="space"></span>
	{#if node}<span
			class="node mono"
			title={t('cluster.managing', { name: node.name })}
			aria-label={t('cluster.managing', { name: node.name })}>{node.name}</span
		>{/if}
	{#if items.length > 1}
		<button
			type="button"
			class="back"
			aria-haspopup="menu"
			aria-expanded={open}
			onclick={() => (open = !open)}
			aria-label={t('desktop.phone.pages')}><Icon name="more" size={20} /></button
		>
		{#if open}
			<div class="pages dropdown-panel" role="menu">
				{#each items as item, i (i)}
					{#if 'label' in item}
						<button
							type="button"
							role="menuitemradio"
							aria-checked={sideActive(item, page.url.pathname, app?.side ?? [])}
							onclick={() => ((open = false), goto(item.href))}><span>{t(item.label)}</span></button
						>
					{/if}
				{/each}
			</div>
		{/if}
	{/if}
</header>

<style>
	.phone-bar {
		position: sticky;
		top: 0;
		z-index: 30;
		display: flex;
		align-items: center;
		gap: 8px;
		height: calc(52px + env(safe-area-inset-top));
		padding: env(safe-area-inset-top) calc(8px + env(safe-area-inset-right)) 0
			calc(8px + env(safe-area-inset-left));
		background: var(--header);
		border-bottom: 1px solid var(--border);
	}
	.node {
		max-width: 38%;
		padding: 4px 10px;
		border-radius: 999px;
		border: 1px solid color-mix(in srgb, var(--accent) 60%, transparent);
		background: color-mix(in srgb, var(--accent) 14%, transparent);
		font-size: var(--text-sm);
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
		flex: none;
	}
	.back {
		width: 44px;
		height: 44px;
		display: grid;
		place-items: center;
		border: 0;
		border-radius: 10px;
		background: none;
		color: var(--text);
		cursor: pointer;
	}
	b {
		min-width: 0;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.space {
		flex: 1;
	}
	.pages {
		position: absolute;
		right: 8px;
		top: calc(100% + 4px);
		min-width: 14rem;
		max-width: calc(100vw - 16px);
	}
</style>
