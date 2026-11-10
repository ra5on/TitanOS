<script lang="ts">
	// Chooses the node of the cluster this tab manages (lib/cluster-node.ts):
	// in the desktop's top bar ("desk") or on the phone's home ("phone").
	// Shown only in a cluster; nodes that are away cannot be chosen, nodes of
	// another version can.
	import { t } from '#lib/i18n/index.svelte.ts';
	import { activeNode, switchNode } from '#lib/cluster-node.ts';
	import { clusterNodes, manageable, otherVersion } from '#lib/cluster.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import Icon from './Icon.svelte';

	let { variant }: { variant: 'desk' | 'phone' } = $props();

	const id = $props.id();
	let open = $state(false);
	let root = $state<HTMLElement>();

	const current = $derived(
		clusterNodes.nodes.find((n) => n.id === activeNode()) ?? clusterNodes.nodes.find((n) => n.self),
	);
	const admin = $derived(['owner', 'admin'].includes(session.data?.user?.role ?? ''));
	const initials = $derived(
		(current?.name ?? '')
			.split(/[-_.\s]+/)
			.filter(Boolean)
			.slice(0, 2)
			.map((part) => part.charAt(0).toUpperCase())
			.join('') || '?',
	);

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
		if (open && event.key === 'Escape') open = false;
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
		aria-label={t('cluster.switchCurrent', { name: current?.name ?? '' })}
		title={t('cluster.switchCurrent', { name: current?.name ?? '' })}
		onclick={() => (open = !open)}
	>
		{#if variant === 'desk'}
			<span class="dot" class:away={!current?.self && !current?.online} aria-hidden="true"></span>
			<span class="mono name">{current?.name}</span>
			<Icon name="chevronDown" size={14} />
		{:else}
			<span class="mono initials">{initials}</span><span class="dot" aria-hidden="true"></span>
		{/if}
	</button>
	{#if open}
		<div class="menu floating-menu" id="{id}-menu" role="menu" aria-label={t('cluster.switch')}>
			{#each clusterNodes.nodes as node (node.id)}
				{@const usable = manageable(node)}
				<button
					type="button"
					role="menuitemradio"
					aria-checked={node.id === current?.id}
					disabled={!usable}
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
	/* PLAN 64: on the phone's home beside the search; the menu spans the screen. */
	.phone .trigger {
		position: relative;
		justify-content: center;
		width: 44px;
		height: 44px;
		border-radius: 14px;
		border-color: color-mix(in srgb, var(--accent) 60%, transparent);
		background: color-mix(in srgb, var(--accent) 16%, transparent);
	}
	.phone .initials {
		font-size: var(--text-base);
		font-weight: 700;
	}
	.phone .trigger > .dot {
		position: absolute;
		right: -3px;
		bottom: -3px;
		width: 11px;
		height: 11px;
		border: 2px solid var(--surface);
	}
	.phone .menu {
		position: fixed;
		top: calc(64px + env(safe-area-inset-top));
		left: 8px;
		right: 8px;
		width: auto;
		max-width: none;
		z-index: 60;
		background: var(--surface);
	}
	.phone .menu button,
	.phone .menu a {
		min-height: 44px;
	}
</style>
