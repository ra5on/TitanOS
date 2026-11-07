<script lang="ts">
	// PLAN 63: the remote view ("Fernansicht") of a Docker app. TitanOS opens
	// the app in a browser on the NAS; this view shows its picture (WebCodecs
	// video on a canvas), plays its sound and sends mouse, keys, text, the
	// clipboard and uploads back (protocol: docs/FERNANSICHT.md, remote.ts).
	import { onMount, untrack } from 'svelte';
	import Icon from '../components/Icon.svelte';
	import Dropdown from '../components/Dropdown.svelte';
	import { api } from '../api.ts';
	import { apiPath, apiSocket } from '../cluster-node.ts';
	import { formatBytes, formatPercent } from '../format.ts';
	import { t, type MessageKey } from '../i18n/index.svelte.ts';
	import {
		AUDIO_JITTER,
		AUDIO_MAX_AHEAD,
		chunkRanges,
		cursorCss,
		fitRect,
		inputMessages,
		isDesktopShortcut,
		isHalfKey,
		macOptionChar,
		fitText,
		isPaste,
		keyMessage,
		loadView,
		mapPoint,
		MAX_DECODE_QUEUE,
		MSG_AUDIO,
		MSG_VIDEO,
		missingSupport,
		parseAudio,
		parseMessage,
		parseVideo,
		QUALITIES,
		sanitizeStatus,
		saveView,
		sizeMessage,
		UPLOAD_HIGH_WATER,
		uploadChunk,
		viewerError,
		wheelDelta,
		type ByeReason,
		type Gpu,
		type Quality,
		type ServerMessage,
		type ImageStatus,
		type ViewerError,
	} from './remote.ts';

	let {
		app,
		title,
		phone = false,
		onopentab,
		onback,
		inTitle = false,
		menuHost = null,
	}: {
		/** The Docker app (compose project) as the NAS names it. */
		app: string;
		title: string;
		/** Full screen on the phone with its own bar (draft 03). */
		phone?: boolean;
		onopentab: () => void;
		onback?: () => void;
		/** PLAN 66: in a window the controls live in the window's ⋯ menu, not in a bar. */
		inTitle?: boolean;
		/** The open ⋯ menu of the window the controls are put into. */
		menuHost?: HTMLElement | null;
	} = $props();

	/** Moves the menu's part into the window's ⋯ menu (it stays this component's). */
	function portal(node: HTMLElement, target: HTMLElement) {
		target.appendChild(node);
		return { destroy: () => node.remove() };
	}
	let section = $state<'' | 'files' | 'clip' | 'quality'>('');

	type Phase =
		| 'connecting'
		| 'live'
		| 'image'
		| 'memory'
		| 'hostnet'
		| 'ended'
		| 'unsupported'
		| 'insecure'
		| 'codec';
	let phase = $state<Phase>('connecting');
	// The text of the end: a bye reason or a refusal code from titand.
	let endKey = $state<MessageKey>('viewer.bye.error');
	let hello = $state<{ width: number; height: number; gpu: Gpu; audio: boolean; codec: string }>();
	let stats = $state<{ fps: number; latencyMs: number }>();
	let painted = $state(false);
	let image = $state<ImageStatus>();
	const prefs = $state(untrack(() => loadView(app)));
	let noAudio = $state(false);
	let remoteClip = $state('');
	let clipDraft = $state('');
	let clipSent = $state(false);
	let copied = $state(false);
	let downloads = $state<{ id: string; name: string; size: number }[]>([]);
	let uploads = $state<
		{
			id: number;
			name: string;
			size: number;
			sent: number;
			state: 'run' | 'done' | 'failed';
			error?: string;
		}[]
	>([]);
	let cursor = $state('default');
	let box = $state({ w: 0, h: 0 });
	let showVolume = $state(false);

	let screen = $state<HTMLDivElement>();
	let canvas = $state<HTMLCanvasElement>();
	let ime = $state<HTMLInputElement>();
	let fileInput = $state<HTMLInputElement>();

	// Not reactive: the connection, decoders and audio graph.
	let ws: WebSocket | null = null;
	let decoder: VideoDecoder | null = null;
	let videoConfig: VideoDecoderConfig | null = null;
	let supportedCodec = '';
	let waitKey = true;
	let droppedKey = false;
	let lastKeyAsk = 0;
	let pending: VideoFrame | null = null;
	let raf = 0;
	let audioDecoder: AudioDecoder | null = null;
	let audioCtx: AudioContext | null = null;
	let gain: GainNode | null = null;
	let nextAudio = 0;
	let liveSince = 0;
	let retried = false;
	let closed = false;
	let uploadSeq = 0;
	let sizeTimer = 0;
	let imageTimer = 0;
	let lastMouse = { x: 0, y: 0, buttons: 0 };
	const held = new Set<string>();
	const FPS: (30 | 60)[] = [30, 60];

	const shown = $derived(
		hello
			? fitRect(box.w, box.h, hello.width, hello.height)
			: { left: 0, top: 0, width: 0, height: 0 },
	);
	const live = $derived(phase === 'live');
	const stateWord = $derived(
		phase === 'live'
			? ''
			: phase === 'connecting'
				? t('viewer.state.connecting')
				: t('viewer.state.ended'),
	);
	const gpuText = $derived(hello ? t(`viewer.gpu.${hello.gpu}` as MessageKey) : '');
	const statsText = $derived(
		stats && hello
			? t('viewer.stats', {
					fps: Math.round(stats.fps),
					latency: Math.round(stats.latencyMs),
					gpu: gpuText,
				})
			: gpuText,
	);
	const volumeText = $derived(formatPercent(prefs.muted ? 0 : prefs.volume * 100));
	const quality = $derived<Quality>(prefs.quality ?? 'auto');

	function send(msg: object) {
		if (ws?.readyState === WebSocket.OPEN) ws.send(JSON.stringify(msg));
	}

	// ---- Connection ----

	function connect() {
		stop();
		closed = false;
		phase = 'connecting';
		painted = false;
		const socket = new WebSocket(apiSocket(`/viewer/${encodeURIComponent(app)}/stream`));
		socket.binaryType = 'arraybuffer';
		ws = socket;
		let opened = false;
		let ended = false;
		socket.onopen = () => {
			opened = true;
			sendSize();
			if (prefs.quality || prefs.fps)
				send({ t: 'quality', mode: prefs.quality ?? 'auto', fps: prefs.fps ?? 60 });
		};
		socket.onmessage = (event) => {
			if (typeof event.data === 'string') {
				const msg = parseMessage(event.data);
				if (msg?.t === 'bye' || msg?.t === 'error') ended = true;
				if (msg) void onText(msg);
				return;
			}
			const buf = event.data as ArrayBuffer;
			if (!buf.byteLength) return;
			const kind = new DataView(buf).getUint8(0);
			if (kind === MSG_VIDEO) onVideo(buf);
			else if (kind === MSG_AUDIO) onAudio(buf);
		};
		socket.onclose = (event) => {
			if (ws !== socket) return;
			ws = null;
			releaseInput(false);
			if (closed || ended) return;
			const code = viewerError(event.reason);
			if (code) return refuse(code);
			if (!opened) return void refused();
			// A network drop: one new attempt, unless the last one was just now.
			if (phase === 'live' && (!retried || Date.now() - liveSince > 30_000)) {
				retried = true;
				phase = 'connecting';
				setTimeout(() => !closed && connect(), 1000);
				return;
			}
			end('lost');
		};
	}

	/** Ends the stream (window closed, reconnect, a final state). */
	function stop() {
		closed = true;
		if (ws) {
			const socket = ws;
			ws = null;
			socket.onclose = null;
			socket.close();
		}
		closeVideo();
		closeAudio();
		clearTimeout(imageTimer);
	}

	function end(reason: ByeReason | 'lost' | 'refused') {
		endKey = `viewer.bye.${reason}` as MessageKey;
		phase = 'ended';
		stop();
	}

	// titand refused the view ({"t":"error"} and the close reason).
	function refuse(code: ViewerError, loading?: ImageStatus) {
		stop();
		if (code === 'viewer_image') {
			phase = 'image';
			image = loading;
			// A failed download is not retried in a loop (titand tries at most once a minute).
			if (loading?.state !== 'error') imageTimer = window.setTimeout(watchImage, 2000);
		} else if (code === 'viewer_memory') phase = 'memory';
		else if (code === 'viewer_hostnet') phase = 'hostnet';
		else {
			endKey = `viewer.err.${code}` as MessageKey;
			phase = 'ended';
		}
	}

	// Refused before it began: a refused upgrade carries no reason, so the
	// status says whether the viewer program is still missing.
	async function refused() {
		try {
			const status = sanitizeStatus(await api.get('/viewer/status', AbortSignal.timeout(8000)));
			if (status.image.state !== 'ready') return refuse('viewer_image');
		} catch {
			// No status (older NAS, no right): the generic message.
		}
		end('refused');
	}

	// The viewer program is loading: progress every 2 s, then connect.
	async function watchImage() {
		clearTimeout(imageTimer);
		if (phase !== 'image') return;
		try {
			const status = sanitizeStatus(await api.get('/viewer/status', AbortSignal.timeout(8000)));
			image = status.image;
			if (status.image.state === 'ready') {
				image = undefined;
				return connect();
			}
			if (status.image.state === 'error') return;
		} catch {
			// Try again in a moment; the window shows the last known state.
		}
		if (phase === 'image') imageTimer = window.setTimeout(watchImage, 2000);
	}

	async function onText(msg: ServerMessage) {
		switch (msg.t) {
			case 'hello':
				hello = {
					width: msg.width,
					height: msg.height,
					gpu: msg.gpu,
					audio: msg.audio,
					codec: msg.codec,
				};
				await configureVideo(msg.codec, msg.width, msg.height);
				if (msg.audio) configureAudio();
				else closeAudio();
				if (phase === 'connecting') {
					phase = 'live';
					liveSince = Date.now();
				}
				break;
			case 'stats':
				stats = { fps: msg.fps, latencyMs: msg.latencyMs };
				break;
			case 'clipboard':
				remoteClip = msg.text;
				copied = false;
				break;
			case 'download':
				downloads = [{ id: msg.id, name: msg.name, size: msg.size }, ...downloads].slice(0, 20);
				break;
			case 'cursor':
				cursor = cursorCss(msg.shape);
				break;
			case 'uploaded': {
				const u = uploads.find((x) => x.id === msg.id);
				if (u && msg.error) {
					// The NAS refused it: too big, no room left or a bad name.
					u.state = 'failed';
					u.error = msg.error;
				} else if (u) {
					u.state = 'done';
					u.sent = u.size;
				}
				break;
			}
			case 'bye':
				end(msg.reason);
				break;
			case 'error':
				refuse(viewerError(msg.code) ?? 'viewer_error', msg.image);
				break;
		}
	}

	// ---- Video ----

	let generation = 0;
	async function configureVideo(codec: string, width: number, height: number) {
		// Two hellos in a row: only the newest sets up a decoder.
		const mine = ++generation;
		closeVideo();
		waitKey = true;
		droppedKey = false;
		if (codec !== supportedCodec) {
			const base: VideoDecoderConfig = { codec, optimizeForLatency: true };
			let config: VideoDecoderConfig | null = { ...base, hardwareAcceleration: 'prefer-hardware' };
			try {
				if (!(await VideoDecoder.isConfigSupported(config)).supported) {
					config = (await VideoDecoder.isConfigSupported(base)).supported ? base : null;
				}
			} catch {
				config = null;
			}
			if (mine !== generation) return;
			if (!config) {
				phase = 'codec';
				stop();
				return;
			}
			videoConfig = config;
			supportedCodec = codec;
		}
		if (closed || mine !== generation) return;
		if (canvas) {
			canvas.width = width;
			canvas.height = height;
		}
		openDecoder();
		// A key frame went by while the decoder was being set up.
		if (droppedKey) askKeyframe(true);
	}

	function openDecoder() {
		if (!videoConfig) return;
		decoder = new VideoDecoder({
			output: (frame) => {
				pending?.close();
				pending = frame;
				if (!raf) raf = requestAnimationFrame(draw);
			},
			error: () => {
				// Broken stream or lost GPU: a fresh decoder and a key frame.
				decoder = null;
				if (closed) return;
				openDecoder();
				waitKey = true;
				askKeyframe(true);
			},
		});
		decoder.configure(videoConfig);
	}

	function closeVideo() {
		if (decoder && decoder.state !== 'closed') decoder.close();
		decoder = null;
		pending?.close();
		pending = null;
		if (raf) cancelAnimationFrame(raf);
		raf = 0;
	}

	function askKeyframe(now = false) {
		const at = Date.now();
		if (!now && at - lastKeyAsk < 1000) return;
		lastKeyAsk = at;
		send({ t: 'keyframe' });
	}

	function onVideo(buf: ArrayBuffer) {
		const packet = parseVideo(buf);
		if (!packet) return;
		if (!decoder || decoder.state !== 'configured') {
			if (packet.key) droppedKey = true;
			return;
		}
		if (waitKey && !packet.key) return askKeyframe();
		// Behind: drop until the next key frame (delta frames depend on each other).
		if (!packet.key && decoder.decodeQueueSize > MAX_DECODE_QUEUE) {
			waitKey = true;
			return askKeyframe();
		}
		waitKey = false;
		try {
			decoder.decode(
				new EncodedVideoChunk({
					type: packet.key ? 'key' : 'delta',
					timestamp: packet.timestamp,
					data: packet.data,
				}),
			);
		} catch {
			waitKey = true;
			askKeyframe(true);
		}
	}

	// Only the newest decoded picture is drawn, once per screen refresh.
	function draw() {
		raf = 0;
		const frame = pending;
		pending = null;
		if (!frame) return;
		const ctx = canvas?.getContext('2d');
		if (ctx && canvas) {
			ctx.drawImage(frame, 0, 0, canvas.width, canvas.height);
			painted = true;
		}
		frame.close();
	}

	// ---- Sound ----

	function configureAudio() {
		if (audioDecoder) return;
		if (typeof AudioDecoder !== 'function') {
			noAudio = true;
			return;
		}
		try {
			audioDecoder = new AudioDecoder({
				output: play,
				error: () => {
					audioDecoder = null;
				},
			});
			audioDecoder.configure({ codec: 'opus', sampleRate: 48000, numberOfChannels: 2 });
		} catch {
			audioDecoder = null;
			noAudio = true;
		}
	}

	function closeAudio() {
		if (audioDecoder && audioDecoder.state !== 'closed') audioDecoder.close();
		audioDecoder = null;
	}

	/** The AudioContext may only start after a gesture of the user (autoplay rules). */
	function startAudio() {
		if (!hello?.audio || noAudio) return;
		try {
			if (!audioCtx) {
				audioCtx = new AudioContext({ sampleRate: 48000, latencyHint: 'interactive' });
				gain = audioCtx.createGain();
				gain.connect(audioCtx.destination);
				applyVolume();
			}
			if (audioCtx.state === 'suspended') void audioCtx.resume();
		} catch {
			noAudio = true;
		}
	}

	function applyVolume() {
		if (gain) gain.gain.value = prefs.muted ? 0 : prefs.volume;
	}

	function onAudio(buf: ArrayBuffer) {
		if (!audioDecoder || audioDecoder.state !== 'configured') return;
		if (!audioCtx || audioCtx.state !== 'running' || prefs.muted) return;
		const packet = parseAudio(buf);
		if (!packet) return;
		try {
			audioDecoder.decode(
				new EncodedAudioChunk({ type: 'key', timestamp: packet.timestamp, data: packet.data }),
			);
		} catch {
			// One lost packet is 20 ms of silence.
		}
	}

	// A small jitter buffer: play ~60 ms behind, never further ahead than 250 ms.
	function play(data: AudioData) {
		const ctx = audioCtx;
		if (!ctx || !gain) return data.close();
		try {
			const buffer = ctx.createBuffer(data.numberOfChannels, data.numberOfFrames, data.sampleRate);
			for (let c = 0; c < data.numberOfChannels; c++)
				data.copyTo(buffer.getChannelData(c), { planeIndex: c, format: 'f32-planar' });
			const now = ctx.currentTime;
			if (nextAudio < now + 0.005) nextAudio = now + AUDIO_JITTER;
			if (nextAudio > now + AUDIO_MAX_AHEAD) return;
			const source = ctx.createBufferSource();
			source.buffer = buffer;
			source.connect(gain);
			source.start(nextAudio);
			nextAudio += buffer.duration;
		} catch {
			// Skip this packet.
		} finally {
			data.close();
		}
	}

	function setVolume(value: number) {
		prefs.volume = Math.min(1, Math.max(0, value));
		if (prefs.volume > 0) prefs.muted = false;
		applyVolume();
		saveView(app, $state.snapshot(prefs));
	}

	function toggleMute() {
		startAudio();
		prefs.muted = !prefs.muted;
		applyVolume();
		saveView(app, $state.snapshot(prefs));
	}

	function setQuality(mode: Quality, fps: 30 | 60) {
		prefs.quality = mode;
		prefs.fps = fps;
		saveView(app, $state.snapshot(prefs));
		send({ t: 'quality', mode, fps });
	}

	// ---- Size ----

	function sendSize() {
		clearTimeout(sizeTimer);
		if (box.w > 0 && box.h > 0) send(sizeMessage(box.w, box.h, window.devicePixelRatio || 1));
	}

	// ---- Mouse, wheel and touch ----

	function point(event: { clientX: number; clientY: number }) {
		if (!canvas || !hello) return null;
		const r = canvas.getBoundingClientRect();
		return mapPoint(
			event.clientX,
			event.clientY,
			{ left: r.left, top: r.top, width: r.width, height: r.height },
			hello.width,
			hello.height,
		);
	}

	function mouse(x: number, y: number, buttons: number, force = false) {
		if (!force && x === lastMouse.x && y === lastMouse.y && buttons === lastMouse.buttons) return;
		lastMouse = { x, y, buttons };
		send({ t: 'mouse', x, y, buttons });
	}

	type Touch = { x: number; y: number; sx: number; sy: number };
	const touches = new Map<number, Touch>();
	let gesture: 'none' | 'tap' | 'pan' | 'done' = 'none';
	let longPress = 0;

	function pointerDown(event: PointerEvent) {
		if (!live) return;
		canvas?.focus({ preventScroll: true });
		startAudio();
		if (event.pointerType === 'touch') {
			event.preventDefault();
			touches.set(event.pointerId, {
				x: event.clientX,
				y: event.clientY,
				sx: event.clientX,
				sy: event.clientY,
			});
			canvas?.setPointerCapture(event.pointerId);
			if (touches.size === 1) {
				gesture = 'tap';
				const p = point(event);
				clearTimeout(longPress);
				// Long press = right click.
				longPress = window.setTimeout(() => {
					if (gesture !== 'tap' || !p) return;
					gesture = 'done';
					mouse(p.x, p.y, 0, true);
					mouse(p.x, p.y, 2);
					mouse(p.x, p.y, 0);
				}, 550);
			} else {
				clearTimeout(longPress);
				gesture = 'pan';
			}
			return;
		}
		event.preventDefault();
		canvas?.setPointerCapture(event.pointerId);
		const p = point(event);
		if (p) mouse(p.x, p.y, event.buttons);
	}

	function pointerMove(event: PointerEvent) {
		if (!live) return;
		if (event.pointerType === 'touch') {
			const touch = touches.get(event.pointerId);
			if (!touch) return;
			const dx = event.clientX - touch.x;
			const dy = event.clientY - touch.y;
			touch.x = event.clientX;
			touch.y = event.clientY;
			if (gesture === 'tap' && Math.hypot(touch.x - touch.sx, touch.y - touch.sy) > 10) {
				clearTimeout(longPress);
				gesture = 'pan';
			}
			// Swiping scrolls: the content follows the finger.
			if (gesture === 'pan' && (dx || dy)) {
				const n = touches.size;
				send({ t: 'wheel', dx: Math.round(-dx / n), dy: Math.round(-dy / n) });
			}
			return;
		}
		const p = point(event);
		if (p) mouse(p.x, p.y, event.buttons);
	}

	function pointerUp(event: PointerEvent) {
		if (event.pointerType === 'touch') {
			const touch = touches.get(event.pointerId);
			touches.delete(event.pointerId);
			if (touch && gesture === 'tap' && touches.size === 0) {
				clearTimeout(longPress);
				const p = point(event);
				// Tap = click.
				if (p) {
					mouse(p.x, p.y, 0, true);
					mouse(p.x, p.y, 1);
					mouse(p.x, p.y, 0);
				}
			}
			if (!touches.size) gesture = 'none';
			return;
		}
		if (!live) return;
		const p = point(event);
		if (p) mouse(p.x, p.y, event.buttons);
	}

	function wheel(event: WheelEvent) {
		if (!live) return;
		event.preventDefault();
		const d = wheelDelta(event.deltaX, event.deltaY, event.deltaMode);
		if (d.dx || d.dy) send({ t: 'wheel', ...d });
	}

	// ---- Keyboard and clipboard ----

	let pasteKey: KeyboardEvent | null = null;
	let pasteTimer = 0;

	function keyDown(event: KeyboardEvent) {
		if (!live) return;
		// Alt + Q/W/M, Alt + arrows and Ctrl + Space belong to the desktop.
		if (isDesktopShortcut(event)) return;
		startAudio();
		if (isPaste(event)) {
			// The browser's paste event brings the text; the key follows it.
			pasteKey = event;
			clearTimeout(pasteTimer);
			pasteTimer = window.setTimeout(() => pasteKey && pressPaste(), 200);
			return;
		}
		event.preventDefault();
		// Dead keys and IME input: the finished character follows.
		if (isHalfKey(event)) return;
		held.add(event.code);
		send(remoteKey(event, true));
	}

	const mac = /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent);
	/** A key as the NAS gets it; Option characters of a Mac go without Alt. */
	function remoteKey(event: KeyboardEvent, down: boolean) {
		const msg = keyMessage(event, down);
		if (macOptionChar(event, mac)) msg.mods.alt = false;
		return msg;
	}

	function keyUp(event: KeyboardEvent) {
		if (!held.has(event.code)) return;
		event.preventDefault();
		held.delete(event.code);
		send(remoteKey(event, false));
	}

	function pressPaste() {
		const event = pasteKey;
		pasteKey = null;
		clearTimeout(pasteTimer);
		if (!event) return;
		send(keyMessage(event, true));
		send(keyMessage(event, false));
	}

	function paste(event: ClipboardEvent) {
		if (!live) return;
		event.preventDefault();
		const text = event.clipboardData?.getData('text/plain') ?? '';
		if (text) send(fitText('clipboard', text));
		if (pasteKey) pressPaste();
	}

	/** Lets go of held keys and buttons, so nothing stays pressed on the NAS (e.g. Alt after Alt + Q). */
	function releaseInput(sendUp = true) {
		if (sendUp)
			for (const code of held)
				send({
					t: 'key',
					code,
					key: '',
					down: false,
					mods: { shift: false, ctrl: false, alt: false, meta: false },
				});
		held.clear();
		if (sendUp && lastMouse.buttons) mouse(lastMouse.x, lastMouse.y, 0);
		else lastMouse.buttons = 0;
	}

	async function copyRemote() {
		try {
			await navigator.clipboard.writeText(remoteClip);
			copied = true;
		} catch {
			copied = false;
		}
	}

	function sendClip(event: SubmitEvent) {
		event.preventDefault();
		if (!clipDraft) return;
		send(fitText('clipboard', clipDraft));
		clipSent = true;
	}

	// The phone's keyboard types into a hidden field; its input goes over as text and keys.
	const SENTINEL = '​​';
	function imeBefore(event: InputEvent) {
		if (event.isComposing || event.inputType === 'insertCompositionText') return;
		const out = inputMessages(event.inputType, event.data);
		if (!out.length) return;
		event.preventDefault();
		for (const m of out) send(m.t === 'text' ? fitText('text', m.text) : m);
	}
	function imeComposed(event: CompositionEvent) {
		if (event.data) send(fitText('text', event.data));
		if (ime) ime.value = SENTINEL;
	}
	function imeKey(event: KeyboardEvent) {
		// Hardware and iOS keyboards report real keys; Android only 229.
		if (event.keyCode === 229 || event.isComposing) return;
		if (event.key === 'Backspace' || event.key === 'Enter' || event.key === 'Tab') {
			event.preventDefault();
			send(keyMessage(event, true));
			send(keyMessage(event, false));
		}
	}
	function openKeyboard() {
		if (!ime) return;
		ime.value = SENTINEL;
		ime.focus();
		startAudio();
	}

	// ---- Uploads ----

	async function upload(files: FileList | null) {
		for (const file of Array.from(files ?? [])) {
			const id = ++uploadSeq;
			uploads = [...uploads, { id, name: file.name, size: file.size, sent: 0, state: 'run' }];
			const item = uploads[uploads.length - 1];
			const socket = ws;
			if (!socket || socket.readyState !== WebSocket.OPEN) {
				item.state = 'failed';
				continue;
			}
			socket.send(JSON.stringify({ t: 'upload', id, name: file.name, size: file.size }));
			for (const [from, to] of chunkRanges(file.size)) {
				// The picture keeps flowing: never more than ~1 MB waiting in the socket.
				while (socket.readyState === WebSocket.OPEN && socket.bufferedAmount > UPLOAD_HIGH_WATER)
					await new Promise((r) => setTimeout(r, 30));
				if (socket.readyState !== WebSocket.OPEN) {
					item.state = 'failed';
					break;
				}
				const data = new Uint8Array(await file.slice(from, to).arrayBuffer());
				socket.send(uploadChunk(id, data));
				item.sent = to;
			}
		}
		if (fileInput) fileInput.value = '';
	}

	// ---- Life cycle ----

	onMount(() => {
		const why = missingSupport(globalThis as never);
		if (why) {
			phase = why === 'insecure' ? 'insecure' : 'unsupported';
			return;
		}
		connect();
		return () => {
			stop();
			releaseInput(false);
			void audioCtx?.close();
			audioCtx = null;
			clearTimeout(sizeTimer);
			clearTimeout(longPress);
		};
	});

	// The visible area follows the window; the NAS adapts its screen 300 ms after the last change.
	$effect(() => {
		if (!screen) return;
		const observer = new ResizeObserver(([entry]) => {
			const r = entry.contentRect;
			box = { w: r.width, h: r.height };
			clearTimeout(sizeTimer);
			sizeTimer = window.setTimeout(sendSize, 300);
		});
		observer.observe(screen);
		return () => observer.disconnect();
	});

	function reconnect() {
		retried = false;
		connect();
	}
</script>

{#snippet volumeControl()}
	<button
		type="button"
		class="icon-btn"
		onclick={toggleMute}
		aria-pressed={prefs.muted}
		title={prefs.muted ? t('viewer.unmute') : t('viewer.mute')}
		aria-label={prefs.muted ? t('viewer.unmute') : t('viewer.mute')}
	>
		<Icon name="volume" size={16} />
		{#if prefs.muted}<span class="slash" aria-hidden="true"></span>{/if}
	</button>
	<input
		class="volume"
		type="range"
		min="0"
		max="100"
		step="1"
		value={Math.round((prefs.muted ? 0 : prefs.volume) * 100)}
		aria-label={t('viewer.volume')}
		aria-valuetext={volumeText}
		oninput={(e) => {
			startAudio();
			setVolume(Number(e.currentTarget.value) / 100);
		}}
	/>
	<span class="pct mono">{volumeText}</span>
{/snippet}

{#snippet qualityItems(close: () => void)}
	{#each QUALITIES as q (q)}
		<button
			type="button"
			role="menuitemradio"
			aria-checked={quality === q}
			onclick={() => {
				setQuality(q, prefs.fps ?? 60);
				close();
			}}><span>{t(`viewer.q.${q}` as MessageKey)}</span></button
		>
	{/each}
	<hr />
	{#each FPS as fps (fps)}
		<button
			type="button"
			role="menuitemradio"
			aria-checked={(prefs.fps ?? 60) === fps}
			onclick={() => {
				setQuality(quality, fps);
				close();
			}}><span>{t('viewer.fpsValue', { fps })}</span></button
		>
	{/each}
{/snippet}

{#snippet clipboardPanel()}
	<div class="clip">
		<b>{t('viewer.clipboardRemote')}</b>
		{#if remoteClip}
			<pre class="remote-clip">{remoteClip}</pre>
			<button type="button" class="btn" onclick={copyRemote}
				><Icon name="copy" size={15} />{copied ? t('viewer.copied') : t('viewer.copy')}</button
			>
		{:else}<p class="muted">{t('viewer.clipboardEmpty')}</p>{/if}
	</div>
	<hr />
	<form class="clip" onsubmit={sendClip}>
		<label class="stack"
			><span>{t('viewer.clipboardField')}</span>
			<textarea class="field" rows="3" bind:value={clipDraft} oninput={() => (clipSent = false)}
			></textarea>
		</label>
		<button type="submit" class="btn" disabled={!clipDraft || !live}
			>{t('viewer.clipboardSend')}</button
		>
		{#if clipSent}<p class="muted small" role="status">{t('viewer.clipboardSent')}</p>{/if}
	</form>
{/snippet}

{#snippet filesPanel()}
	<button type="button" class="btn" disabled={!live} onclick={() => fileInput?.click()}
		><Icon name="upload" size={15} />{t('viewer.upload')}</button
	>
	<p class="muted small">{t('viewer.uploadHint')}</p>
	{#if uploads.length}
		<ul class="list">
			{#each uploads as u (u.id)}
				<li>
					<span class="name">{u.name}</span>
					<span class="muted small" role="status"
						>{u.state === 'done'
							? t('viewer.uploadDone')
							: u.state === 'failed'
								? u.error
									? t(`viewer.uploadError.${u.error}` as 'viewer.uploadError.failed')
									: t('viewer.uploadFailed')
								: formatPercent(u.size ? (100 * u.sent) / u.size : 100)}</span
					>
					{#if u.state === 'run'}<progress max={u.size || 1} value={u.sent}></progress>{/if}
				</li>
			{/each}
		</ul>
	{/if}
	<hr />
	<b>{t('viewer.downloads')}</b>
	{#if downloads.length}
		<ul class="list">
			{#each downloads as d (d.id)}
				<li>
					<a
						href={apiPath(
							`/viewer/${encodeURIComponent(app)}/downloads/${encodeURIComponent(d.id)}`,
						)}
						download={d.name}
						><Icon name="download" size={14} /> <span class="name">{d.name}</span></a
					>
					<span class="muted small">{formatBytes(d.size)}</span>
				</li>
			{/each}
		</ul>
	{:else}<p class="muted small">{t('viewer.noDownloads')}</p>{/if}
{/snippet}

<div class="remote" class:phone>
	{#if phone}
		<header class="phone-head">
			<button
				type="button"
				class="icon-btn big"
				onclick={onback}
				aria-label={t('desktop.phone.home')}><Icon name="chevronLeft" size={22} /></button
			>
			<b class="title">{title}</b>
			<span class="pill" class:live class:ended={!live && phase !== 'connecting'}
				><span class="dot" aria-hidden="true"></span>{t('viewer.pill')}{#if stateWord}<span
						class="sr-only">: {stateWord}</span
					>{/if}</span
			>
			<span class="fill"></span>
			<button
				type="button"
				class="icon-btn big"
				onclick={toggleMute}
				aria-pressed={prefs.muted}
				aria-label={prefs.muted ? t('viewer.unmute') : t('viewer.mute')}
				><Icon name="volume" size={19} />{#if prefs.muted}<span class="slash" aria-hidden="true"
					></span>{/if}</button
			>
			<button
				type="button"
				class="icon-btn big"
				onclick={openKeyboard}
				disabled={!live}
				aria-label={t('viewer.keyboard')}><Icon name="keyboard" size={19} /></button
			>
			<Dropdown label={t('viewer.more')} icon="more" align="right" kind="panel">
				{#snippet children(close)}
					<b>{t('viewer.quality')}</b>
					<div class="menu-list" role="menu">{@render qualityItems(close)}</div>
					<hr />
					{@render clipboardPanel()}
					<hr />
					{@render filesPanel()}
					<hr />
					<button type="button" class="btn" onclick={onopentab}
						><Icon name="external" size={15} />{t('desktop.window.openTab')}</button
					>
				{/snippet}
			</Dropdown>
		</header>
	{:else if inTitle}
		{#if menuHost}
			<div class="title-menu" use:portal={menuHost}>
				{#if hello?.audio && !noAudio}
					<span class="vol">{@render volumeControl()}</span>
				{:else if noAudio}<p class="muted small pad">{t('viewer.noAudio')}</p>{/if}
				<hr />
				<button type="button" role="menuitem" disabled={!live} onclick={() => fileInput?.click()}
					><Icon name="upload" size={16} /><span>{t('viewer.menu.open')}</span></button
				>
				<button
					type="button"
					role="menuitem"
					aria-expanded={section === 'files'}
					onclick={() => (section = section === 'files' ? '' : 'files')}
					><Icon name="download" size={16} /><span>{t('viewer.downloads')}</span><kbd
						>{downloads.length}</kbd
					></button
				>
				{#if section === 'files'}<div class="sub">{@render filesPanel()}</div>{/if}
				<button
					type="button"
					role="menuitem"
					aria-expanded={section === 'clip'}
					onclick={() => (section = section === 'clip' ? '' : 'clip')}
					><Icon name="paste" size={16} /><span>{t('viewer.clipboard')}</span></button
				>
				{#if section === 'clip'}<div class="sub">{@render clipboardPanel()}</div>{/if}
				<button
					type="button"
					role="menuitem"
					aria-expanded={section === 'quality'}
					onclick={() => (section = section === 'quality' ? '' : 'quality')}
					><Icon name="monitor" size={16} /><span>{t('viewer.quality')}</span><kbd
						>{t(`viewer.q.${quality}` as MessageKey)} ›</kbd
					></button
				>
				{#if section === 'quality'}<div class="sub" role="group">
						{@render qualityItems(() => (section = ''))}
					</div>{/if}
				<hr />
				<button type="button" role="menuitem" onclick={reconnect}
					><Icon name="restart" size={16} /><span>{t('viewer.reconnect')}</span></button
				>
				<p class="menu-line muted">
					<span class="pill" class:live class:ended={!live && phase !== 'connecting'}
						><span class="dot" aria-hidden="true"></span>{t('viewer.pill')}{#if stateWord}
							· {stateWord}{/if}</span
					>{#if live && statsText}<span class="stats">{statsText}</span>{/if}
				</p>
			</div>
		{/if}
	{:else}
		<div class="view-bar" role="toolbar" aria-label={t('viewer.toolbar', { app: title })}>
			<span class="pill" class:live class:ended={!live && phase !== 'connecting'}
				><span class="dot" aria-hidden="true"></span>{t('viewer.pill')}{#if stateWord}
					· {stateWord}{/if}</span
			>
			{#if live && statsText}<span class="stats muted">{statsText}</span>{/if}
			<span class="fill"></span>
			{#if hello?.audio && !noAudio}
				<span class="vol">{@render volumeControl()}</span>
			{:else if noAudio}<span class="muted small">{t('viewer.noAudio')}</span>{/if}
			<Dropdown
				label={t('viewer.quality')}
				text={t('viewer.qualityValue', { mode: t(`viewer.q.${quality}` as MessageKey) })}
				align="right"
				compact
				icon="monitor"
			>
				{#snippet children(close)}{@render qualityItems(close)}{/snippet}
			</Dropdown>
			<Dropdown
				label={t('viewer.clipboard')}
				text={t('viewer.clipboard')}
				icon="paste"
				kind="panel"
				align="right"
				compact
			>
				{#snippet children()}{@render clipboardPanel()}{/snippet}
			</Dropdown>
			<Dropdown
				label={t('viewer.files')}
				text={t('viewer.files')}
				icon="download"
				kind="panel"
				align="right"
				compact
			>
				{#snippet children()}{@render filesPanel()}{/snippet}
			</Dropdown>
		</div>
	{/if}
	<input
		type="file"
		multiple
		hidden
		bind:this={fileInput}
		onchange={(e) => upload(e.currentTarget.files)}
	/>
	<div class="screen" bind:this={screen}>
		<canvas
			bind:this={canvas}
			class:hidden={!hello || phase !== 'live'}
			style:left="{shown.left}px"
			style:top="{shown.top}px"
			style:width="{shown.width}px"
			style:height="{shown.height}px"
			style:cursor
			tabindex="0"
			aria-label={t('viewer.screen', { app: title })}
			onpointerdown={pointerDown}
			onpointermove={pointerMove}
			onpointerup={pointerUp}
			onpointercancel={pointerUp}
			onwheel={wheel}
			onkeydown={keyDown}
			onkeyup={keyUp}
			onpaste={paste}
			onblur={() => releaseInput()}
			oncontextmenu={(e) => e.preventDefault()}
		></canvas>
		<input
			class="ime"
			bind:this={ime}
			aria-label={t('viewer.keyboardField')}
			autocomplete="off"
			autocapitalize="off"
			spellcheck="false"
			onbeforeinput={imeBefore}
			oncompositionend={imeComposed}
			onkeydown={imeKey}
			oninput={(e) => {
				if (!(e as unknown as InputEvent).isComposing) e.currentTarget.value = SENTINEL;
			}}
		/>
		{#if phase === 'connecting' || (phase === 'live' && !painted)}
			<div class="note" role="status">
				<span class="spinner" aria-hidden="true"></span>
				<p>{t('viewer.starting')}</p>
			</div>
		{:else if phase === 'image'}
			<div class="note" role="status">
				<Icon name="download" size={36} />
				<h3>{t('viewer.image.title')}</h3>
				<p>{t('viewer.image.text')}</p>
				{#if image?.state === 'error'}
					<p class="error-text">{t('viewer.image.failed', { error: image.error })}</p>
					<button type="button" class="btn" onclick={reconnect}>{t('common.retry')}</button>
				{:else if image && image.size > 0}
					<progress max={image.size} value={image.bytes}></progress>
					<p class="muted small">
						{t('viewer.image.progress', {
							done: formatBytes(image.bytes),
							total: formatBytes(image.size),
						})}
					</p>
				{:else}<span class="spinner" aria-hidden="true"></span>{/if}
			</div>
		{:else if phase !== 'live'}
			<div class="note" role="alert">
				<Icon name="monitor" size={36} />
				{#if phase === 'memory'}
					<h3>{t('viewer.memory.title')}</h3>
					<p>{t('viewer.memory.text')}</p>
				{:else if phase === 'hostnet'}
					<h3>{t('viewer.hostnet.title')}</h3>
					<p>{t('viewer.hostnet.text')}</p>
				{:else if phase === 'unsupported' || phase === 'insecure'}
					<h3>{t('viewer.unsupported.title')}</h3>
					<p>{t(phase === 'insecure' ? 'viewer.insecure.text' : 'viewer.unsupported.text')}</p>
				{:else if phase === 'codec'}
					<h3>{t('viewer.unsupported.title')}</h3>
					<p>{t('viewer.codec.text', { codec: hello?.codec ?? '' })}</p>
				{:else}
					<h3>{t('viewer.ended.title')}</h3>
					<p>{t(endKey)}</p>
				{/if}
				<div class="actions">
					{#if phase === 'ended' || phase === 'memory'}
						<button type="button" class="btn btn-primary" onclick={reconnect}
							>{t('viewer.reconnect')}</button
						>
					{/if}
					<button type="button" class="btn" onclick={onopentab}
						>{t('desktop.window.openTab')}</button
					>
				</div>
			</div>
		{/if}
	</div>
	{#if phone && live}
		<div class="chips">
			{#if hello?.audio && !noAudio}
				<button
					type="button"
					class="chip-btn"
					aria-expanded={showVolume}
					onclick={() => {
						startAudio();
						showVolume = !showVolume;
					}}>{t('viewer.volumeValue', { value: volumeText })}</button
				>
			{/if}
			<button type="button" class="chip-btn" onclick={openKeyboard}>{t('viewer.keyboard')}</button>
			{#if showVolume}
				<div class="volume-sheet">{@render volumeControl()}</div>
			{/if}
		</div>
	{/if}
</div>

<style>
	.remote {
		position: relative;
		flex: 1;
		min-width: 0;
		min-height: 0;
		display: flex;
		flex-direction: column;
		background: #000;
		container-type: inline-size;
	}
	/* PLAN 66: the part of the window's ⋯ menu. */
	.title-menu {
		display: flex;
		flex-direction: column;
		gap: 2px;
		max-height: min(70vh, 560px);
		overflow-y: auto;
	}
	.title-menu .vol {
		display: flex;
		align-items: center;
		gap: 8px;
		padding: 4px 10px;
	}
	.title-menu .sub {
		display: flex;
		flex-direction: column;
		gap: 8px;
		margin: 2px 0 6px 26px;
		padding: 6px 0 6px 10px;
		border-left: 2px solid var(--border);
	}
	.title-menu .menu-line {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 4px 10px;
		margin: 6px 10px 2px;
		font-size: var(--text-xs);
	}
	.title-menu .pad {
		margin: 4px 10px;
	}
	.view-bar,
	.phone-head {
		flex: none;
		display: flex;
		align-items: center;
		gap: 8px;
		min-height: 40px;
		padding: 4px 10px;
		background: var(--header);
		border-bottom: 1px solid var(--border);
		color: var(--text);
		font-size: var(--text-sm);
		user-select: none;
		-webkit-user-select: none;
	}
	.view-bar :global(.dropdown-panel),
	.phone-head :global(.dropdown-panel) {
		user-select: text;
		-webkit-user-select: text;
	}
	.view-bar :global(.btn) {
		min-height: 1.875rem;
		height: 1.875rem;
		font-size: var(--text-sm);
	}
	.phone-head {
		min-height: calc(52px + env(safe-area-inset-top));
		padding: env(safe-area-inset-top) 6px 0;
		gap: 4px;
	}
	.title {
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
		min-width: 0;
	}
	.fill {
		flex: 1;
	}
	.pill {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		padding: 2px 10px;
		border-radius: 999px;
		background: var(--chip);
		color: var(--muted);
		font-weight: 600;
		white-space: nowrap;
	}
	.pill.live {
		background: color-mix(in srgb, var(--ok-text) 16%, transparent);
		color: var(--ok-text);
	}
	.pill.ended {
		color: var(--muted);
	}
	.dot {
		width: 7px;
		height: 7px;
		border-radius: 50%;
		background: currentColor;
	}
	.stats {
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
		min-width: 0;
	}
	.vol {
		display: inline-flex;
		align-items: center;
		gap: 6px;
	}
	.volume {
		width: 110px;
	}
	.pct {
		width: 3.2em;
		text-align: right;
		color: var(--muted);
	}
	.icon-btn {
		position: relative;
		display: grid;
		place-items: center;
		width: 30px;
		height: 30px;
		border: 0;
		border-radius: 7px;
		background: none;
		color: var(--text);
		cursor: pointer;
		flex: none;
	}
	.icon-btn.big {
		width: 40px;
		height: 40px;
	}
	.icon-btn:hover {
		background: color-mix(in srgb, var(--text) 9%, transparent);
	}
	.icon-btn:disabled {
		opacity: 0.5;
	}
	.slash {
		position: absolute;
		width: 2px;
		height: 60%;
		background: currentColor;
		transform: rotate(45deg);
	}
	.screen {
		position: relative;
		flex: 1;
		min-height: 0;
		overflow: hidden;
	}
	canvas {
		position: absolute;
		display: block;
		outline: none;
		touch-action: none;
		user-select: none;
		-webkit-user-select: none;
		-webkit-touch-callout: none;
	}
	canvas.hidden {
		visibility: hidden;
	}
	canvas:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
	.ime {
		position: absolute;
		left: 0;
		bottom: 0;
		width: 1px;
		height: 1px;
		opacity: 0;
		border: 0;
		padding: 0;
		font-size: 16px;
		pointer-events: none;
	}
	.note {
		position: absolute;
		inset: 0;
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: 12px;
		padding: 30px;
		text-align: center;
		background: var(--bg);
		color: var(--text);
	}
	.note :global(svg) {
		color: var(--muted);
	}
	.note h3 {
		margin: 0;
	}
	.note p {
		margin: 0;
		max-width: 480px;
		color: var(--muted);
		line-height: 1.5;
	}
	.note progress {
		width: min(320px, 80%);
	}
	.error-text {
		color: var(--error-text) !important;
	}
	.actions {
		display: flex;
		gap: 10px;
		flex-wrap: wrap;
		justify-content: center;
	}
	.spinner {
		width: 28px;
		height: 28px;
		border-radius: 50%;
		border: 3px solid color-mix(in srgb, var(--text) 18%, transparent);
		border-top-color: var(--accent);
		animation: spin 0.9s linear infinite;
	}
	@keyframes spin {
		to {
			transform: rotate(360deg);
		}
	}
	.clip {
		display: flex;
		flex-direction: column;
		gap: 8px;
		align-items: flex-start;
	}
	.clip label {
		align-self: stretch;
	}
	.remote-clip {
		margin: 0;
		max-height: 8rem;
		max-width: 100%;
		overflow: auto;
		white-space: pre-wrap;
		overflow-wrap: anywhere;
		font-size: var(--text-sm);
		padding: 8px;
		border-radius: var(--radius-small);
		background: var(--chip);
		align-self: stretch;
	}
	.list {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: 6px;
	}
	.list li {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 4px 8px;
	}
	.list .name {
		flex: 1;
		min-width: 0;
		overflow-wrap: anywhere;
	}
	.list a {
		display: inline-flex;
		align-items: center;
		gap: 4px;
		flex: 1;
		min-width: 0;
	}
	.list progress {
		width: 100%;
	}
	.menu-list {
		display: flex;
		flex-direction: column;
	}
	.muted {
		color: var(--muted);
	}
	.small {
		font-size: var(--text-sm);
	}
	.chips {
		position: absolute;
		left: 0;
		right: 0;
		bottom: calc(18px + env(safe-area-inset-bottom));
		display: flex;
		flex-wrap: wrap;
		justify-content: center;
		gap: 10px;
		pointer-events: none;
	}
	.chip-btn,
	.volume-sheet {
		pointer-events: auto;
		border: 1px solid var(--border);
		border-radius: 999px;
		background: var(--surface);
		color: var(--text);
		box-shadow: var(--shadow-menu);
	}
	.chip-btn {
		min-height: 36px;
		padding: 0 14px;
		font: inherit;
		font-size: var(--text-sm);
		cursor: pointer;
	}
	.volume-sheet {
		flex-basis: 100%;
		max-width: 320px;
		display: flex;
		align-items: center;
		gap: 8px;
		padding: 6px 12px;
	}
	.volume-sheet .volume {
		flex: 1;
	}
	@container (max-width: 760px) {
		.stats {
			display: none;
		}
	}
	@container (max-width: 600px) {
		.view-bar .pct,
		.view-bar .volume {
			display: none;
		}
	}
	@media (prefers-reduced-motion: reduce) {
		.spinner {
			animation: none;
		}
	}
</style>
