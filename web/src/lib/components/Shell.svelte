<script lang="ts">
	// The frame around every page once someone is logged in.
	import type { Snippet } from 'svelte';
	import { page } from '$app/state';
	import ConnectionBanner from './ConnectionBanner.svelte';
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
	import { afterNavigate } from '$app/navigation';
	import Icon from './Icon.svelte';
	import { navForRole } from '#lib/navigation.ts';
	import { agentAppMode } from '#lib/agent-app.ts';

	let { children }: { children: Snippet } = $props();

	// A shell is a way into the system: administrators only.
	const dockerPage = $derived(
		page.url.pathname === '/docker' || page.url.pathname.startsWith('/docker/'),
	);
	const admin = $derived(['owner', 'admin'].includes(session.data?.user?.role ?? ''));

	let shortcutsOpen = $state(false);
	// The sidebar on the desktop; on a phone "Side" means a round menu
	// button at the bottom left that slides the same sidebar in.
	let desktop = $state(matchMedia('(min-width: 761px)').matches);
	$effect(() => {
		const query = matchMedia('(min-width: 761px)');
		const change = () => (desktop = query.matches);
		query.addEventListener('change', change);
		return () => query.removeEventListener('change', change);
	});
	// The KI-Agent installed as an app of its own shows no TitanOS frame.
	const agentApp = $derived(agentAppMode(page.url));
	const side = $derived(appearance.current.nav.layout === 'side' && desktop && !agentApp);
	// The area the page belongs to, for the button's name.
	const pageLabel = $derived(
		navForRole(session.data?.user?.role ?? 'user', session.services).find((item) =>
			item.href === '/'
				? page.url.pathname === '/'
				: page.url.pathname === item.href || page.url.pathname.startsWith(item.href + '/'),
		)?.label ?? 'nav.label',
	);
	const sidePhone = $derived(appearance.current.nav.layout === 'side' && !desktop && !agentApp);
	let drawer = $state<HTMLDialogElement>();
	let drawerOpen = $state(false);
	const openDrawer = () => drawer?.showModal();
	const closeDrawer = () => drawer?.close();
	afterNavigate(() => closeDrawer());
	$effect(() => {
		if (!sidePhone) closeDrawer();
	});
	// The button is hidden while the terminal covers the bottom of the window.
	const fab = $derived(sidePhone && !(admin && terminal.open));
	// Room for the button: pages end above it, bars at the bottom start beside it.
	$effect(() => {
		const root = document.documentElement.style;
		if (fab) root.setProperty('--fab-clear', '76px');
		else root.removeProperty('--fab-clear');
		return () => root.removeProperty('--fab-clear');
	});
	// A swipe from the left edge opens the menu, a swipe to the left closes it.
	let swipe: { x: number; y: number; open: boolean } | null = null;
	function swipeStart(event: TouchEvent) {
		if (!sidePhone || event.touches.length !== 1) return;
		const { clientX: x, clientY: y } = event.touches[0];
		const open = drawer?.open ?? false;
		swipe = open || x <= 24 ? { x, y, open } : null;
	}
	function swipeMove(event: TouchEvent) {
		if (!swipe) return;
		const dx = event.touches[0].clientX - swipe.x;
		const dy = Math.abs(event.touches[0].clientY - swipe.y);
		if (dy > Math.abs(dx)) swipe = null;
		else if (!swipe.open && dx > 50) {
			swipe = null;
			openDrawer();
		} else if (swipe.open && dx < -50) {
			swipe = null;
			closeDrawer();
		}
	}
	// Without head line and status bar nothing sticks below or above them.
	$effect(() => {
		const root = document.documentElement.style;
		root.setProperty('--header-height', side || sidePhone || agentApp ? '0px' : '60px');
		root.setProperty('--status-height', side || sidePhone || agentApp ? '0px' : '34px');
		return () => {
			root.removeProperty('--header-height');
			root.removeProperty('--status-height');
		};
	});
	// The VM pages have their own entry for running tasks.
	const ownTasks = $derived(['/vms', '/vms/screen'].includes(page.url.pathname));

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

<svelte:window
	onkeydown={hotkey}
	ontouchstart={swipeStart}
	ontouchmove={swipeMove}
	ontouchend={() => (swipe = null)}
/>

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
	{#if side}<SideNav tasks={!ownTasks} note={shell.statusNote} />{/if}
	<div
		class="shell"
		style:padding-bottom={admin && terminal.open && !terminal.full
			? `${terminal.height}px`
			: undefined}
	>
		{#if !side && !sidePhone && !agentApp}<Header tasks={!ownTasks} />{/if}
		<main
			class:readable={!dockerPage}
			class:dashboard={page.url.pathname === '/'}
			class:agent-app={agentApp}
		>
			<ClusterBanner />
			{#if !dockerPage}<ConnectionBanner />{/if}
			{@render children()}
		</main>
		{#if !side && !sidePhone && !agentApp}<StatusBar note={shell.statusNote} />{/if}
	</div>
</div>
{#if sidePhone}
	{#if fab}<button
			type="button"
			class="fab"
			aria-label={t('nav.openMenu', { page: t(pageLabel) })}
			aria-haspopup="dialog"
			aria-expanded={drawerOpen}
			onclick={openDrawer}><Icon name="menu" size={24} /></button
		>{/if}
	<!-- Modal: Escape closes it, focus stays inside; a tap beside it closes it too. -->
	<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_noninteractive_element_interactions -->
	<dialog
		class="drawer"
		aria-label={t('nav.label')}
		bind:this={drawer}
		onclose={() => (drawerOpen = false)}
		ontoggle={() => (drawerOpen = drawer?.open ?? false)}
		onclick={(event) => {
			if (event.target === drawer) closeDrawer();
		}}
	>
		{#if drawerOpen}<SideNav
				drawer
				tasks={!ownTasks}
				note={shell.statusNote}
				onclose={closeDrawer}
			/>{/if}
	</dialog>
{/if}
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
	.fab {
		position: fixed;
		left: 16px;
		bottom: calc(16px + env(safe-area-inset-bottom));
		z-index: 25;
		display: grid;
		place-items: center;
		width: 52px;
		height: 52px;
		border: 1px solid var(--border);
		border-radius: 50%;
		background: var(--menu-bg, var(--surface));
		backdrop-filter: var(--menu-backdrop, none);
		box-shadow: var(--shadow-menu);
		color: var(--text);
		cursor: pointer;
	}
	.fab:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	.drawer {
		position: fixed;
		inset: 0 auto 0 0;
		width: auto;
		max-width: none;
		height: 100dvh;
		max-height: none;
		margin: 0;
		padding: 0;
		border: 0;
		background: transparent;
		box-shadow: var(--shadow-menu);
		color: inherit;
	}
	/* showModal() focuses the dialog itself (the sidebar inside is rendered
	   just after); Safari then draws the focus ring, a line along its right edge. */
	.drawer:focus,
	.drawer:focus-visible {
		outline: none;
	}
	.drawer::backdrop {
		background: var(--overlay, rgb(0 0 0 / 0.5));
	}
	.drawer[open] {
		animation: slide-in 180ms ease-out;
	}
	@keyframes slide-in {
		from {
			transform: translateX(-100%);
		}
	}
	@media (prefers-reduced-motion: reduce) {
		.drawer[open] {
			animation: none;
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
		gap: 10px var(--space-4);
		padding: 10px var(--space-4);
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
		padding-top: var(--space-3);
	}
	main.agent-app {
		padding: 0;
	}
	@media (max-width: 760px) {
		main.dashboard {
			padding-top: var(--space-3);
		}
		main {
			padding: 16px var(--page-pad) calc(24px + var(--fab-clear, 0px));
		}
	}
</style>
