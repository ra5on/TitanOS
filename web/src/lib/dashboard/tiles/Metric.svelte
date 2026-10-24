<script lang="ts">
	import type { TileOptions } from '../options.ts';
	import History from './History.svelte';
	let {
		value,
		label,
		unit = '%',
		options,
		history = [],
		compact = false,
		showCompactLabel = false,
	}: {
		value: number | null;
		label: string;
		unit?: string;
		options: TileOptions;
		history?: (number | null)[];
		compact?: boolean;
		showCompactLabel?: boolean;
	} = $props();
	const mode = $derived(options.display === 'auto' ? 'bar' : options.display);
	const known = $derived(value !== null && Number.isFinite(value));
	const bounded = $derived(!known ? 0 : Math.max(0, Math.min(100, value!)));
</script>

<div
	class="metric"
	class:ring={mode === 'ring'}
	class:compact={compact && mode === 'bar'}
	class:compact-label={showCompactLabel}
>
	{#if mode === 'ring'}<svg class="ring-svg" viewBox="0 0 100 100" aria-hidden="true"
			><circle class="track" cx="50" cy="50" r="43" />{#if known}<circle
					class="level metric-ring-level"
					cx="50"
					cy="50"
					r="43"
					pathLength="100"
					stroke-dasharray="{bounded} 100"
				/>{/if}</svg
		>{/if}
	<div class="reading" data-edit-part="value">
		<span>{!known ? '–' : Math.round(value!)}</span><small>{unit}</small>
	</div>
	<span class="label" data-edit-part="labels">{label}</span>
	{#if compact && mode === 'bar'}<div
			class="bar"
			role="progressbar"
			aria-label={label}
			aria-valuemin="0"
			aria-valuemax="100"
			aria-valuenow={!known ? undefined : bounded}
		>
			<span class="metric-level" class:unknown={!known} style:width="{bounded}%"></span>
		</div>{/if}
</div>
{#if mode === 'bar' && !compact}<div
		class="bar"
		role="progressbar"
		aria-valuemin="0"
		aria-valuemax="100"
		aria-valuenow={!known ? undefined : bounded}
		aria-label={label}
	>
		<span class="metric-level" class:unknown={!known} style:width="{bounded}%"></span>
	</div>{/if}
{#if mode === 'history'}<History values={history} />{/if}

<style>
	.metric {
		position: relative;
		display: flex;
		align-items: baseline;
		justify-content: space-between;
		gap: 8px;
		min-width: 0;
		width: 100%;
		container-type: inline-size;
	}
	.reading {
		display: inline-flex;
		align-items: baseline;
		gap: 0.18em;
		flex: none;
		order: 2;
		white-space: nowrap;
		font: var(--tile-value-weight, 400) min(var(--tile-value-size, 18px), 32cqi)/1.2
			var(--tile-value-font, 'IBM Plex Mono');
		color: var(--tile-value-color, var(--text));
	}
	.reading small {
		font-size: 0.65em;
		color: var(--tile-label-color, var(--muted));
	}
	.label {
		font-size: var(--tile-label-size, 12px);
		color: var(--tile-label-color, var(--muted));
		overflow-wrap: anywhere;
		min-width: 0;
	}
	.metric.ring {
		width: min(
			100%,
			max(
				150px,
				calc(var(--tile-value-size, 18px) * 3.6),
				calc(var(--tile-label-size, 12px) * 11.2)
			)
		);
		aspect-ratio: 1;
		margin: auto;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: 4px;
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
		order: 0;
		font-size: min(var(--tile-value-size, 18px), 28cqi);
		text-align: center;
	}
	.ring .label {
		max-width: 70%;
		text-align: center;
		font-size: min(var(--tile-label-size, 12px), 9cqi);
	}
	.compact {
		align-items: center;
	}
	.compact .bar {
		flex: 1;
		min-width: 16px;
		margin-top: 0;
	}
	.compact .reading {
		font-size: min(var(--tile-value-size, 18px), 27cqi);
	}
	.compact-label.compact .label {
		display: block;
		max-width: 35%;
		flex: none;
	}
	.compact .label {
		display: none;
	}
	.bar {
		height: 6px;
		min-height: 6px;
		flex: none;
		background: var(--track);
		border-radius: var(--radius-bar);
		overflow: hidden;
		margin-top: 6px;
	}
	.bar span {
		display: block;
		background: var(--accent);
		height: 100%;
	}
</style>
