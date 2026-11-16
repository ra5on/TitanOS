<script lang="ts">
	// The working figure of the KI-Agent: three shaded spheres in space
	// (lib/agent-figures.ts says where they are). Drawn with transforms only,
	// one frame at a time; standing still with "Animationen: Aus" and with
	// "less motion" of the system, and while the page is not shown.
	//
	// 1.01 (owner 16 Nov 2026: "teilweise keine sanften Übergänge", "alle
	// drei Punkte gleich groß"): the three spheres always have one size; the
	// figure stays when the kind of work or its size changes and glides into
	// the new motion and size instead of being built anew.
	import { untrack } from 'svelte';
	import type { BusyKind } from '#lib/agent.ts';
	import { FIGURES, SPHERE, blendSpots, figureSpots, type Spot } from '#lib/agent-figures.ts';

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

	/** How long a change of figure or size takes, in seconds. */
	const GLIDE = 0.45;

	let box = $state<HTMLElement>();
	let dots = $state<HTMLElement[]>([]);
	const shadow = $derived((FIGURES[kind] ?? FIGURES['']).shadow);

	// The figure and size the spheres come from while they glide.
	let from: { kind: BusyKind; size: number; at: number; spots: Spot[] | null } | null = null;
	const first = untrack(() => size);
	let shownKind: BusyKind = untrack(() => kind);
	let shownSize = first;
	let last: Spot[] = [];
	let lastSize = first;

	function place(spots: Spot[], width: number) {
		const half = width / 2;
		const d = Math.max(3, width * SPHERE);
		if (box && width !== lastSize) {
			box.style.width = box.style.height = `${width}px`;
			lastSize = width;
		}
		spots.forEach((p, n) => {
			const el = dots[n];
			if (!el) return;
			el.style.width = el.style.height = `${d}px`;
			el.style.margin = `${-d / 2}px 0 0 ${-d / 2}px`;
			el.style.transform = `translate(${p.x * half}px, ${p.y * half}px) scaleY(${p.squash ?? 1})`;
			el.style.zIndex = String(Math.round(p.z * 10) + 10);
			el.style.filter = `brightness(${0.78 + (p.z + 1) * 0.2})`;
		});
		last = spots;
	}

	function draw(t: number, glide: boolean) {
		if (kind !== shownKind || size !== shownSize) {
			// Start from where the spheres stand now, also in the middle of
			// another change.
			from = glide
				? { kind: shownKind, size: lastSize, at: t, spots: last.length ? last : null }
				: null;
			shownKind = kind;
			shownSize = size;
		}
		const target = figureSpots(kind, t);
		if (!from) {
			place(target, size);
			return;
		}
		const k = (t - from.at) / GLIDE;
		if (k >= 1) {
			from = null;
			place(target, size);
			return;
		}
		const start = from.spots ?? figureSpots(from.kind, t);
		const e = k * k * (3 - 2 * k);
		place(blendSpots(start, target, k), from.size + (size - from.size) * e);
	}

	$effect(() => {
		void kind;
		void size;
		const still =
			calm ||
			typeof requestAnimationFrame === 'undefined' ||
			(typeof matchMedia !== 'undefined' && matchMedia('(prefers-reduced-motion: reduce)').matches);
		if (still) {
			draw(0.6, false);
			return;
		}
		let frame = 0;
		const start = performance.now();
		const step = (now: number) => {
			draw((now - start) / 1000, true);
			frame = requestAnimationFrame(step);
		};
		frame = requestAnimationFrame(step);
		return () => cancelAnimationFrame(frame);
	});
</script>

<span
	class="fig"
	bind:this={box}
	style:width="{first}px"
	style:height="{first}px"
	data-kind={kind}
	aria-hidden="true"
	><u class:off={!shadow}></u>{#each [0, 1, 2] as n (n)}<i bind:this={dots[n]}></i>{/each}</span
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
	/* The ground the spheres hang over; it fades with the figure that has
	   none instead of vanishing. */
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
		transition: opacity 0.45s ease;
	}
	.fig u.off {
		opacity: 0;
	}
	@media (prefers-reduced-motion: reduce) {
		.fig {
			animation: none;
		}
		.fig u {
			transition: none;
		}
	}
</style>
