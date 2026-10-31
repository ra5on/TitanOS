<script lang="ts">
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
	import SideNav from './SideNav.svelte';
	import CommandPalette from './CommandPalette.svelte';
	import { palette, togglePalette } from '#lib/palette.svelte.ts';
	import StatusBar from './StatusBar.svelte';
	import ClusterBanner from './ClusterBanner.svelte';
	import { loadClusterNodes } from '#lib/cluster.svelte.ts';
	import TerminalPanel from './TerminalPanel.svelte';
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
	const side = $derived(appearance.current.nav.layout === 'side');

	// The nodes of the cluster, for the switcher; once per page load.
	$effect(() => {
		void loadClusterNodes();
	});

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
<div class="frame" class:side>
	{#if side}<SideNav />{/if}
	<div
		class="shell"
		style:padding-bottom={admin && terminal.open && !terminal.full
			? `${terminal.height}px`
			: undefined}
	>
		<Header {side} />
		<main class:readable={!dockerPage} class:dashboard={page.url.pathname === '/'}>
			<ClusterBanner />
			{#if !dockerPage}<ConnectionBanner
				/>{#if !['/', '/vms', '/vms/screen'].includes(page.url.pathname)}<TasksPanel
						hideEmptyMobile={page.url.pathname === '/files'}
					/>{/if}{/if}
			{@render children()}
		</main>
		<StatusBar note={shell.statusNote} />
	</div>
</div>
{#if admin}<TerminalPanel />{/if}
{#if palette.open}<CommandPalette onclose={() => (palette.open = false)} />{/if}
{#if shortcutsOpen}<ShortcutsDialog {admin} onclose={() => (shortcutsOpen = false)} />{/if}

<style>
	/* As an app on the home screen the phone's home indicator lies over
	   the bottom edge. */
	@media (display-mode: standalone) {
		.shell {
			padding-bottom: env(safe-area-inset-bottom);
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
	.frame.side {
		display: flex;
		align-items: flex-start;
	}
	.frame.side > .shell {
		flex: 1 1 auto;
		min-width: 0;
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
	@media (max-width: 760px) {
		main.dashboard {
			padding-top: 12px;
		}
		main {
			padding: 16px var(--page-pad) 24px;
		}
	}
</style>
