// Size of the VM console on the VMs page: what was dragged at its corner
// (stored in the preferences), fitted to the room the window leaves.

import { api } from '#lib/api.ts';
import { apiErrorText } from '#lib/i18n/index.svelte.ts';
import { session } from '#lib/session.svelte.ts';
import { fitHeight, vncHeight, vncWidth } from '#lib/vnc-size.ts';

// What lies between the top of the window and the console when the page
// is scrolled to its toolbar: the sticky header and the toolbar itself.
function above(element: HTMLElement): number {
	const header = document.querySelector('header')?.getBoundingClientRect().height ?? 60;
	const toolbar = element.closest('.vm-page')?.querySelector<HTMLElement>('.command-bar');
	const fromToolbar = toolbar
		? element.getBoundingClientRect().top - toolbar.getBoundingClientRect().top
		: 0;
	return header + 8 + fromToolbar;
}

export class ConsoleSize {
	frame = $state<HTMLElement>();
	fullscreen = $state(false);
	height = $state(vncHeight(session.vnc));
	width = $state(vncWidth(session.vnc));
	error = $state('');
	// Room for the console when its toolbar sits right below the header,
	// with the line below it: then all of the picture is in view at once.
	room = $state(0);
	readonly fitted = $derived(fitHeight(this.height, this.room));

	measure = () => {
		const frame = this.frame;
		if (!frame || this.fullscreen) return;
		const below =
			frame.nextElementSibling instanceof HTMLElement
				? frame.nextElementSibling.offsetHeight + 24
				: 16;
		// Fit where the frame actually starts, keeping the heading and tabs visible.
		const top = Math.max(above(frame), frame.getBoundingClientRect().top);
		this.room = Math.max(1, window.innerHeight - top - below);
	};

	/** Call in an $effect: measures now and again when the window or the page around changes. */
	observe() {
		if (!this.frame) return;
		const element = this.frame;
		this.measure();
		// Notices above the console move it down; the window may change too.
		const sizes = new ResizeObserver(this.measure);
		if (element.parentElement) sizes.observe(element.parentElement);
		const workspace = element.closest('.vm-page');
		if (workspace) sizes.observe(workspace);
		window.addEventListener('resize', this.measure);
		return () => {
			sizes.disconnect();
			window.removeEventListener('resize', this.measure);
		};
	}

	/** Scrolls the page once so that the toolbar sits below the header, if the console's bottom is cut off. */
	bringIntoView() {
		const element = this.frame;
		if (!element) return;
		requestAnimationFrame(() => {
			const box = element.getBoundingClientRect();
			if (box.bottom <= window.innerHeight) return;
			const toolbar =
				element.closest('.vm-page')?.querySelector<HTMLElement>('.command-bar') ?? element;
			const header = document.querySelector('header')?.getBoundingClientRect().height ?? 60;
			window.scrollTo({
				top: toolbar.getBoundingClientRect().top + window.scrollY - header - 8,
				behavior: 'smooth',
			});
		});
	}

	// Width and height are dragged at the corner. They are stored when the
	// mouse lets go, not while dragging, so the stick is written once per change.
	#dragFrom: { width: number; height: number } | null = null;
	dragStart = () => {
		const box = this.frame?.getBoundingClientRect();
		this.#dragFrom = box ? { width: box.width, height: box.height } : null;
	};
	dragEnd = async () => {
		const frame = this.frame;
		if (!this.#dragFrom || !frame || this.fullscreen) return;
		const box = frame.getBoundingClientRect();
		const moved =
			Math.abs(box.width - this.#dragFrom.width) >= 2 ||
			Math.abs(box.height - this.#dragFrom.height) >= 2;
		this.#dragFrom = null;
		if (!moved) return;
		// Dragged to the full column again: follow the column from now on.
		const column = frame.parentElement?.clientWidth ?? 0;
		const nextWidth = box.width >= column - 2 ? null : vncWidth({ width: box.width });
		// A height at the limit of the window keeps what was stored, so a
		// larger window later shows more again.
		const nextHeight =
			Math.abs(box.height - this.fitted) < 2 && this.fitted < this.height
				? this.height
				: vncHeight({ height: box.height });
		if (nextWidth === this.width && nextHeight === this.height) return;
		this.height = nextHeight;
		this.width = nextWidth;
		const value =
			this.width === null ? { height: this.height } : { height: this.height, width: this.width };
		const previous = session.vnc;
		session.vnc = value;
		this.error = '';
		try {
			await api.put('/preferences/vnc', value);
		} catch (err) {
			session.vnc = previous;
			this.error = apiErrorText(err);
		}
	};
}
