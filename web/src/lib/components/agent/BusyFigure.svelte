<script lang="ts">
	// The working figure of the KI-Agent: three shaded spheres in space
	// (lib/agent-figures.ts says where they are). Drawn with transforms only,
	// one frame at a time; standing still with "Animationen: Aus" and with
	// "less motion" of the system, and while the page is not shown.
	import type { BusyKind } from '#lib/agent.ts';
	import { FIGURES, figureSpots } from '#lib/agent-figures.ts';

	let {
		kind = '',
		size = 26,
		calm = false,
	}: {
		kind?: BusyKind;
		/** Width and height in px. */
		size?: number;
		calm?: boolean;
	} = $props();

	let dots = $state<HTMLElement[]>([]);
	const shadow = $derived((FIGURES[kind] ?? FIGURES['']).shadow);

	function draw(t: number) {
		const half = size / 2;
		figureSpots(kind, t).forEach((p, n) => {
			const el = dots[n];
			if (!el) return;
			const d = Math.max(3, size * 0.42 * p.s * (1 + p.z * 0.28));
			el.style.width = el.style.height = `${d}px`;
			el.style.margin = `${-d / 2}px 0 0 ${-d / 2}px`;
			el.style.transform = `translate(${p.x * half}px, ${p.y * half}px) scaleY(${p.squash ?? 1})`;
			el.style.zIndex = String(Math.round(p.z * 10) + 10);
			el.style.filter = `brightness(${0.78 + (p.z + 1) * 0.2})`;
		});
	}
	$effect(() => {
		void kind;
		void size;
		const still =
			calm ||
			typeof requestAnimationFrame === 'undefined' ||
			(typeof matchMedia !== 'undefined' && matchMedia('(prefers-reduced-motion: reduce)').matches);
		if (still) {
			draw(0.6);
			return;
		}
		let frame = 0;
		const start = performance.now();
		const step = (now: number) => {
			draw((now - start) / 1000);
			frame = requestAnimationFrame(step);
		};
		frame = requestAnimationFrame(step);
		return () => cancelAnimationFrame(frame);
	});
</script>

<span class="fig" style:width="{size}px" style:height="{size}px" data-kind={kind} aria-hidden="true"
	>{#if shadow}<u></u>{/if}{#each [0, 1, 2] as n (n)}<i bind:this={dots[n]}></i>{/each}</span
>

<style>
	.fig {
		flex: none;
		position: relative;
		display: inline-block;
		animation: appear 0.3s cubic-bezier(0.22, 1, 0.36, 1) both;
	}
	@keyframes appear {
		from {
			opacity: 0;
			transform: scale(0.5);
		}
	}
	/* A sphere: light from the upper left, its own colour, a darker rim. */
	.fig i {
		position: absolute;
		left: 50%;
		top: 50%;
		border-radius: 50%;
		background: radial-gradient(
			circle at 34% 28%,
			color-mix(in srgb, var(--ag-warm, var(--accent)) 45%, white) 0,
			var(--ag-warm, var(--accent)) 46%,
			color-mix(in srgb, var(--ag-warm, var(--accent)) 58%, black) 100%
		);
		will-change: transform;
	}
	/* The ground the spheres hang over. */
	.fig u {
		position: absolute;
		left: 15%;
		bottom: -8%;
		width: 70%;
		height: 12%;
		border-radius: 50%;
		background: color-mix(in srgb, var(--ag-warm, var(--accent)) 35%, black);
		filter: blur(2px);
		opacity: 0.4;
	}
	@media (prefers-reduced-motion: reduce) {
		.fig {
			animation: none;
		}
	}
</style>
