import { soundForNotices } from './sound.svelte.ts';
import { api } from './api.ts';
import { tOr } from './i18n/index.svelte.ts';
import { bellShows, sanitizeBell, type BellSettings } from './notify-groups.ts';

export type Notice = {
	id: number;
	time: string;
	severity: 'info' | 'warning' | 'error';
	kind: string;
	subject: string;
	/** Title and text as they are sent out, in the language of the settings. */
	title: string;
	text: string;
	/** The page the message is about (a camera's event, PLAN 82-5). */
	link?: string;
	/** A sentence after the text, e.g. ha_unknown (PLAN 83). */
	note?: string;
};

/** A page of TitanOS a message may open: never another site. */
export const noticeOwnLink = (notice: Notice) =>
	notice.link?.startsWith('/') && !notice.link.startsWith('//') ? notice.link : '';

const SEEN_KEY = 'titanos.notifications.seen';

function readSeen(): number {
	try {
		return Number(localStorage.getItem(SEEN_KEY)) || 0;
	} catch {
		return 0;
	}
}

const READ_KEY = 'titanos.notifications.read';

function readIds(): number[] {
	try {
		const list = JSON.parse(localStorage.getItem(READ_KEY) ?? '[]');
		return Array.isArray(list) ? list.filter((id) => Number.isInteger(id)).slice(-200) : [];
	} catch {
		return [];
	}
}

/**
 * What happened on the NAS, the newest first; `seen` is the time up to which
 * it was looked at in this browser, `read` single messages marked read
 * (PLAN 87: a swipe in the phone's list).
 */
export const notifications = $state<{ list: Notice[]; seen: number; read: number[] }>({
	list: [],
	seen: readSeen(),
	read: readIds(),
});

/** PLAN 87: one message counts as read (kept in this browser, like `seen`). */
export function markRead(id: number) {
	if (notifications.read.includes(id)) return;
	notifications.read = [...notifications.read, id].slice(-200);
	try {
		localStorage.setItem(READ_KEY, JSON.stringify(notifications.read));
	} catch {
		// Without storage it counts as new again after a reload.
	}
}

/** Whether a message is new to this browser. */
export function isUnread(notice: Notice): boolean {
	return Date.parse(notice.time) > notifications.seen && !notifications.read.includes(notice.id);
}

/** What this account wants to see in its bell; the NAS keeps every message. */
export const bell = $state<{ settings: BellSettings }>({ settings: sanitizeBell(null) });

/** The messages the bell shows under the account's filter. */
export function visibleNotices(): Notice[] {
	return notifications.list.filter((notice) => bellShows(bell.settings, notice));
}

let generation = 0;

export async function loadNotifications() {
	const request = ++generation;
	try {
		const result = await api.get<{ messages: Notice[] }>('/notifications');
		if (request === generation) {
			notifications.list = result.messages;
			soundForNotices(result.messages.filter((notice) => bellShows(bell.settings, notice)));
		}
	} catch {
		// The list stays as it is; the next change brings it again.
	}
}

export async function dismissNotifications(ids: number[]) {
	++generation;
	const result = await api.post<{ messages: Notice[] }>('/notifications/dismiss', { ids });
	++generation;
	notifications.list = result.messages;
	await loadNotifications();
}

export function unreadCount(): number {
	return visibleNotices().filter(isUnread).length;
}

/** Marks everything in the list as looked at. */
export function markSeen() {
	const newest = notifications.list[0];
	if (!newest) return;
	notifications.seen = Date.parse(newest.time);
	try {
		localStorage.setItem(SEEN_KEY, String(notifications.seen));
	} catch {
		// Without storage they count as new again after a reload.
	}
}

/** Title and text of a message in the language of whoever looks at it. */
export function noticeText(notice: Notice): { title: string; text: string } {
	const fill = (text: string) => text.replaceAll('{subject}', notice.subject);
	const translated = tOr(`notify.kind.${notice.kind}.text`, '');
	// The NAS's text carries the note already; a translated one gets it here.
	const note = translated && notice.note ? tOr(`notify.note.${notice.note}`, '') : '';
	return {
		title: fill(tOr(`notify.kind.${notice.kind}.title`, notice.title)),
		text: translated ? fill(translated) + (note ? ` ${note}` : '') : fill(notice.text),
	};
}
