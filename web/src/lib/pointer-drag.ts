/**
 * Follows one pointer from a press until it is released, the same way the
 * dashboard editor moves its tiles: listeners on the window (a live preview
 * moves the dragged node and takes the pointer capture away from its handle),
 * a small threshold before anything moves, and Escape, a cancelled pointer or
 * leaving the window abort, so nothing stays half-dragged.
 * Returns a function that aborts the drag (for example when the page closes).
 */
export function dragPointer(
	event: PointerEvent,
	handlers: {
		/** Called for every move once the pointer travelled `threshold` pixels. */
		move: (e: PointerEvent) => void;
		/** Released after a move. */
		end: () => void;
		/** Escape, pointercancel or the window lost focus after a move. */
		abort: () => void;
		/** Released or aborted before the threshold: nothing was dragged. */
		none?: () => void;
	},
	threshold = 5,
): () => void {
	const start = { x: event.clientX, y: event.clientY };
	const handle = event.currentTarget instanceof Element ? event.currentTarget : null;
	let moved = false,
		done = false;
	try {
		handle?.setPointerCapture(event.pointerId);
	} catch {
		// A synthetic or already released pointer cannot be captured.
	}
	function finish(action: (() => void) | undefined) {
		if (done) return;
		done = true;
		window.removeEventListener('pointermove', onMove);
		window.removeEventListener('pointerup', onUp);
		window.removeEventListener('pointercancel', onCancel);
		window.removeEventListener('blur', abort);
		document.removeEventListener('keydown', onKey, true);
		if (handle?.hasPointerCapture(event.pointerId)) handle.releasePointerCapture(event.pointerId);
		action?.();
	}
	const abort = () => finish(moved ? handlers.abort : handlers.none);
	const onMove = (e: PointerEvent) => {
		if (e.pointerId !== event.pointerId) return;
		if (!moved && Math.abs(e.clientX - start.x) + Math.abs(e.clientY - start.y) < threshold) return;
		moved = true;
		handlers.move(e);
	};
	const onUp = (e: PointerEvent) =>
		e.pointerId === event.pointerId && finish(moved ? handlers.end : handlers.none);
	const onCancel = (e: PointerEvent) => e.pointerId === event.pointerId && abort();
	const onKey = (e: KeyboardEvent) => {
		if (e.key !== 'Escape') return;
		e.preventDefault();
		e.stopPropagation();
		abort();
	};
	window.addEventListener('pointermove', onMove);
	window.addEventListener('pointerup', onUp);
	window.addEventListener('pointercancel', onCancel);
	window.addEventListener('blur', abort);
	document.addEventListener('keydown', onKey, true);
	return abort;
}
