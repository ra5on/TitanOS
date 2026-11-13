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
	import TasksPanel from './TasksPanel.svelte';
	import CommandPalette from './CommandPalette.svelte';
	import { palette, togglePalette } from '#lib/palette.svelte.ts';
	import ClusterBanner from './ClusterBanner.svelte';
	import { loadClusterNodes } from '#lib/cluster.svelte.ts';
	import TerminalPanel from './TerminalPanel.svelte';
	import ShortcutsDialog from './ShortcutsDialog.svelte';
	import { appearance } from '#lib/theme/appearance.svelte.ts';
	import { effectOn } from '#lib/theme/themes.ts';
	import { afterNavigate } from '$app/navigation';
	import { jumpToSetting } from '#lib/settings-jump.ts';
	import { agentAppMode } from '#lib/agent-app.ts';
	import { agentFull } from '#lib/agent-full.svelte.ts';
	// PLAN 61: the desktop mode, the phone's home grid and pages in windows.
	// The desktop and the phone's home load only where they are shown, not
	// in every window's frame (PLAN 65-6).
	import type Desktop from '#lib/desktop/Desktop.svelte';
	import type PhoneHome from '#lib/desktop/PhoneHome.svelte';
	import PhoneBar from '#lib/desktop/PhoneBar.svelte';
	import { appForPath } from '#lib/desktop/apps.ts';
	import { announcePage, frameMessage, inWindow, ownPath } from '#lib/desktop/frame.ts';
	import { frameFor, mode } from '#lib/desktop/mode.svelte.ts';
	import { deskKey } from '#lib/desktop/menubar/keys.ts';
	import { goto } from '$app/navigation';
	import { pageTitle } from '#lib/page-title.svelte.ts';

	let { children }: { children: Snippet } = $props();
	// PLAN 66: once a web app was full screen on the phone it stays mounted (it guards unsaved work on Back).
	let phoneApp = $state(false);
	$effect(() => {
		if (page.state.webapp) phoneApp = true;
	});

	// A shell is a way into the system: administrators only.
	const dockerPage = $derived(
		page.url.pathname === '/docker' || page.url.pathname.startsWith('/docker/'),
	);
	const admin = $derived(['owner', 'admin'].includes(session.data?.user?.role ?? ''));

	let shortcutsOpen = $state(false);
	// Phones up to 760 px: the agent is an app of its own there.
	let phone = $state(matchMedia('(max-width: 760px)').matches);
	$effect(() => {
		const query = matchMedia('(max-width: 760px)');
		const change = () => (phone = query.matches);
		query.addEventListener('change', change);
		return () => query.removeEventListener('change', change);
	});
	// The KI-Agent installed as an app of its own shows no TitanOS frame,
	// nor does the agent switched to full screen (PLAN 47). On the phone it
	// is always an app of its own with its own menu (PLAN 48).
	const agentApp = $derived(
		agentAppMode(page.url) || (page.url.pathname.startsWith('/agent') && (agentFull.on || phone)),
	);
	// PLAN 61/65: a page in a window, the desktop, the phone, or the page
	// alone (pages of no program such as /welcome, the agent app, test scripts).
	const known = $derived(page.url.pathname === '/' || !!appForPath(page.url.pathname));
	const frame = $derived(frameFor({ known, agentApp, agentAppMode: agentAppMode(page.url) }));
	let DesktopView = $state<typeof Desktop | null>(null);
	let PhoneHomeView = $state<typeof PhoneHome | null>(null);
	$effect(() => {
		if (frame === 'desktop' && !DesktopView)
			void import('#lib/desktop/Desktop.svelte').then((m) => (DesktopView = m.default));
		if (frame === 'phone' && page.url.pathname === '/' && !PhoneHomeView)
			void import('#lib/desktop/PhoneHome.svelte').then((m) => (PhoneHomeView = m.default));
	});
	afterNavigate(() => {
		if (inWindow) announcePage(page.url.pathname + page.url.search, pageTitle.value);
		// PLAN 86: a hit of the settings search leads to the row itself (tab,
		// rows on the way opened, the setting marked).
		const path = page.url.pathname;
		if (path.startsWith('/settings') || path === '/account' || page.url.hash.startsWith('#find='))
			void jumpToSetting(page.url);
	});
	$effect(() => {
		if (inWindow) announcePage(page.url.pathname + page.url.search, pageTitle.value);
	});
	$effect(() => {
		if (!inWindow) return;
		// The side bar of the window moves the frame to another page of its program.
		const message = (event: MessageEvent) => {
			const msg = frameMessage(event);
			// A page may send it on at once (an area's old address, PLAN 86):
			// the first navigation then ends as "aborted", which is no error.
			if (msg?.type === 'titanos-goto' && event.source === window.parent && ownPath(msg.href))
				goto(msg.href).catch(() => {});
		};
		// Keys of the desktop (Alt+Q, Alt+W, Alt+arrows, Ctrl+Space) also work inside a window.
		const key = (event: KeyboardEvent) => {
			const desk = !!deskKey(event) || ((event.ctrlKey || event.metaKey) && event.code === 'Space');
			if (!desk) return;
			event.preventDefault();
			window.parent.postMessage(
				{
					type: 'titanos-key',
					key: deskKey(event) || event.key,
					alt: event.altKey,
					ctrl: event.ctrlKey,
					shift: event.shiftKey,
					meta: event.metaKey,
				},
				location.origin,
			);
		};
		window.addEventListener('message', message);
		window.addEventListener('keydown', key);
		return () => {
			window.removeEventListener('message', message);
			window.removeEventListener('keydown', key);
		};
	});
	// Only the phone's bar sticks above a page.
	$effect(() => {
		const root = document.documentElement.style;
		root.setProperty(
			'--header-height',
			frame === 'phone' ? 'calc(52px + env(safe-area-inset-top))' : '0px',
		);
		return () => root.removeProperty('--header-height');
	});

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
{#if frame === 'window'}
	<main class="in-window" class:readable={!dockerPage}>
		{#if !dockerPage}<ConnectionBanner />{/if}
		{@render children()}
	</main>
{:else if frame === 'desktop'}
	{#if DesktopView}<DesktopView />{/if}
{:else if frame === 'phone'}
	{#if page.url.pathname === '/'}
		{#if PhoneHomeView}<PhoneHomeView />{/if}
	{:else}
		<PhoneBar />
		<main class="phone-page" class:readable={!dockerPage}>
			<ClusterBanner />
			{#if !dockerPage}<ConnectionBanner />{/if}
			{@render children()}
		</main>
	{/if}
	<!-- PLAN 66: a web app full screen (opened from the home grid or the file manager). -->
	{#if page.state.webapp || phoneApp}
		{#await import('#lib/webapps/WebAppPhone.svelte') then { default: WebAppPhone }}
			<WebAppPhone />
		{/await}
	{/if}
{:else}
	<!-- The page alone; the page ends above the open terminal, so that all of it can be scrolled into view. -->
	<div
		class="bare"
		class:agent-app={agentApp}
		style:padding-bottom={admin && terminal.open && !terminal.full
			? `${terminal.height}px`
			: undefined}
	>
		<!-- The test scripts' page alone looks as in a window (lib/desktop/mode.svelte.ts). -->
		<main class:readable={!dockerPage} class:agent-app={agentApp} class:in-window={mode.bare}>
			<ClusterBanner />
			{#if !dockerPage}<ConnectionBanner />{/if}
			{@render children()}
		</main>
	</div>
{/if}
{#if admin && (frame === 'window' ? page.url.pathname === '/terminal' : frame !== 'desktop')}<TerminalPanel
	/>{/if}
{#if palette.open}<CommandPalette onclose={() => (palette.open = false)} />{/if}
{#if shortcutsOpen}<ShortcutsDialog {admin} onclose={() => (shortcutsOpen = false)} />{/if}
{#if shell.tasksSheet}<TasksPanel sheetOnly />{/if}

<style>
	/* PLAN 61: a page alone in a window of the desktop, or full screen on the phone. */
	.in-window,
	.phone-page {
		padding: var(--page-pad);
		min-height: 100dvh;
	}
	/* As an app on the home screen the phone's home indicator lies over
	   the bottom edge. */
	@media (display-mode: standalone) {
		/* The agent app keeps that space itself (PLAN 59: counted twice it
		   left a gap above the keyboard and a page that scrolls). */
		.bare:not(.agent-app) {
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
		gap: var(--space-2-5) var(--space-4);
		padding: var(--space-2-5) var(--space-4);
		background: var(--warn-bg);
		border-bottom: 1px solid var(--warn-border);
		color: var(--warn-text);
		font-size: 0.875rem;
	}
	.bare {
		position: relative;
		z-index: 1;
		min-height: 100vh;
		display: flex;
		flex-direction: column;
	}
	.bare main {
		flex: 1 1 auto;
		width: 100%;
		min-width: 0;
		margin: 0 auto;
		padding: var(--page-pad) var(--page-pad) 28px;
		display: flex;
		flex-direction: column;
		gap: var(--gap);
	}
	.bare main.agent-app {
		padding: 0;
	}
	@media (max-width: 760px) {
		.bare main:not(.agent-app) {
			padding: 16px var(--page-pad) 24px;
		}
	}
</style>
