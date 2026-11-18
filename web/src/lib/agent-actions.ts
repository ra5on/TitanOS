// 1.13 (owner 18 Nov 2026, Q27–Q29): the rights of the KI-Agent per kind
// of action for mail, events, contacts and reminders – "Nur Vorschläge"
// (suggest: a draft the person finishes), "Fragen" (ask), "Selbst
// ausführen" (allow); reading mail on or off; deletes always ask. The
// administrator's values are the NAS-wide cap (settings.policy.actionRights),
// every account may be stricter for itself (GET/PUT /agent/rights/me).
// Decides like backend/internal/agent/actions.go (the same table,
// backend/internal/agent/testdata/data-rights.json, tests/agent.test.mjs).
// No runes here, so node --test can import it.

import { api } from './api.ts';
import { dataRight, type DataArea, type Settings } from './agent.ts';

export const ACTION_KEYS = [
	'mail.read',
	'mail.draft',
	'mail.send',
	'mail.move',
	'event.create',
	'event.update',
	'event.delete',
	'contact.create',
	'contact.update',
	'contact.delete',
	'task.create',
	'task.update',
	'task.complete',
	'task.delete',
] as const;
export type ActionKey = (typeof ACTION_KEYS)[number];
export type ActionMode = 'suggest' | 'ask' | 'allow' | 'deny';

/** The groups of the settings, in this order. */
export const ACTION_GROUPS: { id: 'mail' | 'event' | 'contact' | 'task'; keys: ActionKey[] }[] = [
	{ id: 'mail', keys: ['mail.read', 'mail.draft', 'mail.send', 'mail.move'] },
	{ id: 'event', keys: ['event.create', 'event.update', 'event.delete'] },
	{ id: 'contact', keys: ['contact.create', 'contact.update', 'contact.delete'] },
	{ id: 'task', keys: ['task.create', 'task.update', 'task.complete', 'task.delete'] },
];

export const isDelete = (key: string) => key.endsWith('.delete');

export function actionArea(key: string): 'mail' | DataArea {
	if (key.startsWith('mail.')) return 'mail';
	if (key.startsWith('contact.')) return 'contacts';
	return 'calendar';
}

/** The values a key may hold: mail.read allow/deny, a delete ask only. */
export function actionValues(key: string): ActionMode[] {
	if (key === 'mail.read') return ['allow', 'deny'];
	if (isDelete(key)) return ['ask'];
	return ['suggest', 'ask', 'allow'];
}

export function validAction(key: string, v: unknown): v is ActionMode {
	return (
		(ACTION_KEYS as readonly string[]).includes(key) && actionValues(key).includes(v as ActionMode)
	);
}

const RANK: Record<ActionMode, number> = { allow: 0, ask: 1, suggest: 2, deny: 3 };

/** The stricter of two modes; an unknown value counts as deny. */
export function stricterAction(a: string, b: string): ActionMode {
	if (!(a in RANK) || !(b in RANK)) return 'deny';
	return RANK[b as ActionMode] > RANK[a as ActionMode] ? (b as ActionMode) : (a as ActionMode);
}

type Policy = Settings['policy'] & { actionRights?: Partial<Record<string, string>> };

function mailGate(policy: Policy, kind: 'read' | 'change'): 'allow' | 'deny' {
	const read = policy.actionRights?.['mail.read'] === 'deny' ? 'deny' : 'allow';
	if (kind === 'read' || read === 'deny') return read;
	return policy.preset === 'read' ? 'deny' : 'allow';
}

/** The NAS-wide mode of an action (Policy.ActionMode in Go). */
export function nasAction(policy: Policy, key: ActionKey): ActionMode {
	const area = actionArea(key);
	if (area === 'mail') {
		if (key === 'mail.read') return mailGate(policy, 'read');
		if (mailGate(policy, 'change') === 'deny') return 'deny';
	} else if (dataRight(policy, area).write === 'deny') return 'deny';
	const own = policy.actionRights?.[key];
	if (validAction(key, own)) return own;
	if (isDelete(key)) return 'ask';
	const dr = area !== 'mail' ? policy.dataRights?.[area] : undefined;
	if (dr && (dr.write === 'allow' || dr.write === 'ask')) return dr.write;
	switch (policy.preset) {
		case 'all':
			return key === 'mail.send' ? 'ask' : 'allow';
		case 'read':
			return 'deny';
	}
	return 'suggest';
}

/** The mode that holds: NAS, the account's own, and after reading
 *  personal data sending mail asks (EffectiveAction in Go). */
export function actionMode(
	policy: Policy,
	own: Partial<Record<string, string>> | undefined,
	key: ActionKey,
	read = false,
): ActionMode {
	let mode = nasAction(policy, key);
	const mine = own?.[key];
	if (validAction(key, mine)) mode = stricterAction(mode, mine);
	if (actionArea(key) === 'mail' && own?.['mail.read'] === 'deny') mode = 'deny';
	if (isDelete(key) && mode === 'allow') mode = 'ask';
	if (key === 'mail.send' && read && mode === 'allow') mode = 'ask';
	return mode;
}

/** What an account may choose for itself: "wie vom NAS" ('') and every
 *  value at least as strict as the NAS gives. */
export function ownChoices(key: ActionKey, nas: ActionMode): ('' | ActionMode)[] {
	if (nas === 'deny') return [''];
	return ['', ...actionValues(key).filter((v) => RANK[v] > RANK[nas])];
}

/** The page's answer of /agent/rights/me and /agent/status (actions). */
export type ActionsView = {
	effective: Record<ActionKey, ActionMode>;
	nas: Record<ActionKey, ActionMode>;
	own: Partial<Record<ActionKey, ActionMode>>;
	keys: ActionKey[];
};

export const getMyRights = () => api.get<ActionsView>('/agent/rights/me');
export const setMyRights = (own: Partial<Record<ActionKey, ActionMode>>) =>
	api.put<ActionsView>('/agent/rights/me', { own });
