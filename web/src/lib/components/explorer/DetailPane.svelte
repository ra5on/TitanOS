<script lang="ts">
	// PLAN 75-19 (style W4): the details of the selected row at the right of
	// a list – a large icon, the name, a line below, the main actions and
	// label/value pairs. On narrow windows it is not shown; the list opens
	// the entry's own page instead.
	import type { Snippet } from 'svelte';
	import Icon from '#lib/components/Icon.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';

	type Props = {
		title: string;
		sub?: string;
		icon?: Snippet;
		actions?: Snippet;
		children?: Snippet;
		/** label → value; empty values are left out. */
		facts?: [string, string][];
		onclose?: () => void;
	};
	let { title, sub, icon, actions, children, facts = [], onclose }: Props = $props();
</script>

<aside class="detail-pane" aria-label={title}>
	{#if onclose}<button
			type="button"
			class="btn icon-btn small close"
			aria-label={t('common.close')}
			title={t('common.close')}
			onclick={onclose}><Icon name="close" size={14} /></button
		>{/if}
	{#if icon}<div class="big">{@render icon()}</div>{/if}
	<h3>{title}</h3>
	{#if sub}<p class="sub muted">{sub}</p>{/if}
	{#if actions}<div class="acts">{@render actions()}</div>{/if}
	{#if facts.some(([, v]) => v)}
		<dl>
			{#each facts.filter(([, v]) => v) as [label, value] (label)}
				<dt>{label}</dt>
				<dd>{value}</dd>
			{/each}
		</dl>
	{/if}
	{#if children}{@render children()}{/if}
</aside>

<style>
	.detail-pane {
		align-self: start;
		position: sticky;
		top: var(--space-2);
		min-width: 0;
		padding: var(--space-4);
		border-left: 1px solid var(--border);
	}
	.close {
		position: absolute;
		top: var(--space-2);
		right: var(--space-2);
	}
	.big :global(.app-icon),
	.big :global(svg) {
		width: 52px;
		height: 52px;
	}
	h3 {
		margin: var(--space-2-5) 0 2px;
		font-size: var(--text-lg);
		overflow-wrap: anywhere;
	}
	.sub {
		margin: 0;
		font-size: var(--text-base);
	}
	.acts {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-1-5);
		margin-top: var(--space-3);
	}
	dl {
		display: grid;
		grid-template-columns: minmax(90px, auto) minmax(0, 1fr);
		column-gap: var(--space-3);
		row-gap: var(--space-2);
		margin: var(--space-4) 0 0;
		font-size: var(--text-base);
	}
	dt {
		color: var(--muted);
	}
	dd {
		margin: 0;
		overflow-wrap: anywhere;
	}
</style>
