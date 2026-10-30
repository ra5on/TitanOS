type ScrollState = { overflow: boolean; left: boolean; right: boolean };

// Keep touch/trackpad scrolling and ordinary link clicks native. Only a mouse
// movement beyond the threshold becomes a drag, including releases outside nav.
export function horizontalNav(shell: HTMLElement, onChange: (state: ScrollState) => void) {
	const nav = shell.querySelector('nav')!;
	const items = nav.firstElementChild as HTMLElement;
	let pointer: number | undefined;
	let startX = 0;
	let startScroll = 0;
	let dragged = false;
	let suppressClick = false;
	const update = () =>
		onChange({
			overflow: items.scrollWidth > shell.clientWidth + 1,
			left: nav.scrollLeft > 1,
			right: nav.scrollLeft + nav.clientWidth < nav.scrollWidth - 1,
		});
	const down = (event: PointerEvent) => {
		if (
			event.pointerType !== 'mouse' ||
			event.button !== 0 ||
			event.ctrlKey ||
			event.metaKey ||
			event.shiftKey ||
			event.altKey
		)
			return;
		// Let the native scrollbar handle clicks and thumb dragging itself.
		if (event.clientY >= nav.getBoundingClientRect().top + nav.clientHeight) return;
		pointer = event.pointerId;
		startX = event.clientX;
		startScroll = nav.scrollLeft;
		dragged = suppressClick = false;
	};
	const move = (event: PointerEvent) => {
		if (pointer !== event.pointerId) return;
		if (!(event.buttons & 1)) {
			end();
			return;
		}
		const delta = event.clientX - startX;
		if (!dragged && Math.abs(delta) < 5) return;
		if (!dragged) {
			dragged = true;
			nav.setPointerCapture(event.pointerId);
			nav.classList.add('dragging');
		}
		suppressClick = true;
		event.preventDefault();
		nav.scrollLeft = startScroll - delta;
	};
	const end = () => {
		if (pointer !== undefined && nav.hasPointerCapture(pointer)) nav.releasePointerCapture(pointer);
		pointer = undefined;
		nav.classList.remove('dragging');
	};
	const click = (event: MouseEvent) => {
		if (!suppressClick || event.detail === 0) return;
		suppressClick = false;
		event.preventDefault();
		event.stopPropagation();
	};
	const observer = new ResizeObserver(() => {
		// A resize (including arrows appearing or a language change) must not
		// leave the active tab behind an arrow or outside the visible strip.
		const active = items.querySelector('[aria-current]');
		if (active) {
			const box = active.getBoundingClientRect();
			const viewport = nav.getBoundingClientRect();
			if (box.left < viewport.left || box.width > viewport.width)
				nav.scrollLeft += box.left - viewport.left;
			else if (box.right > viewport.right) nav.scrollLeft += box.right - viewport.right;
		}
		update();
	});
	for (const element of [shell, nav, items]) observer.observe(element);
	nav.addEventListener('scroll', update, { passive: true });
	nav.addEventListener('pointerdown', down);
	window.addEventListener('pointermove', move);
	window.addEventListener('pointerup', end);
	window.addEventListener('pointercancel', end);
	window.addEventListener('blur', end);
	nav.addEventListener('lostpointercapture', end);
	nav.addEventListener('click', click, true);
	update();
	return {
		destroy() {
			end();
			observer.disconnect();
			nav.removeEventListener('scroll', update);
			nav.removeEventListener('pointerdown', down);
			window.removeEventListener('pointermove', move);
			window.removeEventListener('pointerup', end);
			window.removeEventListener('pointercancel', end);
			window.removeEventListener('blur', end);
			nav.removeEventListener('lostpointercapture', end);
			nav.removeEventListener('click', click, true);
		},
	};
}
