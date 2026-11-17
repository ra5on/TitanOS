<script lang="ts">
	// 1.05: the frame of every page of T-Mail (docs/TMAIL.md). Without the
	// account's app release the pages say so instead of asking titand (which
	// would refuse every request with mail_disabled).
	import type { Snippet } from 'svelte';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import { lockedSeconds } from '#lib/tmail/checklock.svelte.ts';
	import { loadAccounts, mail } from '#lib/tmail/store.svelte.ts';

	let { children }: { children: Snippet } = $props();

	const allowed = $derived(session.services?.mail === true);
	$effect(() => {
		if (allowed && mail.accounts === null) void loadAccounts();
	});
</script>

{#if !allowed}
	<EmptyState title={t('mail.disabled.title')} text={t('mail.disabled.text')} />
{:else if mail.accounts === null}
	<p class="muted" role="status">{t('common.loading')}</p>
{:else}
	{#if lockedSeconds() > 0}
		<!-- mail_rate_limited (any route): titand waits; the checks stay off meanwhile. -->
		<p class="notice-warn limit" role="status">
			{t('mail.limit.wait', { seconds: lockedSeconds() })}
		</p>
	{/if}
	{@render children()}
{/if}

<style>
	.limit {
		margin: 0 0 var(--space-2);
	}
</style>
