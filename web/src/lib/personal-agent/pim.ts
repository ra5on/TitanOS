import { api } from '#lib/api.ts';
import type { AgentDraft } from '#lib/agent.ts';
import type { Message } from '#lib/tmail/types.ts';
import type { Job, Source } from './api.ts';

export function mailSelection(account: string, folder: string, m: Message): Source[] {
	const ref = { account, folder, uid: String(m.uid), messageId: m.messageId || '', date: m.date };
	const id = [account, folder, m.uid].map(encodeURIComponent).join('/');
	return [
		{ kind: 'mail', id, label: m.subject, ref },
		...m.attachments
			.filter((p) => p.type.toLowerCase() === 'application/pdf' && !p.inline)
			.map((p) => ({
				kind: 'mail.pdf',
				id: id + '/' + p.part,
				label: p.name,
				ref: { ...ref, part: p.part },
			})),
	];
}
export function pimSelection(
	kind: 'calendar' | 'contacts' | 'reminders',
	collection: string,
	object: string,
	label: string,
	extra: Record<string, string> = {},
): Source[] {
	return [
		{
			kind,
			id: encodeURIComponent(collection) + '/' + encodeURIComponent(object),
			label,
			ref: { collection, object, ...extra },
		},
	];
}
/** A folder UID is scoped to its mailbox and folder. Source display names do not identify it. */
export function samePIMSource(a: Source | undefined, b: Source | undefined): boolean {
	if (!a || !b) return false;
	const fields = [
		'account',
		'folder',
		'uid',
		'messageId',
		'date',
		'collection',
		'object',
		'occurrence',
	];
	return a.kind === b.kind && a.id === b.id && fields.every((k) => a.ref?.[k] === b.ref?.[k]);
}
export async function loadPersonalDraft(id: string): Promise<AgentDraft | null> {
	if (!/^[a-zA-Z0-9_-]{1,80}$/.test(id)) return null;
	try {
		const job = await api.get<Job>('/personal-agent/jobs/' + encodeURIComponent(id));
		const data = job.status === 'done' ? job.result?.data : null;
		if (!data || typeof data !== 'object' || !('draft' in data)) return null;
		const draft = data.draft as AgentDraft;
		if (
			!draft ||
			!['mail', 'calendar', 'contacts', 'tasks'].includes(draft.app) ||
			!draft.fields ||
			typeof draft.fields !== 'object'
		)
			return null;
		return draft;
	} catch {
		return null;
	}
}
