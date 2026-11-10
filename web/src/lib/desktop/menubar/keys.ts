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

const DESK_CODES: Record<string, string> = { KeyQ: 'q', KeyW: 'w', KeyM: 'm', KeyK: 'k' };

/** Text is being typed here: Alt/Option + arrow moves the cursor by words (macOS). */
function typingTarget(target: EventTarget | null): boolean {
	const el = target as HTMLElement | null;
	if (!el?.closest) return false;
	if (el.isContentEditable) return true;
	const field = el.closest('input, textarea, select') as HTMLInputElement | null;
	if (!field) return false;
	if (field.tagName !== 'INPUT') return true;
	return !['checkbox', 'radio', 'button', 'submit', 'range', 'color', 'file'].includes(field.type);
}

/**
 * PLAN 76 (F-12-10): the desktop's key behind Alt – 'q', 'w', 'm', 'k' or an
 * arrow ('ArrowLeft' …), else ''. On macOS Option changes the character
 * (Option+W gives "∑"), so the letter falls back to the key's place on the
 * keyboard. Arrows stay with a text field.
 */
export function deskKey(event: KeyboardEvent): string {
	if (!event.altKey || event.ctrlKey || event.metaKey) return '';
	const key = event.key;
	if (key.startsWith('Arrow')) return typingTarget(event.target) ? '' : key;
	const lower = key.toLowerCase();
	if (lower === 'q' || lower === 'w' || lower === 'm' || lower === 'k') return lower;
	// A plain letter of another layout stays what it is; only changed characters fall back.
	if (/^[a-z]$/.test(lower)) return '';
	return DESK_CODES[event.code] ?? '';
}
