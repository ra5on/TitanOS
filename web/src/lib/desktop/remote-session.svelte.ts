// PLAN 63: the remote view's connection to titand and everything that goes
// back to the app: mouse, wheel and touch, keys, text, the clipboard and
// uploads (protocol: docs/FERNANSICHT.md, remote.ts). Picture and sound are
// in remote-media.svelte.ts.
import { onMount } from 'svelte';
import { api } from '../api.ts';
import { apiPath, apiSocket } from '../cluster-node.ts';
import type { MessageKey } from '../i18n/index.svelte.ts';
import {
	chunkRanges,
	cursorCss,
	inputMessages,
	isDesktopShortcut,
	isHalfKey,
	macOptionChar,
	fitText,
	isPaste,
	keyMessage,
	mapPoint,
	MSG_AUDIO,
	MSG_TILE,
	MSG_VIDEO,
	missingSupport,
	parseMessage,
	sanitizeStatus,
	sizeMessage,
	UPLOAD_HIGH_WATER,
	uploadChunk,
	viewerError,
	wheelDelta,
	type ByeReason,
	type Quality,
	type ServerMessage,
	type ImageStatus,
	type ViewerError,
} from './remote.ts';
import { RemoteMedia } from './remote-media.svelte.ts';

type Touch = { x: number; y: number; sx: number; sy: number };

export class RemoteSession extends RemoteMedia {
	// ---- Connection ----

	connect = () => {
		this.stop();
		this.closed = false;
		this.phase = 'connecting';
		this.painted = false;
		const socket = new WebSocket(apiSocket(`/viewer/${encodeURIComponent(this.app)}/stream`));
		socket.binaryType = 'arraybuffer';
		this.ws = socket;
		let opened = false;
		let ended = false;
		socket.onopen = () => {
			opened = true;
			this.sendSize();
			if (this.prefs.quality || this.prefs.fps || this.prefs.transport)
				this.send({
					t: 'quality',
					mode: this.prefs.quality ?? 'auto',
					fps: this.prefs.fps ?? 30,
					webp: true,
					...(this.prefs.transport ? { transport: this.prefs.transport } : {}),
				});
		};
		socket.onmessage = (event) => {
			if (typeof event.data === 'string') {
				const msg = parseMessage(event.data);
				if (msg?.t === 'bye' || msg?.t === 'error') ended = true;
				if (msg) void this.onText(msg);
				return;
			}
			const buf = event.data as ArrayBuffer;
			if (!buf.byteLength) return;
			const kind = new DataView(buf).getUint8(0);
			if (kind === MSG_VIDEO) this.onVideo(buf);
			else if (kind === MSG_TILE) this.onTile(buf);
			else if (kind === MSG_AUDIO) this.onAudio(buf);
		};
		socket.onclose = (event) => {
			if (this.ws !== socket) return;
			this.ws = null;
			this.releaseInput(false);
			if (this.closed || ended) return;
			const code = viewerError(event.reason);
			if (code) return this.refuse(code);
			if (!opened) return void this.refused();
			// A network drop: one new attempt, unless the last one was just now.
			if (this.phase === 'live' && (!this.retried || Date.now() - this.liveSince > 30_000)) {
				this.retried = true;
				this.phase = 'connecting';
				setTimeout(() => !this.closed && this.connect(), 1000);
				return;
			}
			this.end('lost');
		};
	};

	end = (reason: ByeReason | 'lost' | 'refused') => {
		this.endKey = `viewer.bye.${reason}` as MessageKey;
		this.endDetail = '';
		this.phase = 'ended';
		this.stop();
	};

	// titand refused the view ({"t":"error"} and the close reason).
	refuse = (code: ViewerError, loading?: ImageStatus, detail = '') => {
		this.stop();
		this.endDetail = detail;
		if (code === 'viewer_image') {
			this.phase = 'image';
			this.image = loading;
			// A failed download is not retried in a loop (titand tries at most once a minute).
			if (loading?.state !== 'error') this.imageTimer = window.setTimeout(this.watchImage, 2000);
		} else if (code === 'viewer_memory') this.phase = 'memory';
		else if (code === 'viewer_hostnet') this.phase = 'hostnet';
		else {
			this.endKey = `viewer.err.${code}` as MessageKey;
			this.phase = 'ended';
		}
	};

	// Refused before it began: a refused upgrade carries no reason, so the
	// status says whether the viewer program is still missing.
	refused = async () => {
		try {
			const status = sanitizeStatus(await api.get('/viewer/status', AbortSignal.timeout(8000)));
			if (this.gone) return;
			if (status.image.state !== 'ready') return this.refuse('viewer_image');
		} catch {
			// No status (older NAS, no right): the generic message.
		}
		if (!this.gone) this.end('refused');
	};

	// The viewer program is loading: progress every 2 s, then connect.
	watchImage = async () => {
		clearTimeout(this.imageTimer);
		if (this.phase !== 'image') return;
		try {
			const status = sanitizeStatus(await api.get('/viewer/status', AbortSignal.timeout(8000)));
			// The window was closed while the answer was on its way: nothing connects any more.
			if (this.gone) return;
			this.image = status.image;
			if (status.image.state === 'ready') {
				this.image = undefined;
				return this.connect();
			}
			if (status.image.state === 'error') return;
		} catch {
			// Try again in a moment; the window shows the last known state.
		}
		if (this.phase === 'image' && !this.gone)
			this.imageTimer = window.setTimeout(this.watchImage, 2000);
	};

	onText = async (msg: ServerMessage) => {
		switch (msg.t) {
			case 'hello':
				this.hello = {
					width: msg.width,
					height: msg.height,
					gpu: msg.gpu,
					audio: msg.audio,
					codec: msg.codec,
				};
				await this.configureVideo(msg.codec, msg.width, msg.height);
				if (msg.audio) this.configureAudio();
				else this.closeAudio();
				if (this.phase === 'connecting') {
					this.phase = 'live';
					this.liveSince = Date.now();
				}
				break;
			case 'stats':
				this.stats = { fps: msg.fps, latencyMs: msg.latencyMs };
				break;
			case 'mode':
				this.streamMode = msg.mode;
				break;
			case 'clipboard':
				this.remoteClip = msg.text;
				this.copied = false;
				break;
			case 'download':
				this.downloads = [{ id: msg.id, name: msg.name, size: msg.size }, ...this.downloads].slice(
					0,
					20,
				);
				break;
			case 'cursor':
				this.cursor = cursorCss(msg.shape);
				break;
			case 'uploaded': {
				const u = this.uploads.find((x) => x.id === msg.id);
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
				this.end(msg.reason);
				break;
			case 'error':
				this.refuse(
					viewerError(msg.code) ?? 'viewer_error',
					msg.image,
					typeof msg.detail === 'string' ? msg.detail.slice(0, 400) : '',
				);
				break;
		}
	};

	// ---- Size ----

	sendSize = () => {
		clearTimeout(this.sizeTimer);
		if (this.box.w > 0 && this.box.h > 0)
			this.send(sizeMessage(this.box.w, this.box.h, window.devicePixelRatio || 1));
	};

	// ---- Mouse, wheel and touch ----

	point = (event: { clientX: number; clientY: number }) => {
		if (!this.canvas || !this.hello) return null;
		const r = this.canvas.getBoundingClientRect();
		return mapPoint(
			event.clientX,
			event.clientY,
			{ left: r.left, top: r.top, width: r.width, height: r.height },
			this.hello.width,
			this.hello.height,
		);
	};

	mouse = (x: number, y: number, buttons: number, force = false) => {
		if (
			!force &&
			x === this.lastMouse.x &&
			y === this.lastMouse.y &&
			buttons === this.lastMouse.buttons
		)
			return;
		this.lastMouse = { x, y, buttons };
		this.send({ t: 'mouse', x, y, buttons });
	};

	touches = new Map<number, Touch>();
	gesture: 'none' | 'tap' | 'pan' | 'done' = 'none';
	longPress = 0;

	pointerDown = (event: PointerEvent) => {
		if (!this.live) return;
		this.canvas?.focus({ preventScroll: true });
		this.startAudio();
		if (event.pointerType === 'touch') {
			event.preventDefault();
			this.touches.set(event.pointerId, {
				x: event.clientX,
				y: event.clientY,
				sx: event.clientX,
				sy: event.clientY,
			});
			this.canvas?.setPointerCapture(event.pointerId);
			if (this.touches.size === 1) {
				this.gesture = 'tap';
				const p = this.point(event);
				clearTimeout(this.longPress);
				// Long press = right click.
				this.longPress = window.setTimeout(() => {
					if (this.gesture !== 'tap' || !p) return;
					this.gesture = 'done';
					this.mouse(p.x, p.y, 0, true);
					this.mouse(p.x, p.y, 2);
					this.mouse(p.x, p.y, 0);
				}, 550);
			} else {
				clearTimeout(this.longPress);
				this.gesture = 'pan';
			}
			return;
		}
		event.preventDefault();
		this.canvas?.setPointerCapture(event.pointerId);
		const p = this.point(event);
		if (p) this.mouse(p.x, p.y, event.buttons);
	};

	pointerMove = (event: PointerEvent) => {
		if (!this.live) return;
		if (event.pointerType === 'touch') {
			const touch = this.touches.get(event.pointerId);
			if (!touch) return;
			const dx = event.clientX - touch.x;
			const dy = event.clientY - touch.y;
			touch.x = event.clientX;
			touch.y = event.clientY;
			if (this.gesture === 'tap' && Math.hypot(touch.x - touch.sx, touch.y - touch.sy) > 10) {
				clearTimeout(this.longPress);
				this.gesture = 'pan';
			}
			// Swiping scrolls: the content follows the finger.
			if (this.gesture === 'pan' && (dx || dy)) {
				const n = this.touches.size;
				this.send({ t: 'wheel', dx: Math.round(-dx / n), dy: Math.round(-dy / n) });
			}
			return;
		}
		const p = this.point(event);
		if (p) this.mouse(p.x, p.y, event.buttons);
	};

	pointerUp = (event: PointerEvent) => {
		if (event.pointerType === 'touch') {
			const touch = this.touches.get(event.pointerId);
			this.touches.delete(event.pointerId);
			if (touch && this.gesture === 'tap' && this.touches.size === 0) {
				clearTimeout(this.longPress);
				const p = this.point(event);
				// Tap = click.
				if (p) {
					this.mouse(p.x, p.y, 0, true);
					this.mouse(p.x, p.y, 1);
					this.mouse(p.x, p.y, 0);
				}
			}
			if (!this.touches.size) this.gesture = 'none';
			return;
		}
		if (!this.live) return;
		const p = this.point(event);
		if (p) this.mouse(p.x, p.y, event.buttons);
	};

	wheel = (event: WheelEvent) => {
		if (!this.live) return;
		event.preventDefault();
		const d = wheelDelta(event.deltaX, event.deltaY, event.deltaMode);
		if (d.dx || d.dy) this.send({ t: 'wheel', ...d });
	};

	// ---- Keyboard and clipboard ----

	pasteKey: KeyboardEvent | null = null;
	pasteTimer = 0;

	keyDown = (event: KeyboardEvent) => {
		if (!this.live) return;
		// Alt + Q/W/M, Alt + arrows and Ctrl + Space belong to the desktop.
		if (isDesktopShortcut(event)) return;
		this.startAudio();
		if (isPaste(event)) {
			// The browser's paste event brings the text; the key follows it.
			this.pasteKey = event;
			clearTimeout(this.pasteTimer);
			this.pasteTimer = window.setTimeout(() => this.pasteKey && this.pressPaste(), 200);
			return;
		}
		event.preventDefault();
		// Dead keys and IME input: the finished character follows.
		if (isHalfKey(event)) return;
		this.held.add(event.code);
		this.send(this.remoteKey(event, true));
	};

	mac = /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent);
	/** A key as the NAS gets it; Option characters of a Mac go without Alt. */
	remoteKey = (event: KeyboardEvent, down: boolean) => {
		const msg = keyMessage(event, down);
		if (macOptionChar(event, this.mac)) msg.mods.alt = false;
		return msg;
	};

	keyUp = (event: KeyboardEvent) => {
		if (!this.held.has(event.code)) return;
		event.preventDefault();
		this.held.delete(event.code);
		this.send(this.remoteKey(event, false));
	};

	pressPaste = () => {
		const event = this.pasteKey;
		this.pasteKey = null;
		clearTimeout(this.pasteTimer);
		if (!event) return;
		this.send(keyMessage(event, true));
		this.send(keyMessage(event, false));
	};

	paste = (event: ClipboardEvent) => {
		if (!this.live) return;
		event.preventDefault();
		const text = event.clipboardData?.getData('text/plain') ?? '';
		if (text) this.send(fitText('clipboard', text));
		if (this.pasteKey) this.pressPaste();
	};

	/** Lets go of held keys and buttons, so nothing stays pressed on the NAS (e.g. Alt after Alt + Q). */
	releaseInput = (sendUp = true) => {
		if (sendUp)
			for (const code of this.held)
				this.send({
					t: 'key',
					code,
					key: '',
					down: false,
					mods: { shift: false, ctrl: false, alt: false, meta: false },
				});
		this.held.clear();
		if (sendUp && this.lastMouse.buttons) this.mouse(this.lastMouse.x, this.lastMouse.y, 0);
		else this.lastMouse.buttons = 0;
	};

	copyRemote = async () => {
		try {
			await navigator.clipboard.writeText(this.remoteClip);
			this.copied = true;
		} catch {
			this.copied = false;
		}
	};

	sendClip = (event: SubmitEvent) => {
		event.preventDefault();
		if (!this.clipDraft) return;
		this.send(fitText('clipboard', this.clipDraft));
		this.clipSent = true;
	};

	// The phone's keyboard types into a hidden field; its input goes over as text and keys.
	SENTINEL = '​​';
	imeBefore = (event: InputEvent) => {
		if (event.isComposing || event.inputType === 'insertCompositionText') return;
		const out = inputMessages(event.inputType, event.data);
		if (!out.length) return;
		event.preventDefault();
		for (const m of out) this.send(m.t === 'text' ? fitText('text', m.text) : m);
	};
	imeComposed = (event: CompositionEvent) => {
		if (event.data) this.send(fitText('text', event.data));
		if (this.ime) this.ime.value = this.SENTINEL;
	};
	imeKey = (event: KeyboardEvent) => {
		// Hardware and iOS keyboards report real keys; Android only 229.
		if (event.keyCode === 229 || event.isComposing) return;
		if (event.key === 'Backspace' || event.key === 'Enter' || event.key === 'Tab') {
			event.preventDefault();
			this.send(keyMessage(event, true));
			this.send(keyMessage(event, false));
		}
	};
	openKeyboard = () => {
		if (!this.ime) return;
		this.ime.value = this.SENTINEL;
		this.ime.focus();
		this.startAudio();
	};

	// ---- Uploads ----

	upload = async (files: FileList | null) => {
		for (const file of Array.from(files ?? [])) {
			const id = ++this.uploadSeq;
			this.uploads = [
				...this.uploads,
				{ id, name: file.name, size: file.size, sent: 0, state: 'run' },
			];
			const item = this.uploads[this.uploads.length - 1];
			const socket = this.ws;
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
		if (this.fileInput) this.fileInput.value = '';
	};

	// ---- Life cycle ----
	reconnect = () => {
		this.retried = false;
		this.connect();
	};

	/** PLAN 67-1: what the menu bar of the window does with the remote view. */
	menu = {
		live: () => this.phase === 'live',
		upload: () => this.fileInput?.click(),
		reconnect: this.reconnect,
		quality: () => this.quality,
		setQuality: (q: Quality) => this.setQuality(q, this.prefs.fps ?? 30),
		audio: () => !!this.hello?.audio && !this.noAudio,
		muted: () => this.prefs.muted,
		toggleMute: this.toggleMute,
		fullscreen: () => void this.screen?.requestFullscreen?.().catch(() => {}),
		remoteClip: () => this.remoteClip,
		/** Sends the text on this computer's clipboard to the app. */
		clipSend: async () => {
			try {
				const text = await navigator.clipboard.readText();
				if (text) this.send(fitText('clipboard', text));
			} catch {
				// No permission: the ⋯ menu's clipboard field still works.
			}
		},
		/** Puts the app's clipboard onto this computer's. */
		clipGet: this.copyRemote,
		/** Files the app offered for download (Ablage › Heruntergeladen ›). */
		downloads: () => this.downloads.map((d) => ({ id: d.id, text: d.name })),
		download: (id: string) => {
			const d = this.downloads.find((x) => x.id === id);
			if (!d) return;
			const a = document.createElement('a');
			a.href = apiPath(
				`/viewer/${encodeURIComponent(this.app)}/downloads/${encodeURIComponent(d.id)}`,
			);
			a.download = d.name;
			a.click();
		},
		/** Ansicht › Statistik: frames, delay and GPU over the picture. */
		stats: () => this.showStats,
		toggleStats: () => (this.showStats = !this.showStats),
	};
	showStats = $state(false);

	constructor(app: () => string) {
		super(app);
		// ---- Life cycle ----

		onMount(() => {
			const why = missingSupport(globalThis as never);
			if (why) {
				this.phase = why === 'insecure' ? 'insecure' : 'unsupported';
				return;
			}
			this.connect();
			return () => {
				this.gone = true;
				this.stop();
				this.releaseInput(false);
				void this.audioCtx?.close();
				this.audioCtx = null;
				clearTimeout(this.sizeTimer);
				clearTimeout(this.longPress);
			};
		});

		// The visible area follows the window; the NAS adapts its screen 300 ms after the last change.
		$effect(() => {
			if (!this.screen) return;
			const observer = new ResizeObserver(([entry]) => {
				const r = entry.contentRect;
				this.box = { w: r.width, h: r.height };
				clearTimeout(this.sizeTimer);
				this.sizeTimer = window.setTimeout(this.sendSize, 300);
			});
			observer.observe(this.screen);
			return () => observer.disconnect();
		});
	}
}
