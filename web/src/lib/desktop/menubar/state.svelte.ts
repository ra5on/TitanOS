// PLAN 67-1: what the pages in the windows announced (top window only).
// Never stored: a window's commands exist while its page runs.
import type { CommandStates } from './protocol.ts';

export const frameMenus = $state<{ byWindow: Record<string, CommandStates> }>({ byWindow: {} });

export function setFrameCommands(winId: string, states: CommandStates) {
	frameMenus.byWindow[winId] = states;
}

export function clearFrameCommands(winId: string) {
	delete frameMenus.byWindow[winId];
}
