<script lang="ts">
	import { motifPoints, type Motif } from './guide.ts';
	let {
		motif = '',
		calm = false,
		paused = false,
	}: { motif?: Motif; calm?: boolean; paused?: boolean } = $props();
	let figure = $state<HTMLElement>();
	let elapsed = 0;
	// Each dot keeps its identity across the orbit, the outline and the handoff.
	// The browser interpolates transform keyframes. There is no JS frame loop.
	function position(index: number, seconds: number, shape: Motif) {
		const angle = seconds * Math.PI + (index * 2 * Math.PI) / 3;
		const orbit = { x: Math.cos(angle) * 0.64, y: Math.sin(angle) * 0.35 };
		if (!shape) return orbit;
		const p = motifPoints(shape, seconds)[index];
		const blend = Math.min(1, Math.max(0, (1 - Math.cos((seconds * Math.PI) / 2)) * 0.68));
		return { x: orbit.x + (p.x - orbit.x) * blend, y: orbit.y + (p.y - orbit.y) * blend };
	}
	$effect(() => {
		const el = figure,
			shape = motif,
			still = calm,
			stopped = paused;
		if (!el) return;
		const elements = [...el.querySelectorAll<HTMLElement>('i')];
		const start = performance.now(),
			offset = elapsed;
		const transform = (i: number, seconds: number) => {
			const { x, y } = position(i, seconds, shape);
			return 'translate3d(' + x * 235 + '%,' + y * 235 + '%,0)';
		};
		if (still || stopped || !el.animate) {
			elements.forEach(
				(dot, i) =>
					(dot.style.transform = stopped
						? transform(i, offset)
						: 'translateX(' + (i - 1) * 140 + '%)'),
			);
			return;
		}
		const duration = 4000;
		const animations = elements.map((dot, i) => {
			const keys = Array.from({ length: 81 }, (_, n) => ({
				transform: transform(i, offset + (n * duration) / 80000),
			}));
			return dot.animate(keys, { duration, iterations: Infinity, easing: 'linear' });
		});
		return () => {
			elapsed = (offset + (performance.now() - start) / 1000) % 4;
			animations.forEach((a) => a.cancel());
		};
	});
</script>

<span bind:this={figure} class="pa-dots" aria-hidden="true"><i></i><i></i><i></i></span>

<style>
	.pa-dots {
		position: relative;
		flex: none;
		display: inline-grid;
		place-items: center;
		width: clamp(20px, calc(var(--ag-dot-size, 10px) * 2.2), 92px);
		aspect-ratio: 1;
		vertical-align: middle;
	}
	i {
		position: absolute;
		width: 18%;
		aspect-ratio: 1;
		border-radius: 50%;
		background: var(--pa-dots, var(--accent));
	}
</style>
