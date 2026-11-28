<script lang="ts" module>
	/** What the three dots are doing (§125, B3/B4). */
	export type Mood = 'idle' | 'listen' | 'think' | 'work' | 'done';
</script>

<script lang="ts">
	// 1.40 (§125, owner 28.11.2026): the three dots as a living figure. Idle
	// they breathe, listening they lift with the voice, thinking they circle,
	// working they run a figure of eight. Done (owner: "die Punkte bewegen
	// sich an die Position zum Haken, 3 Punkte … Und dann werden die
	// miteinander verbunden und sind dadurch dann ein fertiger Haken"): each
	// dot moves to one corner of a tick, then a line is drawn from dot to dot
	// and the finished tick glows once. calm: they stand in a row (or as the
	// finished tick) and nothing moves.
	let {
		mood = 'idle',
		size = 120,
		calm = false,
		label = '',
		ondone,
	}: {
		mood?: Mood;
		size?: number;
		calm?: boolean;
		label?: string;
		ondone?: () => void;
	} = $props();
	let dots = $state<HTMLElement[]>([]);
	let line = $state<SVGPolylineElement>();
	const TICK = [
		[-0.62, 0.02],
		[-0.16, 0.46],
		[0.66, -0.5],
	];
	const ease = (x: number) => 0.5 - Math.cos(Math.PI * x) / 2;
	$effect(() => {
		const m = mood,
			still = calm,
			R = size * 0.3,
			d = Math.max(6, size * 0.13);
		const els = dots.filter(Boolean);
		if (els.length !== 3 || !line) return;
		const pts = TICK.map(([a, b]) => [size / 2 + a * R, size / 2 + b * R]);
		const len =
			Math.hypot(pts[1][0] - pts[0][0], pts[1][1] - pts[0][1]) +
			Math.hypot(pts[2][0] - pts[1][0], pts[2][1] - pts[1][1]);
		line.setAttribute('points', pts.map((p) => p.join(',')).join(' '));
		line.setAttribute('stroke-width', String(d * 0.62));
		line.setAttribute('stroke-dasharray', String(len));
		const since = performance.now();
		let frame = 0,
			told = false;
		const place = (now: number) => {
			const s = now / 1000,
				t = now - since;
			let k2 = 0;
			els.forEach((dot, i) => {
				let x = 0,
					y = 0,
					sc = 1,
					op = 1,
					glow = '';
				const ph = (i * 2 * Math.PI) / 3;
				if (still) {
					if (m === 'done') {
						x = TICK[i][0] * R;
						y = TICK[i][1] * R;
						k2 = 1;
					} else x = (i - 1) * R * 0.62;
				} else if (m === 'idle') {
					const b = Math.sin(s * 1.3) * 0.5 + 0.5;
					x = (i - 1) * (R * 0.62 + b * 3);
					y = Math.sin(s * 1.3 + i * 0.7) * 2.5;
					sc = 0.92 + b * 0.12;
					op = 0.75 + b * 0.25;
				} else if (m === 'listen') {
					const v = Math.abs(Math.sin(s * 7 + i * 1.7)) * (0.5 + 0.5 * Math.sin(s * 2.1 + i));
					x = (i - 1) * R * 0.7;
					y = -v * R * 0.5;
					sc = 1 + v * 0.35;
				} else if (m === 'think') {
					x = Math.cos(s * 2.4 + ph) * R * 0.75;
					y = Math.sin(s * 2.4 + ph) * R * 0.38;
					sc = 0.8 + (0.25 * (Math.sin(s * 2.4 + ph) + 1)) / 2;
				} else if (m === 'work') {
					const a = s * 2.2 + ph,
						dd = 1 + Math.sin(a) ** 2;
					x = (Math.cos(a) / dd) * R * 1.1;
					y = ((Math.sin(a) * Math.cos(a)) / dd) * R * 0.9;
					sc = 0.85 + (0.3 * (Math.cos(a) + 1)) / 2;
				} else if (m === 'done') {
					const k = Math.min(1, t / 650);
					const ox = (i - 1) * R * 0.62;
					x = ox + (TICK[i][0] * R - ox) * ease(k);
					y = TICK[i][1] * R * ease(k);
					k2 = Math.min(1, Math.max(0, (t - 700) / 520));
					const after = Math.max(0, 1 - (t - 1220) / 380);
					if (k2 >= 1) {
						sc = 1 + 0.18 * after;
						glow = `0 0 ${14 * Math.max(0, 1 - (t - 1220) / 900)}px var(--pa-done, #3fb950)`;
					}
				}
				dot.style.transform = `translate(${x}px,${y}px) scale(${sc})`;
				dot.style.opacity = String(op);
				dot.style.background = m === 'done' && (k2 > 0 || still) ? 'var(--pa-done, #3fb950)' : '';
				dot.style.boxShadow = glow;
			});
			line!.setAttribute('stroke-dashoffset', String(len * (1 - ease(k2))));
			line!.style.opacity = m === 'done' && k2 > 0 ? '1' : '0';
			if (m === 'done' && !told && (still || t > 1600)) {
				told = true;
				ondone?.();
			}
			if (!still) frame = requestAnimationFrame(place);
		};
		frame = requestAnimationFrame(place);
		return () => cancelAnimationFrame(frame);
	});
</script>

<span
	class="pa-figure"
	style="--fig:{size}px;--dot:{Math.max(6, size * 0.13)}px"
	role={label ? 'img' : undefined}
	aria-label={label || undefined}
	aria-hidden={label ? undefined : 'true'}
>
	<svg viewBox="0 0 {size} {size}" width={size} height={size} aria-hidden="true"
		><polyline
			bind:this={line}
			fill="none"
			stroke="var(--pa-done, #3fb950)"
			stroke-linecap="round"
			stroke-linejoin="round"
		/></svg
	>
	{#each [0, 1, 2] as i (i)}<i bind:this={dots[i]}></i>{/each}
</span>

<style>
	.pa-figure {
		position: relative;
		display: inline-grid;
		place-items: center;
		width: var(--fig);
		height: var(--fig);
		flex: none;
	}
	svg {
		position: absolute;
		inset: 0;
		overflow: visible;
	}
	polyline {
		opacity: 0;
	}
	i {
		position: absolute;
		left: 50%;
		top: 50%;
		width: var(--dot);
		height: var(--dot);
		margin: calc(var(--dot) / -2) 0 0 calc(var(--dot) / -2);
		border-radius: 50%;
		background: var(--pa-dots, var(--pa-accent));
		box-shadow: 0 0 calc(var(--dot) * 1.2)
			color-mix(in srgb, var(--pa-dots, var(--pa-accent)) 45%, transparent);
		will-change: transform;
	}
</style>
