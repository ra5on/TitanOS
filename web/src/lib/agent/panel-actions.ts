// 1.13 (owner 18 Nov 2026): the context actions of the agent's panel per
// program, as chips. Each sends a prompt in the language of the page; what
// is open goes with it as a reference (AgentPanel's context), never its
// content. Kontakte and Kalender take theirs from here after their merge:
//   <AgentPanel app="contacts" context={…} actions={contactsPanelActions((k) => t(k))} …/>
import type { PanelAction } from '../components/agent/AgentPanel.svelte';
import type { MessageKey } from '../i18n/index.svelte.ts';

type T = (key: MessageKey) => string;

function action(t: T, app: string, id: string, needsContext: boolean, fill = false): PanelAction {
	return {
		id,
		label: t(`agentpim.act.${app}.${id}` as MessageKey),
		prompt: t(`agentpim.act.${app}.${id}.prompt` as MessageKey),
		needsContext,
		fill,
	};
}

/** Mail: summarize, draft an answer, an event from the mail, the sender as
 *  a contact, suggest a folder – each about the open mail. */
export function mailPanelActions(t: T): PanelAction[] {
	return ['summarize', 'reply', 'event', 'contact', 'folder'].map((id) =>
		action(t, 'mail', id, true),
	);
}

/** Kalender: an event in plain words, free time, the day's or week's overview. */
export function calendarPanelActions(t: T): PanelAction[] {
	return [
		action(t, 'calendar', 'event', false, true),
		action(t, 'calendar', 'free', false),
		action(t, 'calendar', 'day', false),
		action(t, 'calendar', 'week', false),
	];
}

/** Kontakte: duplicates, a contact from a signature or text, complete data. */
export function contactsPanelActions(t: T): PanelAction[] {
	return [
		action(t, 'contacts', 'duplicates', false),
		action(t, 'contacts', 'fromText', false, true),
		action(t, 'contacts', 'complete', true),
	];
}
