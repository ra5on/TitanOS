// Plays a stretch of a camera's recording (HLS from the NAS, Frigate's
// VOD) in a video element: the recordings page and an event without its
// clip (PLAN 82-5) share it.

/**
 * Chrome, Edge and Safari play HLS themselves; Firefox needs hls.js.
 * `ready` runs once the stream can start, `failed` when it cannot
 * (`unsupported`: the browser lacks the camera's codec). Returns what
 * stops hls.js again (nothing to stop for the browser's own player).
 */
export async function attachVod(
	el: HTMLVideoElement,
	src: string,
	ready: () => void,
	failed: (unsupported: boolean) => void,
): Promise<() => void> {
	if (el.canPlayType('application/vnd.apple.mpegurl')) {
		el.src = src;
		el.addEventListener('loadedmetadata', ready, { once: true });
		return () => {};
	}
	try {
		const { default: Hls } = await import('hls.js');
		if (!Hls.isSupported()) {
			failed(true);
			return () => {};
		}
		const player = new Hls();
		player.on(Hls.Events.ERROR, (_event, data) => {
			// A browser without the camera's codec (H.264/H.265) says so in plain words.
			if (data.details === Hls.ErrorDetails.MANIFEST_INCOMPATIBLE_CODECS_ERROR) failed(true);
			else if (data.fatal) failed(false);
		});
		player.on(Hls.Events.MANIFEST_PARSED, ready);
		player.loadSource(src);
		player.attachMedia(el);
		return () => player.destroy();
	} catch {
		failed(false);
		return () => {};
	}
}

/**
 * Starts a video at once: with sound where the browser allows it, else
 * without (a page opened from a push has had no tap yet). Says whether it
 * plays muted.
 */
export async function autoplay(el: HTMLVideoElement): Promise<boolean> {
	try {
		await el.play();
		return false;
	} catch {
		el.muted = true;
		try {
			await el.play();
		} catch {
			/* the controls are there to start it by hand */
		}
		return true;
	}
}
