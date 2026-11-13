// PLAN 82-3: whom a camera tells and how – the accounts it can choose,
// their ways, the pause and the window. The NAS decides again for every
// message who may get it (backend/internal/nvr/recipients.go); these
// helpers only shape the form and the overview.
import { t, type MessageKey } from '../i18n/index.svelte.ts';
import { OBJECT_GROUP_IDS, OBJECT_GROUPS, type ObjectGroup } from './texts.ts';
import type { CameraNotify, NotifyRecipient, NotifyWindow } from './types.ts';

/** An account as GET /nvr/recipients lists it. */
export type RecipientAccount = {
	name: string;
	/** Owner or administrator: the only ones with a bell. */
	admin: boolean;
	/** Has an e-mail address of its own (it also needs the mail server). */
	email: boolean;
	/** Devices that take camera messages, and those that switched them off. */
	pushDevices: number;
	pushDevicesOff: number;
	/** IDs of the cameras it may see now. */
	cameras: string[];
	/** IDs of the cameras whose recognised names and plates it may see (plate and face messages). */
	names?: string[];
};
export type RecipientsAnswer = { accounts: RecipientAccount[]; mailServer: boolean };

export type Way = 'bell' | 'push' | 'email';
export const WAYS: Way[] = ['bell', 'push', 'email'];

/** The pauses the NAS takes, in seconds (none, 1, 5, 15, 30, 60 minutes). */
export const PAUSE_CHOICES = [0, 60, 300, 900, 1800, 3600] as const;
/** A camera that names no pause waits 60 seconds (0.78). */
export const DEFAULT_PAUSE = 60;
/** 1 Monday … 7 Sunday, as the NAS counts them. */
export const DAYS = [1, 2, 3, 4, 5, 6, 7] as const;

/** Whether an account may be chosen at a camera (no ID: a new camera, which nobody but administrators may see). */
export function eligible(account: RecipientAccount, camera?: string): boolean {
	return account.admin || (!!camera && account.cameras.includes(camera));
}

/** Why a chosen account gets nothing now ('' when it gets the messages). */
export function staleReason(
	user: string,
	accounts: RecipientAccount[],
	camera?: string,
): '' | 'gone' | 'noRight' {
	const account = accounts.find((a) => a.name === user);
	if (!account) return 'gone';
	return eligible(account, camera) ? '' : 'noRight';
}

/**
 * Whether an account would get no plate or face messages of a camera that
 * reports them: it lacks the right "Namen und Kennzeichen" there.
 */
export function missesNames(
	account: RecipientAccount,
	camera: string | undefined,
	kinds: string[],
) {
	if (account.admin || !kinds.some((k) => k === 'plate' || k === 'face')) return false;
	return !camera || !(account.names ?? []).includes(camera);
}

/** The ways an account can have: the bell only as an administrator. */
export function waysFor(account: Pick<RecipientAccount, 'admin'>): Way[] {
	return account.admin ? WAYS : ['push', 'email'];
}

/** The ways an entry has. */
export function waysOf(entry: NotifyRecipient | undefined): Way[] {
	return entry ? WAYS.filter((way) => entry[way]) : [];
}

/**
 * What a camera of 0.78 (no list) meant: every administrator with its old
 * bell and push. Its e-mail went to the address of the NAS, which is
 * nobody's own: e-mail is left for the administrator to choose.
 */
export function legacyRecipients(
	notify: Pick<CameraNotify, 'titan' | 'push'>,
	accounts: RecipientAccount[],
): NotifyRecipient[] {
	if (!notify.titan && !notify.push) return [];
	return accounts
		.filter((a) => a.admin)
		.map((a) => ({ user: a.name, bell: notify.titan, push: notify.push, email: false }));
}

/** Switches one way of one account; an account left without any way leaves the list. */
export function toggleWay(
	list: NotifyRecipient[],
	user: string,
	way: Way,
	on: boolean,
): NotifyRecipient[] {
	const entry = list.find((r) => r.user === user) ?? {
		user,
		bell: false,
		push: false,
		email: false,
	};
	const next = { ...entry, [way]: on };
	const rest = list.filter((r) => r.user !== user);
	if (!next.bell && !next.push && !next.email) return rest;
	return [...rest, next].sort((a, b) => a.user.localeCompare(b.user));
}

/** Runs of following days: [1,2,3,4,5,7] → [[1,5],[7,7]]. */
export function dayRuns(days: number[]): [number, number][] {
	const sorted = [...new Set(days)].filter((d) => d >= 1 && d <= 7).sort((a, b) => a - b);
	const runs: [number, number][] = [];
	for (const day of sorted) {
		const last = runs.at(-1);
		if (last && last[1] === day - 1) last[1] = day;
		else runs.push([day, day]);
	}
	return runs;
}

/** Days as words: "täglich", "Mo–Fr", "Sa, So", "Mo, Mi". */
export function daysText(days: number[]): string {
	if (dayRuns(days).length === 1 && dayRuns(days)[0][0] === 1 && dayRuns(days)[0][1] === 7)
		return t('nvr.notify.daily');
	const day = (d: number) => t(`nvr.notify.day.${d}` as MessageKey);
	return dayRuns(days)
		.flatMap(([from, to]) =>
			to - from >= 2
				? [`${day(from)}–${day(to)}`]
				: Array.from({ length: to - from + 1 }, (_, i) => day(from + i)),
		)
		.join(', ');
}

/** A pause as words. */
export function pauseText(seconds: number | undefined): string {
	return t(`nvr.notify.pause.${seconds ?? DEFAULT_PAUSE}` as MessageKey);
}

/** The window as words: "Mo–Fr, 22:00–06:00" or "meldet jederzeit". */
export function windowText(window: NotifyWindow | undefined): string {
	if (!window?.on) return t('nvr.notify.always');
	return t('nvr.notify.windowShort', {
		days: daysText(window.days),
		from: window.from,
		to: window.to,
	});
}

/** A window the form can send: on switching on, every day and 22:00–06:00 unless chosen. */
export function windowOn(window: NotifyWindow | undefined, on: boolean): NotifyWindow {
	const w = window ?? { on: false, days: [], from: '', to: '' };
	if (!on) return { ...w, on };
	return {
		on,
		days: w.days.length ? w.days : [...DAYS],
		from: w.from || '22:00',
		to: w.to || '06:00',
	};
}

/** The kinds a camera reports, as the pills of the form name them. */
export type KindPill = ObjectGroup | 'plate' | 'face' | 'offline';
export const KIND_PILLS: KindPill[] = [...OBJECT_GROUP_IDS, 'plate', 'face', 'offline'];
export const kindMembers = (pill: KindPill): readonly string[] =>
	pill === 'plate' || pill === 'face' || pill === 'offline' ? [pill] : OBJECT_GROUPS[pill];
export function kindPillsOf(kinds: string[]): KindPill[] {
	return KIND_PILLS.filter((pill) => kindMembers(pill).some((k) => kinds.includes(k)));
}

/** "alex (Glocke, Push)". */
export function recipientText(entry: NotifyRecipient): string {
	const ways = waysOf(entry).map((way) => t(`nvr.notify.way.${way}` as MessageKey));
	return `${entry.user} (${ways.join(', ')})`;
}
