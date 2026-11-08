import type { ChatInfo } from '#lib/agent.ts';

/** The sheets and dialogs of ChatSheets.svelte (the rest of the agent page's dialogs stay there). */
export type ChatSheet =
	| { kind: 'actions'; chat: ChatInfo }
	| { kind: 'archive' }
	| { kind: 'move'; chat: ChatInfo }
	| { kind: 'rename'; chat: ChatInfo; title: string }
	| { kind: 'delete'; chat: ChatInfo }
	| { kind: 'deleteAll' };

export const chatSheetKinds: readonly ChatSheet['kind'][] = [
	'actions',
	'archive',
	'move',
	'rename',
	'delete',
	'deleteAll',
];

export function isChatSheet(d: { kind: string } | null): d is ChatSheet {
	return !!d && (chatSheetKinds as readonly string[]).includes(d.kind);
}
