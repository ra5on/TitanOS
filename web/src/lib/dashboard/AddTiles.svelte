<script lang="ts">
	import { t } from '#lib/i18n/index.svelte.ts';
	import Icon from '#lib/components/Icon.svelte';
	import {
		TILES,
		sourceId,
		duplicateId,
		type StaticTileId,
		type TileId,
		type TilePlacement,
	} from './tiles.ts';
	import type { WorkloadGroup } from './tile-info.ts';

	// The "Add a tile" side panel of the editor.
	let {
		hidden,
		shown,
		admin,
		workloadGroups,
		selection = $bindable(),
		onadd,
		onclose,
	}: {
		hidden: StaticTileId[];
		shown: TilePlacement[];
		admin: boolean;
		workloadGroups: WorkloadGroup[];
		selection: string;
		onadd: (id: TileId) => void;
		onclose: () => void;
	} = $props();
</script>

<aside class="tile add-tiles" aria-label={t('dashboard.hidden')}>
	<div class="add-head">
		<h2 class="tile-title">{t('dashboard.hidden')}</h2>
		<button
			class="tile-action"
			title={t('common.close')}
			aria-label={t('common.close')}
			onclick={onclose}><Icon name="close" size={16} /></button
		>
	</div>
	{#if hidden.length}<div class="tile-choices">
			{#each hidden as id}<button
					class="btn"
					disabled={shown.length >= 100}
					onclick={() => onadd(id)}><Icon name="plus" size={14} />{t(TILES[id].title)}</button
				>{/each}
		</div>{:else}<p class="muted">{t('dashboard.editor.allAdded')}</p>{/if}
	{#if admin}<label
			>{t('dashboard.workload')}<select class="field" bind:value={selection}
				><option value="">{t('dashboard.pickWorkload')}</option
				>{#each workloadGroups as group}<optgroup label={t(group.label)}
						>{#each group.choices as choice}<option value={choice.id}
								>{choice.name}{shown.some((p) => sourceId(p.id) === choice.id)
									? ` · ${t('dashboard.onDashboard')}`
									: ''}</option
							>{/each}</optgroup
					>{/each}</select
			></label
		><button
			class="btn"
			disabled={!selection || shown.length >= 100}
			onclick={() =>
				onadd(
					shown.some((p) => p.id === selection)
						? duplicateId(selection as TileId)
						: (selection as TileId),
				)}>{t('dashboard.addWorkload')}</button
		>{/if}
	<p class="muted">{t('dashboard.hiddenHint')}</p>
</aside>

<style>
	.add-tiles {
		position: sticky;
		top: var(--dashboard-sticky-top, 76px);
		width: 320px;
		flex: none;
		max-height: calc(100vh - var(--dashboard-sticky-top, 76px) - 19px);
		overflow: auto;
		box-sizing: border-box;
		padding: 18px;
		display: flex;
		flex-direction: column;
		gap: 15px;
		background: var(--surface);
	}
	.add-head {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 8px;
	}
	/* The same heading and close button as the tile heads on the dashboard. */
	.tile-title {
		flex: 1;
		min-width: 0;
		font-size: var(--tile-title-size, 13px);
		font-weight: var(--tile-title-weight, 500);
		font-family: var(--tile-title-font);
		color: var(--tile-title-color, var(--text));
		text-align: var(--tile-title-align, left);
		line-height: 1.3;
		letter-spacing: 0;
		text-transform: uppercase;
		overflow-wrap: anywhere;
	}
	.tile-action {
		border: 0;
		background: transparent;
		color: var(--muted);
		padding: 3px;
		cursor: pointer;
		display: flex;
		align-items: center;
		flex: none;
	}
	.tile-choices {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
	}
	.add-tiles label {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 10px;
	}
	.add-tiles select {
		width: auto;
		max-width: 100%;
	}
	.add-tiles p {
		font-size: 0.8rem;
	}
	@media (max-width: 760px) {
		.add-tiles {
			position: fixed;
			inset: auto 10px 12px;
			top: auto;
			width: auto;
			max-height: 75vh;
			z-index: 40;
		}
	}
</style>
