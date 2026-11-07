// PLAN 67-1: how a page in a window tells the top bar which menu commands it
// has, and how the top bar runs one. Only ids and states cross the frame;
// texts and shortcuts come from menus.ts, never from the page – except the
// entries of a list submenu (shares, recent files, keys to send …), which
// are plain text the bar shows as text, never as markup.
//
//   frame → top:  { type: 'titanos-menu', commands: [{ id, enabled, checked?, items? }] }
//   top → frame:  { type: 'titanos-menu-action', id, arg? }   (arg: the chosen list entry)
//   top → frame:  { type: 'titanos-menu-get' }   (asks for the states again)

export type ListItem = { id: string; text: string; checked?: boolean };
export type CommandState = {
	id: string;
	enabled: boolean;
	checked?: boolean;
	items?: ListItem[];
};
export type CommandStates = Record<
	string,
	{ enabled: boolean; checked?: boolean; items?: ListItem[] }
>;

export type MenuMessage =
	| { type: 'titanos-menu'; commands: CommandState[] }
	| { type: 'titanos-menu-action'; id: string; arg?: string }
	| { type: 'titanos-menu-get' };

/** No page has more; anything longer is not ours. */
export const MAX_COMMANDS = 300;
/** Entries of one list submenu. */
export const MAX_ITEMS = 40;
const MAX_ARG = 1000;
const MAX_TEXT = 120;
const ID = /^[a-z][a-zA-Z0-9]*(\.[a-zA-Z0-9]+){1,3}$/;

export function validCommandId(id: unknown): id is string {
	return typeof id === 'string' && id.length <= 80 && ID.test(id);
}

function readItems(raw: unknown): ListItem[] | undefined {
	if (!Array.isArray(raw)) return undefined;
	const out: ListItem[] = [];
	for (const entry of raw.slice(0, MAX_ITEMS)) {
		if (!entry || typeof entry !== 'object') continue;
		const e = entry as { id?: unknown; text?: unknown; checked?: unknown };
		if (typeof e.id !== 'string' || e.id.length > MAX_ARG) continue;
		if (typeof e.text !== 'string' || !e.text.trim()) continue;
		const item: ListItem = { id: e.id, text: e.text.slice(0, MAX_TEXT) };
		if (typeof e.checked === 'boolean') item.checked = e.checked;
		out.push(item);
	}
	return out;
}

/**
 * Reads what a frame announced: only well-formed entries whose id the
 * program's menus know. Null when the message is not an announcement at all.
 */
export function readCommandStates(data: unknown, known: Set<string>): CommandStates | null {
	if (!data || typeof data !== 'object') return null;
	const msg = data as { type?: unknown; commands?: unknown };
	if (msg.type !== 'titanos-menu' || !Array.isArray(msg.commands)) return null;
	if (msg.commands.length > MAX_COMMANDS) return null;
	const out: CommandStates = {};
	for (const raw of msg.commands) {
		if (!raw || typeof raw !== 'object') continue;
		const c = raw as { id?: unknown; enabled?: unknown; checked?: unknown; items?: unknown };
		if (!validCommandId(c.id) || !known.has(c.id) || typeof c.enabled !== 'boolean') continue;
		const state: CommandStates[string] = { enabled: c.enabled };
		if (typeof c.checked === 'boolean') state.checked = c.checked;
		const items = readItems(c.items);
		if (items) state.items = items;
		out[c.id] = state;
	}
	return out;
}

/** Reads a request of the top bar (in the frame). */
export function readMenuRequest(
	data: unknown,
): { type: 'titanos-menu-action'; id: string; arg?: string } | { type: 'titanos-menu-get' } | null {
	if (!data || typeof data !== 'object') return null;
	const msg = data as { type?: unknown; id?: unknown; arg?: unknown };
	if (msg.type === 'titanos-menu-get') return { type: 'titanos-menu-get' };
	if (msg.type !== 'titanos-menu-action' || !validCommandId(msg.id)) return null;
	if (msg.arg === undefined) return { type: 'titanos-menu-action', id: msg.id };
	if (typeof msg.arg !== 'string' || msg.arg.length > MAX_ARG) return null;
	return { type: 'titanos-menu-action', id: msg.id, arg: msg.arg };
}
