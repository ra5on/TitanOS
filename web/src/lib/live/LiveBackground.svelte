<script lang="ts">
	// PLAN 74: a built-in picture that moves (login page, desktop). The still
	// picture lies underneath as long as WebGL draws nothing: without WebGL,
	// with "weniger Bewegung", when `moving` is off or before the picture has
	// loaded. The drawing stops while the tab is hidden or `paused` is set
	// (a window moves) and keeps to about 30 frames a second.
	import { onMount } from 'svelte';
	import { FRAGMENT, VERTEX, type LivePicture } from './shaders.ts';

	type Props = {
		picture: LivePicture;
		moving?: boolean;
		paused?: boolean;
		/** Follow the pointer a little (parallax). */
		pointer?: boolean;
	};
	let { picture, moving = true, paused = false, pointer = true }: Props = $props();

	let canvas = $state<HTMLCanvasElement>();
	let drawing = $state(false);
	const src = $derived(`/backgrounds/${picture}.webp`);

	// What the drawing loop reads; the effect below keeps it in step with the props.
	const ctl = {
		moving: true,
		paused: false,
		pointer: true,
		sync: (() => {}) as () => void,
	};
	$effect(() => {
		ctl.moving = moving;
		ctl.paused = paused;
		ctl.pointer = pointer;
		void picture;
		ctl.sync();
	});

	const reduced =
		typeof matchMedia === 'function' && matchMedia('(prefers-reduced-motion: reduce)').matches;

	onMount(() => {
		if (!canvas || reduced) return;
		const gl = canvas.getContext('webgl', { antialias: false, preserveDrawingBuffer: false });
		if (!gl) return;
		const compile = (type: number, text: string) => {
			const s = gl.createShader(type)!;
			gl.shaderSource(s, text);
			gl.compileShader(s);
			return gl.getShaderParameter(s, gl.COMPILE_STATUS) ? s : null;
		};
		let program: WebGLProgram | null = null;
		let texture: WebGLTexture | null = null;
		let size = [1, 1];
		let shown = '';
		let frame = 0;
		let last = 0;
		const start = performance.now();
		let mx = 0,
			my = 0,
			tx = 0,
			ty = 0;

		function build(which: LivePicture) {
			if (program) gl!.deleteProgram(program);
			const vs = compile(gl!.VERTEX_SHADER, VERTEX);
			const fs = compile(gl!.FRAGMENT_SHADER, FRAGMENT[which]);
			if (!vs || !fs) return null;
			const p = gl!.createProgram()!;
			gl!.attachShader(p, vs);
			gl!.attachShader(p, fs);
			gl!.linkProgram(p);
			return gl!.getProgramParameter(p, gl!.LINK_STATUS) ? p : null;
		}
		const buffer = gl.createBuffer();
		gl.bindBuffer(gl.ARRAY_BUFFER, buffer);
		gl.bufferData(gl.ARRAY_BUFFER, new Float32Array([-1, -1, 1, -1, -1, 1, 1, 1]), gl.STATIC_DRAW);

		function load(which: LivePicture) {
			shown = which;
			drawing = false;
			const image = new Image();
			image.onload = () => {
				if (shown !== which) return;
				program = build(which);
				if (!program) return;
				texture ??= gl!.createTexture();
				gl!.bindTexture(gl!.TEXTURE_2D, texture);
				gl!.texImage2D(gl!.TEXTURE_2D, 0, gl!.RGBA, gl!.RGBA, gl!.UNSIGNED_BYTE, image);
				for (const [k, v] of [
					[gl!.TEXTURE_MIN_FILTER, gl!.LINEAR],
					[gl!.TEXTURE_MAG_FILTER, gl!.LINEAR],
					[gl!.TEXTURE_WRAP_S, gl!.CLAMP_TO_EDGE],
					[gl!.TEXTURE_WRAP_T, gl!.CLAMP_TO_EDGE],
				])
					gl!.texParameteri(gl!.TEXTURE_2D, k, v);
				size = [image.naturalWidth, image.naturalHeight];
				draw(performance.now(), true);
			};
			image.src = `/backgrounds/${which}.webp`;
		}

		function draw(now: number, force = false) {
			frame = 0;
			if (!program || !canvas) return;
			if (!force && now - last < 33) return schedule();
			last = now;
			const ratio = Math.min(devicePixelRatio || 1, 1.5);
			const w = Math.round(canvas.clientWidth * ratio),
				h = Math.round(canvas.clientHeight * ratio);
			if (canvas.width !== w || canvas.height !== h) {
				canvas.width = w;
				canvas.height = h;
			}
			gl!.viewport(0, 0, w, h);
			gl!.useProgram(program);
			const at = gl!.getAttribLocation(program, 'p');
			gl!.bindBuffer(gl!.ARRAY_BUFFER, buffer);
			gl!.enableVertexAttribArray(at);
			gl!.vertexAttribPointer(at, 2, gl!.FLOAT, false, 0, 0);
			gl!.activeTexture(gl!.TEXTURE0);
			gl!.bindTexture(gl!.TEXTURE_2D, texture);
			mx += (tx - mx) * 0.05;
			my += (ty - my) * 0.05;
			gl!.uniform1i(gl!.getUniformLocation(program, 'img'), 0);
			gl!.uniform2f(gl!.getUniformLocation(program, 'isz'), size[0], size[1]);
			gl!.uniform2f(gl!.getUniformLocation(program, 'res'), w, h);
			gl!.uniform1f(gl!.getUniformLocation(program, 't'), (now - start) / 1000 + 8);
			gl!.uniform2f(gl!.getUniformLocation(program, 'm'), mx, -my);
			gl!.drawArrays(gl!.TRIANGLE_STRIP, 0, 4);
			drawing = true;
			schedule();
		}
		function schedule() {
			if (frame || !ctl.moving || ctl.paused || document.hidden) return;
			frame = requestAnimationFrame((now) => draw(now));
		}
		const move = (e: PointerEvent) => {
			if (!ctl.pointer) return;
			tx = e.clientX / innerWidth - 0.5;
			ty = e.clientY / innerHeight - 0.5;
		};
		const visible = () => schedule();
		addEventListener('pointermove', move, { passive: true });
		document.addEventListener('visibilitychange', visible);
		const resize = new ResizeObserver(() => draw(performance.now(), true));
		resize.observe(canvas);

		ctl.sync = () => {
			if (picture !== shown) load(picture);
			else schedule();
		};
		ctl.sync();
		return () => {
			ctl.sync = () => {};
			cancelAnimationFrame(frame);
			removeEventListener('pointermove', move);
			document.removeEventListener('visibilitychange', visible);
			resize.disconnect();
			gl.getExtension('WEBGL_lose_context')?.loseContext();
		};
	});
</script>

<div class="live" aria-hidden="true">
	<div class="still" style:background-image={`url("${src}")`}></div>
	<canvas bind:this={canvas} class:on={drawing && moving && !reduced}></canvas>
</div>

<style>
	.live,
	.still,
	canvas {
		position: absolute;
		inset: 0;
		width: 100%;
		height: 100%;
	}
	.still {
		background: center / cover no-repeat;
	}
	canvas {
		opacity: 0;
		transition: opacity 0.6s;
	}
	canvas.on {
		opacity: 1;
	}
</style>
