<script lang="ts">
	import { untrack } from 'svelte';
	import { historySlots, historyPath, historyFrame } from '#lib/metric-history.ts';
	let {
		values,
		capacity = 90,
		height = 64,
		adaptive = false,
		label,
	}: {
		values: readonly (number | null)[];
		capacity?: number;
		height?: number;
		adaptive?: boolean;
		label?: string;
	} = $props();
	let svg = $state<SVGSVGElement>();
	let displayed = $state<(number | null)[]>([]),
		peak = $state(100);
	let signature = '';
	let initialized = false;
	let heldPeak = 0;
	const path = $derived(historyPath(displayed, peak, height));
	$effect(() => {
		const target = historySlots(values, capacity);
		const key = JSON.stringify(target);
		const element = svg;
		if (!element || key === signature) return;
		signature = key;
		return untrack(() => {
			const targetPeak = adaptive
				? Math.max(1, ...target.filter((v): v is number => v !== null))
				: 100;
			const nextPeak = adaptive ? Math.max(heldPeak, targetPeak) : 100;
			heldPeak = nextPeak;
			const fromPeak = peak;
			const from = displayed;
			const media = matchMedia('(prefers-reduced-motion: reduce)');
			const duration = parseFloat(getComputedStyle(element).getPropertyValue('--metric-duration'));
			if (
				!initialized ||
				document.hidden ||
				media.matches ||
				!duration ||
				from.length !== target.length
			) {
				displayed = target;
				peak = nextPeak;
				initialized = true;
				return;
			}
			const start = performance.now();
			let frame = 0,
				stopped = false;
			const stop = () => {
				if (stopped) return;
				stopped = true;
				cancelAnimationFrame(frame);
				displayed = target;
				peak = nextPeak;
			};
			const step = (now: number) => {
				if (stopped) return;
				if (document.hidden || media.matches) {
					stop();
					return;
				}
				const t = Math.min(1, (now - start) / duration);
				const eased = 1 - Math.pow(1 - t, 3);
				displayed = historyFrame(from, target, eased);
				peak = fromPeak + (nextPeak - fromPeak) * eased;
				if (t < 1) frame = requestAnimationFrame(step);
			};
			frame = requestAnimationFrame(step);
			document.addEventListener('visibilitychange', stop);
			media.addEventListener('change', stop);
			return () => {
				cancelAnimationFrame(frame);
				document.removeEventListener('visibilitychange', stop);
				media.removeEventListener('change', stop);
			};
		});
	});
</script>

<svg
	bind:this={svg}
	viewBox={`0 0 240 ${height}`}
	preserveAspectRatio="none"
	role={label ? 'img' : undefined}
	aria-label={label}
	aria-hidden={label ? undefined : 'true'}
	style:height={`${height}px`}
	><path class="guide" d={`M0 4H240M0 ${height / 2}H240M0 ${height - 3}H240`} /><path
		d={path}
	/></svg
>

<style>
	svg {
		width: 100%;
		min-height: 0;
		display: block;
		flex: none;
		fill: none;
	}
	path {
		stroke: var(--accent);
		stroke-width: 1.7;
		vector-effect: non-scaling-stroke;
		stroke-linejoin: round;
	}
	.guide {
		stroke: var(--border);
		stroke-width: 1;
	}
</style>
