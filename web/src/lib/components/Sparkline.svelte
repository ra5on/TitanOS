<script lang="ts">
	// A small line of the last values (0–100), newest on the right. Only
	// shape, no axes: the number next to it says what it is.
	type Props = { values: number[]; points?: number; label: string };
	let { values, points = 40, label }: Props = $props();

	const line = $derived.by(() => {
		const recent = values.slice(-points);
		if (recent.length < 2) return '';
		const step = 100 / (points - 1);
		const start = (points - recent.length) * step;
		return recent.map((value, index) => `${(start + index * step).toFixed(1)},${(22 - Math.min(100, Math.max(0, value)) * 0.2).toFixed(1)}`).join(' ');
	});
</script>

<svg class="sparkline" viewBox="0 0 100 24" preserveAspectRatio="none" role="img" aria-label={label}>
	{#if line}<polyline points={line} />{/if}
</svg>

<style>
	.sparkline {
		display: block;
		width: 100%;
		height: 24px;
	}
	polyline {
		fill: none;
		stroke: var(--accent);
		stroke-width: 1.5;
		stroke-linejoin: round;
		vector-effect: non-scaling-stroke;
	}
</style>
