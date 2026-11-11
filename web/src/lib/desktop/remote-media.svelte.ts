// PLAN 63: the remote view's picture and sound. What titand sends is decoded
// here (WebCodecs video or tiles on a canvas, Opus through an AudioContext);
// the state the view shows lives here too. The connection, input and uploads
// are in remote-session.svelte.ts (protocol: docs/FERNANSICHT.md, remote.ts).
import { untrack } from 'svelte';
import { formatPercent } from '../format.ts';
import { t, type MessageKey } from '../i18n/index.svelte.ts';
import {
	AUDIO_JITTER,
	AUDIO_MAX_AHEAD,
	fitRect,
	loadView,
	MAX_DECODE_QUEUE,
	CODEC_TILES,
	parseAudio,
	parseTile,
	crispScale,
	parseVideo,
	saveView,
	type Gpu,
	type Quality,
	type Transport,
	type ImageStatus,
} from './remote.ts';

export type Phase =
	| 'connecting'
	| 'live'
	| 'image'
	| 'memory'
	| 'hostnet'
	| 'ended'
	| 'unsupported'
	| 'insecure'
	| 'codec';

export class RemoteMedia {
	readonly #app: () => string;
	/** The Docker app (compose project) as the NAS names it. */
	get app() {
		return this.#app();
	}
	phase = $state<Phase>('connecting');
	// The text of the end: a bye reason or a refusal code from titand.
	endKey = $state<MessageKey>('viewer.bye.error');
	// For administrators: why titand could not start the view (PLAN 75-8).
	endDetail = $state('');
	hello = $state<{ width: number; height: number; gpu: Gpu; audio: boolean; codec: string }>();
	stats = $state<{ fps: number; latencyMs: number }>();
	painted = $state(false);
	// PLAN 72: which transport the viewer uses right now.
	streamMode = $state<'tiles' | 'video'>('video');
	image = $state<ImageStatus>();
	prefs: ReturnType<typeof loadView>;
	noAudio = $state(false);
	remoteClip = $state('');
	clipDraft = $state('');
	clipSent = $state(false);
	copied = $state(false);
	downloads = $state<{ id: string; name: string; size: number }[]>([]);
	uploads = $state<
		{
			id: number;
			name: string;
			size: number;
			sent: number;
			state: 'run' | 'done' | 'failed';
			error?: string;
		}[]
	>([]);
	cursor = $state('default');
	box = $state({ w: 0, h: 0 });

	screen = $state<HTMLDivElement>();
	canvas = $state<HTMLCanvasElement>();
	ime = $state<HTMLInputElement>();
	fileInput = $state<HTMLInputElement>();

	// Not reactive: the connection, decoders and audio graph.
	ws: WebSocket | null = null;
	decoder: VideoDecoder | null = null;
	videoConfig: VideoDecoderConfig | null = null;
	supportedCodec = '';
	waitKey = true;
	droppedKey = false;
	lastKeyAsk = 0;
	pending: VideoFrame | null = null;
	raf = 0;
	audioDecoder: AudioDecoder | null = null;
	audioCtx: AudioContext | null = null;
	gain: GainNode | null = null;
	nextAudio = 0;
	liveSince = 0;
	retried = false;
	closed = false;
	uploadSeq = 0;
	sizeTimer = 0;
	imageTimer = 0;
	/** The view was taken down (window closed); answers still on their way are dropped. */
	gone = false;
	lastMouse = { x: 0, y: 0, buttons: 0 };
	held = new Set<string>();
	FPS: (30 | 60)[] = [30, 60];

	shown = $derived(
		this.hello
			? fitRect(this.box.w, this.box.h, this.hello.width, this.hello.height)
			: { left: 0, top: 0, width: 0, height: 0 },
	);
	// PLAN 72: integer enlargement without smoothing (sharp text), as KasmVNC.
	crisp = $derived(
		!!this.hello &&
			crispScale(this.shown.width, this.hello.width, globalThis.devicePixelRatio ?? 1),
	);
	live = $derived(this.phase === 'live');
	stateWord = $derived(
		this.phase === 'live'
			? ''
			: this.phase === 'connecting'
				? t('viewer.state.connecting')
				: t('viewer.state.ended'),
	);
	gpuText = $derived(this.hello ? t(`viewer.gpu.${this.hello.gpu}` as MessageKey) : '');
	// PLAN 72: the transport in front (tiles or video).
	statsText = $derived(
		(this.hello ? t(`viewer.mode.${this.streamMode}` as MessageKey) + ' · ' : '') +
			(this.stats && this.hello
				? t('viewer.stats', {
						fps: Math.round(this.stats.fps),
						latency: Math.round(this.stats.latencyMs),
						gpu: this.gpuText,
					})
				: this.gpuText),
	);
	// prefs is set in the constructor (it needs the app's name), so these two read it lazily.
	volumeText = $derived.by(() => formatPercent(this.prefs.muted ? 0 : this.prefs.volume * 100));
	quality = $derived.by<Quality>(() => this.prefs.quality ?? 'auto');

	send = (msg: object) => {
		if (this.ws?.readyState === WebSocket.OPEN) this.ws.send(JSON.stringify(msg));
	};

	/** Ends the stream (window closed, reconnect, a final state). */
	stop = () => {
		this.closed = true;
		if (this.ws) {
			const socket = this.ws;
			this.ws = null;
			socket.onclose = null;
			socket.close();
		}
		this.closeVideo();
		this.closeAudio();
		clearTimeout(this.imageTimer);
	};

	// ---- Video ----

	generation = 0;
	configureVideo = async (codec: string, width: number, height: number) => {
		// Two hellos in a row: only the newest sets up a decoder.
		const mine = ++this.generation;
		this.closeVideo();
		this.waitKey = true;
		this.droppedKey = false;
		if (codec === CODEC_TILES) {
			// PLAN 72: pictures of changed areas, no decoder; the canvas keeps
			// what it shows until the tiles arrive.
			this.streamMode = 'tiles';
			this.sizeCanvas(width, height);
			return;
		}
		this.streamMode = 'video';
		if (codec !== this.supportedCodec) {
			const base: VideoDecoderConfig = { codec, optimizeForLatency: true };
			let config: VideoDecoderConfig | null = { ...base, hardwareAcceleration: 'prefer-hardware' };
			try {
				if (!(await VideoDecoder.isConfigSupported(config)).supported) {
					config = (await VideoDecoder.isConfigSupported(base)).supported ? base : null;
				}
			} catch {
				config = null;
			}
			if (mine !== this.generation) return;
			if (!config) {
				this.phase = 'codec';
				this.stop();
				return;
			}
			this.videoConfig = config;
			this.supportedCodec = codec;
		}
		if (this.closed || mine !== this.generation) return;
		this.sizeCanvas(width, height);
		this.openDecoder();
		// A key frame went by while the decoder was being set up.
		if (this.droppedKey) this.askKeyframe(true);
	};

	// Setting the size clears a canvas, even to the same size: only on a change.
	sizeCanvas = (width: number, height: number) => {
		if (this.canvas && (this.canvas.width !== width || this.canvas.height !== height)) {
			this.canvas.width = width;
			this.canvas.height = height;
		}
	};

	// ---- Tiles (PLAN 72) ----

	// Pictures are decoded side by side but drawn in the order they came,
	// so an older picture never covers a newer one.
	tileChain: Promise<void> = Promise.resolve();
	onTile = (buf: ArrayBuffer) => {
		const tile = parseTile(buf);
		if (!tile || !this.canvas) return;
		const mine = this.generation;
		const bitmap = createImageBitmap(new Blob([tile.data as BlobPart], { type: tile.type }));
		this.tileChain = this.tileChain
			.then(() => bitmap)
			.then((picture) => {
				const ctx = this.canvas?.getContext('2d');
				if (ctx && mine === this.generation && !this.closed) {
					ctx.drawImage(picture, tile.x, tile.y, tile.w, tile.h);
					this.painted = true;
				}
				picture.close();
			})
			.catch(() => {
				// A broken picture: the next change of that area replaces it.
			});
	};

	openDecoder = () => {
		if (!this.videoConfig) return;
		this.decoder = new VideoDecoder({
			output: (frame) => {
				this.pending?.close();
				this.pending = frame;
				if (!this.raf) this.raf = requestAnimationFrame(this.draw);
			},
			error: () => {
				// Broken stream or lost GPU: a fresh decoder and a key frame.
				this.decoder = null;
				if (this.closed) return;
				this.openDecoder();
				this.waitKey = true;
				this.askKeyframe(true);
			},
		});
		this.decoder.configure(this.videoConfig);
	};

	closeVideo = () => {
		if (this.decoder && this.decoder.state !== 'closed') this.decoder.close();
		this.decoder = null;
		this.pending?.close();
		this.pending = null;
		if (this.raf) cancelAnimationFrame(this.raf);
		this.raf = 0;
	};

	askKeyframe = (now = false) => {
		const at = Date.now();
		if (!now && at - this.lastKeyAsk < 1000) return;
		this.lastKeyAsk = at;
		this.send({ t: 'keyframe' });
	};

	onVideo = (buf: ArrayBuffer) => {
		const packet = parseVideo(buf);
		if (!packet) return;
		if (!this.decoder || this.decoder.state !== 'configured') {
			if (packet.key) this.droppedKey = true;
			return;
		}
		if (this.waitKey && !packet.key) return this.askKeyframe();
		// Behind: drop until the next key frame (delta frames depend on each other).
		if (!packet.key && this.decoder.decodeQueueSize > MAX_DECODE_QUEUE) {
			this.waitKey = true;
			return this.askKeyframe();
		}
		this.waitKey = false;
		try {
			this.decoder.decode(
				new EncodedVideoChunk({
					type: packet.key ? 'key' : 'delta',
					timestamp: packet.timestamp,
					data: packet.data,
				}),
			);
		} catch {
			this.waitKey = true;
			this.askKeyframe(true);
		}
	};

	// Only the newest decoded picture is drawn, once per screen refresh.
	draw = () => {
		this.raf = 0;
		const frame = this.pending;
		this.pending = null;
		if (!frame) return;
		const ctx = this.canvas?.getContext('2d');
		if (ctx && this.canvas) {
			ctx.drawImage(frame, 0, 0, this.canvas.width, this.canvas.height);
			this.painted = true;
		}
		frame.close();
	};

	// ---- Sound ----

	configureAudio = () => {
		if (this.audioDecoder) return;
		if (typeof AudioDecoder !== 'function') {
			this.noAudio = true;
			return;
		}
		try {
			this.audioDecoder = new AudioDecoder({
				output: this.play,
				error: () => {
					this.audioDecoder = null;
				},
			});
			this.audioDecoder.configure({ codec: 'opus', sampleRate: 48000, numberOfChannels: 2 });
		} catch {
			this.audioDecoder = null;
			this.noAudio = true;
		}
	};

	closeAudio = () => {
		if (this.audioDecoder && this.audioDecoder.state !== 'closed') this.audioDecoder.close();
		this.audioDecoder = null;
	};

	/** The AudioContext may only start after a gesture of the user (autoplay rules). */
	startAudio = () => {
		if (!this.hello?.audio || this.noAudio) return;
		try {
			if (!this.audioCtx) {
				this.audioCtx = new AudioContext({ sampleRate: 48000, latencyHint: 'interactive' });
				this.gain = this.audioCtx.createGain();
				this.gain.connect(this.audioCtx.destination);
				this.applyVolume();
			}
			if (this.audioCtx.state === 'suspended') void this.audioCtx.resume();
		} catch {
			this.noAudio = true;
		}
	};

	applyVolume = () => {
		if (this.gain) this.gain.gain.value = this.prefs.muted ? 0 : this.prefs.volume;
	};

	onAudio = (buf: ArrayBuffer) => {
		if (!this.audioDecoder || this.audioDecoder.state !== 'configured') return;
		if (!this.audioCtx || this.audioCtx.state !== 'running' || this.prefs.muted) return;
		const packet = parseAudio(buf);
		if (!packet) return;
		try {
			this.audioDecoder.decode(
				new EncodedAudioChunk({ type: 'key', timestamp: packet.timestamp, data: packet.data }),
			);
		} catch {
			// One lost packet is 20 ms of silence.
		}
	};

	// A small jitter buffer: play ~60 ms behind, never further ahead than 250 ms.
	play = (data: AudioData) => {
		const ctx = this.audioCtx;
		if (!ctx || !this.gain) return data.close();
		try {
			const buffer = ctx.createBuffer(data.numberOfChannels, data.numberOfFrames, data.sampleRate);
			for (let c = 0; c < data.numberOfChannels; c++)
				data.copyTo(buffer.getChannelData(c), { planeIndex: c, format: 'f32-planar' });
			const now = ctx.currentTime;
			if (this.nextAudio < now + 0.005) this.nextAudio = now + AUDIO_JITTER;
			if (this.nextAudio > now + AUDIO_MAX_AHEAD) return;
			const source = ctx.createBufferSource();
			source.buffer = buffer;
			source.connect(this.gain);
			source.start(this.nextAudio);
			this.nextAudio += buffer.duration;
		} catch {
			// Skip this packet.
		} finally {
			data.close();
		}
	};

	setVolume = (value: number) => {
		this.prefs.volume = Math.min(1, Math.max(0, value));
		if (this.prefs.volume > 0) this.prefs.muted = false;
		this.applyVolume();
		saveView(this.app, $state.snapshot(this.prefs));
	};

	toggleMute = () => {
		this.startAudio();
		this.prefs.muted = !this.prefs.muted;
		this.applyVolume();
		saveView(this.app, $state.snapshot(this.prefs));
	};

	setQuality = (
		mode: Quality,
		fps: 30 | 60,
		transport: Transport = this.prefs.transport ?? 'auto',
	) => {
		this.prefs.quality = mode;
		this.prefs.fps = fps;
		this.prefs.transport = transport;
		saveView(this.app, $state.snapshot(this.prefs));
		this.send({ t: 'quality', mode, fps, transport, webp: true });
	};

	constructor(app: () => string) {
		this.#app = app;
		this.prefs = $state(untrack(() => loadView(this.app)));
	}
}
