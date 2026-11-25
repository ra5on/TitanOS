<script lang="ts">
	// The working figure of the KI-Agent: three flat dots moving in space
	// (lib/agent-figures.ts says where they are). Moved with transforms only,
	// as an animation the browser plays itself (1.07, below); standing still
	// with "Animationen: Aus" and with "less motion" of the system.
	//
	// One clock runs on across every change (1.01, owner 16 Nov 2026: "wird
	// die immer wieder von vorne neu geladen"): a new word or kind of work
	// never starts the motion again. The three dots always have one size.
	//
	// 1.05 (owner 17 Nov 2026: "Größe der Punkte anpassen"): the figure is as
	// wide as --ag-dot-size (Einstellungen › Darstellung, 18 px by default)
	// unless a size is given; the dots and their paths grow with it, because
	// every length below is a share of the figure.
	import type { BusyKind, DotMode } from '#lib/agent.ts';
	import { untrack } from 'svelte';
	import {
		BLOCK,
		FIGURES,
		SPHERE,
		figureAt,
		nightFigure,
		easeUnit,
		type Spot,
	} from '#lib/agent-figures.ts';

	let {
		kind = '',
		mode = 'both',
		tool = false,
		size,
		calm = false,
	}: {
		kind?: BusyKind;
		mode?: DotMode;
		tool?: boolean;
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
	// While it moves, places are in px (half the figure's width per unit):
	// the browser plays a transform with lengths on its compositor, one with
	// percentages of the dot not always. A new size plays anew (below).
	let box = $state<HTMLElement>();
	let half = 0;
	const transform = (p: Spot) =>
		half > 0
			? `translate3d(${(p.x * half).toFixed(2)}px, ${(p.y * half).toFixed(2)}px, ${(p.z * 4).toFixed(2)}px)`
			: `translate3d(${(p.x * unit).toFixed(2)}%, ${(p.y * unit).toFixed(2)}%, ${(p.z * 4).toFixed(2)}px)`;

	/** The still picture at the clock. */
	function place() {
		(mode === 'titan'
			? figureAt(kind, clock)
			: [-0.64, 0, 0.64].map((x) => ({ x, y: 0, z: 0 }))
		).forEach((p, n) => {
			if (dots[n]) dots[n].style.transform = transform(p);
		});
	}

	// 1.07 (owner 17 Nov 2026: "Die Animation der 3 Punkte am Arbeitsplatz
	// ist nicht flüssig"): the motion is no longer drawn by a script frame
	// by frame. The path of the next seconds is worked out at once and
	// handed to the browser as an animation of the transform only
	// (translate3d; depth is the z of the 3D space, not a z-index). The
	// browser plays it on its compositor: no style or layout work per frame,
	// and it stays smooth while the page is busy (an answer streaming in).
	// Before a stretch ends the next one begins where the dots are – the
	// clock runs on with the real time; when a stretch was not followed in
	// time (page in the background), the figure stands at its end and goes
	// on from there, never with a jump to make up for the time lost.
	const STRETCH = 3000; // ms of motion one set of key frames covers
	const RATE = 30; // key frames per second; the browser goes between them
	let playing: Animation[] = [];
	let from = 0; // the clock at the start of the stretch
	let speed = 1;
	let next: ReturnType<typeof setTimeout> | undefined;

	/** Where the clock stands now in the stretch that plays. */
	function clockNow(): number {
		const t = Number(playing[0]?.currentTime ?? 0);
		return from + (Math.min(Math.max(0, t), STRETCH) / 1000) * speed;
	}
	function stop() {
		clearTimeout(next);
		next = undefined;
		if (playing.length) clock = clockNow();
		for (const a of playing) a.cancel();
		playing = [];
	}
	// Blend on the same running clock. A second tool has the same target.
	let blendStart = 0,
		blendFrom = 0,
		blendTo = 0;
	function weight(at: number) {
		return blendFrom + (blendTo - blendFrom) * easeUnit((at - blendStart) / 0.7);
	}
	const target = $derived(mode === 'braid' || (mode === 'both' && tool) ? 1 : 0);
	const legacyKind = $derived(mode === 'titan' ? kind : '');
	function play() {
		stop();
		from = clock;
		if (blendTo !== target) {
			blendFrom = weight(clock);
			blendTo = target;
			blendStart = clock;
		}
		speed = mode === 'titan' ? (FIGURES[legacyKind] ?? FIGURES['']).speed : 1;
		half = (box?.offsetWidth ?? 0) / 2;
		const frames: Keyframe[][] = [[], [], []];
		for (let i = 0; i <= (STRETCH * RATE) / 1000; i++) {
			const at = from + (i / RATE) * speed;
			const points = mode === 'titan' ? figureAt(legacyKind, at) : nightFigure(at, weight(at));
			points.forEach((point, n) => frames[n].push({ transform: transform(point) }));
		}
		playing = dots.map((el, n) => {
			el.style.transform = String(frames[n][0].transform);
			return el.animate(frames[n], { duration: STRETCH, fill: 'forwards' });
		});
		next = setTimeout(play, STRETCH - 400);
	}
	let reduced = $state(false);
	$effect(() => {
		if (typeof matchMedia === 'undefined') return;
		const media = matchMedia('(prefers-reduced-motion: reduce)');
		const update = () => (reduced = media.matches);
		update();
		media.addEventListener?.('change', update);
		return () => media.removeEventListener?.('change', update);
	});
	let begun = false;
	$effect(() => {
		void mode;
		void target;
		void legacyKind;
		if (dots.filter(Boolean).length !== 3) return;
		if (calm || reduced || typeof Element.prototype.animate !== 'function') {
			untrack(place);
			return;
		}
		if (!begun) {
			begun = true;
			if (mode === 'titan') clock = BLOCK * Math.floor(Math.random() * 50);
			blendFrom = blendTo = target;
		}
		untrack(play);
		return stop;
	});
	// Another size (Einstellungen › Darstellung): the places of the stretch
	// are worked out again for it.
	$effect(() => {
		if (!box || typeof ResizeObserver === 'undefined') return;
		const watch = new ResizeObserver(() => {
			if (playing.length && box && box.offsetWidth / 2 !== half) play();
		});
		watch.observe(box);
		return () => watch.disconnect();
	});
</script>

<span
	bind:this={box}
	class="fig"
	style:--fig-size={size ? `${size}px` : null}
	style:--fig-dot="{SPHERE * 100}%"
	data-kind={kind}
	data-figure={mode}
	data-pattern={mode === 'titan' ? 'titan' : target ? 'braid' : 'magnet'}
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
		/* What happens inside never moves the line around it; the dots
		   stand in one 3D space, so their z decides what covers what. */
		contain: layout;
		transform-style: preserve-3d;
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
		/* Flat dots without a shine (1.05): a layer of their own is safe. */
		will-change: transform;
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
