<script lang="ts">
	import { tileOptions, type TileOptions } from '../options.ts';
	let { options = tileOptions(null, 'temperatures') }: { options?: TileOptions } = $props();
	import { t } from '#lib/i18n/index.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import { system } from '#lib/system.svelte.ts';
	import { tileTemperatures } from '../temperatures.ts';
	const selected = $derived(
		tileTemperatures(session.dashboard, options.items, system.info?.temperatures ?? []),
	);
	let showAll = $state(false);
</script>

{#if selected.length === 0}
	<p class="muted">
		{t(options.items === null ? 'temperatures.choose' : 'temperatures.chooseTile')}
	</p>
{:else}
	<dl class:grid={options.list === 'grid'}>
		{#each selected.slice(0, showAll ? selected.length : options.rows) as choice (choice.id)}
			{@const sensor = choice.sensor}
			{@const hot =
				sensor?.celsius != null && sensor.criticalC != null && sensor.celsius >= sensor.criticalC}
			<div class:hot>
				<dt>
					<span>{choice.label}</span>{#if options.details}<span class="muted chip-name"
							>{choice.chip}{sensor ? ` · ${sensor.device.split('/').at(-1)}` : ''}</span
						>{/if}
				</dt>
				<dd class="mono" data-edit-part="value">
					{sensor?.celsius == null ? '–' : `${sensor.celsius.toFixed(1)} °C`}{#if hot}<span
							class="critical">{t('temperatures.critical')}</span
						>{/if}
				</dd>
			</div>
		{/each}
	</dl>
	{#if selected.length > options.rows}<p class="muted note">
			{t('dashboard.visibleItems', {
				count: showAll ? selected.length : options.rows,
				total: selected.length,
			})}{#if !showAll}
				<button class="link" onclick={() => (showAll = true)}>{t('dashboard.showAll')}</button>{/if}
		</p>{/if}
	{#if selected.some((choice) => choice.sensor?.celsius == null)}<p class="muted note">
			{t('temperatures.unavailable')}
		</p>{/if}
{/if}
<a href="/settings/temperatures">{t('temperatures.configure')}</a>

<style>
	dl {
		margin: 0;
		display: flex;
		flex-direction: column;
		gap: 12px;
	}
	dl.grid {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(min(130px, 100%), 1fr));
		gap: 16px;
	}
	dl.grid > div {
		flex-direction: column;
		gap: 4px;
	}
	dl.grid dd {
		text-align: left;
	}
	dl > div {
		display: flex;
		justify-content: space-between;
		gap: 16px;
	}
	dt {
		display: flex;
		flex-direction: column;
		gap: 2px;
		min-width: 0;
		overflow-wrap: anywhere;
		font-size: 0.875rem;
	}
	dd {
		font-size: var(--tile-value-size, 16px);
		font-family: var(--tile-value-font);
		font-weight: var(--tile-value-weight);
		margin: 0;
		white-space: nowrap;
		text-align: right;
	}
	.chip-name,
	.note,
	a,
	.critical {
		font-size: 0.75rem;
	}
	.critical {
		display: block;
	}
	.hot dd {
		font-size: var(--tile-value-size, 16px);
		font-family: var(--tile-value-font);
		font-weight: var(--tile-value-weight);
		color: var(--warn-text);
	}
	p {
		margin: 0;
	}
</style>
