// 1.13 (Baustand step 4, owner 18 Nov 2026): the account's own order of
// the entries in the side bars of Mail (folders per mailbox), Kontakte
// (address books) and Kalender (calendars). Kept on the NAS per account,
// preference kind "order" (backend/internal/api/order_preferences.go):
// a save sends only its program's key, the NAS keeps the others. Names
// the order does not know (a new folder) come after the known ones in the
// program's own order; names that are gone are ignored. Other windows and
// tabs of this browser follow at once (BroadcastChannel).
import { api } from './api.ts';
import { apiErrorText, t } from './i18n/index.svelte.ts';
import { session } from './session.svelte.ts';

export type SideOrder = {
	/** Folder paths per mailbox id. */
	mail: Record<string, string[]>;
	/** Address book ids. */
	contacts: string[];
	/** Calendar ids (the birthdays too). */
	calendar: string[];
	/** 1.15: the order of the mailboxes – the first is the main one. */
	mailAccounts: string[];
	/** 1.15: folder paths taken out of the side bar, per mailbox. */
	mailHidden: Record<string, string[]>;
};
export type OrderProgram = 'mail' | 'contacts' | 'calendar';
type OrderKey = OrderProgram | 'mailAccounts' | 'mailHidden';

// The bounds of the NAS (order_preferences.go); more is cut, never sent.
const MAILBOXES = 20;
const FOLDERS = 500;
const FOLDER_BYTES = 1024;
const COLLECTIONS = 100;
const MAILBOX_ID = /^[a-z0-9][a-z0-9-]{0,31}$/;
// pimstore.ValidCollection: lower-case names or a UUID.
const COLLECTION_ID =
	/^(?:[a-z0-9-]{1,64}|[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12})$/;
// eslint-disable-next-line no-control-regex
const CONTROL = /[\u0000-\u001f\u007f-\u009f]/;

const bytes = (s: string) => new TextEncoder().encode(s).length;

function cleanList(raw: unknown, max: number, valid: (s: string) => boolean): string[] {
	if (!Array.isArray(raw)) return [];
	const out: string[] = [];
	for (const x of raw)
		if (typeof x === 'string' && valid(x) && !out.includes(x) && out.length < max) out.push(x);
	return out;
}
const validFolder = (s: string) => s !== '' && bytes(s) <= FOLDER_BYTES && !CONTROL.test(s);
const validCollection = (s: string) => COLLECTION_ID.test(s);

/** What the NAS sent (or an older one did not): within the bounds, never throws. */
export function sanitizeOrder(raw: unknown): SideOrder {
	const o = raw && typeof raw === 'object' ? (raw as Record<string, unknown>) : {};
	const perBox = (raw: unknown) => {
		const out: Record<string, string[]> = Object.create(null);
		if (raw && typeof raw === 'object' && !Array.isArray(raw))
			for (const [id, list] of Object.entries(raw as Record<string, unknown>).slice(0, MAILBOXES))
				if (MAILBOX_ID.test(id)) out[id] = cleanList(list, FOLDERS, validFolder);
		return out;
	};
	return {
		mail: perBox(o.mail),
		contacts: cleanList(o.contacts, COLLECTIONS, validCollection),
		calendar: cleanList(o.calendar, COLLECTIONS, validCollection),
		mailAccounts: cleanList(o.mailAccounts, MAILBOXES, (s) => MAILBOX_ID.test(s)),
		mailHidden: perBox(o.mailHidden),
	};
}

/**
 * The items in the account's order: those it names first, in its order;
 * the others after them, in the order they came. Stable, never drops one.
 */
export function applyOrder<T>(items: T[], order: string[], keyOf: (item: T) => string): T[] {
	if (!order.length) return items;
	const rank = new Map(order.map((k, i) => [k, i]));
	return items
		.map((item, i) => ({ item, i, r: rank.get(keyOf(item)) }))
		.sort((a, b) => {
			if (a.r !== undefined && b.r !== undefined) return a.r - b.r;
			if (a.r !== undefined) return -1;
			if (b.r !== undefined) return 1;
			return a.i - b.i;
		})
		.map((x) => x.item);
}

/** `keys` with `key` moved to `to` (clamped); the same array when nothing moves. */
export function moveKey(keys: string[], key: string, to: number): string[] {
	const from = keys.indexOf(key);
	if (from < 0) return keys;
	const at = Math.max(0, Math.min(keys.length - 1, to));
	if (at === from) return keys;
	const rest = keys.filter((k) => k !== key);
	return [...rest.slice(0, at), key, ...rest.slice(at)];
}

/**
 * The saved order of one group after it was shown as `shown` (all of the
 * group's current names, in the new order): the group's names first, then
 * the names of the order that are not in the group (other parents' folders
 * of the same mailbox keep their places).
 */
export function mergeGroup(saved: string[], shown: string[]): string[] {
	const inGroup = new Set(shown);
	return [...shown, ...saved.filter((k) => !inGroup.has(k))];
}

export const sideOrderState = $state<{ error: string }>({ error: '' });

/**
 * The account's order now: what the session read, changed in this page
 * or by another window of this browser (both write it back there).
 */
export function sideOrder(): SideOrder {
	return sanitizeOrder(session.order);
}

/** The saved order of a program (for Mail: of one mailbox). */
export function orderOf(program: OrderProgram, mailbox = ''): string[] {
	const o = sideOrder();
	if (program === 'mail') return o.mail[mailbox] ?? [];
	return o[program];
}

const CHANNEL = 'titanos-side-order';
let channel: BroadcastChannel | null = null;
function ownChannel(): BroadcastChannel | null {
	if (channel || typeof BroadcastChannel === 'undefined') return channel;
	channel = new BroadcastChannel(CHANNEL);
	channel.onmessage = (event: MessageEvent) => {
		const data = event.data as { user?: unknown; order?: unknown } | null;
		if (!data || data.user !== (session.data?.user?.name ?? '')) return;
		session.order = sanitizeOrder(data.order) as unknown as Record<string, unknown>;
	};
	return channel;
}
/** Starts following the changes of other windows (the frame calls it once). */
export function followSideOrder() {
	ownChannel();
}

const timers = new Map<OrderKey, ReturnType<typeof setTimeout>>();
/** A moment after the last change (a few steps with the keys are one save). */
const SAVE_AFTER = 600;

/**
 * Sets the order of a program (for Mail: of one mailbox; `mailboxes`, the
 * mailboxes there are now, drops the orders of removed ones) and saves it.
 */
export function setOrder(
	program: OrderProgram,
	list: string[],
	opts: { mailbox?: string; mailboxes?: string[] } = {},
) {
	const next = sanitizeOrder($state.snapshot(sideOrder()));
	if (program === 'mail') {
		if (!opts.mailbox) return;
		next.mail[opts.mailbox] = list;
		if (opts.mailboxes)
			for (const id of Object.keys(next.mail))
				if (!opts.mailboxes.includes(id)) delete next.mail[id];
	} else next[program] = list;
	session.order = sanitizeOrder(next) as unknown as Record<string, unknown>;
	saveSoon(program);
}

/** 1.15 (115-4): the order of the mailboxes, saved on the NAS. */
export function setMailAccounts(ids: string[]) {
	const next = sanitizeOrder($state.snapshot(sideOrder()));
	next.mailAccounts = ids;
	session.order = sanitizeOrder(next) as unknown as Record<string, unknown>;
	saveSoon('mailAccounts');
}

/** 1.15 (115-4): the folders of a mailbox taken out of the side bar. */
export function setMailHidden(mailbox: string, paths: string[], mailboxes: string[]) {
	const next = sanitizeOrder($state.snapshot(sideOrder()));
	next.mailHidden[mailbox] = paths;
	for (const id of Object.keys(next.mailHidden))
		if (!mailboxes.includes(id) || !next.mailHidden[id].length) delete next.mailHidden[id];
	session.order = sanitizeOrder(next) as unknown as Record<string, unknown>;
	saveSoon('mailHidden');
}

function saveSoon(program: OrderKey) {
	clearTimeout(timers.get(program));
	timers.set(
		program,
		setTimeout(() => {
			timers.delete(program);
			const value = $state.snapshot(sideOrder()[program]);
			api.put('/preferences/order', { [program]: value }).then(
				() => {
					sideOrderState.error = '';
					ownChannel()?.postMessage({
						user: session.data?.user?.name ?? '',
						order: $state.snapshot(sideOrder()),
					});
				},
				(err) => {
					sideOrderState.error = t('ui.sort.saveFailed', { reason: apiErrorText(err) });
				},
			);
		}, SAVE_AFTER),
	);
}
