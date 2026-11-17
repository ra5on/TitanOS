<script lang="ts">
	// The working figure of the KI-Agent: three flat dots moving in space
	// (lib/agent-figures.ts says where they are). Drawn with transforms only,
	// one frame at a time; standing still with "Animationen: Aus" and with
	// "less motion" of the system, and while the page is not shown.
	//
	// 1.01 (owner 16 Nov 2026: "teilweise keine sanften Übergänge", "alle
	// drei Punkte gleich groß"): the three spheres always have one size; the
	// figure stays when the kind of work or its size changes and glides into
	// the new motion and size instead of being built anew. One clock runs
	// on across every change ("wird die immer wieder von vorne neu
	// geladen"): a new word or a kind with the same figure only changes the
	// pace, never starts the motion again; only another figure glides.
	import { untrack } from 'svelte';
	import type { BusyKind } from '#lib/agent.ts';
	import {
		FIGURES,
		SPHERE,
		blendSpots,
		figureAt,
		sameFigure,
		type Spot,
	} from '#lib/agent-figures.ts';

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

	// The figure and size the spheres come from while they glide.
	let from: { size: number; at: number; spots: Spot[] } | null = null;
	// Units of motion so far (seconds × the figure's speed) and seconds.
	let clock = 0;
	let seconds = 0;
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
			// Depth shows in what covers what; all three keep one tone (owner
			// 16 Nov 2026: "alle in dem gleich Farbton").
			el.style.zIndex = String(Math.round(p.z * 10) + 10);
		});
		last = spots;
	}

	/** One frame: dt seconds since the last one; glide = animate changes. */
	function draw(dt: number, glide: boolean) {
		seconds += dt;
		clock += dt * (FIGURES[kind] ?? FIGURES['']).speed;
		if (kind !== shownKind || size !== shownSize) {
			// Another figure or size glides from where the spheres stand now,
			// also in the middle of another change; the same figure at another
			// pace goes straight on.
			const other = !sameFigure(kind, shownKind) || size !== shownSize;
			from = glide && other && last.length ? { size: lastSize, at: seconds, spots: last } : null;
			shownKind = kind;
			shownSize = size;
		}
		const target = figureAt(kind, clock);
		if (!from) {
			place(target, size);
			return;
		}
		const k = (seconds - from.at) / GLIDE;
		if (k >= 1) {
			from = null;
			place(target, size);
			return;
		}
		const e = k * k * (3 - 2 * k);
		place(blendSpots(from.spots, target, k), from.size + (size - from.size) * e);
	}

	// Standing still: drawn again whenever what it shows changes.
	const still = $derived(
		calm ||
			typeof requestAnimationFrame === 'undefined' ||
			(typeof matchMedia !== 'undefined' && matchMedia('(prefers-reduced-motion: reduce)').matches),
	);
	$effect(() => {
		void kind;
		void size;
		if (still) untrack(() => draw(0, false));
	});
	// Moving: one loop for the life of the figure. It reads kind and size
	// every frame, so a change never restarts it.
	$effect(() => {
		if (still) return;
		let frame = 0;
		let before = performance.now();
		const step = (now: number) => {
			draw(Math.min(0.1, Math.max(0, (now - before) / 1000)), true);
			before = now;
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
	>{#each [0, 1, 2] as n (n)}<i bind:this={dots[n]}></i>{/each}</span
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
	/* A dot: flat and exactly its colour (1.05, owner 17 Nov 2026: "die 3
	   Punkte … sind irgendwie heller als die Schrift, wie als wenn da
	   irgendwas drüber gelegt ist"; "eine Art Schimmer … was dann auch den
	   Boden leuchten lässt") – no gradient, no highlight, no shadow, no
	   ground below the figure. Depth shows only in the motion and in what
	   covers what. */
	.fig i {
		position: absolute;
		left: 50%;
		top: 50%;
		border-radius: 50%;
		background: var(--ag-dots, var(--ag-warm, var(--accent)));
	}
	@media (prefers-reduced-motion: reduce) {
		.fig {
			animation: none;
		}
	}
</style>
