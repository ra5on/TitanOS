<script lang="ts">
	import { chartSegments, type MonitorSample } from '#lib/operations.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	let {
		samples,
		value,
		label,
		unit = '',
		maximum,
	}: {
		samples: MonitorSample[];
		value: (s: MonitorSample) => number | null;
		label: string;
		unit?: string;
		maximum?: number;
	} = $props();
	const segments = $derived(chartSegments(samples, value));
	const start = $derived(samples.length ? Date.parse(samples[0].at) : 0),
		end = $derived(samples.length ? Date.parse(samples.at(-1)!.at) : 1);
	const high = $derived(maximum ?? Math.max(1, ...segments.flatMap((s) => s.map((p) => p.value))));
	const points = (segment: { at: string; value: number }[]) =>
		segment
			.map(
				(p) =>
					`${40 + (690 * (Date.parse(p.at) - start)) / Math.max(1, end - start)},${165 - (140 * Math.max(0, Math.min(high, p.value))) / high}`,
			)
			.join(' ');
	const last = $derived(segments.at(-1)?.at(-1));
</script>

<section class="tile chart">
	<div class="head">
		<h2 class="tile-title">{label}</h2>
		<span class="mono"
			>{last
				? `${last.value.toLocaleString(undefined, { maximumFractionDigits: 2 })} ${unit}`
				: '—'}</span
		>
	</div>
	{#if segments.length}<p class="scale">
			{high.toLocaleString(undefined, { maximumFractionDigits: 1 })}
			{unit}
		</p>
		<svg viewBox="0 0 750 195" role="img" aria-label={label}
			><title>{label}</title><line x1="40" y1="165" x2="730" y2="165" /><line
				x1="40"
				y1="25"
				x2="730"
				y2="25"
			/>{#each segments as segment, i (i)}<polyline
					points={points(segment)}
				/>{#each segment.length === 1 ? segment : [] as p (p.at)}<circle
						cx={40 + (690 * (Date.parse(p.at) - start)) / Math.max(1, end - start)}
						cy={165 - (140 * p.value) / high}
						r="2"
					/>{/each}{/each}</svg
		>
		<div class="axis mono">
			<time datetime={samples[0].at}
				>{new Date(start).toISOString().slice(5, 16).replace('T', ' ')}</time
			><time datetime={samples.at(-1)!.at}
				>{new Date(end).toISOString().slice(5, 16).replace('T', ' ')}</time
			>
		</div>{:else}<p class="muted">{t('ops.noSamples')}</p>{/if}
</section>

<style>
	.chart {
		padding: var(--tile-pad);
		min-width: 0;
	}
	.head {
		display: flex;
		justify-content: space-between;
		gap: 10px;
		flex-wrap: wrap;
	}
	.head span {
		font-size: 0.85rem;
	}
	svg {
		width: 100%;
		height: auto;
		min-height: 130px;
		overflow: visible;
	}
	line {
		stroke: var(--border);
		stroke-width: 1;
	}
	polyline {
		fill: none;
		stroke: var(--accent);
		stroke-width: 2;
		vector-effect: non-scaling-stroke;
	}
	circle {
		fill: var(--accent);
	}
	.scale {
		font-size: 0.7rem;
		color: var(--muted);
		margin-bottom: -20px;
	}
	.axis {
		display: flex;
		justify-content: space-between;
		gap: 10px;
		font-size: 0.7rem;
		color: var(--muted);
	}
</style>
