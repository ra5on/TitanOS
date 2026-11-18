// What deleting an account takes along of the KI-Agent (1.03, A-42), of
// T-Mail (1.05) and of T-Kalender and Kontakte (1.11): the
// delete dialog names it before (GET /users/{name}/agent-data). A NAS
// without the route (an older version) answers 404; the dialog then
// stays as it was, without this list.
import { t, type MessageKey } from './i18n/index.svelte.ts';

export type AgentData = {
	/** false: the agent's volume is not there; the counts are then 0. */
	available: boolean;
	chats: number;
	/** Projects the account owns; `handedOver` of them go to a member. */
	projects: number;
	handedOver: number;
	memories: number;
	skills: number;
	schedules: number;
	notes: number;
	/** The account has a folder in the share KI-Agent. */
	folder: boolean;
	/** 1.05: the account's T-Mail mailboxes (kept on the stick, counted without the agent's volume too). */
	mailAccounts: number;
	/**
	 * 1.11: events and contacts of T-Kalender and Kontakte (on the volume of
	 * the system data, counted whatever the agent's volume says). Undefined
	 * when the NAS does not say (older NAS, or `calendarUnknown`).
	 */
	calendarEvents?: number;
	contacts?: number;
	/** The NAS could not count them now (no data volume): named without numbers. */
	calendarUnknown: boolean;
};

const COUNTS = [
	'chats',
	'projects',
	'handedOver',
	'memories',
	'skills',
	'schedules',
	'notes',
] as const;

/** The answer checked; null for anything that is not one (the dialog shows no list then). */
export function sanitizeAgentData(raw: unknown): AgentData | null {
	if (!raw || typeof raw !== 'object') return null;
	const r = raw as Record<string, unknown>;
	if (typeof r.available !== 'boolean') return null;
	const count = (v: unknown) =>
		typeof v === 'number' && Number.isFinite(v) && v > 0 ? Math.floor(v) : 0;
	const out = { available: r.available, folder: r.folder === true } as AgentData;
	for (const key of COUNTS) out[key] = count(r[key]);
	out.mailAccounts = count(r.mailAccounts);
	out.calendarUnknown = r.calendarUnknown === true;
	if (!out.calendarUnknown) {
		if (typeof r.calendarEvents === 'number') out.calendarEvents = count(r.calendarEvents);
		if (typeof r.contacts === 'number') out.contacts = count(r.contacts);
	}
	// A subset of the projects, never more.
	out.handedOver = Math.min(out.handedOver, out.projects);
	return out;
}

export type AgentDataText = {
	/** What goes with the account, one line each. */
	deleted: string[];
	/** Shared projects that stay with a member, if any. */
	kept: string;
	/** Instead of the list: nothing to delete, or counted without the volume. */
	note: string;
};

/** The lines of the delete dialog. */
export function agentDataText(d: AgentData): AgentDataText {
	// T-Mail lives on the stick: its mailboxes are counted with or without the volume.
	const mail =
		d.mailAccounts > 0 ? [t('users.agentData.mailAccounts', { count: d.mailAccounts })] : [];
	// 1.11: T-Kalender and Kontakte; without the data volume named, never "0".
	if (d.calendarUnknown) mail.push(t('users.agentData.part.calendar'));
	else {
		if (d.calendarEvents)
			mail.push(t('users.agentData.calendarEvents', { count: d.calendarEvents }));
		if (d.contacts) mail.push(t('users.agentData.contacts', { count: d.contacts }));
	}
	if (!d.available)
		return {
			deleted: [
				...(
					[
						'users.agentData.part.chats',
						'users.agentData.part.projects',
						'users.agentData.part.memories',
						'users.agentData.part.skills',
						'users.agentData.part.schedules',
						'users.agentData.part.notes',
						'users.agentData.part.folder',
					] as MessageKey[]
				).map((key) => t(key)),
				...mail,
			],
			kept: t('users.agentData.part.shared'),
			note: t('users.agentData.unavailable'),
		};
	const rows: [MessageKey, number][] = [
		['users.agentData.chats', d.chats],
		['users.agentData.projects', d.projects - d.handedOver],
		['users.agentData.memories', d.memories],
		['users.agentData.skills', d.skills],
		['users.agentData.schedules', d.schedules],
		['users.agentData.notes', d.notes],
	];
	const deleted = rows.filter(([, n]) => n > 0).map(([key, n]) => t(key, { count: n }));
	if (d.folder) deleted.push(t('users.agentData.part.folder'));
	deleted.push(...mail);
	return {
		deleted,
		kept: d.handedOver > 0 ? t('users.agentData.handedOver', { count: d.handedOver }) : '',
		note: deleted.length || d.handedOver ? '' : t('users.agentData.nothing'),
	};
}
