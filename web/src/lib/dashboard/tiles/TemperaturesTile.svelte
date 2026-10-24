<script lang="ts">
	import { tileOptions, type TileOptions } from '../options.ts';
	let { options = tileOptions(null, 'temperatures') }: { options?: TileOptions } = $props();
	import { t } from '#lib/i18n/index.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import { system } from '#lib/system.svelte.ts';
	import { temperatureSelection } from '../temperatures.ts';
	const selected = $derived(temperatureSelection(session.dashboard));
</script>

{#if selected.length === 0}
	<p class="muted">{t('temperatures.choose')}</p>
{:else}
	<dl>
		{#each selected as choice (choice.id)}
			{@const sensor = system.info?.temperatures?.find((sensor) => sensor.id === choice.id)}
			{@const hot =
				sensor?.celsius != null && sensor.criticalC != null && sensor.celsius >= sensor.criticalC}
			<div class:hot>
				<dt>
					<span>{sensor?.label ?? choice.label}</span>{#if options.details}<span
							class="muted chip-name"
							>{sensor?.chip ?? choice.chip}{sensor
								? ` · ${sensor.device.split('/').at(-1)}`
								: ''}</span
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
	{#if selected.some((choice) => system.info?.temperatures?.find((sensor) => sensor.id === choice.id)?.celsius == null)}<p
			class="muted note"
		>
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
