import { apiPath } from '../cluster-node.ts';
import { mailApi } from '../tmail/api.ts';
import type { Source, Job } from './api.ts';

export function sourceLocation(
	source: Source,
): { href: string; pdf: boolean; page: number } | null {
	const r = source.ref ?? {},
		query = (p: Record<string, string>) => new URLSearchParams(p).toString();
	const page = Math.max(1, Math.min(100000, Math.trunc(Number(r.page || r.first) || 1)));
	if (source.kind === 'attachment' && source.id)
		return {
			href: apiPath(
				'/personal-agent/attachments/' + encodeURIComponent(source.id) + '/content',
				null,
			),
			pdf: true,
			page,
		};
	if (source.kind === 'mail.pdf' && r.account && r.folder && /^\d+$/.test(r.uid) && r.part)
		return { href: mailApi.partUrl(r.account, r.folder, Number(r.uid), r.part), pdf: true, page };
	if (source.kind === 'mail' && r.account && r.folder && /^\d+$/.test(r.uid))
		return { href: '/mail?' + query({ a: r.account, f: r.folder, m: r.uid }), pdf: false, page };
	if (source.kind === 'calendar' && r.collection && r.object)
		return {
			href:
				'/calendar/event?' +
				query({
					cal: r.collection,
					id: r.object,
					...(r.occurrence ? { occurrence: r.occurrence } : {}),
				}),
			pdf: false,
			page,
		};
	if (source.kind === 'calendar.range' && r.start)
		return { href: '/calendar?' + query({ date: r.start.slice(0, 10) }), pdf: false, page };
	if (source.kind === 'contacts' && r.collection && r.object)
		return { href: '/contacts?' + query({ book: r.collection, id: r.object }), pdf: false, page };
	if (source.kind === 'reminders') return { href: '/calendar?view=tasks', pdf: false, page };
	if (source.kind === 'backup' && source.id)
		return {
			href: '/settings/backups/data#backup-' + encodeURIComponent(source.id),
			pdf: false,
			page,
		};
	if ((source.kind === 'file' || source.kind === 'folder') && r.root && source.id) {
		if (source.kind === 'file' && source.id.toLowerCase().endsWith('.pdf'))
			return {
				href: apiPath('/files/download?' + query({ root: r.root, path: source.id }), null),
				pdf: true,
				page,
			};
		return {
			href:
				'/files?' +
				query({
					root: r.root,
					path: source.kind === 'folder' ? source.id : source.id.split('/').slice(0, -1).join('/'),
				}),
			pdf: false,
			page,
		};
	}
	return null;
}
// Intermediate results remain in the job while the next step is running. Only
// its current preview can describe a pending action; completion has its own receipt.
export function jobOutput(j: Job) {
	if (j.status === 'done') return j.result;
	if (j.status === 'failed' && !j.preview) return j.result;
	return j.preview;
}
export function stepTool(j: Job) {
	return j.currentTool || j.request.tool;
}
export function jobProgressChanged(before: Job | undefined, next: Job) {
	return (
		!before ||
		before.status !== next.status ||
		(next.status === 'running' &&
			(stepTool(before) !== stepTool(next) || before.completedSteps !== next.completedSteps))
	);
}
export function visibleSources(job: Job) {
	return (
		jobOutput(job)?.sources ?? (job.status === 'running' ? job.request.sources : undefined) ?? []
	);
}
export function sendPreview(j: Job): boolean {
	const data = jobOutput(j)?.data as Record<string, unknown> | undefined;
	return (
		stepTool(j) === 'mail.send' ||
		(j.status === 'done' && data?.accepted === true) ||
		!!(typeof data?.inReplyTo === 'string' && typeof data?.account === 'string')
	);
}
export function mailFields(j: Job) {
	const data = jobOutput(j)?.data as Record<string, unknown> | undefined;
	const draft = data?.draft as { app?: string; fields?: Record<string, unknown> } | undefined;
	const fields =
		draft?.app === 'mail'
			? draft.fields
			: j.status !== 'done' && sendPreview(j) && typeof data?.text === 'string'
				? data
				: null;
	if (!fields) return null;
	const string = (key: string) => (typeof fields[key] === 'string' ? (fields[key] as string) : '');
	const list = (key: string) =>
		Array.isArray(fields[key])
			? (fields[key] as unknown[])
					.map((item) =>
						typeof item === 'string' ? item : String((item as { name?: string })?.name ?? ''),
					)
					.filter(Boolean)
					.join(', ')
			: string(key);
	return {
		to: list('to'),
		from:
			string('senderName') && string('from')
				? string('senderName') + ' <' + string('from') + '>'
				: string('from'),
		account: string('mailbox') || string('a') || string('account'),
		cc: list('cc'),
		bcc: list('bcc'),
		subject: string('subject'),
		text: string('text'),
		attachments: Array.isArray(fields.attachments)
			? fields.attachments
					.map((item) => {
						if (!item || typeof item !== 'object') return String(item ?? '');
						const { name, type, size, sha256 } = item;
						return [
							name,
							type,
							typeof size === 'number' ? size + ' B' : '',
							sha256 && 'SHA-256 ' + sha256,
						]
							.filter(Boolean)
							.join(' · ');
					})
					.join('\n')
			: string('attachments'),
		isDraft: draft?.app === 'mail',
	};
}
