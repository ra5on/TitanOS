// PLAN 67-1: Alt alone or F10 puts the keyboard into the menu bar. Alt
// counts only when no other key (and no click) came between pressing and
// releasing it, so Alt+Q, Alt+W … stay what they are. Keys that belong to a
// screen (VM, remote view, terminal) stay there.

const OWN = 'canvas, .xterm, .terminal, [data-keys="own"]';

let altAlone = false;

/** Whether the key press belongs to a remote screen or a terminal. */
export function screenOwnsKeys(target: EventTarget | null): boolean {
	const el = target as Element | null;
	return !!el?.closest?.(OWN);
}

/** Call on every keydown; true when the menu bar should take the focus (F10). */
export function menuKeyDown(event: KeyboardEvent): boolean {
	if (event.key === 'Alt') {
		altAlone = !event.repeat && !event.ctrlKey && !event.metaKey && !event.shiftKey;
		if (screenOwnsKeys(event.target)) altAlone = false;
		return false;
	}
	altAlone = false;
	return (
		event.key === 'F10' &&
		!event.shiftKey &&
		!event.altKey &&
		!event.ctrlKey &&
		!event.metaKey &&
		!screenOwnsKeys(event.target)
	);
}

/** Call on every keyup; true when Alt was pressed and released alone. */
export function menuKeyUp(event: KeyboardEvent): boolean {
	if (event.key !== 'Alt') return false;
	const alone = altAlone;
	altAlone = false;
	return alone;
}

/** A click or anything else between: Alt is no longer alone. */
export function menuKeyReset() {
	altAlone = false;
}
