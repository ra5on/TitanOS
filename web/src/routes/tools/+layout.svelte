<script lang="ts">
	import type { Snippet } from 'svelte';
	import { page } from '$app/state';
	import { session } from '#lib/session.svelte.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	let { children }: { children: Snippet } = $props();
	const admin = $derived(['owner', 'admin'].includes(session.data?.user?.role ?? ''));
	// The task manager (PLAN 61) is the one tools page users see: its
	// overview, read only.
	const forUsers = $derived(page.url.pathname.replace(/\/$/, '') === '/tools/taskmanager');
</script>

<!-- PLAN 61/65: the window (or the phone's bar) lists the tools; the page
     takes the whole width. -->
{#if admin || (forUsers && session.data?.user)}<div class="settings-page column">
		{@render children()}
	</div>{:else}<p class="notice-error" role="alert">
		{t('error.forbidden')}
	</p>{/if}
