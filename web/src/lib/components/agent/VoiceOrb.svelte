<script lang="ts">
	// The figure of the Sprechmodus (0.95, owner 15 Nov 2026: "etwas
	// Außergewöhnliches"): an instrument drawn on a canvas – a dial of
	// ticks, three rings of arcs that turn against each other, a wreath of
	// bars and a glowing core. Listening, the bars follow the voice
	// (the microphone's spectrum); thinking, the rings race and three
	// sparks circle the core; speaking, the bars roll like a voice;
	// resting, everything dims and slows. "Animationen: Aus" and "Weniger
	// Bewegung" draw one still picture per state.
	import { onMount } from 'svelte';

	let {
		figure,
		level = 0,
		spectrum,
		calm = false,
	}: {
		figure: 'listen' | 'think' | 'speak' | 'rest';
		/** Loudness of the microphone, 0 to 1. */
		level?: number;
		/** The microphone's spectrum right now (0–255 per band), if any. */
		spectrum?: () => Uint8Array | null;
		calm?: boolean;
	} = $props();

	let canvas = $state<HTMLCanvasElement>();
	const BARS = 96;
	const bars = new Float32Array(BARS);

	onMount(() => {
		const el = canvas;
		const g = el?.getContext?.('2d');
		if (!el || !g) return;
		const still =
			calm || (window.matchMedia?.('(prefers-reduced-motion: reduce)').matches ?? false);
		const warm = getComputedStyle(el).getPropertyValue('--ag-warm').trim() || '#d9734a';
		const rgb = /^#([0-9a-f]{6})$/i.exec(warm);
		const [r, gr, b] = rgb
			? [0, 2, 4].map((i) => parseInt(rgb[1].slice(i, i + 2), 16))
			: [217, 115, 74];
		const ink = (a: number) => `rgba(${r},${gr},${b},${a})`;
		const light = (a: number) =>
			`rgba(${Math.min(255, r + 38)},${Math.min(255, gr + 90)},${Math.min(255, b + 110)},${a})`;

		let raf = 0;
		let last = performance.now();
		let energy = 0;
		let turn = 0;
		let speed = 1;
		let glow = 0.5;
		let shown = '';

		function size() {
			const dpr = Math.min(3, window.devicePixelRatio || 1);
			const box = el!.getBoundingClientRect();
			const w = Math.max(1, Math.round(box.width * dpr));
			if (el!.width !== w) el!.width = el!.height = w;
			return w;
		}

		// A voice that is not measured (the answer read aloud): three slow
		// waves and a quick one, so the bars roll like speech.
		const voice = (i: number, t: number) => {
			const a = (i / BARS) * Math.PI * 2;
			const talk = 0.55 + 0.45 * Math.sin(t * 7.3) * Math.sin(t * 2.1 + 1);
			return (
				Math.max(
					0,
					0.5 +
						0.3 * Math.sin(a * 3 + t * 5) +
						0.2 * Math.sin(a * 7 - t * 8) +
						0.15 * Math.sin(a * 13 + t * 13),
				) * Math.max(0.15, talk)
			);
		};

		function draw(now: number) {
			const dt = Math.min(0.05, (now - last) / 1000);
			last = now;
			const t = now / 1000;
			const w = size();
			const c = w / 2;
			const u = w / 300; // one unit: the figure is drawn on 300 × 300
			const fig = figure;

			// Where the state wants to be; everything eases there.
			const wantSpeed = fig === 'think' ? 3.4 : fig === 'speak' ? 1.3 : fig === 'rest' ? 0.25 : 1;
			const wantGlow = fig === 'rest' ? 0.22 : fig === 'think' ? 0.75 : 0.6;
			const k = still ? 1 : 1 - Math.pow(0.001, dt);
			speed += (wantSpeed - speed) * k;
			glow += (wantGlow - glow) * k;
			if (!still) turn += dt * speed;

			// The bars.
			const data = fig === 'listen' ? spectrum?.() : null;
			let sum = 0;
			for (let i = 0; i < BARS; i++) {
				let want = 0.04;
				if (data && data.length) {
					// Mirrored, voice bands only: the wreath is symmetric.
					const half = i < BARS / 2 ? i : BARS - 1 - i;
					const bin = 2 + Math.floor((half / (BARS / 2)) * Math.min(data.length - 3, 90));
					want = Math.pow(data[bin] / 255, 1.6);
				} else if (fig === 'listen') {
					want = 0.05 + level * 0.5;
				} else if (fig === 'speak') {
					want = still ? 0.45 : voice(i, t);
				} else if (fig === 'think') {
					const a = (i / BARS) * Math.PI * 2;
					want = still ? 0.2 : 0.12 + 0.2 * Math.pow(Math.max(0, Math.sin(a * 2 - t * 4)), 6);
				}
				bars[i] += (want - bars[i]) * (still ? 1 : want > bars[i] ? 0.5 : 0.14);
				sum += bars[i];
			}
			const loud = Math.min(1, (sum / BARS) * 2.4);
			energy += (loud - energy) * (still ? 1 : 0.2);

			g!.clearRect(0, 0, w, w);
			g!.lineCap = 'round';

			// Glow behind everything.
			const halo = g!.createRadialGradient(c, c, 0, c, c, 150 * u);
			halo.addColorStop(0, ink(0.3 * glow + 0.25 * energy));
			halo.addColorStop(0.45, ink(0.1 * glow + 0.08 * energy));
			halo.addColorStop(1, ink(0));
			g!.fillStyle = halo;
			g!.fillRect(0, 0, w, w);

			// The dial: sixty ticks, every fifth longer.
			g!.lineWidth = 1.2 * u;
			for (let i = 0; i < 60; i++) {
				const a = (i / 60) * Math.PI * 2 + turn * 0.05;
				const long = i % 5 === 0;
				const r1 = 139 * u;
				const r0 = r1 - (long ? 9 : 4) * u;
				g!.strokeStyle = ink((long ? 0.6 : 0.28) * (0.4 + glow));
				g!.beginPath();
				g!.moveTo(c + Math.cos(a) * r0, c + Math.sin(a) * r0);
				g!.lineTo(c + Math.cos(a) * r1, c + Math.sin(a) * r1);
				g!.stroke();
			}

			// Three rings of arcs, turning against each other.
			const ring = (
				radius: number,
				parts: number,
				sweep: number,
				rate: number,
				width: number,
				a: number,
			) => {
				g!.lineWidth = width * u;
				g!.strokeStyle = ink(a * (0.35 + glow));
				for (let i = 0; i < parts; i++) {
					const from = turn * rate + (i / parts) * Math.PI * 2;
					g!.beginPath();
					g!.arc(c, c, radius * u, from, from + sweep);
					g!.stroke();
				}
			};
			ring(124, 2, 1.9, 0.35, 2.4, 0.85);
			ring(113, 3, 1.05, -0.6, 1.4, 0.6);
			ring(103, 24, 0.12, 0.9, 3.2, 0.42);

			// The wreath of bars.
			const base = 62 * u + energy * 5 * u;
			g!.lineWidth = 2.3 * u;
			g!.shadowColor = ink(0.9);
			g!.shadowBlur = 8 * u;
			for (let i = 0; i < BARS; i++) {
				const a = (i / BARS) * Math.PI * 2 - Math.PI / 2;
				const len = (3 + bars[i] * 34) * u;
				g!.strokeStyle = ink(0.45 + bars[i] * 0.55);
				g!.beginPath();
				g!.moveTo(c + Math.cos(a) * base, c + Math.sin(a) * base);
				g!.lineTo(c + Math.cos(a) * (base + len), c + Math.sin(a) * (base + len));
				g!.stroke();
			}

			// The core.
			const beat = still ? 0 : Math.sin(t * (fig === 'think' ? 5 : 1.6)) * 0.03;
			const core = (38 + energy * 12) * u * (1 + beat);
			g!.shadowBlur = (26 + energy * 30) * u;
			const fill = g!.createRadialGradient(
				c - core * 0.25,
				c - core * 0.3,
				core * 0.05,
				c,
				c,
				core,
			);
			fill.addColorStop(0, light(0.95 * (0.45 + glow)));
			fill.addColorStop(0.55, ink(0.92 * (0.45 + glow)));
			fill.addColorStop(1, ink(0.5 * (0.35 + glow)));
			g!.fillStyle = fill;
			g!.beginPath();
			g!.arc(c, c, core, 0, Math.PI * 2);
			g!.fill();
			g!.shadowBlur = 0;
			g!.lineWidth = 1.2 * u;
			g!.strokeStyle = light(0.5 * glow);
			g!.beginPath();
			g!.arc(c, c, core + 5 * u, 0, Math.PI * 2);
			g!.stroke();

			// Thinking: three sparks circle the core, each with a tail.
			if (fig === 'think') {
				for (let s = 0; s < 3; s++) {
					for (let q = 0; q < 10; q++) {
						const a = turn * 1.5 + (s / 3) * Math.PI * 2 - q * 0.07;
						g!.fillStyle = light((1 - q / 10) * 0.9);
						g!.beginPath();
						g!.arc(
							c + Math.cos(a) * 52 * u,
							c + Math.sin(a) * 52 * u,
							(3.2 - q * 0.25) * u,
							0,
							Math.PI * 2,
						);
						g!.fill();
					}
				}
			}

			if (still) {
				// One picture per state; drawn again only when the state changes.
				shown = fig;
				return;
			}
			raf = requestAnimationFrame(draw);
		}

		raf = requestAnimationFrame(draw);
		const check = still
			? setInterval(() => {
					if (shown !== figure) draw(performance.now());
				}, 200)
			: undefined;
		return () => {
			cancelAnimationFrame(raf);
			clearInterval(check);
		};
	});
</script>

<canvas bind:this={canvas} class="orb-canvas" data-figure={figure} aria-hidden="true"></canvas>

<style>
	.orb-canvas {
		display: block;
		width: 100%;
		height: 100%;
	}
</style>
