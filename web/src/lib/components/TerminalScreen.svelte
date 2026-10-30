<script lang="ts">
	import { apiSocket } from '#lib/cluster-node.ts';
	// The query of a container console: shell and user, both optional.
	function containerQuery(shell: string, user?: string): string {
		const query = new URLSearchParams();
		if (shell) query.set('shell', shell);
		if (user) query.set('user', user);
		const text = query.toString();
		return text ? `?${text}` : '';
	}
	// One session of the terminal panel: xterm.js in the browser, a WebSocket
	// to titand, a shell behind it.
	import '@xterm/xterm/css/xterm.css';

	import { FitAddon } from '@xterm/addon-fit';
	import { SearchAddon } from '@xterm/addon-search';
	import { Terminal } from '@xterm/xterm';
	import { t } from '#lib/i18n/index.svelte.ts';
	import type { TerminalSession } from '#lib/terminal.svelte.ts';

	type Props = {
		session: TerminalSession;
		active: boolean;
		fontSize: number;
		/** The password was entered again; without that titand refuses. */
		confirmed: boolean;
		/**
		 * titand refused the session. Resolves to whether there is a reason
		 * that can be mended: the confirmation ran out, terminals are off.
		 */
		onrefused: () => Promise<boolean>;
	};
	let { session, active, fontSize, confirmed, onrefused }: Props = $props();

	let screen = $state<HTMLElement>();
	let phase = $state<'waiting' | 'open' | 'ended'>('waiting');
	let received = false;
	let noShell = $state(false);

	let term: Terminal | undefined;
	let fit: FitAddon | undefined;
	let search: SearchAddon | undefined;
	let socket: WebSocket | undefined;

	/** The terminal's colors follow the theme. */
	function themeColors() {
		const css = getComputedStyle(document.documentElement);
		const read = (name: string) => css.getPropertyValue(name).trim();
		return {
			background: read('--bg'),
			foreground: read('--text'),
			cursor: read('--accent'),
			selectionBackground: read('--chip'),
		};
	}

	// The terminal itself lives as long as the tab, whatever the connection does.
	$effect(() => {
		if (!screen) return;
		const terminal = new Terminal({
			fontFamily: "'IBM Plex Mono', monospace",
			fontSize: 13,
			cursorBlink: true,
			scrollback: 5000,
			theme: themeColors(),
		});
		fit = new FitAddon();
		search = new SearchAddon();
		terminal.loadAddon(fit);
		terminal.loadAddon(search);
		terminal.open(screen);
		term = terminal;

		const encoder = new TextEncoder();
		const input = terminal.onData(
			(data) => socket?.readyState === WebSocket.OPEN && socket.send(encoder.encode(data)),
		);
		const resized = terminal.onResize(
			() =>
				socket?.readyState === WebSocket.OPEN &&
				socket.send(JSON.stringify({ cols: terminal.cols, rows: terminal.rows })),
		);
		const observer = new ResizeObserver(() => screen?.offsetParent && fit?.fit());
		observer.observe(screen);
		// A changed theme changes the colors of what is open, too.
		const themed = new MutationObserver(() => (terminal.options.theme = themeColors()));
		themed.observe(document.documentElement, { attributes: true });

		return () => {
			observer.disconnect();
			themed.disconnect();
			input.dispose();
			resized.dispose();
			if (socket) socket.onclose = null;
			socket?.close();
			terminal.dispose();
			term = undefined;
		};
	});

	// Connects as soon as the password was confirmed.
	$effect(() => {
		if (!confirmed || !term || socket || phase === 'ended') return;
		const terminal = term;
		const path =
			session.kind === 'nas'
				? '/terminal/shell'
				: session.kind === 'vm'
					? `/vms/${session.container}/serial`
					: session.kind === 'lxc'
						? `/lxc/${session.container}/console${session.shell ? '?mode=console' : ''}`
						: `/docker/containers/${session.container}/console${containerQuery(session.shell, session.user)}`;
		const connection = new WebSocket(apiSocket(path));
		connection.binaryType = 'arraybuffer';
		socket = connection;
		let opened = false;
		connection.onopen = () => {
			opened = true;
			phase = 'open';
			if (session.kind === 'nas') {
				terminal.writeln(`\x1b[90m${t('terminal.nasBanner')}\x1b[0m`);
				terminal.writeln(`\x1b[33m${t('terminal.nasWarning')}\x1b[0m\r\n`);
			}
			if (session.kind === 'vm') terminal.writeln(`\x1b[90m${t('terminal.vmBanner')}\x1b[0m\r\n`);
			if (session.kind === 'lxc' && session.shell)
				terminal.writeln(`\x1b[90m${t('terminal.lxcBanner')}\x1b[0m\r\n`);
			connection.send(JSON.stringify({ cols: terminal.cols, rows: terminal.rows }));
			if (active) terminal.focus();
		};
		connection.onmessage = (event) => {
			received = true;
			terminal.write(new Uint8Array(event.data as ArrayBuffer));
		};
		connection.onclose = () => {
			socket = undefined;
			if (!opened) {
				// Refused before it began: the caller finds out why. Without
				// a reason it can mend, this session is over.
				onrefused().then((mendable) => !mendable && (phase = 'ended'));
				return;
			}
			// Minimal images come without any shell: the session ends before a prompt appears.
			noShell = session.kind === 'ct' && !received;
			phase = 'ended';
		};
	});

	$effect(() => {
		if (term && term.options.fontSize !== fontSize) {
			term.options.fontSize = fontSize;
			fit?.fit();
		}
	});

	// A hidden terminal has no size; fit it when its tab comes to the front.
	$effect(() => {
		if (active && term) {
			fit?.fit();
			term.focus();
		}
	});

	export function copy() {
		const selection = term?.getSelection();
		if (selection) navigator.clipboard?.writeText(selection);
	}

	export async function paste() {
		try {
			const text = await navigator.clipboard.readText();
			if (text) term?.paste(text);
		} catch {
			// The browser did not allow reading the clipboard; Ctrl+Shift+V still works.
		}
		term?.focus();
	}

	export function find(text: string, backwards = false) {
		if (!text) return;
		if (backwards) search?.findPrevious(text);
		else search?.findNext(text);
	}

	/** Whether the connection to the shell is still open. */
	export function connected(): boolean {
		return phase === 'open';
	}

	export function focus() {
		term?.focus();
	}
</script>

<div class="session" class:hidden={!active}>
	<div class="screen" bind:this={screen}></div>
	{#if phase === 'ended'}
		<p class="ended" role="status">
			{t(noShell ? 'docker.consoleNoShell' : 'docker.consoleClosed')}
		</p>
	{/if}
</div>

<style>
	.session {
		position: absolute;
		inset: 0;
		display: flex;
		flex-direction: column;
	}
	.session.hidden {
		visibility: hidden;
	}
	.screen {
		flex: 1 1 auto;
		min-height: 0;
		padding: 8px 12px;
		overflow: hidden;
	}
	.ended {
		padding: 6px 16px;
		background: var(--warn-bg);
		color: var(--warn-text);
		font-size: 0.8125rem;
	}
</style>
