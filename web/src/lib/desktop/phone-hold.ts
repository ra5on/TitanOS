// Observe a stationary primary press without capturing the pointer or preventing
// the browser from scrolling. The completed hold consumes only its ensuing click.
export function phoneHold(node: HTMLElement, edit: () => void) {
	let timer: ReturnType<typeof setTimeout> | undefined;
	let x = 0,
		y = 0,
		pointer: number | undefined;
	let held = false;
	function cancel() {
		clearTimeout(timer);
		pointer = undefined;
	}
	function start(e: PointerEvent) {
		cancel();
		held = false;
		if (e.button !== 0 || !e.isPrimary) return;
		x = e.clientX;
		y = e.clientY;
		pointer = e.pointerId;
		timer = setTimeout(() => {
			held = true;
			edit();
		}, 550);
	}
	function move(e: PointerEvent) {
		if (e.pointerId === pointer && Math.hypot(e.clientX - x, e.clientY - y) > 8) cancel();
	}
	function click(e: MouseEvent) {
		if (held) {
			e.preventDefault();
			e.stopImmediatePropagation();
			held = false;
		}
	}
	function context(e: Event) {
		e.preventDefault();
	}
	node.addEventListener('pointerdown', start);
	node.addEventListener('pointermove', move);
	node.addEventListener('pointercancel', cancel);
	node.addEventListener('pointerleave', cancel);
	node.addEventListener('pointerup', cancel);
	node.addEventListener('click', click, true);
	node.addEventListener('contextmenu', context);
	return {
		destroy() {
			cancel();
			node.removeEventListener('pointerdown', start);
			node.removeEventListener('pointermove', move);
			node.removeEventListener('pointercancel', cancel);
			node.removeEventListener('pointerleave', cancel);
			node.removeEventListener('pointerup', cancel);
			node.removeEventListener('click', click, true);
			node.removeEventListener('contextmenu', context);
		},
	};
}
