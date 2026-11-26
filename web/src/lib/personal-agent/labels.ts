import { t, type MessageKey } from '../i18n/index.svelte.ts';
import type { Capability, Job, Source } from './api.ts';
import type { IconName } from '../components/Icon.svelte';

export const msg = (key: string) => t(('pa.' + key) as MessageKey);
export function sourceTitle(s: Source): string {
	return s.label || s.id;
}
export function iconFor(kind: string): IconName {
	const group = kind.split('.')[0].split('-')[0];
	switch (group) {
		case 'mail':
			return 'mail';
		case 'calendar':
		case 'event':
			return 'calendar';
		case 'task':
		case 'reminders':
			return 'check';
		case 'contact':
		case 'contacts':
			return 'person';
		case 'file':
		case 'files':
		case 'office':
		case 'attachment':
		case 'document':
			return 'fileText';
		case 'folder':
			return 'folder';
		case 'backup':
			return 'disk';
		case 'chat':
			return 'chat';
		case 'speech':
			return 'mic';
		default:
			return 'sparkle';
	}
}
export function reason(cap: Capability): string {
	return cap.reason
		? {
				admin: msg('ceiling') + ' · ' + msg('never'),
				personal: msg('permissions') + ' · ' + msg('never'),
				admin_ask: msg('ceiling') + ' · ' + msg('ask'),
				personal_ask: msg('permissions') + ' · ' + msg('ask'),
				approval_required: msg('sensitive'),
				app: msg('appUnavailable'),
			}[cap.reason] || cap.reason
		: '';
}
export function jobTitle(job: Job, caps: Capability[]): string {
	return (
		job.result?.title ||
		job.preview?.title ||
		caps.find((c) => c.id === job.request.tool)?.title ||
		job.request.tool
	);
}
export function outputKind(job: Job): string {
	if (job.result?.data && typeof job.result.data === 'object' && 'draft' in job.result.data)
		return msg('draft');
	if (
		job.request.tool === 'mail.send' &&
		job.result?.data &&
		typeof job.result.data === 'object' &&
		'accepted' in job.result.data &&
		job.result.data.accepted === true
	)
		return msg('verified');
	return msg(job.result ? 'result' : 'preview');
}

export function failure(job: Job): string {
	switch (job.error) {
		case 'effect_unconfirmed':
			return msg('uncertain');
		case 'effect_rejected':
			return msg(job.request.tool === 'mail.send' ? 'mailRejected' : 'actionRejected');
		case 'action_already_attempted':
			return msg('duplicate');
		case 'permission_revoked':
			return msg('revoked');
		case 'tool_failed':
			return msg('failedTool');
		default:
			return job.error || '';
	}
}
