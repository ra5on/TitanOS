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

/** The widest picture kept of the last frame (a 4K frame would take 33 MB). */
const FRAME_WIDTH = 1280;

/** The picture on the screen, for the caller to show while the stream comes back. */
function grabFrame(video: HTMLVideoElement): HTMLCanvasElement | undefined {
	if (video.readyState < 2 || !video.videoWidth || !video.videoHeight) return undefined;
	try {
		const scale = Math.min(1, FRAME_WIDTH / video.videoWidth);
		const canvas = document.createElement('canvas');
		canvas.width = Math.round(video.videoWidth * scale);
		canvas.height = Math.round(video.videoHeight * scale);
		canvas.getContext('2d')?.drawImage(video, 0, 0, canvas.width, canvas.height);
		return canvas;
	} catch {
		return undefined;
	}
}

/**
 * Seconds of video kept behind what is shown. More than the longest key
 * frame interval of a camera: removing the key frame of the picture on the
 * screen removes everything up to the next key frame (MSE's coded frame
 * removal), and the picture stops until that key frame comes.
 */
const KEEP_BEHIND = 30;
/** Old video is removed in steps of at least this many seconds, not with every fragment. */
const TRIM_STEP = 10;
/** Further behind the live edge than this, the picture jumps to it (a hidden tab, a stall). */
const JUMP_BEHIND = 5;
/** Without a fragment for this long the connection counts as stalled. */
const STALL_MS = 8000;
/** Fragments waiting for the SourceBuffer; more than this means the tab cannot keep up. */
const MAX_PENDING = 8 * 1024 * 1024;

/**
 * Plays a live stream into a video element. `onplaying` fires once the
 * first picture plays (until then the element shows nothing: the caller
 * keeps its still picture), `onend` once when the stream is over for any
 * reason – with the last picture shown, if there was one, so that the
 * caller never shows an empty element while the stream comes back.
 */
export function startMse(
	video: HTMLVideoElement,
	url: string,
	onplaying: () => void,
	onend: (end: MseEnd, frame?: HTMLCanvasElement) => void,
): MseSession {
	const found = mediaSourceClass();
	let done = false;
	let ws: WebSocket | null = null;
	let objectURL = '';
	let stall: ReturnType<typeof setTimeout> | undefined;
	let started = false;
	let shown = false;

	const playing = () => {
		if (done || shown) return;
		shown = true;
		onplaying();
	};
	// A picture the browser cannot decode after all ends the stream.
	const broken = () => {
		if (video.error) finish({ kind: 'lost' });
	};
	video.addEventListener('playing', playing);
	video.addEventListener('error', broken);

	function finish(end: MseEnd | null) {
		if (done) return;
		done = true;
		clearTimeout(stall);
		video.removeEventListener('playing', playing);
		video.removeEventListener('error', broken);
		const frame = end && shown ? grabFrame(video) : undefined;
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
		if (end) onend(end, frame);
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

	/** Stays at the live edge: catches up gently, jumps when far behind. */
	function catchUp() {
		if (!sb || !sb.buffered.length) return;
		const last = sb.buffered.length - 1;
		const edge = sb.buffered.end(last);
		const from = sb.buffered.start(last);
		try {
			if (ms.readyState === 'open')
				ms.setLiveSeekableRange(Math.max(from, edge - KEEP_BEHIND), edge);
			if (video.currentTime < from || edge - video.currentTime > JUMP_BEHIND)
				video.currentTime = Math.max(from, edge - 1);
			const gap = edge - video.currentTime;
			video.playbackRate = gap > 2 ? 1.5 : gap > 0.6 ? 1.1 : 1;
		} catch {
			/* the source closed meanwhile */
		}
	}

	/**
	 * Removes video long played, KEEP_BEHIND seconds behind the picture and
	 * at least TRIM_STEP at once. True: a removal runs, appending waits for
	 * its end.
	 */
	function trim(): boolean {
		if (!sb || sb.updating || !sb.buffered.length) return false;
		const first = sb.buffered.start(0);
		const upTo = video.currentTime - KEEP_BEHIND;
		if (upTo - first < TRIM_STEP) return false;
		try {
			sb.remove(first, upTo);
			return true;
		} catch {
			return false;
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
								if (done) return;
								catchUp();
								if (!trim()) flush();
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
					// With sound the browser may refuse to start without a tap:
					// then without sound, and if even that fails, the element is
					// shown anyway (its controls are the page's own).
					void video
						.play()
						.catch(() => {
							video.muted = true;
							return video.play();
						})
						.catch(playing);
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
