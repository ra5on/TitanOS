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
};

const SEEN_KEY = 'titanos.notifications.seen';

function readSeen(): number {
	try {
		return Number(localStorage.getItem(SEEN_KEY)) || 0;
	} catch {
		return 0;
	}
}

/** What happened on the NAS, the newest first; `seen` is the time up to which it was looked at in this browser. */
export const notifications = $state<{ list: Notice[]; seen: number }>({ list: [], seen: readSeen() });

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
		if (request === generation) { notifications.list = result.messages; soundForNotices(result.messages.filter((notice) => bellShows(bell.settings, notice))); }
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
	return visibleNotices().filter((notice) => Date.parse(notice.time) > notifications.seen).length;
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
	return {
		title: fill(tOr(`notify.kind.${notice.kind}.title`, notice.title)),
		text: fill(tOr(`notify.kind.${notice.kind}.text`, notice.text))
	};
}
