<script lang="ts">
	let {
		listen,
		spectrum,
		calm,
	}: { listen: boolean; spectrum: () => Uint8Array | null; calm: boolean } = $props();
	let canvas = $state<HTMLCanvasElement>();
	$effect(() => {
		const el = canvas,
			active = listen,
			still = calm;
		const ctx = el?.getContext('2d');
		if (!el || !ctx) return;
		let frame = 0,
			next = 0;
		const paint = (time: number) => {
			if (time >= next) {
				next = time + 40;
				const bins = active ? spectrum() : null,
					pen = ctx!;
				pen.clearRect(0, 0, 240, 240);
				pen.strokeStyle = getComputedStyle(el).color;
				pen.lineWidth = 3;
				pen.lineCap = 'round';
				for (let i = 0; i < 48; i++) {
					const a = (i * Math.PI) / 24,
						b = bins?.[Math.floor((i * (bins.length - 1)) / 48)] ?? 0;
					const radial = still ? 3 : 3 + (b * 22) / 255;
					const c = Math.cos(a),
						s = Math.sin(a);
					pen.beginPath();
					pen.moveTo(120 + c * 78, 120 + s * 78);
					pen.lineTo(120 + c * (78 + radial), 120 + s * (78 + radial));
					pen.stroke();
				}
			}
			if (active && !still) frame = requestAnimationFrame(paint);
		};
		paint(0);
		return () => cancelAnimationFrame(frame);
	});
</script>

<div class="spectrum" aria-hidden="true">
	<canvas width="240" height="240" bind:this={canvas}></canvas><span></span>
</div>

<style>
	.spectrum {
		position: relative;
		display: grid;
		place-items: center;
		width: 100%;
		aspect-ratio: 1;
		border-radius: 50%;
		color: var(--pa-orb, var(--accent));
	}
	canvas {
		position: absolute;
		inset: 0;
		width: 100%;
		height: 100%;
	}
	span {
		width: 56%;
		aspect-ratio: 1;
		border-radius: 50%;
		background: radial-gradient(
			at 36% 28%,
			color-mix(in srgb, currentColor 52%, white),
			currentColor 40%,
			color-mix(in srgb, currentColor 26%, var(--pa-background)) 94%
		);
	}
</style>
