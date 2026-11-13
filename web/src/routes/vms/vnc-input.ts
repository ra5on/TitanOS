// PLAN 87: input for the VM screen on a phone. noVNC takes keys only as
// keydown events on its canvas; a phone neither opens its keyboard for a
// canvas nor sends usable keydowns (Android keyboards send "Unidentified",
// key code 229, and the text arrives as input). So the screen keeps a hidden
// text field: what changes in it is turned into X11 keysyms here.

/** X11 keysyms of the keys the screen sends itself. */
export const KEYSYM = {
	BackSpace: 0xff08,
	Tab: 0xff09,
	Return: 0xff0d,
	Escape: 0xff1b,
	Delete: 0xffff,
	Home: 0xff50,
	Left: 0xff51,
	Up: 0xff52,
	Right: 0xff53,
	Down: 0xff54,
	PageUp: 0xff55,
	PageDown: 0xff56,
	End: 0xff57,
	Insert: 0xff63,
	Shift: 0xffe1,
	Control: 0xffe3,
	Alt: 0xffe9,
	Super: 0xffeb,
} as const;

/** F1 … F12. */
export const fKey = (n: number) => 0xffbe + (n - 1);

/** The physical key (KeyboardEvent.code) noVNC passes on, where there is one. */
export const KEY_CODE: Record<number, string> = {
	[KEYSYM.BackSpace]: 'Backspace',
	[KEYSYM.Tab]: 'Tab',
	[KEYSYM.Return]: 'Enter',
	[KEYSYM.Escape]: 'Escape',
	[KEYSYM.Delete]: 'Delete',
	[KEYSYM.Home]: 'Home',
	[KEYSYM.Left]: 'ArrowLeft',
	[KEYSYM.Up]: 'ArrowUp',
	[KEYSYM.Right]: 'ArrowRight',
	[KEYSYM.Down]: 'ArrowDown',
	[KEYSYM.PageUp]: 'PageUp',
	[KEYSYM.PageDown]: 'PageDown',
	[KEYSYM.End]: 'End',
	[KEYSYM.Insert]: 'Insert',
	[KEYSYM.Shift]: 'ShiftLeft',
	[KEYSYM.Control]: 'ControlLeft',
	[KEYSYM.Alt]: 'AltLeft',
	[KEYSYM.Super]: 'MetaLeft',
	...Object.fromEntries(Array.from({ length: 12 }, (_, i) => [fKey(i + 1), `F${i + 1}`])),
};

/** Named keys a keyboard reports with a usable `key` (a hardware keyboard, or Enter/Backspace of most phone keyboards). */
const NAMED: Record<string, number> = {
	Backspace: KEYSYM.BackSpace,
	Tab: KEYSYM.Tab,
	Enter: KEYSYM.Return,
	Escape: KEYSYM.Escape,
	Delete: KEYSYM.Delete,
	Home: KEYSYM.Home,
	End: KEYSYM.End,
	PageUp: KEYSYM.PageUp,
	PageDown: KEYSYM.PageDown,
	Insert: KEYSYM.Insert,
	ArrowLeft: KEYSYM.Left,
	ArrowUp: KEYSYM.Up,
	ArrowRight: KEYSYM.Right,
	ArrowDown: KEYSYM.Down,
	...Object.fromEntries(Array.from({ length: 12 }, (_, i) => [`F${i + 1}`, fKey(i + 1)])),
};

/** The keysym of a named key, or null (characters come through the text field). */
export function namedKeysym(key: string): number | null {
	return NAMED[key] ?? null;
}

/**
 * The keysym of one character: Latin-1 is the code point itself, a few
 * legacy keysyms the servers know by name, everything else the Unicode
 * keysym 0x01000000 + code point.
 */
export function keysymForChar(ch: string): number {
	const code = ch.codePointAt(0) ?? 0;
	if (ch === '\n') return KEYSYM.Return;
	if (ch === '\t') return KEYSYM.Tab;
	if ((code >= 0x20 && code <= 0x7e) || (code >= 0xa0 && code <= 0xff)) return code;
	if (code === 0x20ac) return 0x20ac; // EuroSign
	return 0x01000000 | code;
}

/**
 * What changed in the text field: how many characters were taken back at
 * its end and what was typed after that. Phone keyboards rewrite a whole
 * word at once (autocorrect, composition); the guest sees Backspaces and
 * the new letters, as if typed.
 */
export function diffInput(before: string, after: string): { back: number; text: string } {
	const a = [...before];
	const b = [...after];
	let same = 0;
	while (same < a.length && same < b.length && a[same] === b[same]) same++;
	return { back: a.length - same, text: b.slice(same).join('') };
}

/** The field always keeps this one character, so Backspace still has something to delete. */
export const SENTINEL = ' ';

/**
 * Trackpad: a finger's movement becomes a faster cursor the faster it
 * moves (like a laptop's touchpad). `dt` in milliseconds.
 */
export function accelerate(delta: number, dt: number): number {
	const speed = Math.abs(delta) / Math.max(dt, 1);
	const factor = speed < 0.2 ? 1 : speed < 0.8 ? 1.6 : 2.4;
	return delta * factor;
}

/** PLAN 87: the screen is used by touch – the phone, or a tablet without a mouse. */
export function touchScreen(phone: boolean): boolean {
	return phone || (typeof matchMedia === 'function' && matchMedia('(pointer: coarse)').matches);
}

const POINTER_KEY = 'titanos.vnc.pointer';
/** How the finger works on the picture, kept in this browser (it belongs to the device). */
export function readPointerMode(): 'trackpad' | 'direct' {
	try {
		return localStorage.getItem(POINTER_KEY) === 'direct' ? 'direct' : 'trackpad';
	} catch {
		return 'trackpad';
	}
}
export function keepPointerMode(mode: 'trackpad' | 'direct') {
	try {
		localStorage.setItem(POINTER_KEY, mode);
	} catch {
		// Not kept in this browser; trackpad again next time.
	}
}
