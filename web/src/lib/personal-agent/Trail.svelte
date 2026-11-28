<script lang="ts" module>
	/** What the frame of the composer shows (§125, A1/A4/A5). */
	export type TrailMode = 'rest' | 'type' | 'work' | 'wait' | 'done' | 'voice';
</script>

<script lang="ts">
	// 1.40 (§125, owner 28.11.2026): instead of the blue focus ring, a light
	// runs around the whole composer. A1: a comet every few seconds at rest
	// and while typing. A4: while the agent works it circles calmly without
	// a pause, waiting for you breathes amber, done flashes green once. A5:
	// while dictating the frame follows the voice. The line lies exactly on
	// the middle of the 1 px border and moves at the same speed everywhere
	// around the round shape. With "reduce motion" nothing moves; the state
	// stays as text next to it (Workspace).
	let {
		mode = 'rest',
		calm = false,
		level = 0,
	}: { mode?: TrailMode; calm?: boolean; level?: number } = $props();
	let host = $state<HTMLElement>();
	let w = $state(0),
		h = $state(0),
		r = $state(0);
	const uid = 'pa-trail-' + Math.random().toString(36).slice(2, 8);
	let glow = $state<SVGRectElement>(),
		tail = $state<SVGRectElement>(),
		head = $state<SVGRectElement>(),
		ring = $state<SVGRectElement>(),
		halo = $state<HTMLElement>(),
		grad = $state<SVGLinearGradientElement>();
	$effect(() => {
		const frame = host?.parentElement;
		if (!frame) return;
		const measure = () => {
			const b = frame.getBoundingClientRect();
			w = b.width;
			h = b.height;
			r = parseFloat(getComputedStyle(frame).borderTopLeftRadius) || 0;
		};
		measure();
		const o = new ResizeObserver(measure);
		o.observe(frame);
		return () => o.disconnect();
	});
	const ease = (x: number) => 0.5 - Math.cos(Math.PI * x) / 2;
	// The head starts at the top in the middle of the frame.
	const top = () => (w + h > 0 ? (w / 2 / (2 * (w + h))) * 100 : 0);
	function comet(p: number, alpha: number) {
		const parts: [SVGRectElement | undefined, number, number][] = [
			[glow, 26, 0.5],
			[tail, 20, 0.9],
			[head, 2.2, 1],
		];
		for (const [el, len, op] of parts) {
			if (!el) continue;
			el.setAttribute('stroke-dasharray', len + ' ' + (100 - len));
			el.setAttribute('stroke-dashoffset', String(len - p));
			el.setAttribute('opacity', String(alpha * op));
		}
	}
	$effect(() => {
		const m = mode,
			still = calm;
		if (!head || !ring || !halo) return;
		const since = performance.now();
		let frame = 0;
		const draw = (now: number) => {
			const s = (now - since) / 1000;
			let ringOpacity = 0,
				ringColor = `url(#${uid}g)`,
				ringWidth = 1.6,
				shadow = 'none';
			if (still) {
				comet(0, 0);
				if (m === 'wait') {
					ringColor = 'var(--pa-wait, #e3b341)';
					ringOpacity = 0.9;
				} else if (m === 'work' || m === 'voice') ringOpacity = 0.7;
			} else if (m === 'rest' || m === 'type') {
				const loop = m === 'type' ? 3.4 : 2.6,
					pause = m === 'type' ? 1.2 : 4.2,
					k = (s % (loop + pause)) / loop;
				if (k <= 1) comet(((top() + ease(k) * 100) % 100) + 100, Math.sin(Math.PI * k) ** 0.6);
				else comet(0, 0);
			} else if (m === 'work') {
				comet(top() + ((s / 3.2) % 1) * 100 + 100, 1);
				shadow = '0 0 24px -6px color-mix(in srgb, var(--pa-accent) 55%, transparent)';
			} else if (m === 'wait') {
				comet(0, 0);
				const b = Math.sin(s * 2.2) * 0.5 + 0.5;
				ringColor = 'var(--pa-wait, #e3b341)';
				ringOpacity = 0.45 + b * 0.55;
				shadow = `0 0 ${12 + b * 18}px -2px color-mix(in srgb, var(--pa-wait, #e3b341) ${15 + b * 25}%, transparent)`;
			} else if (m === 'done') {
				comet(0, 0);
				const k = Math.min(1, s / 1.4);
				ringColor = 'var(--pa-done, #3fb950)';
				ringOpacity = 1 - k;
				ringWidth = 2;
				shadow = `0 0 ${24 * (1 - k)}px color-mix(in srgb, var(--pa-done, #3fb950) ${45 * (1 - k)}%, transparent)`;
			} else if (m === 'voice') {
				comet(0, 0);
				const lvl = Math.max(0, Math.min(1, level));
				ringOpacity = 0.5 + lvl * 0.5;
				ringWidth = 1.5 + lvl * 2.5;
				grad?.setAttribute('gradientTransform', `rotate(${(s * 40) % 360} .5 .5)`);
				shadow = `0 0 ${10 + lvl * 34}px ${lvl * 4}px color-mix(in srgb, var(--pa-accent) ${12 + lvl * 35}%, transparent)`;
			}
			ring!.setAttribute('stroke', ringColor);
			ring!.setAttribute('opacity', String(ringOpacity));
			ring!.setAttribute('stroke-width', String(ringWidth));
			halo!.style.boxShadow = shadow;
			if (!still) frame = requestAnimationFrame(draw);
		};
		frame = requestAnimationFrame(draw);
		return () => cancelAnimationFrame(frame);
	});
</script>

<span bind:this={host} class="pa-trail" aria-hidden="true">
	<span bind:this={halo} class="pa-trail-halo"></span>
	{#if w > 0}<svg viewBox="0 0 {w} {h}" width={w} height={h}>
			<defs>
				<filter id="{uid}b" x="-20%" y="-60%" width="140%" height="220%"
					><feGaussianBlur stdDeviation="5" /></filter
				>
				<linearGradient bind:this={grad} id="{uid}g" x1="0" x2="1"
					><stop offset="0" stop-color="var(--pa-accent)" /><stop
						offset=".5"
						stop-color="var(--pa-trail-2, #b48cff)"
					/><stop offset="1" stop-color="var(--pa-trail-3, #4fd1c5)" /></linearGradient
				>
			</defs>
			<rect
				bind:this={glow}
				x="0.5"
				y="0.5"
				width={Math.max(0, w - 1)}
				height={Math.max(0, h - 1)}
				rx={Math.max(0, r - 0.5)}
				pathLength="100"
				fill="none"
				stroke-linecap="round"
				stroke="var(--pa-accent)"
				stroke-width="9"
				stroke-dasharray="0 100"
				opacity="0"
				filter="url(#{uid}b)"
			/>
			<rect
				bind:this={tail}
				x="0.5"
				y="0.5"
				width={Math.max(0, w - 1)}
				height={Math.max(0, h - 1)}
				rx={Math.max(0, r - 0.5)}
				pathLength="100"
				fill="none"
				stroke-linecap="round"
				stroke="var(--pa-accent)"
				stroke-width="2"
				stroke-dasharray="0 100"
				opacity="0"
			/>
			<rect
				bind:this={head}
				x="0.5"
				y="0.5"
				width={Math.max(0, w - 1)}
				height={Math.max(0, h - 1)}
				rx={Math.max(0, r - 0.5)}
				pathLength="100"
				fill="none"
				stroke-linecap="round"
				stroke="#fff"
				stroke-width="2.6"
				stroke-dasharray="0 100"
				opacity="0"
			/>
			<rect
				bind:this={ring}
				x="0.5"
				y="0.5"
				width={Math.max(0, w - 1)}
				height={Math.max(0, h - 1)}
				rx={Math.max(0, r - 0.5)}
				pathLength="100"
				fill="none"
				stroke-linecap="round"
				stroke="var(--pa-accent)"
				stroke-width="1.6"
				stroke-dasharray="100 0"
				opacity="0"
			/>
		</svg>{/if}
</span>

<style>
	.pa-trail,
	.pa-trail-halo {
		position: absolute;
		inset: -1px;
		border-radius: inherit;
		pointer-events: none;
	}
	.pa-trail {
		z-index: 2;
	}
	.pa-trail-halo {
		inset: 0;
		z-index: -1;
		transition: box-shadow 0.4s;
	}
	svg {
		position: absolute;
		inset: 0;
		overflow: visible;
	}
</style>
