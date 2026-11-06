// The terminal panel: which sessions are open and how the panel looks.
// It lives outside the pages, so sessions go on when the page changes.
import { embedded } from './embedded.ts';

export type TerminalSession = {
	id: number;
	/** A root shell on the NAS, a shell inside a container, the serial port of a VM, or a system container. */
	kind: 'nas' | 'ct' | 'vm' | 'lxc';
	title: string;
	/** ID of the container for `ct`, name of the VM for `vm`, of the system container for `lxc`. */
	container: string;
	/** '' picks bash where the image has it and sh everywhere else. */
	shell: '' | 'bash' | 'sh';
	/** For `ct`: the user the shell runs as; empty is the one of the image. */
	user?: string;
	started: number;
};

const STORAGE_KEY = 'titanos.terminal';
export const MIN_HEIGHT = 180;

function remembered(): { height: number; fontSize: number } {
	try {
		const stored = JSON.parse(localStorage.getItem(STORAGE_KEY) ?? '{}');
		return {
			height: Number(stored.height) || 380,
			fontSize: Math.min(18, Math.max(11, Number(stored.fontSize) || 13)),
		};
	} catch {
		return { height: 380, fontSize: 13 };
	}
}

export const terminal = $state({
	open: false,
	/** Fills the page below the header: the terminal in a browser tab of its own. */
	full: false,
	sessions: [] as TerminalSession[],
	active: 0,
	...remembered(),
});

let nextId = 1;

export function remember() {
	try {
		localStorage.setItem(
			STORAGE_KEY,
			JSON.stringify({ height: terminal.height, fontSize: terminal.fontSize }),
		);
	} catch {
		// It just will not be remembered in this browser.
	}
}

export type NewSession = Omit<TerminalSession, 'id' | 'started'>;

function add(session: NewSession) {
	// In a window of the desktop (PLAN 62-5) a console opens in the Terminal
	// window; the page asking for it has no panel of its own.
	if (embedded && location.pathname !== '/terminal') {
		window.parent.postMessage({ type: 'titanos-console', session }, location.origin);
		return;
	}
	const id = nextId++;
	terminal.sessions.push({ ...session, id, started: Date.now() });
	terminal.active = id;
	terminal.open = true;
}

export function addNasShell() {
	const count = terminal.sessions.filter((session) => session.kind === 'nas').length;
	add({ kind: 'nas', title: count === 0 ? 'nas' : `nas (${count + 1})`, container: '', shell: '' });
}

/** Opens the console of a container, or shows the one that is open already. */
export function addContainerConsole(container: string, name: string) {
	const existing = terminal.sessions.find(
		(session) => session.kind === 'ct' && session.container === container,
	);
	if (existing) {
		terminal.active = existing.id;
		terminal.open = true;
		return;
	}
	add({ kind: 'ct', title: name, container, shell: '' });
}

/** Opens the serial console of a VM, or shows the one that is open already. */
export function addVmConsole(name: string, label = name) {
	const existing = terminal.sessions.find(
		(session) => session.kind === 'vm' && session.container === name,
	);
	if (existing) {
		terminal.active = existing.id;
		terminal.open = true;
		return;
	}
	add({ kind: 'vm', title: label, container: name, shell: '' });
}

/** Opens a shell (or, with `console`, the text console) of a system container. */
export function addLXCConsole(name: string, label = name, mode: 'shell' | 'console' = 'shell') {
	const shell = mode === 'console' ? 'sh' : '';
	const existing = terminal.sessions.find(
		(session) => session.kind === 'lxc' && session.container === name && session.shell === shell,
	);
	if (existing) {
		terminal.active = existing.id;
		terminal.open = true;
		return;
	}
	add({
		kind: 'lxc',
		title: mode === 'console' ? `${label} (tty)` : label,
		container: name,
		shell,
	});
}

/** Opens a session asked for by another window (validated: only the known kinds). */
export function openRequested(value: unknown) {
	if (typeof value !== 'object' || value === null) return;
	const s = value as Record<string, unknown>;
	const text = (v: unknown, max = 200) => (typeof v === 'string' ? v.slice(0, max) : '');
	const container = text(s.container);
	const title = text(s.title, 80) || container;
	if (s.kind === 'nas') addNasShell();
	else if (s.kind === 'ct' && container) addContainerConsole(container, title);
	else if (s.kind === 'vm' && container) addVmConsole(container, title);
	else if (s.kind === 'lxc' && container)
		addLXCConsole(
			container,
			title.replace(/ \(tty\)$/, ''),
			s.shell === 'sh' ? 'console' : 'shell',
		);
}

export function closeSession(id: number) {
	const at = terminal.sessions.findIndex((session) => session.id === id);
	if (at < 0) return;
	terminal.sessions.splice(at, 1);
	if (terminal.active === id) terminal.active = terminal.sessions[Math.max(0, at - 1)]?.id ?? 0;
}

export function toggleTerminal() {
	terminal.open = !terminal.open;
}
