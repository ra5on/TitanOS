// PLAN 79-7: live video of a camera. titand passes go2rtc's MSE protocol
// through unchanged (docs/NVR.md "Live-Bild"): the browser says which codecs
// it can play ({"type":"mse","value":"<codecs>"}), go2rtc answers with the
// type of the stream ({"type":"mse","value":"<mime>"}) and then sends
// fragments of an MP4 as binary messages, which go into a SourceBuffer.
// titand adds {"t":"error","code":…} and {"t":"bye","reason":…}.

/** The codecs go2rtc is asked for, most common first (as its own player does). */
const CODECS = [
	'avc1.640029', // H.264 high 4.1
	'avc1.64002A', // H.264 high 4.2
	'avc1.640033', // H.264 high 5.1
	'hvc1.1.6.L153.B0', // H.265 main 5.1
	'mp4a.40.2', // AAC LC
	'mp4a.40.5', // AAC HE
	'flac',
	'opus',
];

type MediaSourceClass = typeof MediaSource;

/** MediaSource, or Safari's ManagedMediaSource (iPhone from iOS 17.1); null: no MSE here. */
export function mediaSourceClass(): { cls: MediaSourceClass; managed: boolean } | null {
	const w = globalThis as unknown as {
		ManagedMediaSource?: MediaSourceClass;
		MediaSource?: MediaSourceClass;
	};
	if (w.ManagedMediaSource) return { cls: w.ManagedMediaSource, managed: true };
	if (w.MediaSource) return { cls: w.MediaSource, managed: false };
	return null;
}

export function supportedCodecs(isSupported: (type: string) => boolean): string {
	return CODECS.filter((codec) => isSupported(`video/mp4; codecs="${codec}"`)).join();
}

export type MseEnd =
	/** The connection ended or stalled: worth another try. */
	| { kind: 'lost' }
	/** titand said why there is no picture (nvr_no_picture, nvr_not_running …). */
	| { kind: 'error'; code: string }
	/** The session ended or the right was taken away: do not try again. */
	| { kind: 'bye'; reason: string }
	/** The browser cannot play what the camera sends. */
	| { kind: 'unsupported' };

export type MseSession = { close: () => void };

/** Seconds of video kept behind the live edge. */
const KEEP_SECONDS = 5;
/** Without a fragment for this long the connection counts as stalled. */
const STALL_MS = 8000;
/** Fragments waiting for the SourceBuffer; more than this means the tab cannot keep up. */
const MAX_PENDING = 8 * 1024 * 1024;

/**
 * Plays a live stream into a video element. `onplaying` fires with the first
 * fragment, `onend` once when the stream is over for any reason.
 */
export function startMse(
	video: HTMLVideoElement,
	url: string,
	onplaying: () => void,
	onend: (end: MseEnd) => void,
): MseSession {
	const found = mediaSourceClass();
	let done = false;
	let ws: WebSocket | null = null;
	let objectURL = '';
	let stall: ReturnType<typeof setTimeout> | undefined;
	let started = false;

	function finish(end: MseEnd | null) {
		if (done) return;
		done = true;
		clearTimeout(stall);
		if (ws) {
			ws.onclose = ws.onerror = ws.onmessage = ws.onopen = null;
			try {
				ws.close();
			} catch {
				/* already closed */
			}
		}
		try {
			video.pause();
			if (objectURL) {
				URL.revokeObjectURL(objectURL);
				video.removeAttribute('src');
			} else video.srcObject = null;
			video.load();
		} catch {
			/* the element is gone */
		}
		if (end) onend(end);
	}

	if (!found) {
		queueMicrotask(() => finish({ kind: 'unsupported' }));
		return { close: () => finish(null) };
	}

	const ms = new found.cls();
	let sb: SourceBuffer | null = null;
	let pending: Uint8Array[] = [];
	let pendingBytes = 0;

	const alive = () => {
		clearTimeout(stall);
		stall = setTimeout(() => finish({ kind: 'lost' }), STALL_MS);
	};

	function flush() {
		if (!sb || sb.updating || !pending.length) return;
		const data = new Uint8Array(pendingBytes);
		let at = 0;
		for (const part of pending) {
			data.set(part, at);
			at += part.byteLength;
		}
		pending = [];
		pendingBytes = 0;
		try {
			sb.appendBuffer(data);
		} catch {
			finish({ kind: 'lost' });
		}
	}

	function trim() {
		if (!sb || sb.updating || !sb.buffered.length) return;
		const end = sb.buffered.end(sb.buffered.length - 1);
		const start = end - KEEP_SECONDS;
		const first = sb.buffered.start(0);
		try {
			if (start > first) {
				sb.remove(first, start);
				if (ms.readyState === 'open') ms.setLiveSeekableRange(start, end);
			}
			// Stay at the live edge: catch up gently, jump when far behind.
			if (video.currentTime < start) video.currentTime = start;
			const gap = end - video.currentTime;
			video.playbackRate = gap > 2 ? 1.5 : gap > 0.6 ? 1.1 : 1;
		} catch {
			/* a remove that overlaps an append: the next update trims */
		}
	}

	ms.addEventListener(
		'sourceopen',
		() => {
			if (done) return;
			try {
				ws = new WebSocket(url);
			} catch {
				finish({ kind: 'lost' });
				return;
			}
			ws.binaryType = 'arraybuffer';
			ws.onopen = () => {
				alive();
				ws?.send(
					JSON.stringify({
						type: 'mse',
						value: supportedCodecs((type) => found.cls.isTypeSupported(type)),
					}),
				);
			};
			ws.onmessage = (event) => {
				if (done) return;
				if (typeof event.data === 'string') {
					let msg: { type?: string; value?: string; t?: string; code?: string; reason?: string };
					try {
						msg = JSON.parse(event.data);
					} catch {
						return;
					}
					if (msg.t === 'error') finish({ kind: 'error', code: msg.code ?? 'nvr_no_picture' });
					else if (msg.t === 'bye') finish({ kind: 'bye', reason: msg.reason ?? '' });
					else if (msg.type === 'error') finish({ kind: 'error', code: 'nvr_no_picture' });
					else if (msg.type === 'mse' && msg.value && !sb) {
						try {
							sb = ms.addSourceBuffer(msg.value);
							sb.mode = 'segments';
							sb.addEventListener('updateend', () => {
								flush();
								trim();
							});
							flush();
						} catch {
							finish({ kind: 'unsupported' });
						}
					}
					return;
				}
				alive();
				const part = new Uint8Array(event.data as ArrayBuffer);
				pending.push(part);
				pendingBytes += part.byteLength;
				if (pendingBytes > MAX_PENDING) {
					finish({ kind: 'lost' });
					return;
				}
				flush();
				if (!started) {
					started = true;
					void video.play().catch(() => {});
					onplaying();
				}
			};
			ws.onerror = () => finish({ kind: 'lost' });
			ws.onclose = () => finish({ kind: 'lost' });
		},
		{ once: true },
	);

	if (found.managed) {
		// Safari: a managed source plays only without AirPlay or as srcObject.
		(video as HTMLVideoElement & { disableRemotePlayback: boolean }).disableRemotePlayback = true;
		video.srcObject = ms as unknown as MediaProvider;
	} else {
		objectURL = URL.createObjectURL(ms);
		video.src = objectURL;
	}
	// The source opens only once the element loads it; never wait for ever.
	stall = setTimeout(() => finish({ kind: 'lost' }), STALL_MS);
	return { close: () => finish(null) };
}
