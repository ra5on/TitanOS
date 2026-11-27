<script lang="ts">
	import { motifPoints, motifOutline, type Motif } from './guide.ts';
	let {
		motif = '',
		calm = false,
		paused = false,
		phase = { seconds: 0 },
	}: { motif?: Motif; calm?: boolean; paused?: boolean; phase?: { seconds: number } } = $props();
	let figure = $state<HTMLElement>();
	let moving = false;
	let lastShape: Motif = '';
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
			offset = phase.seconds;
		const transform = (i: number, seconds: number) => {
			const { x, y } = position(i, seconds, shape);
			return 'translate3d(' + x * 235 + '%,' + y * 235 + '%,0)';
		};
		if (still || !el.animate) {
			moving = false;
			elements.forEach((dot, i) => (dot.style.transform = 'translateX(' + (i - 1) * 140 + '%)'));
			return;
		}
		if (stopped) {
			const coast = moving ? 0.09 : 0;
			moving = false;
			const settle = elements.map((dot, i) => {
				const from = transform(i, offset);
				const to = transform(i, offset + coast);
				dot.style.transform = to;
				return coast
					? dot.animate([{ transform: from }, { transform: to }], {
							duration: 180,
							easing: 'ease-out',
						})
					: undefined;
			});
			return () => {
				phase.seconds = (offset + coast * Math.min(1, (performance.now() - start) / 180)) % 4;
				settle.forEach((a) => a?.cancel());
			};
		}
		const duration = 4000;
		const morph = moving && shape !== lastShape;
		moving = true;
		lastShape = shape;
		const transitions: Animation[] = [];
		const animations = elements.map((dot, i) => {
			const keys = Array.from({ length: 81 }, (_, n) => ({
				transform: transform(i, offset + (n * duration) / 80000),
			}));
			const cycle = dot.animate(keys, { duration, iterations: Infinity, easing: 'linear' });
			if (morph)
				transitions.push(
					dot.animate(
						[{ transform: dot.style.transform }, { transform: transform(i, offset + 0.16) }],
						{ duration: 160, easing: 'ease-in-out' },
					),
				);
			return cycle;
		});
		return () => {
			phase.seconds = (offset + (performance.now() - start) / 1000) % 4;
			elements.forEach((dot) => (dot.style.transform = getComputedStyle(dot).transform));
			transitions.forEach((a) => a.cancel());
			animations.forEach((a) => a.cancel());
		};
	});
</script>

<span bind:this={figure} class="pa-dots" data-motif={motif} aria-hidden="true"
	>{#if motif && !calm && !paused}<svg viewBox="-1 -1 2 2"
			><polyline points={motifOutline(motif)} /></svg
		>{/if}<i></i><i></i><i></i></span
>

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
	svg {
		position: absolute;
		inset: 0;
		width: 100%;
		height: 100%;
		opacity: 0.28;
		color: var(--pa-dots, var(--accent));
	}
	polyline {
		stroke: currentColor;
		stroke-width: 0.08;
		stroke-linejoin: round;
		fill: none;
	}
	i {
		position: absolute;
		width: 18%;
		aspect-ratio: 1;
		border-radius: 50%;
		background: var(--pa-dots, var(--accent));
	}
</style>
