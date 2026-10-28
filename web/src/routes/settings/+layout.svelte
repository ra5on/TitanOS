<script lang="ts">
	import type { Snippet } from 'svelte';
	import { page } from '$app/state';
	import { session } from '#lib/session.svelte.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { userSettingsPaths } from '#lib/settings-search.ts';
	let { children }: { children: Snippet } = $props();
	const allowed = $derived(['owner', 'admin'].includes(session.data?.user?.role ?? '') || userSettingsPaths.includes(page.url.pathname.replace(/\/$/, '')));
	// The overview spreads its tiles over the whole width; every page behind it
	// is one readable column of sections (see .settings-page in app.css).
	const overview = $derived(page.url.pathname.replace(/\/$/, '') === '/settings');
</script>
{#if allowed}<div class="settings-page" class:column={!overview}>{@render children()}</div>{:else}<p class="notice-error" role="alert">{t('error.forbidden')}</p>{/if}
