<script lang="ts">
	import type { Snippet } from 'svelte';
	import { page } from '$app/state';
	import { session } from '#lib/session.svelte.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { userSettingsPaths } from '#lib/settings-search.ts';
	let { children }: { children: Snippet } = $props();
	const allowed = $derived(['owner', 'admin'].includes(session.data?.user?.role ?? '') || userSettingsPaths.includes(page.url.pathname.replace(/\/$/, '')));
</script>
{#if allowed}{@render children()}{:else}<p class="notice-error" role="alert">{t('error.forbidden')}</p>{/if}
