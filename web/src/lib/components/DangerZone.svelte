<script lang="ts">
	// "Danger zone": the destroying actions of a detail or edit view (delete,
	// remove, leave …), gathered at its end and set apart from the normal
	// actions. The buttons inside open the usual confirmation dialog. Inside a
	// card it is a section with a line above; `tile` makes it a tile of its
	// own where the view is a column of tiles.
	import type { Snippet } from 'svelte';
	import { t } from '#lib/i18n/index.svelte.ts';

	type Props = { hint?: string; tile?: boolean; children: Snippet };
	let { hint, tile = false, children }: Props = $props();

	const id = $props.id();
</script>

<section class="danger-zone" class:tile aria-labelledby="{id}-title">
	<div>
		<strong id="{id}-title">{t('ui.dangerZone')}</strong>
		{#if hint}<p class="muted">{hint}</p>{/if}
	</div>
	<div class="danger-actions">{@render children()}</div>
</section>

<style>
	.danger-zone {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-2) var(--space-4);
		margin-top: var(--space-1-5);
		padding-top: var(--space-3);
		border-top: 1px solid var(--border);
	}
	.danger-zone.tile {
		margin: 0;
		padding: calc(var(--tile-pad) + 2px);
		border-top: 1px solid var(--panel-border);
	}
	.danger-zone > div:first-child {
		flex: 1 1 16rem;
		min-width: 0;
	}
	strong {
		color: var(--error-text);
		font-size: var(--text-base);
		letter-spacing: 0.04em;
		text-transform: uppercase;
	}
	p {
		margin: var(--space-1) 0 0;
		font-size: var(--text-base);
	}
	.danger-actions {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2);
	}
</style>
