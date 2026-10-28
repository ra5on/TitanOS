<script lang="ts">
	import AppTabBar from './AppTabBar.svelte';
	// The frame around every page once someone is logged in.
	import type { Snippet } from 'svelte';
	import { page } from '$app/state';
	import ConnectionBanner from './ConnectionBanner.svelte';
	import TasksPanel from './TasksPanel.svelte';
	import { session } from '#lib/session.svelte.ts';
	import { shell } from '#lib/shell.svelte.ts';
	import { watchStorage } from '#lib/storage.svelte.ts';
	import { api } from '#lib/api.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { system, watchSystem } from '#lib/system.svelte.ts';
	import { terminal, toggleTerminal } from '#lib/terminal.svelte.ts';
	import Header from './Header.svelte';
	import CommandPalette from './CommandPalette.svelte';
	import { palette, togglePalette } from '#lib/palette.svelte.ts';
	import StatusBar from './StatusBar.svelte';
	import TerminalPanel from './TerminalPanel.svelte';
	import Icon from './Icon.svelte';
	import { assistant, loadAssistant, toggleAssistant } from '#lib/assistant.svelte.ts';
	import ShortcutsDialog from './ShortcutsDialog.svelte';
	import { appearance } from '#lib/theme/appearance.svelte.ts';
	import { effectOn } from '#lib/theme/themes.ts';

	let { children }: { children: Snippet } = $props();

	// A shell is a way into the system: administrators only.
	const dockerPage = $derived(
		page.url.pathname === '/docker' || page.url.pathname.startsWith('/docker/'),
	);
	const admin = $derived(['owner', 'admin'].includes(session.data?.user?.role ?? ''));

	let shortcutsOpen = $state(false);

	/** Whether a key press belongs to a field, an editor or the terminal. */
	function typing(target: EventTarget | null) {
		const el = target as HTMLElement | null;
		return !!el?.closest?.('input, textarea, select, [contenteditable], .xterm, .terminal, canvas');
	}

	function hotkey(event: KeyboardEvent) {
		// "?" lists the shortcuts, unless someone is typing or a dialog is open.
		if (
			event.key === '?' &&
			!event.ctrlKey &&
			!event.metaKey &&
			!event.altKey &&
			effectOn(appearance.current, 'shortcuts') &&
			!typing(event.target) &&
			!document.querySelector('dialog[open]')
		) {
			event.preventDefault();
			shortcutsOpen = true;
			return;
		}
		if (
			(event.ctrlKey || event.metaKey) &&
			!event.altKey &&
			!event.shiftKey &&
			event.code === 'KeyK'
		) {
			if (!palette.open && document.querySelector('dialog[open]')) return;
			event.preventDefault();
			togglePalette();
			return;
		}
		// Ctrl+` wherever the key is; layouts differ in what it types.
		if (
			admin &&
			event.ctrlKey &&
			!event.altKey &&
			!event.metaKey &&
			(event.key === '`' || event.code === 'Backquote')
		) {
			event.preventDefault();
			toggleTerminal();
		}
		// Ctrl+J opens and closes the chat with the assistant.
		if (
			assistant.status?.available &&
			event.ctrlKey &&
			!event.altKey &&
			!event.metaKey &&
			!event.shiftKey &&
			event.code === 'KeyJ'
		) {
			event.preventDefault();
			toggleAssistant();
		}
	}

	const pendingSeconds = $derived(system.info?.networkPendingSeconds ?? 0);
	let settling = $state(false);
	async function settle(action: 'confirm' | 'revert') {
		settling = true;
		try {
			await api.post(`/network/${action}`);
		} catch {
			// The next look at the system says what became of it.
		}
		settling = false;
	}

	$effect(() => watchSystem());
	$effect(() => watchStorage());
	// Whether this account may chat decides the buttons for the assistant.
	$effect(() => {
		if (session.data?.user?.name) loadAssistant();
	});
	const fab = $derived(
		!!assistant.status?.available && assistant.prefs.floating && page.url.pathname !== '/assistant',
	);
</script>

<svelte:window onkeydown={hotkey} />

<!-- The page ends above the open terminal, so that all of it can be scrolled into view. -->
{#if admin && pendingSeconds > 0}
	<!-- A network change on trial: whoever still reaches the NAS confirms it, from any page. -->
	<div class="trial" role="alert">
		<span>{t('netcfg.pending', { seconds: pendingSeconds })}</span>
		<button
			type="button"
			class="btn btn-primary"
			onclick={() => settle('confirm')}
			disabled={settling}>{t('netcfg.keep')}</button
		>
		<button type="button" class="btn" onclick={() => settle('revert')} disabled={settling}
			>{t('netcfg.back')}</button
		>
	</div>
{/if}
<div
	class="shell"
	style:padding-bottom={admin && terminal.open && !terminal.full
		? `${terminal.height}px`
		: undefined}
>
	<Header />
	<main class:readable={!dockerPage} class:dashboard={page.url.pathname === '/'}>
		{#if !dockerPage}<ConnectionBanner
			/>{#if !['/', '/vms', '/vms/screen'].includes(page.url.pathname)}<TasksPanel hideEmptyMobile={page.url.pathname === '/files'} />{/if}{/if}
		{@render children()}
	</main>
	<StatusBar note={shell.statusNote} />
</div>
<AppTabBar />
{#if admin}<TerminalPanel />{/if}
{#if palette.open}<CommandPalette onclose={() => (palette.open = false)} />{/if}
{#if shortcutsOpen}<ShortcutsDialog {admin} assistant={!!assistant.status?.available} onclose={() => (shortcutsOpen = false)} />{/if}
{#if fab}
	<button
		type="button"
		class="fab"
		style:bottom={admin && terminal.open && !terminal.full
			? `${terminal.height + 16}px`
			: undefined}
		aria-label={t('assistant.title')}
		title={t('assistant.shortcut')}
		onclick={toggleAssistant}
	>
		<Icon name="chat" size={22} />
	</button>
{/if}

<style>
	@media (display-mode: standalone) and (max-width: 760px) {
		.shell {
			padding-bottom: calc(64px + env(safe-area-inset-bottom));
		}
		.fab {
			bottom: calc(80px + env(safe-area-inset-bottom));
		}
	}
	.trial {
		position: sticky;
		top: 0;
		z-index: 30;
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: center;
		gap: 10px 16px;
		padding: 10px 16px;
		background: var(--warn-bg);
		border-bottom: 1px solid var(--warn-border);
		color: var(--warn-text);
		font-size: 0.875rem;
	}
	.shell {
		position: relative;
		z-index: 1;
		min-height: 100vh;
		display: flex;
		flex-direction: column;
	}
	main {
		flex: 1 1 auto;
		width: 100%;
		min-width: 0;
		margin: 0 auto;
		padding: var(--page-pad) var(--page-pad) 28px;
		display: flex;
		flex-direction: column;
		gap: var(--gap);
	}
	main.dashboard {
		padding-top: 12px;
	}
	.fab {
		position: fixed;
		right: 20px;
		bottom: 44px;
		z-index: 29;
		display: flex;
		align-items: center;
		justify-content: center;
		width: 52px;
		height: 52px;
		padding: 0;
		border: 0;
		border-radius: 50%;
		background: var(--accent);
		color: var(--on-accent);
		box-shadow: var(--shadow-menu);
		cursor: pointer;
	}
	.fab:hover {
		filter: brightness(1.08);
	}
	.fab:focus-visible {
		outline: 2px solid var(--text);
		outline-offset: 2px;
	}
	@media (max-width: 760px) {
		main.dashboard {
			padding-top: 12px;
		}
		.fab {
			right: 14px;
			bottom: 40px;
		}
		main {
			padding: 16px var(--page-pad) 24px;
		}
	}
</style>
