// 1.13: drafts of the KI-Agent ("Nur Vorschläge") on their way into the
// programs. A card "Öffnen und bearbeiten" opens the program with
// ?agentDraft=<chat>:<entry>; the program fetches the draft from the
// account's own chat (GET /agent/chats/{chat}/drafts/{entry}) and puts the
// fields into its editor as text the person checks – nothing is saved or
// sent without the person. No runes and no navigation here (node --test).
//
// Fields per program (backend: api/agent_mail.go, agent_pim_more.go,
// agent_tasks.go):
// - mail (new, reply): a, to[], cc[], bcc[], subject, text; a reply also
//   f, uid (the mail it answers). move: a, f, uids[], to (a folder).
// - calendar (event): title, start, end, allDay, timezone, location, notes,
//   calendar, alarms (minutes before), rrule (the repetition in the form of
//   the web API: {freq, interval, byDay, byMonthDay, until, count}); a
//   change also cal, id, occurrence and only the fields that change.
// - contacts (contact): name, first, last, emails[{type,value}],
//   phones[{type,value}], org, addresses[{type,street,city,postcode,region,
//   country}], notes, birthday; a change also book, id.
// - tasks (task, complete): title, notes, due, priority, list; a change
//   also id (and done).

import { api } from '../api.ts';
import type { AgentDraft } from '../agent.ts';

export type { AgentDraft };

const ID = /^[A-Za-z0-9_-]{1,64}$/;

/** Reads "<chat>:<entry>"; null for anything else. */
export function parseDraftParam(
	param: string | null | undefined,
): { chat: string; entry: string } | null {
	if (!param) return null;
	const at = param.indexOf(':');
	if (at < 1) return null;
	const chat = param.slice(0, at);
	const entry = param.slice(at + 1);
	return ID.test(chat) && ID.test(entry) ? { chat, entry } : null;
}

/**
 * The draft a program was opened with (the value of ?agentDraft=), or null
 * when there is none or it is gone. Only the account's own chats answer.
 */
export async function loadAgentDraft(param: string | null | undefined): Promise<AgentDraft | null> {
	const ref = parseDraftParam(param);
	if (!ref) return null;
	try {
		const d = await api.get<AgentDraft>(
			`/agent/chats/${encodeURIComponent(ref.chat)}/drafts/${encodeURIComponent(ref.entry)}`,
		);
		return d && typeof d === 'object' && d.fields && typeof d.fields === 'object' ? d : null;
	} catch {
		return null;
	}
}

/** Where a draft opens: the editor of its program with ?agentDraft=. */
export function draftHref(
	draft: Pick<AgentDraft, 'app' | 'kind' | 'fields'>,
	chat: string,
	entry: string,
): string {
	const ref = encodeURIComponent(`${chat}:${entry}`);
	switch (draft.app) {
		case 'mail': {
			if (draft.kind === 'move') {
				const f = draft.fields;
				const uids = Array.isArray(f.uids) ? f.uids : [];
				const q = new URLSearchParams();
				if (typeof f.a === 'string') q.set('a', f.a);
				if (typeof f.f === 'string') q.set('f', f.f);
				if (typeof uids[0] === 'number') q.set('m', String(uids[0]));
				return '/mail?' + q.toString();
			}
			return `/mail/compose?agentDraft=${ref}`;
		}
		case 'calendar':
			return `/calendar/event?agentDraft=${ref}`;
		case 'contacts':
			return `/contacts?agentDraft=${ref}`;
		case 'tasks':
			return `/calendar?agentDraft=${ref}`;
	}
	return '/agent';
}

/** Which steps of a waiting approval can be opened as a draft instead. */
export const DRAFT_TOOLS = new Set([
	'mail_send',
	'mail_draft',
	'mail_move',
	'calendar_create',
	'calendar_update',
	'contacts_create',
	'contacts_update',
	'tasks_create',
	'tasks_update',
	'tasks_complete',
]);

/** The program a tool's draft opens in, for the href of a waiting step. */
export function draftAppOf(tool: string): AgentDraft['app'] {
	if (tool.startsWith('mail_')) return 'mail';
	if (tool.startsWith('contacts_')) return 'contacts';
	if (tool.startsWith('tasks_')) return 'tasks';
	return 'calendar';
}

/** Recipients of a draft as one line each (to, cc, bcc are lists). */
export function draftList(v: unknown): string[] {
	return Array.isArray(v)
		? v.filter((x): x is string => typeof x === 'string' && x.trim() !== '')
		: [];
}
