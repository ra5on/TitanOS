<script lang="ts">
	// Chooses the node of the cluster this tab manages (lib/cluster-node.ts):
	// at the symbol in the header ("logo"), as a card at the top of the
	// sidebar ("card") or as a badge in the folded sidebar ("badge"). Shown
	// only in a cluster; nodes that are away or run another version cannot
	// be chosen.
	import { t } from '#lib/i18n/index.svelte.ts';
	import { activeNode, switchNode } from '#lib/cluster-node.ts';
	import { clusterNodes, manageable } from '#lib/cluster.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import Icon from './Icon.svelte';
	import Logo from './Logo.svelte';

	let { variant }: { variant: 'logo' | 'card' | 'badge' } = $props();

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
		if (node.version !== clusterNodes.version) return t('cluster.otherVersionShort');
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
		title={variant === 'card'
			? undefined
			: t('cluster.switchCurrent', { name: current?.name ?? '' })}
		onclick={() => (open = !open)}
	>
		{#if variant === 'logo'}
			<Logo size={28} /><Icon name="chevronDown" size={14} />
		{:else if variant === 'card'}
			<span class="dot" aria-hidden="true"></span>
			<span class="lines"
				><span class="mono name">{current?.name}</span><span class="muted small"
					>{t('cluster.nodeCount', { count: clusterNodes.nodes.length })}</span
				></span
			>
			<Icon name="chevronDown" size={14} />
		{:else}
			<span class="mono initials">{initials}</span><span class="dot" aria-hidden="true"></span>
		{/if}
	</button>
	{#if open}
		<div class="menu floating-menu" id="{id}-menu" role="menu" aria-label={t('cluster.switch')}>
			{#each clusterNodes.nodes as node (node.id)}
				{@const usable = manageable(node, clusterNodes.version)}
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
		gap: 6px;
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
	.logo .trigger {
		height: 44px;
		padding: 0 6px 0 4px;
		margin-left: -4px;
		color: var(--muted);
	}
	.card .trigger {
		width: 100%;
		gap: 10px;
		padding: 8px 10px;
		border-color: var(--border);
		background: var(--chip);
		text-align: left;
	}
	.card .lines {
		flex: 1;
		min-width: 0;
		display: flex;
		flex-direction: column;
	}
	.card .name {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
		font-size: var(--text-base);
	}
	.badge .trigger {
		position: relative;
		justify-content: center;
		width: 44px;
		height: 44px;
		border-color: var(--accent);
		background: color-mix(in srgb, var(--accent) 12%, transparent);
	}
	.badge .initials {
		font-size: var(--text-base);
		font-weight: 600;
	}
	.badge .trigger > .dot {
		position: absolute;
		right: -3px;
		bottom: -3px;
		width: 11px;
		height: 11px;
		border: 2px solid var(--header);
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
		padding: 6px;
	}
	/* Beside the page the glass would let the tiles shine through. */
	.card .menu,
	.badge .menu {
		background: var(--surface);
	}
	.card .menu {
		width: 100%;
	}
	/* Narrow: the state goes under the name. */
	.card .menu button {
		grid-template-columns: 10px 1fr;
		row-gap: 0;
	}
	.card .menu button .small {
		grid-column: 2;
	}
	.card .menu button .mono {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.badge .menu {
		top: 0;
		left: calc(100% + 10px);
	}
	.menu button,
	.menu a {
		display: grid;
		grid-template-columns: 10px 1fr auto;
		align-items: center;
		gap: 10px;
		min-height: 40px;
		padding: 6px 10px;
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
		margin: 4px 0;
		border: 0;
		border-top: 1px solid var(--border);
	}
	@media (max-width: 760px) {
		.logo .menu {
			position: fixed;
			top: 68px;
			left: 8px;
			right: 8px;
			width: auto;
		}
	}
</style>
