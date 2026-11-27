import { session } from '../session.svelte.ts';
import { api } from '../api.ts';
import { openInProgram } from '../agent/open.ts';
import { agentAppMode } from '../agent-app.ts';
import { windowView } from '../desktop/mode.svelte.ts';
import { openProgram } from '../desktop/programs.svelte.ts';
import { base, type Handoff, type Job } from './api.ts';

export async function openPersonalAgent(input: Handoff): Promise<void> {
	const owner = session.data?.user?.name;
	const { id } = await api.post<{ id: string }>(base + '/contexts', { ...input });
	if (!owner || session.data?.user?.name !== owner) return;
	const href = '/agent?context=' + encodeURIComponent(id);
	// Office editors and widgets can run in the desktop document itself.
	// A top-level navigation here would discard every live editor.
	if (!windowView() && !agentAppMode(new URL(location.href))) {
		await openProgram('app:agent', { href });
	} else {
		openInProgram(href);
	}
}
export function consumeSelection(id: string) {
	return api.post<Handoff>(base + '/contexts/' + encodeURIComponent(id) + '/consume', {});
}
export function nativeDraftLink(job: Job): string {
	const draft = (job.result?.data as { draft?: { app?: string } } | null)?.draft;
	const pages: Record<string, string> = {
		mail: '/mail/compose',
		calendar: '/calendar/event',
		contacts: '/contacts',
		tasks: '/calendar',
	};
	const path = draft?.app && Object.hasOwn(pages, draft.app) ? pages[draft.app] : '';
	return path ? path + '?personalDraft=' + encodeURIComponent(job.id) : '';
}
export function resultLink(job: Job): string {
	const draft = nativeDraftLink(job);
	if (draft) return draft;
	const href = job.result?.link;
	if (!href || !href.startsWith('/') || /[\\\u0000-\u001f]/.test(href)) return '';
	try {
		const url = new URL(href, location.origin);
		return url.origin === location.origin &&
			/^\/(?:files|mail|contacts|calendar|webapps|backups|settings\/backups)(?:\/|$)/.test(
				url.pathname,
			)
			? url.pathname + url.search + url.hash
			: '';
	} catch {
		return '';
	}
}
