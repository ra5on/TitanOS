<script lang="ts">
	// The CPU history as a small line chart, scaled to its own peak.
	let { values }: { values: number[] } = $props();

	const points = $derived.by(() => {
		if (values.length < 2) return '';
		const peak = Math.max(...values, 1);
		return values.map((value, index) => `${((index / (values.length - 1)) * 100).toFixed(1)},${(22 - (value / peak) * 20).toFixed(1)}`).join(' ');
	});
</script>

<svg viewBox="0 0 100 24" preserveAspectRatio="none" aria-hidden="true">
	<line x1="0" y1="23" x2="100" y2="23" />
	{#if points}<polyline {points} />{/if}
</svg>

<style>
	svg {
		display: block;
		width: 100%;
		height: 100%;
	}
	line {
		stroke: var(--border);
		stroke-width: 1;
		vector-effect: non-scaling-stroke;
	}
	polyline {
		fill: none;
		stroke: var(--accent);
		stroke-width: 1.5;
		stroke-linejoin: round;
		vector-effect: non-scaling-stroke;
	}
</style>
