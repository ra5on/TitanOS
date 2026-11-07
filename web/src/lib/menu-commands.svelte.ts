// PLAN 67-1: pages register the commands of their program's menu bar here.
//
//   registerMenuCommands({
//     'files.mkdir': () => show('mkdir'),
//     'files.view.list': { run: () => setView('list'), checked: () => view === 'list' },
//     'files.rename': { run: rename, enabled: () => selected.length === 1 },
//     // A list submenu: the entries come from the page, the chosen one comes back as arg.
//     'files.shares': { items: () => roots.map((r) => ({ id: r.id, text: r.name })), run: open },
//   });
//
// Call it while the page's component starts (top level of its <script>).
// In a window of the desktop the states go to the top bar and follow every
// change; the top bar sends a command back to run. On the phone the page's
// own bar reads them here. The handlers end with the component.
import { onDestroy } from 'svelte';
import { inWindow } from './desktop/frame.ts';
import {
	MAX_ITEMS,
	readMenuRequest,
	type CommandState,
	type CommandStates,
	type ListItem,
	type MenuMessage,
} from './desktop/menubar/protocol.ts';

export type MenuCommand =
	| (() => void)
	| {
			run: (arg?: string) => void;
			enabled?: () => boolean;
			checked?: () => boolean;
			/** A list submenu: its entries (text only); the chosen id is passed to run. */
			items?: () => ListItem[];
	  };
export type MenuCommands = Record<string, MenuCommand>;

const groups = new Map<symbol, MenuCommands>();
/** Changes when commands come or go (for the phone's bar). */
export const menuRegistry = $state({ version: 0 });

function stateOf(id: string, c: MenuCommand): CommandState {
	if (typeof c === 'function') return { id, enabled: true };
	const out: CommandState = { id, enabled: c.enabled ? !!c.enabled() : true };
	if (c.checked) out.checked = !!c.checked();
	if (c.items) {
		out.items = c.items().slice(0, MAX_ITEMS);
		if (!out.items.length) out.enabled = false;
	}
	return out;
}
function plain({ enabled, checked, items }: CommandState): CommandStates[string] {
	const out: CommandStates[string] = { enabled };
	if (checked !== undefined) out.checked = checked;
	if (items) out.items = items;
	return out;
}
function runOne(c: MenuCommand, arg?: string): boolean {
	if (typeof c === 'function') c();
	else if (!c.enabled || c.enabled()) c.run(arg);
	else return false;
	return true;
}

/** The states of every registered command (the newest registration wins). */
export function localCommandStates(): CommandStates {
	void menuRegistry.version;
	const out: CommandStates = {};
	for (const commands of groups.values())
		for (const [id, c] of Object.entries(commands)) out[id] = plain(stateOf(id, c));
	return out;
}

/** Runs a registered command if it is there and enabled; true when it ran. */
export function runLocalCommand(id: string, arg?: string): boolean {
	let found: MenuCommand | undefined;
	for (const commands of groups.values()) if (id in commands) found = commands[id];
	return !!found && runOne(found, arg);
}

let queued = false;
function announce() {
	if (!inWindow || queued) return;
	queued = true;
	queueMicrotask(() => {
		queued = false;
		const commands = Object.entries(localCommandStates()).map(([id, s]) => ({ id, ...s }));
		window.parent.postMessage(
			{ type: 'titanos-menu', commands } satisfies MenuMessage,
			location.origin,
		);
	});
}

let listening = false;
function listen() {
	if (!inWindow || listening) return;
	listening = true;
	window.addEventListener('message', (event) => {
		// Only the desktop that holds this frame may ask.
		if (event.origin !== location.origin || event.source !== window.parent) return;
		const req = readMenuRequest(event.data);
		if (req?.type === 'titanos-menu-get') announce();
		else if (req?.type === 'titanos-menu-action') runLocalCommand(req.id, req.arg);
	});
}

/** Registers the menu commands of a page; call while its component starts. */
export function registerMenuCommands(commands: MenuCommands) {
	const token = Symbol();
	groups.set(token, commands);
	menuRegistry.version++;
	listen();
	// Reading every state here makes the announcement follow each change.
	$effect(() => {
		for (const [id, c] of Object.entries(commands)) stateOf(id, c);
		announce();
	});
	onDestroy(() => {
		groups.delete(token);
		menuRegistry.version++;
		announce();
	});
}

// ---- Windows whose content lives in the desktop itself (web apps, remote view) ----

const windowGroups = new Map<string, MenuCommands>();
export const windowRegistry = $state({ version: 0 });

/** A window of the desktop (not a frame) registers the commands of its content. */
export function registerWindowCommands(winId: string, commands: MenuCommands) {
	windowGroups.set(winId, commands);
	windowRegistry.version++;
	onDestroy(() => {
		windowGroups.delete(winId);
		windowRegistry.version++;
	});
}

/** The state of a window's own command, or undefined when it has none. */
export function windowCommandState(winId: string, id: string): CommandStates[string] | undefined {
	void windowRegistry.version;
	const c = windowGroups.get(winId)?.[id];
	return c ? plain(stateOf(id, c)) : undefined;
}

export function hasWindowCommands(winId: string): boolean {
	void windowRegistry.version;
	return windowGroups.has(winId);
}

export function runWindowCommand(winId: string, id: string, arg?: string): boolean {
	const c = windowGroups.get(winId)?.[id];
	return !!c && runOne(c, arg);
}
