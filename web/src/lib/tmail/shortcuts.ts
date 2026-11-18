// 1.12 (owner 18 Nov 2026, "Tastenkürzel"): the keys of the list. Never
// while one types (fields, editable text), never with Ctrl, Cmd or Alt
// (those belong to the browser and the desktop), never while a dialog is
// open. The window of T-Mail is a frame of its own: its keys never reach
// another program.

export type MailKey = 'next' | 'prev' | 'archive' | 'reply' | 'delete' | 'search' | 'help';

/** The keys as the help shows them, in its order. */
export const MAIL_KEYS: { action: MailKey; keys: string[] }[] = [
	{ action: 'next', keys: ['j'] },
	{ action: 'prev', keys: ['k'] },
	{ action: 'archive', keys: ['e'] },
	{ action: 'reply', keys: ['r'] },
	{ action: 'delete', keys: ['Entf', 'Delete'] },
	{ action: 'search', keys: ['/'] },
	{ action: 'help', keys: ['?'] },
];

type KeyLike = Pick<
	KeyboardEvent,
	'key' | 'ctrlKey' | 'metaKey' | 'altKey' | 'defaultPrevented'
> & {
	target: EventTarget | null;
};

/** Whether the focus is where one types (a field, an editable text, a list box). */
export function typingIn(target: EventTarget | null): boolean {
	if (!target || typeof (target as Element).closest !== 'function') return false;
	const el = target as Element;
	return !!el.closest(
		'input:not([type=checkbox]):not([type=radio]):not([type=button]),textarea,select,[contenteditable=""],[contenteditable="true"],[role="textbox"],[role="combobox"],[role="menu"],dialog',
	);
}

/** The action of a key, or null when it is not one of T-Mail's here. */
export function mailKey(e: KeyLike): MailKey | null {
	if (e.defaultPrevented || e.ctrlKey || e.metaKey || e.altKey || typingIn(e.target)) return null;
	switch (e.key) {
		case 'j':
		case 'J':
			return 'next';
		case 'k':
		case 'K':
			return 'prev';
		case 'e':
		case 'E':
			return 'archive';
		case 'r':
		case 'R':
			return 'reply';
		case 'Delete':
			return 'delete';
		case '/':
			return 'search';
		case '?':
			return 'help';
	}
	return null;
}
