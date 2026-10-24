<script lang="ts">
	import { t } from '#lib/i18n/index.svelte.ts';
	import type { TileOptions } from '../options.ts';
	import History from './History.svelte';
	let {
		value,
		label,
		unit = '%',
		options,
		history = [],
	}: {
		value: number | null;
		label: string;
		unit?: string;
		options: TileOptions;
		history?: (number | null)[];
	} = $props();
	const mode = $derived(options.display === 'auto' ? 'bar' : options.display);
	const bounded = $derived(value === null ? 0 : Math.max(0, Math.min(100, value)));
</script>

<div class="metric" class:ring={mode === 'ring'} style:text-align={options.align}>
	{#if mode === 'ring'}<svg class="ring-svg" viewBox="0 0 100 100" aria-hidden="true"
			><circle class="track" cx="50" cy="50" r="43" />{#if value !== null}<circle
					class="level"
					cx="50"
					cy="50"
					r="43"
					pathLength="100"
					stroke-dasharray="{bounded} 100"
				/>{/if}</svg
		>{/if}
	<div class="reading" data-edit-part="value">
		<span>{value === null ? '–' : Math.round(value)}</span><small>{unit}</small>
	</div>
	<span class="label" data-edit-part="labels">{label}</span>
</div>
{#if mode === 'bar'}<div
		class="bar"
		role="progressbar"
		aria-valuemin="0"
		aria-valuemax="100"
		aria-valuenow={value === null ? undefined : bounded}
		aria-label={label}
	>
		<span style:width="{bounded}%"></span>
	</div>{/if}
{#if mode === 'history'}<History values={history} />{/if}

<style>
	.metric {
		position: relative;
		display: flex;
		flex-direction: column;
		gap: 4px;
		min-width: 0;
	}
	.reading {
		font: var(--tile-value-weight, 400) var(--tile-value-size, 36px)
			var(--tile-value-font, 'IBM Plex Mono');
		color: var(--tile-value-color, var(--text));
		line-height: 1.15;
		overflow-wrap: anywhere;
	}
	.reading small {
		font-size: 0.45em;
		color: var(--tile-label-color, var(--muted));
		margin-left: 5px;
	}
	.label {
		font-size: var(--tile-label-size, 12px);
		color: var(--tile-label-color, var(--muted));
	}
	.metric.ring {
		width: 150px;
		height: 150px;
		max-width: 100%;
		margin: auto;
		justify-content: center;
		text-align: center !important;
		flex: none;
	}
	.ring-svg {
		position: absolute;
		inset: 0;
		width: 100%;
		height: 100%;
		transform: rotate(-90deg);
		fill: none;
		stroke-width: 5;
		pointer-events: none;
	}
	.track {
		stroke: var(--track);
	}
	.level {
		stroke: var(--accent);
		stroke-linecap: round;
	}
	.ring .reading {
		font-size: min(var(--tile-value-size, 36px), 60px);
	}
	.ring .label {
		font-size: min(var(--tile-label-size, 12px), 14px);
	}
	.bar {
		height: 6px;
		min-height: 6px;
		flex: none;
		background: var(--track);
		border-radius: var(--radius-bar);
		overflow: hidden;
	}
	.bar span {
		display: block;
		background: var(--accent);
		height: 100%;
	}
</style>
