<script lang="ts">
	import '@fontsource/ibm-plex-sans/latin-400.css';
	import '@fontsource/ibm-plex-sans/latin-500.css';
	import '@fontsource/ibm-plex-sans/latin-600.css';
	import '@fontsource/ibm-plex-mono/latin-400.css';
	import '@fontsource/ibm-plex-mono/latin-500.css';
	import '../app.css';

	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { afterNavigate, beforeNavigate, goto, onNavigate } from '$app/navigation';
	import { isAgentPath } from '#lib/agent-app.ts';
	import DashboardBackground from '#lib/dashboard/DashboardBackground.svelte';
	import { dashboardVisual } from '#lib/dashboard/visual.ts';
	import { dashboardScene } from '#lib/dashboard/scene.svelte.ts';
	import Backdrop from '#lib/components/Backdrop.svelte';
	import Shell from '#lib/components/Shell.svelte';
	import { i18n, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import Login from '#lib/pages/Login.svelte';
	import Problem from '#lib/pages/Problem.svelte';
	import Setup from '#lib/pages/Setup.svelte';
	import LoginPreview from '#lib/login/LoginPreview.svelte';
	import { loadLoginLook } from '#lib/login/state.svelte.ts';
	import { refreshSession, session } from '#lib/session.svelte.ts';
	import { appearance, initAppearance } from '#lib/theme/appearance.svelte.ts';
	import { effectOn } from '#lib/theme/themes.ts';
	import { pageTitle } from '#lib/page-title.svelte.ts';
	import { clearAgentNotifications, registerServiceWorker } from '#lib/web-push.ts';

	const agentRoute = $derived(isAgentPath(page.url.pathname));
	// Safari reads the manifest only when a page loads: the step between
	// TitanOS and the KI-Agent loads the page anew, so "Add to Home Screen"
	// makes the right app on either side (PLAN 48). Links are turned into a
	// full load; back and forward keep the browser's history and reload
	// once they arrived.
	const crosses = (a: URL, b: URL) => isAgentPath(a.pathname) !== isAgentPath(b.pathname);
	beforeNavigate(({ from, to, type, cancel }) => {
		if (type !== 'link' && type !== 'goto') return;
		if (!from || !to || to.url.origin !== location.origin || !crosses(from.url, to.url)) return;
		cancel();
		location.assign(to.url.href);
	});
	afterNavigate(({ from, to, type }) => {
		if (type === 'popstate' && from && to && crosses(from.url, to.url)) location.reload();
	});

	let { children } = $props();

	// A tapped notification moves the open window (sw.js, PLAN 59): no
	// reload, nothing unsaved gets lost; crossing to the agent loads anew.
	onMount(() => {
		if (!('serviceWorker' in navigator)) return;
		const open = (e: MessageEvent) => {
			const url = e.data?.type === 'open' && typeof e.data.url === 'string' ? e.data.url : '';
			if (url.startsWith('/') && !url.startsWith('//')) void goto(url);
		};
		navigator.serviceWorker.addEventListener('message', open);
		return () => navigator.serviceWorker.removeEventListener('message', open);
	});
	onMount(() => {
		initAppearance();
		refreshSession();
		loadLoginLook();
		registerServiceWorker();
		// Notifications of agent chats read elsewhere go away (PLAN 48).
		const tidy = () => {
			if (document.visibilityState === 'visible' && session.services?.agent)
				void clearAgentNotifications();
		};
		setTimeout(tidy, 1500);
		document.addEventListener('visibilitychange', tidy);
		return () => document.removeEventListener('visibilitychange', tidy);
	});

	$effect(() => {
		document.documentElement.lang = i18n.locale;
	});

	// Page transitions (Settings › Appearance › Effects): only between pages,
	// not for a changed query on the same page, and never for reduced motion.
	onNavigate((navigation) => {
		if (!document.startViewTransition || !effectOn(appearance.current, 'transitions')) return;
		if (navigation.from?.url.pathname === navigation.to?.url.pathname) return;
		if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) return;
		// The header is named only while a transition runs: a named element is
		// a backdrop root, and the glass of the account menu and the
		// notifications in it would no longer blur the page behind them.
		const root = document.documentElement;
		root.dataset.viewTransition = 'running';
		return new Promise((resolve) => {
			const transition = document.startViewTransition(async () => {
				resolve();
				await navigation.complete;
			});
			transition.finished.finally(() => delete root.dataset.viewTransition);
		});
	});

	// The dashboard background; a picture of its own also behind every other
	// page unless switched off. "Like the general background" and "none" stay
	// on the dashboard: the other pages have the general background anyway.
	const shownVisual = $derived(
		dashboardScene.preview ?? dashboardVisual(session.dashboard?.visual),
	);
	const backgroundEverywhere = $derived(
		shownVisual.background.everywhere &&
			(shownVisual.background.source === 'builtin' || shownVisual.background.source === 'custom'),
	);
	// Pages without a PageHead (the dashboard) are named by their place in the navigation.
	const ROUTE_TITLES: Record<string, MessageKey> = {
		'/': 'nav.dashboard',
		'/terminal': 'terminal.title',
	};
	const documentTitle = $derived.by(() => {
		const host = session.data?.hostname;
		const route = ROUTE_TITLES[page.url.pathname];
		const name = session.data?.user ? pageTitle.value || (route ? t(route) : '') : '';
		return [name, host, 'TitanOS'].filter(Boolean).join(' – ');
	});

	// The agent app reaches under the status bar of the iPhone (PLAN 56):
	// translucent status bar and the whole screen there, TitanOS keeps the
	// black bar. iOS reads both at load; crossing to and from the agent
	// reloads the page.
	$effect.pre(() => {
		const status = document.querySelector('meta[name="apple-mobile-web-app-status-bar-style"]');
		const viewport = document.querySelector('meta[name="viewport"]');
		status?.setAttribute('content', agentRoute ? 'black-translucent' : 'black');
		viewport?.setAttribute(
			'content',
			agentRoute
				? 'width=device-width, initial-scale=1, viewport-fit=cover'
				: 'width=device-width, initial-scale=1',
		);
	});
</script>

<svelte:head>
	<title>{documentTitle}</title>
	<!-- The KI-Agent installs as an app of its own (PLAN 44): its own
	     manifest, icon and name on /agent, TitanOS everywhere else. -->
	{#if agentRoute}
		<link rel="manifest" href="/agent.webmanifest" />
		<link rel="apple-touch-icon" href="/icons/agent-180.png" />
		<meta name="apple-mobile-web-app-title" content="TitanOS Agent" />
	{:else}
		<link rel="manifest" href="/manifest.webmanifest" />
		<link rel="apple-touch-icon" href="/icons/app-180.png" />
		<meta name="apple-mobile-web-app-title" content="TitanOS" />
	{/if}
</svelte:head>

{#if session.data?.user && (page.url.pathname === '/' || backgroundEverywhere)}<DashboardBackground
		visual={shownVisual}
		inherited={appearance.current}
		urls={dashboardScene.urls}
	/>{:else}<Backdrop appearance={appearance.current} />{/if}

{#if session.status === 'unreachable'}
	<Problem title={t('unreachable.title')} text={t('unreachable.text')} />
{:else if session.status === 'ready' && session.data}
	{#if !session.data.configAvailable}
		<Problem title={t('config.missingTitle')} text={t('config.missingText')} />
	{:else if session.data.setupRequired}
		<Setup />
	{:else if !session.data.user}
		<Login />
	{:else if page.url.pathname === '/login-preview'}
		<LoginPreview />
	{:else}
		<Shell>{@render children()}</Shell>
	{/if}
{:else if session.status === 'loading'}
	<p class="starting muted" role="status" aria-live="polite">
		<span class="dot" aria-hidden="true"></span>{t('common.loading')}
	</p>
{/if}

<style>
	.starting {
		display: flex;
		align-items: center;
		justify-content: center;
		gap: 10px;
		min-height: 60vh;
		margin: 0;
		font-size: var(--text-md);
		/* Only after a moment, so that a quick start does not flash. */
		animation: appear 0.2s 0.4s both;
	}
	.dot {
		width: 10px;
		height: 10px;
		border-radius: 50%;
		background: var(--accent);
		animation: pulse 1s ease-in-out infinite alternate;
	}
	@keyframes appear {
		from {
			opacity: 0;
		}
	}
	@keyframes pulse {
		to {
			opacity: 0.25;
		}
	}
	@media (prefers-reduced-motion: reduce) {
		.dot {
			animation: none;
		}
	}
</style>
