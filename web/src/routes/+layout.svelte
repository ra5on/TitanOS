<script lang="ts">
	import '@fontsource/ibm-plex-sans/latin-400.css';
	import '@fontsource/ibm-plex-sans/latin-500.css';
	import '@fontsource/ibm-plex-sans/latin-600.css';
	import '@fontsource/ibm-plex-mono/latin-400.css';
	import '@fontsource/ibm-plex-mono/latin-500.css';
	import '../app.css';

	import { onMount } from 'svelte';
	import { page } from '$app/state';
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
	import { pageTitle } from '#lib/page-title.svelte.ts';

	let { children } = $props();

	onMount(() => {
		initAppearance();
		refreshSession();
		loadLoginLook();
	});

	$effect(() => {
		document.documentElement.lang = i18n.locale;
	});

	// Pages without a PageHead (the dashboard) are named by their place in the navigation.
	const ROUTE_TITLES: Record<string, MessageKey> = { '/': 'nav.dashboard', '/terminal': 'terminal.title' };
	const documentTitle = $derived.by(() => {
		const host = session.data?.hostname;
		const route = ROUTE_TITLES[page.url.pathname];
		const name = session.data?.user ? pageTitle.value || (route ? t(route) : '') : '';
		return [name, host, 'TitanOS'].filter(Boolean).join(' – ');
	});
</script>

<svelte:head>
	<title>{documentTitle}</title>
</svelte:head>

{#if page.url.pathname === '/' && session.data?.user}<DashboardBackground
		visual={dashboardScene.preview ?? dashboardVisual(session.dashboard?.visual)}
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
		font-size: 0.875rem;
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
