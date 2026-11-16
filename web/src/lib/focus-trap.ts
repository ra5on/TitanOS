// Sheets and panels of the KI-Agent that are no <dialog> (1.01): on the
// phone the focus left them – Tab went on to the page behind, Escape closed
// every open sheet at once and the focus did not come back to the button
// that opened them. A <dialog> opened with showModal() does all this itself
// (Dialog.svelte); this action does it for the others:
//   - the focus goes in (the element itself when it has a tabindex, or its
//     first control) and Tab/Shift+Tab stay inside;
//   - Escape closes only the sheet on top, not one under it, and not when it
//     belongs to a <dialog> opened above;
//   - when it closes, the focus goes back to where it was.

export type TrapOptions = {
	/** Escape on this sheet (it is the one on top). */
	onescape?: () => void;
	/** 'first': the first control gets the focus, not the sheet itself. */
	initial?: 'self' | 'first';
};

const FOCUSABLE = [
	'a[href]',
	'button:not([disabled])',
	'input:not([disabled]):not([type="hidden"])',
	'select:not([disabled])',
	'textarea:not([disabled])',
	'[contenteditable]:not([contenteditable="false"])',
	'[tabindex]:not([tabindex="-1"])',
].join(',');

/** The open traps, the last one on top. */
const stack: HTMLElement[] = [];

function controls(node: HTMLElement): HTMLElement[] {
	return [...node.querySelectorAll<HTMLElement>(FOCUSABLE)].filter(
		(el) => !el.closest('[hidden], [inert]') && !el.closest('dialog:not([open])'),
	);
}

/** Inside an open <dialog>: it answers its keys itself. */
export function inDialog(target: EventTarget | null): boolean {
	return target instanceof Element && !!target.closest('dialog[open]');
}

/** Inside a <dialog> that is open above the sheet (it handles its keys). */
function inOpenDialog(node: HTMLElement, target: EventTarget | null): boolean {
	const dialog = target instanceof Element ? target.closest('dialog[open]') : null;
	return !!dialog && dialog !== node && !dialog.contains(node);
}

export function trapFocus(node: HTMLElement, options: TrapOptions = {}) {
	let opts = options;
	const back = document.activeElement instanceof HTMLElement ? document.activeElement : null;
	stack.push(node);
	// A sheet that is closing (Svelte makes it inert during its transition
	// out) no longer holds the focus: what it opened may take it.
	const top = () => stack[stack.length - 1] === node && !node.closest('[inert]');
	const enter = () => {
		const first = controls(node)[0];
		if (opts.initial !== 'first' && node.hasAttribute('tabindex')) node.focus();
		else if (first) first.focus();
		else {
			node.tabIndex = -1;
			node.focus();
		}
	};
	enter();

	const keydown = (e: KeyboardEvent) => {
		if (!top() || e.defaultPrevented || inOpenDialog(node, e.target)) return;
		if (e.key === 'Escape') {
			if (!opts.onescape) return;
			e.preventDefault();
			e.stopPropagation();
			opts.onescape();
			return;
		}
		if (e.key !== 'Tab') return;
		const items = controls(node);
		if (!items.length) {
			e.preventDefault();
			node.focus();
			return;
		}
		const first = items[0];
		const last = items[items.length - 1];
		const active = document.activeElement;
		if (!node.contains(active)) {
			e.preventDefault();
			(e.shiftKey ? last : first).focus();
		} else if (e.shiftKey && (active === first || active === node)) {
			e.preventDefault();
			last.focus();
		} else if (!e.shiftKey && active === last) {
			e.preventDefault();
			first.focus();
		}
	};
	// A tap or a screen reader that moves the focus out brings it back in.
	const focusin = (e: FocusEvent) => {
		if (!top() || !(e.target instanceof Node) || node.contains(e.target)) return;
		if (inOpenDialog(node, e.target)) return;
		enter();
	};
	document.addEventListener('keydown', keydown);
	document.addEventListener('focusin', focusin);
	return {
		update(next: TrapOptions = {}) {
			opts = next;
		},
		destroy() {
			document.removeEventListener('keydown', keydown);
			document.removeEventListener('focusin', focusin);
			const i = stack.lastIndexOf(node);
			if (i >= 0) stack.splice(i, 1);
			const active = document.activeElement;
			const lost = !active || active === document.body || node.contains(active);
			if (lost && back?.isConnected) back.focus({ preventScroll: true });
		},
	};
}
