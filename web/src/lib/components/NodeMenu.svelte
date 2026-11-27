<script lang="ts">
	// Chooses the node of the cluster this tab manages (lib/cluster-node.ts):
	// in the desktop's top bar ("desk") or on the phone's home ("phone").
	// The phone home also shows the local hostname on a standalone NAS.
	import { onMount, tick } from 'svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { activeNode, switchNode } from '#lib/cluster-node.ts';
	import {
		clusterNodes,
		homeNode,
		loadClusterNodes,
		manageable,
		otherVersion,
	} from '#lib/cluster.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import { system } from '#lib/system.svelte.ts';
	import Icon from './Icon.svelte';

	let { variant }: { variant: 'desk' | 'phone' | 'hero' } = $props();

	const id = $props.id();
	let open = $state(false);
	let root = $state<HTMLElement>();

	// Switching nodes reloads the tab. Session metadata always belongs to the
	// local NAS, while /system follows the selected node.
	const nodeId = activeNode();
	const current = $derived(nodeId ? clusterNodes.nodes.find((n) => n.id === nodeId) : homeNode());
	const currentName = $derived(
		current?.name ||
			(!nodeId && session.data?.hostname) ||
			system.info?.hostname ||
			t('common.loading'),
	);
	const admin = $derived(['owner', 'admin'].includes(session.data?.user?.role ?? ''));
	let trigger: HTMLButtonElement;
	onMount(() => {
		if (!clusterNodes.loaded) void loadClusterNodes();
	});

	async function toggle() {
		open = !open;
		if (open) {
			void loadClusterNodes();
			await tick();
			root
				?.querySelector<HTMLElement>(
					'[aria-checked="true"]:not(:disabled), .menu button:not(:disabled), .menu a',
				)
				?.focus();
		}
	}

	function nodeState(node: (typeof clusterNodes.nodes)[number]): string {
		if (node.self) return t('cluster.thisNode');
		if (!node.online) return t('cluster.offline');
		if (otherVersion(node, clusterNodes.version))
			return t('cluster.versionShort', { version: node.version ?? '' });
		return t('cluster.online');
	}

	function choose(node: (typeof clusterNodes.nodes)[number]) {
		open = false;
		if (node.id !== current?.id) switchNode(node.self ? null : node.id);
	}

	function onWindowClick(event: MouseEvent) {
		if (open && !root?.contains(event.target as Node)) open = false;
	}
	function onKeydown(event: KeyboardEvent) {
		if (!open) return;
		if (event.key === 'Escape') {
			open = false;
			trigger.focus();
			return;
		}
		if (!root?.contains(event.target as Node)) return;
		const items = Array.from(
			root.querySelectorAll<HTMLElement>('.menu button:not(:disabled), .menu a'),
		);
		if (!items.length) return;
		let index = items.indexOf(document.activeElement as HTMLElement);
		if (event.key === 'ArrowDown') index = (index + 1) % items.length;
		else if (event.key === 'ArrowUp') index = (index + items.length - 1) % items.length;
		else if (event.key === 'Home') index = 0;
		else if (event.key === 'End') index = items.length - 1;
		else return;
		event.preventDefault();
		items[index].focus();
	}
</script>

<svelte:window onclick={onWindowClick} onkeydown={onKeydown} />

<div class="node-menu {variant}" bind:this={root}>
	<button
		type="button"
		class="trigger"
		aria-haspopup="menu"
		aria-expanded={open}
		aria-controls="{id}-menu"
		aria-label={t('cluster.switchCurrent', { name: currentName })}
		title={t('cluster.switchCurrent', { name: currentName })}
		bind:this={trigger}
		onclick={toggle}
	>
		{#if variant === 'desk'}
			<span class="dot" class:away={!current?.self && !current?.online} aria-hidden="true"></span>
			<span class="mono name">{currentName}</span>
			<Icon name="chevronDown" size={14} />
		{:else}
			<span class="current-name">{currentName}</span>
			<span class="switch-label"
				>{t('cluster.switchNas')} <Icon name="chevronDown" size={14} /></span
			>
		{/if}
	</button>
	{#if open}
		<div class="menu floating-menu" id="{id}-menu" role="menu" aria-label={t('cluster.switch')}>
			{#if !clusterNodes.loaded}
				<span class="single" role="status">{t('common.loading')}</span>
			{:else if clusterNodes.failed}
				<span class="single" role="status">{t('cluster.listUnavailable')}</span>
				<button type="button" role="menuitem" class="recover" onclick={() => loadClusterNodes()}
					>{t('cluster.refreshList')}</button
				>
				{#if nodeId}<button
						type="button"
						role="menuitem"
						class="recover"
						onclick={() => switchNode(null)}>{t('cluster.returnHere')}</button
					>{/if}
			{:else if !clusterNodes.nodes.some((node) => !node.self)}
				<span class="single">{t('cluster.noOtherNas')}</span>
			{/if}
			{#each clusterNodes.nodes as node (node.id)}
				{@const usable = manageable(node)}
				<button
					type="button"
					role="menuitemradio"
					aria-checked={node.id === current?.id}
					disabled={!usable || (clusterNodes.failed && !node.self)}
					onclick={() => choose(node)}
				>
					<span class="dot" class:away={!node.online} aria-hidden="true"></span>
					<span class="mono">{node.name}</span>
					<span class="muted small">{nodeState(node)}</span>
				</button>
			{/each}
			{#if admin}
				<hr />
				<a role="menuitem" href="/settings/cluster" onclick={() => (open = false)}
					><Icon name="gear" size={16} />{t('cluster.manageCluster')}</a
				>
			{/if}
		</div>
	{/if}
</div>

<style>
	.hero {
		color: white;
	}
	.single {
		padding: 10px;
		display: block;
	}
	.hero .trigger,
	.phone .trigger {
		background: var(--surface);
		color: var(--text);
		min-height: 48px;
		max-width: 100%;
		gap: 12px;
		padding: 4px 6px 4px 14px;
		border: 1px solid var(--nord-line, var(--border));
		border-radius: 16px;
		justify-content: space-between;
	}
	.hero .current-name,
	.phone .current-name {
		font-size: clamp(1.25rem, 5.6vw, 1.85rem);
		font-weight: 700;
		letter-spacing: -0.035em;
		line-height: 1.15;
		overflow-wrap: anywhere;
		min-width: 0;
		text-align: start;
	}
	.switch-label {
		display: inline-flex;
		gap: 5px;
		align-items: center;
		justify-content: center;
		flex: none;
		min-height: 36px;
		padding: 0 9px;
		border-radius: 11px;
		background: var(--chip);
		color: var(--text);
		font-size: 0.82rem;
		font-weight: 620;
		letter-spacing: normal;
	}
	.menu .recover {
		display: block;
	}
	.hero .menu {
		font-size: 1rem;
		font-weight: 400;
		text-shadow: none;
		left: 0;
		right: auto;
		top: 100%;
		color: var(--text);
		max-width: calc(100vw - 40px);
	}

	.node-menu {
		position: relative;
	}
	.trigger {
		display: flex;
		align-items: center;
		gap: var(--space-1-5);
		padding: 0;
		border: 1px solid transparent;
		border-radius: var(--radius-ctl);
		background: transparent;
		color: var(--text);
		font: inherit;
		cursor: pointer;
	}
	.trigger:hover,
	.trigger[aria-expanded='true'] {
		background: var(--chip);
	}
	.trigger:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	/* PLAN 62/64: in the top bar of the desktop, right after the menu, with
	   the node's name (the state as a dot and in the menu, not by colour alone). */
	.desk {
		flex: none;
	}
	.desk .trigger {
		height: var(--pb, 34px);
		padding: 0 var(--space-2) 0 var(--space-2-5);
		gap: 7px;
		border-color: color-mix(in srgb, var(--accent) 60%, transparent);
		background: color-mix(in srgb, var(--accent) 14%, transparent);
	}
	.desk .name {
		max-width: 140px;
		font-size: var(--text-base);
		font-weight: 600;
		overflow-wrap: anywhere;
	}
	.desk .menu {
		background: var(--surface);
		top: calc(100% + 10px);
		z-index: 9600;
	}
	.dot {
		flex: none;
		width: 8px;
		height: 8px;
		border-radius: 50%;
		background: var(--ok-text);
	}
	.dot.away {
		background: transparent;
		border: 1.5px solid var(--muted);
	}
	.small {
		font-size: var(--text-sm);
	}
	.menu {
		position: absolute;
		z-index: 40;
		top: calc(100% + 6px);
		left: 0;
		width: 280px;
		max-width: calc(100vw - 16px);
		display: flex;
		flex-direction: column;
		padding: var(--space-1-5);
	}
	.menu button,
	.menu a {
		display: grid;
		grid-template-columns: 10px 1fr auto;
		align-items: center;
		gap: var(--space-2-5);
		min-height: 40px;
		padding: var(--space-1-5) var(--space-2-5);
		border: 0;
		border-radius: var(--radius-small);
		background: transparent;
		color: var(--text);
		font: inherit;
		font-size: var(--text-md);
		text-align: left;
		text-decoration: none;
		cursor: pointer;
	}
	.menu a {
		grid-template-columns: 16px 1fr;
		color: var(--accent);
	}
	.menu button:hover:not(:disabled),
	.menu a:hover {
		background: var(--chip);
	}
	.menu button[aria-checked='true'] {
		background: color-mix(in srgb, var(--accent) 14%, transparent);
	}
	.menu button:disabled {
		color: var(--muted);
		cursor: default;
	}
	.menu button:focus-visible,
	.menu a:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
	.menu hr {
		width: 100%;
		margin: var(--space-1) 0;
		border: 0;
		border-top: 1px solid var(--border);
	}
	.phone .trigger {
		width: 100%;
	}
	.phone .current-name {
		font-size: 1rem;
	}
	.phone .menu {
		left: 0;
		right: 0;
		top: calc(100% + 4px);
		width: auto;
		max-width: none;
		background: var(--surface);
	}
	.phone .menu button,
	.phone .menu a,
	.hero .menu button,
	.hero .menu a {
		min-height: 44px;
	}
</style>
