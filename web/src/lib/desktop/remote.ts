// PLAN 63: the remote view ("Fernansicht") of a Docker app – the plain parts
// of the protocol between the window and the viewer on the NAS
// (docs/FERNANSICHT.md): binary headers, the JSON messages, keys, pointer
// mapping and upload chunks. No DOM state here, so all of it can be tested.

export const MSG_VIDEO = 0x01;
export const MSG_AUDIO = 0x02;
export const MSG_UPLOAD = 0x03;
/** Largest piece of an upload in one message. */
export const UPLOAD_CHUNK = 256 * 1024;
/** Waiting for the socket to drain above this many buffered bytes (uploads). */
export const UPLOAD_HIGH_WATER = 1024 * 1024;
/** titand's limit for one text message from the browser (clipboard up to 1 MiB plus JSON). */
export const MAX_TEXT_MESSAGE = (1 << 20) + 4096;
/** Decoder queue above which frames are dropped until the next key frame. */
export const MAX_DECODE_QUEUE = 2;
/** Audio is scheduled this far ahead (jitter buffer) and never further than AUDIO_MAX_AHEAD. */
export const AUDIO_JITTER = 0.06;
export const AUDIO_MAX_AHEAD = 0.25;

export type VideoPacket = { key: boolean; timestamp: number; data: Uint8Array };
export type AudioPacket = { timestamp: number; data: Uint8Array };

function u64(view: DataView, offset: number): number {
	// Microseconds fit a double exactly for ~285 years.
	return view.getUint32(offset) * 2 ** 32 + view.getUint32(offset + 4);
}

/** 0x01 | flags | u64 µs | Annex-B access unit. Null when it is not a video message. */
export function parseVideo(buf: ArrayBuffer): VideoPacket | null {
	if (buf.byteLength < 10) return null;
	const view = new DataView(buf);
	if (view.getUint8(0) !== MSG_VIDEO) return null;
	return {
		key: (view.getUint8(1) & 1) === 1,
		timestamp: u64(view, 2),
		data: new Uint8Array(buf, 10),
	};
}

/** 0x02 | u64 µs | one Opus packet. */
export function parseAudio(buf: ArrayBuffer): AudioPacket | null {
	if (buf.byteLength < 9) return null;
	const view = new DataView(buf);
	if (view.getUint8(0) !== MSG_AUDIO) return null;
	return { timestamp: u64(view, 1), data: new Uint8Array(buf, 9) };
}

/** Builds a video message (tests and tools). */
export function videoMessage(key: boolean, timestamp: number, data: Uint8Array): ArrayBuffer {
	const out = new Uint8Array(10 + data.length);
	const view = new DataView(out.buffer);
	view.setUint8(0, MSG_VIDEO);
	view.setUint8(1, key ? 1 : 0);
	view.setUint32(2, Math.floor(timestamp / 2 ** 32));
	view.setUint32(6, timestamp >>> 0);
	out.set(data, 10);
	return out.buffer;
}

/** 0x03 | u32 upload id | data (at most UPLOAD_CHUNK bytes). */
export function uploadChunk(id: number, data: Uint8Array): ArrayBuffer {
	if (data.length > UPLOAD_CHUNK) throw new RangeError('chunk too large');
	const out = new Uint8Array(5 + data.length);
	const view = new DataView(out.buffer);
	view.setUint8(0, MSG_UPLOAD);
	view.setUint32(1, id >>> 0);
	out.set(data, 5);
	return out.buffer;
}

/** The byte ranges a file of `size` bytes is sent in. An empty file has none. */
export function chunkRanges(size: number, chunk = UPLOAD_CHUNK): [number, number][] {
	const out: [number, number][] = [];
	for (let start = 0; start < size; start += chunk)
		out.push([start, Math.min(size, start + chunk)]);
	return out;
}

// ---- Messages from the viewer ----

export type Gpu = 'vaapi' | 'nvenc' | 'cpu';
export type ByeReason = 'idle' | 'logout' | 'stopped' | 'forbidden' | 'replaced' | 'error';
const BYE_REASONS: ByeReason[] = ['idle', 'logout', 'stopped', 'forbidden', 'replaced', 'error'];
/** Why titand refused a view ({"t":"error","code"} and the close reason). */
export type ViewerError =
	| 'app_forbidden'
	| 'viewer_image'
	| 'viewer_memory'
	| 'viewer_limit'
	| 'viewer_hostnet'
	| 'viewer_no_webui'
	| 'viewer_not_running'
	| 'viewer_volume'
	| 'viewer_agent'
	| 'viewer_name'
	| 'viewer_unavailable'
	| 'viewer_error';
export type ServerMessage =
	| { t: 'hello'; codec: string; width: number; height: number; gpu: Gpu; audio: boolean }
	| { t: 'stats'; fps: number; kbps: number; latencyMs: number }
	| { t: 'clipboard'; text: string }
	| { t: 'download'; name: string; size: number; id: string }
	| { t: 'cursor'; shape: string }
	| { t: 'uploaded'; id: number; error?: 'too_large' | 'bad_name' | 'no_space' | 'failed' }
	| { t: 'bye'; reason: ByeReason; text: string }
	| { t: 'starting' }
	| { t: 'error'; code: string; text: string; image?: ImageStatus };

const VIEWER_ERRORS: ViewerError[] = [
	'app_forbidden',
	'viewer_image',
	'viewer_memory',
	'viewer_limit',
	'viewer_hostnet',
	'viewer_no_webui',
	'viewer_not_running',
	'viewer_volume',
	'viewer_agent',
	'viewer_name',
	'viewer_unavailable',
	'viewer_error',
];

/** A known refusal: the code of an error message or a close reason ("viewer_memory"). */
export function viewerError(text: string | undefined | null): ViewerError | null {
	if (!text) return null;
	return VIEWER_ERRORS.find((code) => text.trim() === code) ?? null;
}

const num = (v: unknown, fallback = 0) =>
	typeof v === 'number' && Number.isFinite(v) ? v : fallback;
const str = (v: unknown) => (typeof v === 'string' ? v : '');

/** Parses one text message; unknown or broken messages give null. */
export function parseMessage(text: string): ServerMessage | null {
	let m: Record<string, unknown>;
	try {
		m = JSON.parse(text);
	} catch {
		return null;
	}
	if (!m || typeof m !== 'object') return null;
	switch (m.t) {
		case 'hello': {
			const width = num(m.width);
			const height = num(m.height);
			if (!str(m.codec) || width <= 0 || height <= 0) return null;
			const gpu = (['vaapi', 'nvenc', 'cpu'] as const).find((g) => g === m.gpu) ?? 'cpu';
			return { t: 'hello', codec: str(m.codec), width, height, gpu, audio: m.audio === true };
		}
		case 'stats':
			return { t: 'stats', fps: num(m.fps), kbps: num(m.kbps), latencyMs: num(m.latencyMs) };
		case 'clipboard':
			return { t: 'clipboard', text: str(m.text) };
		case 'download':
			if (!str(m.id)) return null;
			return { t: 'download', name: str(m.name) || str(m.id), size: num(m.size), id: str(m.id) };
		case 'cursor':
			return { t: 'cursor', shape: str(m.shape) };
		case 'uploaded':
			return {
				t: 'uploaded',
				id: num(m.id),
				error: (['too_large', 'bad_name', 'no_space', 'failed'] as const).find(
					(e) => e === m.error,
				),
			};
		case 'bye': {
			const reason = BYE_REASONS.find((r) => r === m.reason) ?? 'error';
			return { t: 'bye', reason, text: str(m.text) };
		}
		case 'starting':
			return { t: 'starting' };
		case 'error': {
			const out: ServerMessage = { t: 'error', code: str(m.code), text: str(m.text) };
			if (m.image && typeof m.image === 'object') out.image = sanitizeImage(m.image);
			return out;
		}
	}
	return null;
}

// CSS cursors the viewer may ask for; anything else is the default arrow.
const CURSORS = new Set([
	'default',
	'pointer',
	'text',
	'crosshair',
	'move',
	'wait',
	'progress',
	'help',
	'not-allowed',
	'grab',
	'grabbing',
	'col-resize',
	'row-resize',
	'ew-resize',
	'ns-resize',
	'nesw-resize',
	'nwse-resize',
	'zoom-in',
	'zoom-out',
	'none',
	'context-menu',
	'cell',
	'copy',
	'alias',
	'vertical-text',
	'all-scroll',
]);
export function cursorCss(shape: string): string {
	return CURSORS.has(shape) ? shape : 'default';
}

// ---- Input ----

export type Mods = { shift: boolean; ctrl: boolean; alt: boolean; meta: boolean };
export type KeyLike = {
	code: string;
	key: string;
	shiftKey: boolean;
	ctrlKey: boolean;
	altKey: boolean;
	metaKey: boolean;
};

/**
 * Keys that stay with the desktop: Alt + Q/W/M and Alt + arrows (window
 * switcher, close, minimize, dock) and Ctrl/⌘ + Space (main menu).
 */
export function isDesktopShortcut(e: KeyLike): boolean {
	if (e.altKey && !e.ctrlKey && !e.metaKey) {
		const k = e.key.toLowerCase();
		if (k === 'q' || k === 'w' || k === 'm') return true;
		if (['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown'].includes(e.key)) return true;
	}
	return (e.ctrlKey || e.metaKey) && e.code === 'Space';
}

/** Ctrl/⌘ + V: the text goes over first (paste event), then the key. */
export function isPaste(e: KeyLike): boolean {
	return (e.ctrlKey || e.metaKey) && !e.altKey && e.code === 'KeyV';
}

export type KeyMessage = { t: 'key'; code: string; key: string; down: boolean; mods: Mods };

/**
 * A key for the viewer. Both fields always go along; the viewer types
 * printable characters by `key` (independent of the layout) and everything
 * else by `code`. Printable means one character without Ctrl/Alt/Meta –
 * except AltGr characters, which the browser reports as Ctrl+Alt.
 */
export function keyMessage(e: KeyLike, down: boolean): KeyMessage {
	return {
		t: 'key',
		code: e.code,
		key: e.key,
		down,
		mods: { shift: e.shiftKey, ctrl: e.ctrlKey, alt: e.altKey, meta: e.metaKey },
	};
}

/**
 * Keys that are only half a character (dead keys such as ´ before e, IME
 * input): the browser sends the finished character afterwards, which goes
 * over; the half would type a stray character on the NAS.
 */
export function isHalfKey(e: KeyLike & { isComposing?: boolean }): boolean {
	return !!e.isComposing || e.key === 'Dead' || e.key === 'Process' || e.key === 'Unidentified';
}

/**
 * On a Mac, Option (Alt) types characters (Option + L = @ on a German Mac):
 * such a key goes over as the character without Alt, like AltGr elsewhere.
 */
export function macOptionChar(e: KeyLike, mac: boolean): boolean {
	return mac && e.altKey && !e.ctrlKey && !e.metaKey && [...e.key].length === 1;
}

/**
 * A text message cut to what titand accepts (MAX_TEXT_MESSAGE bytes as JSON);
 * a huge paste loses its end instead of closing the view.
 */
export function fitText<T extends 'text' | 'clipboard'>(
	t: T,
	text: string,
): { t: T; text: string } {
	const size = (x: string) => new TextEncoder().encode(JSON.stringify({ t, text: x })).length;
	let out = text;
	while (out && size(out) > MAX_TEXT_MESSAGE) {
		const over = size(out) - MAX_TEXT_MESSAGE;
		// Escapes can take up to 6 bytes per character: cut at least what is over.
		out = [...out].slice(0, -Math.max(1, Math.ceil(over / 6))).join('');
	}
	return { t, text: out };
}

/** Whether a key types a character (sent as `key`) rather than a function (by `code`). */
export function isPrintable(e: KeyLike): boolean {
	return [...e.key].length === 1 && !e.ctrlKey && !e.altKey && !e.metaKey;
}

/** Text of a phone keyboard's beforeinput event as messages (Android sends keyCode 229). */
export function inputMessages(
	inputType: string,
	data: string | null,
): ({ t: 'text'; text: string } | KeyMessage)[] {
	const tap = (code: string, key: string) => {
		const e = { code, key, shiftKey: false, ctrlKey: false, altKey: false, metaKey: false };
		return [keyMessage(e, true), keyMessage(e, false)];
	};
	if (inputType === 'deleteContentBackward') return tap('Backspace', 'Backspace');
	if (inputType === 'deleteContentForward') return tap('Delete', 'Delete');
	if (inputType === 'insertLineBreak' || inputType === 'insertParagraph')
		return tap('Enter', 'Enter');
	if (inputType.startsWith('insert') && data) return [{ t: 'text', text: data }];
	return [];
}

export type Box = { left: number; top: number; width: number; height: number };

/** The largest rectangle of the remote's aspect inside a box (letterboxed, centred). */
export function fitRect(boxW: number, boxH: number, w: number, h: number): Box {
	if (boxW <= 0 || boxH <= 0 || w <= 0 || h <= 0) return { left: 0, top: 0, width: 0, height: 0 };
	const scale = Math.min(boxW / w, boxH / h);
	const width = w * scale;
	const height = h * scale;
	return { left: (boxW - width) / 2, top: (boxH - height) / 2, width, height };
}

/** A point on the shown picture (client coordinates) in remote pixels, clamped to the screen. */
export function mapPoint(
	clientX: number,
	clientY: number,
	shown: Box,
	w: number,
	h: number,
): { x: number; y: number } {
	if (shown.width <= 0 || shown.height <= 0) return { x: 0, y: 0 };
	const x = Math.round(((clientX - shown.left) / shown.width) * w);
	const y = Math.round(((clientY - shown.top) / shown.height) * h);
	return { x: Math.min(w - 1, Math.max(0, x)), y: Math.min(h - 1, Math.max(0, y)) };
}

/** The size message for a visible area in CSS pixels. */
export function sizeMessage(width: number, height: number, scale: number) {
	return {
		t: 'size' as const,
		width: Math.max(1, Math.floor(width)),
		height: Math.max(1, Math.floor(height)),
		scale: Math.round(Math.max(1, Math.min(4, scale || 1)) * 100) / 100,
	};
}

/** A wheel event in pixels (lines and pages turned into pixels). */
export function wheelDelta(dx: number, dy: number, mode: number): { dx: number; dy: number } {
	const f = mode === 1 ? 40 : mode === 2 ? 800 : 1;
	return { dx: Math.round(dx * f), dy: Math.round(dy * f) };
}

// ---- Preferences of a view (per app, only in this browser) ----

export type Quality = 'auto' | 'high' | 'balanced' | 'low';
export const QUALITIES: Quality[] = ['auto', 'high', 'balanced', 'low'];
/**
 * Volume and mute per app; quality and frame rate only once chosen in the
 * window (until then the NAS's settings apply).
 */
export type ViewPrefs = { volume: number; muted: boolean; quality?: Quality; fps?: 30 | 60 };
export const DEFAULT_VOLUME = 0.65;
const prefsKey = (app: string) => 'titanos.viewer.' + app;

export function sanitizeView(v: unknown): ViewPrefs {
	const o = (v && typeof v === 'object' ? v : {}) as Record<string, unknown>;
	const volume =
		typeof o.volume === 'number' && o.volume >= 0 && o.volume <= 1 ? o.volume : DEFAULT_VOLUME;
	const out: ViewPrefs = { volume, muted: o.muted === true };
	const quality = QUALITIES.find((q) => q === o.quality);
	if (quality) out.quality = quality;
	if (o.fps === 30 || o.fps === 60) out.fps = o.fps;
	return out;
}

export function loadView(app: string): ViewPrefs {
	try {
		return sanitizeView(JSON.parse(localStorage.getItem(prefsKey(app)) ?? 'null'));
	} catch {
		return sanitizeView(null);
	}
}

export function saveView(app: string, prefs: ViewPrefs) {
	try {
		localStorage.setItem(prefsKey(app), JSON.stringify(prefs));
	} catch {
		// Private window or storage blocked: the choice lasts for this view.
	}
}

// ---- Settings and state on the NAS (GET/PUT /viewer/settings, GET /viewer/status) ----

export type ViewerSettings = {
	gpu: string;
	quality: Quality;
	fps: 30 | 60;
	audio: boolean;
	volume: string;
};
export type ImageState = 'missing' | 'downloading' | 'loading' | 'ready' | 'error';
const IMAGE_STATES: ImageState[] = ['missing', 'downloading', 'loading', 'ready', 'error'];
export type ImageStatus = {
	state: ImageState;
	version: string;
	/** Download so far and its size in bytes; progress 0–1. */
	bytes: number;
	size: number;
	progress: number;
	error: string;
};
export type ViewerGpu = {
	id: string;
	kind: string;
	title: string;
	available: boolean;
	/** Why it is not available: "vm" (with a VM) or "driver". */
	reason: string;
};
export type ViewerSession = {
	user: string;
	app: string;
	state: string;
	windows: number;
	gpu: string;
	memoryBytes: number;
};
export type ViewerStatus = {
	image: ImageStatus;
	/** The encoder a new view would use: vaapi, nvenc or cpu. */
	gpu: Gpu;
	gpus: ViewerGpu[];
	sessions: ViewerSession[];
	/** Room for further views (-1 unknown) and memory per view. */
	memory: { more: number; perViewBytes: number; availableBytes: number };
	/** The consoles' idle time in minutes (0 = never), -1 when unknown. */
	idleMinutes: number;
};

const obj = (v: unknown) => (v && typeof v === 'object' ? v : {}) as Record<string, unknown>;
const list = (v: unknown) => (Array.isArray(v) ? v : []).map(obj);

export function sanitizeSettings(v: unknown): ViewerSettings {
	const o = obj(v);
	return {
		gpu: str(o.gpu) || 'auto',
		quality: QUALITIES.find((q) => q === o.quality) ?? 'auto',
		fps: o.fps === 30 ? 30 : 60,
		audio: o.audio !== false,
		volume: str(o.volume),
	};
}

export function sanitizeImage(v: unknown): ImageStatus {
	const o = obj(v);
	const bytes = num(o.bytes);
	const size = num(o.size);
	return {
		state: IMAGE_STATES.find((s) => s === o.state) ?? 'missing',
		version: str(o.version),
		bytes,
		size,
		progress: Math.min(1, Math.max(0, num(o.progress, size > 0 ? bytes / size : 0))),
		error: str(o.error),
	};
}

/** Reads GET /viewer/status tolerantly: missing parts become "unknown", never an error. */
export function sanitizeStatus(v: unknown): ViewerStatus {
	const o = obj(v);
	const memory = obj(o.memory);
	return {
		image: sanitizeImage(o.image),
		gpu: (['vaapi', 'nvenc', 'cpu'] as const).find((g) => g === o.gpu) ?? 'cpu',
		gpus: list(o.gpus)
			.map((g) => ({
				id: str(g.id),
				kind: str(g.kind),
				title: str(g.title) || str(g.id),
				available: g.available !== false,
				reason: str(g.reason),
			}))
			.filter((g) => g.id),
		sessions: list(o.sessions).map((x) => ({
			user: str(x.user),
			app: str(x.app),
			state: str(x.state),
			windows: num(x.windows),
			gpu: str(x.gpu),
			memoryBytes: num(x.memoryBytes),
		})),
		memory: {
			more: num(memory.more, -1),
			perViewBytes: num(memory.perViewBytes),
			availableBytes: num(memory.availableBytes),
		},
		idleMinutes: num(o.idleMinutes, -1),
	};
}

/** Why the browser cannot show a remote view, or null when it can. */
export function missingSupport(g: {
	isSecureContext?: boolean;
	VideoDecoder?: unknown;
}): 'insecure' | 'webcodecs' | null {
	if (typeof g.VideoDecoder === 'function') return null;
	return g.isSecureContext === false ? 'insecure' : 'webcodecs';
}
