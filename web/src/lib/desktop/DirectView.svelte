<script lang="ts">
	// PLAN 75-14: an app with its own desktop stream in its window, under its
	// own HTTPS name (like Kasm Workspaces). The enter page of the app proxy
	// says by postMessage that the browser reached the app; no word in time,
	// or no name the browser can reach (IP address, tunnel), and the window
	// takes the remote view (onfallback).
	import { onDestroy, onMount, type Snippet } from 'svelte';
	import { api } from '../api.ts';
	import { t } from '../i18n/index.svelte.ts';
	import { DIRECT_WAIT_MS, isDirectHello } from './direct.ts';

	let {
		app,
		title,
		onfallback,
		splash,
	}: {
		app: string;
		title: string;
		onfallback: () => void;
		splash?: Snippet;
	} = $props();

	let src = $state('');
	let ready = $state(false);
	let origin = '';
	let timer: ReturnType<typeof setTimeout> | undefined;

	function hello(event: MessageEvent) {
		if (!origin || event.origin !== origin || !isDirectHello(event.data)) return;
		ready = true;
		clearTimeout(timer);
	}

	onMount(() => {
		window.addEventListener('message', hello);
		api
			.post<{ url: string }>(`/apps/${encodeURIComponent(app)}/window?direct=1`)
			.then((res) => {
				const url = new URL(res.url);
				if (url.protocol !== 'https:') throw new Error('not https');
				origin = url.origin;
				src = url.href;
				timer = setTimeout(() => {
					if (!ready) onfallback();
				}, DIRECT_WAIT_MS);
			})
			.catch(() => onfallback());
	});
	onDestroy(() => {
		window.removeEventListener('message', hello);
		clearTimeout(timer);
	});
</script>

<div class="direct">
	{#if src}
		<iframe
			{src}
			{title}
			class:hidden={!ready}
			referrerpolicy="no-referrer"
			allow="clipboard-read; clipboard-write; fullscreen; autoplay; microphone"
		></iframe>
	{/if}
	{#if !ready}
		<div class="splash" role="status" aria-live="polite">
			{@render splash?.()}
			<span class="muted">{t('desktop.direct.starting')}</span>
		</div>
	{/if}
</div>

<style>
	.direct {
		position: absolute;
		inset: 0;
		background: var(--bg);
	}
	iframe {
		width: 100%;
		height: 100%;
		border: 0;
		display: block;
	}
	iframe.hidden {
		visibility: hidden;
	}
	.splash {
		position: absolute;
		inset: 0;
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: var(--space-3);
	}
</style>
