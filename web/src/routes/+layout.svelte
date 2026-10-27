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
	import { i18n, t } from '#lib/i18n/index.svelte.ts';
	import Login from '#lib/pages/Login.svelte';
	import Problem from '#lib/pages/Problem.svelte';
	import Setup from '#lib/pages/Setup.svelte';
	import { refreshSession, session } from '#lib/session.svelte.ts';
	import { appearance, initAppearance } from '#lib/theme/appearance.svelte.ts';

	let { children } = $props();

	onMount(() => {
		initAppearance();
		refreshSession();
	});

	$effect(() => {
		document.documentElement.lang = i18n.locale;
	});
</script>

<svelte:head>
	<title>{session.data?.hostname ? `${session.data.hostname} – TitanOS` : 'TitanOS'}</title>
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
	{:else}
		<Shell>{@render children()}</Shell>
	{/if}
{/if}
