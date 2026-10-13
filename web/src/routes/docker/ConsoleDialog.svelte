<script lang="ts">
	// A shell inside a container: xterm.js in the browser, a WebSocket to
	// titand, `docker exec` behind it.
	import '@xterm/xterm/css/xterm.css';

	import { FitAddon } from '@xterm/addon-fit';
	import { Terminal } from '@xterm/xterm';
	import { api, ApiError, type DockerContainer } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';

	type Props = { container: DockerContainer; onclose: () => void };
	let { container, onclose }: Props = $props();

	let phase = $state<'checking' | 'confirm' | 'open' | 'closed'>('checking');
	let password = $state('');
	let error = $state('');
	let screen = $state<HTMLElement>();

	$effect(() => {
		api.get<{ confirmed: boolean }>('/session/confirmed').then(
			(res) => (phase = res.confirmed ? 'open' : 'confirm'),
			() => (phase = 'confirm')
		);
	});

	async function confirm(event: SubmitEvent) {
		event.preventDefault();
		error = '';
		try {
			await api.post('/session/confirm', { password });
			password = '';
			phase = 'open';
		} catch (err) {
			error = errorText(err instanceof ApiError ? err.code : 'internal');
		}
	}

	/** The terminal's colors follow the theme. */
	function themeColors() {
		const css = getComputedStyle(document.documentElement);
		const read = (name: string) => css.getPropertyValue(name).trim();
		return { background: read('--bg'), foreground: read('--text'), cursor: read('--accent'), selectionBackground: read('--chip') };
	}

	let received = false;
	let noShell = $state(false);

	// Runs once the terminal's element exists. It deliberately does not
	// depend on the phase: when the session ends, what is on the screen stays.
	$effect(() => {
		if (!screen) return;
		const terminal = new Terminal({ fontFamily: "'IBM Plex Mono', monospace", fontSize: 14, cursorBlink: true, theme: themeColors() });
		const fit = new FitAddon();
		terminal.loadAddon(fit);
		terminal.open(screen);
		fit.fit();

		const socket = new WebSocket(`${location.protocol === 'https:' ? 'wss' : 'ws'}://${location.host}/api/v1/docker/containers/${container.id}/console`);
		socket.binaryType = 'arraybuffer';
		const sendSize = () => socket.readyState === WebSocket.OPEN && socket.send(JSON.stringify({ cols: terminal.cols, rows: terminal.rows }));
		const encoder = new TextEncoder();
		socket.onopen = () => {
			sendSize();
			terminal.focus();
		};
		socket.onmessage = (event) => {
			received = true;
			terminal.write(new Uint8Array(event.data as ArrayBuffer));
		};
		socket.onclose = () => {
			// Minimal images come without any shell: the session ends before a prompt appears.
			noShell = !received;
			phase = 'closed';
		};
		const input = terminal.onData((data) => socket.readyState === WebSocket.OPEN && socket.send(encoder.encode(data)));
		const resized = terminal.onResize(sendSize);
		const observer = new ResizeObserver(() => fit.fit());
		observer.observe(screen);

		return () => {
			observer.disconnect();
			input.dispose();
			resized.dispose();
			socket.onclose = null;
			socket.close();
			terminal.dispose();
		};
	});
</script>

<Dialog title={t('docker.consoleTitle', { name: container.name })} wide {onclose}>
	{#if phase === 'confirm'}
		<form onsubmit={confirm}>
			<p class="text">{t('docker.consoleConfirm')}</p>
			<label><span>{t('auth.password')}</span>
				<!-- svelte-ignore a11y_autofocus -->
				<input class="field" type="password" bind:value={password} autocomplete="current-password" required autofocus />
			</label>
			{#if error}<p class="notice-warn" role="alert">{error}</p>{/if}
			<div class="dialog-actions">
				<button type="button" class="btn" onclick={onclose}>{t('common.cancel')}</button>
				<button type="submit" class="btn btn-primary">{t('docker.consoleOpen')}</button>
			</div>
		</form>
	{:else if phase === 'open' || phase === 'closed'}
		<div class="screen" bind:this={screen}></div>
		<div class="dialog-actions">
			<span class="muted note" role="status">{phase === 'closed' ? t(noShell ? 'docker.consoleNoShell' : 'docker.consoleClosed') : t('docker.consoleNote')}</span>
			<button type="button" class="btn btn-primary" onclick={onclose}>{t('common.done')}</button>
		</div>
	{/if}
</Dialog>

<style>
	form {
		display: flex;
		flex-direction: column;
		gap: 16px;
	}
	label {
		display: flex;
		flex-direction: column;
		gap: 6px;
		font-size: 0.875rem;
	}
	.text {
		font-size: 0.875rem;
	}
	.screen {
		height: min(60vh, 460px);
		padding: 8px;
		background: var(--bg);
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		overflow: hidden;
	}
	.note {
		margin-right: auto;
		align-self: center;
		font-size: 0.75rem;
	}
</style>
