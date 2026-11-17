<script lang="ts">
	// The working figure of the KI-Agent: three flat dots moving in space
	// (lib/agent-figures.ts says where they are). Drawn with transforms only,
	// one frame at a time; standing still with "Animationen: Aus" and with
	// "less motion" of the system, and while the page is not shown.
	//
	// One clock runs on across every change (1.01, owner 16 Nov 2026: "wird
	// die immer wieder von vorne neu geladen"): a new word or kind of work
	// never starts the motion again. The three dots always have one size.
	//
	// 1.05 (owner 17 Nov 2026: "Größe der Punkte anpassen"): the figure is as
	// wide as --ag-dot-size (Einstellungen › Darstellung, 18 px by default)
	// unless a size is given; the dots and their paths grow with it, because
	// every length below is a share of the figure.
	import type { BusyKind } from '#lib/agent.ts';
	import { FIGURES, SPHERE, figureAt, type Spot } from '#lib/agent-figures.ts';

	let {
		kind = '',
		size,
		calm = false,
	}: {
		kind?: BusyKind;
		/** Width and height in px; without it --ag-dot-size. */
		size?: number;
		calm?: boolean;
	} = $props();

	let dots = $state<HTMLElement[]>([]);
	// Units of motion so far (seconds × the figure's speed).
	let clock = 0;
	// A place in half widths of the figure, as a share of a dot's own size
	// (a percentage in translate() refers to the dot).
	const unit = 100 / (2 * SPHERE);

	function place(spots: Spot[]) {
		spots.forEach((p, n) => {
			const el = dots[n];
			if (!el) return;
			el.style.transform = `translate(${(p.x * unit).toFixed(2)}%, ${(p.y * unit).toFixed(2)}%)`;
			// Depth shows only in what covers what: one size, one colour.
			el.style.zIndex = String(Math.round(p.z * 10) + 10);
		});
	}

	/** One frame: dt seconds since the last one. */
	function draw(dt: number) {
		clock += dt * (FIGURES[kind] ?? FIGURES['']).speed;
		place(figureAt(kind, clock));
	}

	// Standing still: one picture.
	const still = $derived(
		calm ||
			typeof requestAnimationFrame === 'undefined' ||
			(typeof matchMedia !== 'undefined' && matchMedia('(prefers-reduced-motion: reduce)').matches),
	);
	$effect(() => {
		void dots.length;
		if (still) draw(0);
	});
	// Moving: one loop for the life of the figure. It reads the kind every
	// frame, so a change never restarts it.
	$effect(() => {
		if (still) return;
		let frame = 0;
		let before = performance.now();
		const step = (now: number) => {
			draw(Math.min(0.1, Math.max(0, (now - before) / 1000)));
			before = now;
			frame = requestAnimationFrame(step);
		};
		frame = requestAnimationFrame(step);
		return () => cancelAnimationFrame(frame);
	});
</script>

<span
	class="fig"
	style:--fig-size={size ? `${size}px` : null}
	style:--fig-dot="{SPHERE * 100}%"
	data-kind={kind}
	aria-hidden="true"
	>{#each [0, 1, 2] as n (n)}<i bind:this={dots[n]}></i>{/each}</span
>

<style>
	.fig {
		flex: none;
		position: relative;
		display: inline-block;
		width: var(--fig-size, var(--ag-dot-size, 18px));
		height: var(--fig-size, var(--ag-dot-size, 18px));
		animation: appear 0.3s cubic-bezier(0.22, 1, 0.36, 1) both;
		transition:
			width 0.3s ease,
			height 0.3s ease;
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
	   covers what. Size and margin are shares of the figure's width. */
	.fig i {
		position: absolute;
		left: 50%;
		top: 50%;
		width: var(--fig-dot);
		height: var(--fig-dot);
		margin: calc(var(--fig-dot) / -2) 0 0 calc(var(--fig-dot) / -2);
		border-radius: 50%;
		background: var(--ag-dots, var(--ag-warm, var(--accent)));
	}
	:global(.agent.calm) .fig {
		transition: none;
	}
	@media (prefers-reduced-motion: reduce) {
		.fig {
			animation: none;
			transition: none;
		}
	}
</style>
