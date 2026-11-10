// PLAN 76 (F-12-12): the browser saves a file the NAS sends as an attachment.
// The request runs in a hidden frame, never in the page itself: an answer
// that is no file (an error as JSON) would otherwise replace the page – the
// file manager with its upload queue gone, raw JSON in its place. Nothing
// passes through the page's memory.

/**
 * Starts the download. `failed` is called with the error code of the NAS
 * ("" if it cannot be read) when the answer was a page instead of a file.
 */
export function saveFile(url: string, failed?: (code: string) => void) {
	const frame = document.createElement('iframe');
	frame.hidden = true;
	frame.setAttribute('aria-hidden', 'true');
	frame.tabIndex = -1;
	// An attachment never fires "load"; a document shown in the frame does.
	frame.addEventListener('load', () => {
		let code = '';
		try {
			const text = frame.contentDocument?.body?.textContent ?? '';
			const data = JSON.parse(text) as { error?: { code?: unknown } | string; code?: unknown };
			const found =
				typeof data.error === 'object' && data.error ? data.error.code : (data.code ?? data.error);
			if (typeof found === 'string') code = found;
		} catch {
			// Not JSON (a proxy's error page): no code.
		}
		frame.remove();
		failed?.(code);
	});
	frame.src = url;
	document.body.append(frame);
	// The frame is only needed until the browser took the download over.
	setTimeout(() => frame.remove(), 10 * 60_000);
}
