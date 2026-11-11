<script lang="ts">
	import type { Snippet } from 'svelte';
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { session } from '#lib/session.svelte.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { userSettingsPaths } from '#lib/settings-search.ts';
	import { loadSettingsStates } from '#lib/settings-states.svelte.ts';
	import { markAreaSeen } from '#lib/settings-seen.svelte.ts';
	let { children }: { children: Snippet } = $props();
	const admin = $derived(['owner', 'admin'].includes(session.data?.user?.role ?? ''));
	const allowed = $derived(
		admin || userSettingsPaths.includes(page.url.pathname.replace(/\/$/, '')),
	);
	// The states beside some areas are read once when the settings open, not
	// on every page change inside them and never on a timer.
	onMount(() => loadSettingsStates(admin));
	// Opening an area once takes its "new" badge away.
	$effect(() => markAreaSeen(page.url.pathname));
</script>

<!-- PLAN 61/65: the window (or the phone's bar) lists the settings pages;
     the page takes the whole width. PLAN 78-1: the pages are groups of rows
     (components/settings); the collapsible tiles of style E3
     (settings-groups.ts) remain for the pages of other programs only. -->
<div class="settings-shell">
	{#if allowed}<div class="settings-page column">
			{@render children()}
		</div>{:else}<p class="notice-error" role="alert">
			{t('error.forbidden')}
		</p>{/if}
</div>

<style>
	.settings-shell {
		display: flex;
		flex-direction: column;
		gap: var(--gap);
		min-width: 0;
	}
	.settings-shell :global(.settings-page.column) {
		max-width: none;
	}
	/* Settings are dense forms: buttons and fields a little smaller than
	   elsewhere, still following font size and density. Touch keeps 44 px. */
	@media (min-width: 761px) and (pointer: fine) {
		.settings-shell {
			--control-height: 1.875rem;
		}
		:global([data-density='compact']) .settings-shell {
			--control-height: 1.75rem;
		}
		.settings-shell :global(:is(.btn, .field)) {
			font-size: var(--text-base);
		}
		.settings-shell :global(.btn) {
			padding: 0 0.75rem;
		}
	}
</style>
