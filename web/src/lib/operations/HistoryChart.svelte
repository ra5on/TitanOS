<script lang="ts">
	import { chartSegments, type MonitorSample } from '#lib/operations.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { formatDateTime } from '#lib/format.ts';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
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
	const xOf = (time: number) => 40 + (690 * (time - start)) / Math.max(1, end - start);
	const yOf = (reading: number) => 165 - (140 * Math.max(0, Math.min(high, reading))) / high;
	const points = (segment: { at: string; value: number }[]) =>
		segment.map((p) => `${xOf(Date.parse(p.at))},${yOf(p.value)}`).join(' ');
	const last = $derived(segments.at(-1)?.at(-1));
	const readings = $derived(
		segments.flat().map((point) => ({ ...point, time: Date.parse(point.at) })),
	);
	let index = $state<number | null>(null);
	const reading = $derived(index === null ? undefined : readings[index]);
	const activeReading = $derived(reading ?? readings.at(-1));
	const readingValue = (amount: number) =>
		`${amount.toLocaleString(undefined, { maximumFractionDigits: 2 })} ${unit}`.trim();

	function inspect(event: PointerEvent & { currentTarget: SVGSVGElement }) {
		if (event.pointerType === 'touch' && event.type === 'pointermove') return;
		const matrix = event.currentTarget.getScreenCTM();
		if (!matrix || !readings.length) return;
		const x = new DOMPoint(event.clientX, event.clientY).matrixTransform(matrix.inverse()).x;
		const time = start + ((x - 40) / 690) * (end - start);
		let left = 0;
		let right = readings.length - 1;
		while (left < right) {
			const middle = (left + right) >>> 1;
			if (time > (readings[middle].time + readings[middle + 1].time) / 2) left = middle + 1;
			else right = middle;
		}
		index = left;
	}

	function inspectKey(event: KeyboardEvent) {
		if (event.key === 'Escape') index = null;
		if (!['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown', 'Home', 'End'].includes(event.key))
			return;
		event.preventDefault();
		const last = readings.length - 1;
		if (event.key === 'Home') index = 0;
		else if (event.key === 'End') index = last;
		else {
			const step = event.key === 'ArrowLeft' || event.key === 'ArrowDown' ? -1 : 1;
			index = Math.max(0, Math.min(last, (index ?? last) + step));
		}
	}
</script>

<!-- PLAN 78-5: the name and the latest value are a plain heading; the chart
     keeps its own surface below it. -->
<SettingGroup title={label}>
	{#snippet tools()}<span class="mono last"
			>{last
				? `${last.value.toLocaleString(undefined, { maximumFractionDigits: 2 })} ${unit}`
				: '—'}</span
		>{/snippet}
	<div class="tile chart">
		{#if segments.length}<p class="scale">
				{high.toLocaleString(undefined, { maximumFractionDigits: 1 })}
				{unit}
			</p>
			<svg
				viewBox="0 0 750 195"
				role="slider"
				aria-label={label}
				aria-valuemin={1}
				aria-valuemax={readings.length}
				aria-valuenow={(index ?? readings.length - 1) + 1}
				aria-valuetext={activeReading
					? `${formatDateTime(new Date(activeReading.at))}, ${readingValue(activeReading.value)}`
					: undefined}
				tabindex="0"
				onpointermove={inspect}
				onpointerdown={inspect}
				onpointerleave={(event) => {
					if (event.pointerType !== 'touch') index = null;
				}}
				onfocus={() => (index ??= readings.length - 1)}
				onblur={() => (index = null)}
				onkeydown={inspectKey}
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
						/>{/each}{/each}
				{#if reading}
					<line
						class="reading-line"
						x1={xOf(reading.time)}
						x2={xOf(reading.time)}
						y1="25"
						y2="165"
					/>
					<circle cx={xOf(reading.time)} cy={yOf(reading.value)} r="4" />
				{/if}
			</svg>
			<div class="axis mono">
				<time datetime={samples[0].at}
					>{new Date(start).toISOString().slice(5, 16).replace('T', ' ')}</time
				><time datetime={samples.at(-1)!.at}
					>{new Date(end).toISOString().slice(5, 16).replace('T', ' ')}</time
				>
				{#if reading}
					<div class="reading">
						<time datetime={reading.at}>{formatDateTime(new Date(reading.at))}</time>
						<strong>{readingValue(reading.value)}</strong>
					</div>
				{/if}
			</div>{:else}<p class="muted">{t('ops.noSamples')}</p>{/if}
	</div>
</SettingGroup>

<style>
	.chart {
		position: relative;
		padding: var(--tile-pad);
		min-width: 0;
	}
	.reading {
		position: absolute;
		top: 8px;
		right: 8px;
		display: flex;
		flex-wrap: wrap;
		gap: 3px 8px;
		max-width: calc(100% - 16px);
		padding: 4px 7px;
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		background: var(--surface);
		font-size: var(--text-sm);
		pointer-events: none;
	}
	.reading-line {
		stroke: var(--accent);
		stroke-dasharray: 4 4;
	}
	.last {
		font-size: var(--text-base);
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
		font-size: var(--text-xs);
		color: var(--muted);
		margin-bottom: -20px;
	}
	.axis {
		display: flex;
		justify-content: space-between;
		gap: var(--space-2-5);
		font-size: var(--text-xs);
		color: var(--muted);
	}
</style>
