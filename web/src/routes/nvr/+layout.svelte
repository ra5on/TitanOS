<script lang="ts">
	// PLAN 79-7: the frame of every page of the NVR. It follows the NVR's
	// state, shows the setup while the NVR is switched off, the privacy notice
	// until it is dismissed and what stops the NVR. The window's side bar (or
	// the phone's bar) lists the pages.
	import type { Snippet } from 'svelte';
	import { page } from '$app/state';
	import { api } from '#lib/api.ts';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { nvr, nvrAdmin, refreshNvr, watchNvr } from '#lib/nvr/nvr.svelte.ts';
	import StateBanner from '#lib/nvr/StateBanner.svelte';
	import Setup from './Setup.svelte';

	let { children }: { children: Snippet } = $props();

	$effect(() => watchNvr());

	const admin = $derived(nvrAdmin());
	const path = $derived(page.url.pathname.replace(/\/$/, ''));
	const adminPage = $derived(/^\/nvr\/(cameras|settings|diagnosis)$/.test(path));
	const live = $derived(path === '/nvr');
	// "No NVR on this NAS" only when it is not there at all: a normal account
	// also gets "unavailable" while a switched-on NVR is down (stopped,
	// failed, Docker starting after a boot) – then the page and its banner
	// say that it does not run right now.
	const status = $derived(nvr.status);

	// The privacy notice: administrators dismiss it for the NAS (settings),
	// other accounts for this browser.
	const SEEN = 'titanos.nvr.privacy';
	let seenHere = $state(false);
	$effect(() => {
		try {
			seenHere = localStorage.getItem(SEEN) === '1';
		} catch {
			/* private window */
		}
	});
	const showPrivacy = $derived(
		!!status?.enabled && (admin ? !status.privacyNoticeDismissed : !seenHere),
	);
	let privacyBusy = $state(false);
	async function dismissPrivacy() {
		if (!admin) {
			seenHere = true;
			try {
				localStorage.setItem(SEEN, '1');
			} catch {
				/* only for this visit */
			}
			return;
		}
		privacyBusy = true;
		try {
			await api.put('/nvr/settings', { privacyNoticeDismissed: true });
			await refreshNvr();
		} catch {
			// The notice stays; the settings page shows why saving fails.
		} finally {
			privacyBusy = false;
		}
	}
</script>

{#if !nvr.loaded}
	<p class="muted" role="status">{t('common.loading')}</p>
{:else if !status || (status.state === 'unavailable' && !status.enabled)}
	<p class="notice-error" role="alert">{nvr.error || t('error.nvr_unavailable')}</p>
{:else if adminPage && !admin}
	<p class="notice-error" role="alert">{t('error.forbidden')}</p>
{:else if !status.enabled && path !== '/nvr/diagnosis'}
	{#if admin}
		<div class="settings-page column"><Setup /></div>
	{:else}
		<EmptyState title={t('nvr.off.title')} text={t('nvr.off.user')} />
	{/if}
{:else}
	<div class="nvr-page" class:live class:settings-page={!live} class:column={!live}>
		{#if showPrivacy}
			<div class="privacy" role="note" id="nvr-privacy">
				<div>
					<strong>{t('nvr.privacy.title')}</strong>
					<span>{t('nvr.privacy.text')}</span>
				</div>
				<button type="button" class="btn" disabled={privacyBusy} onclick={dismissPrivacy}
					>{t('nvr.privacy.dismiss')}</button
				>
			</div>
		{/if}
		{#if path !== '/nvr/diagnosis'}<StateBanner compact={live} />{/if}
		{@render children()}
	</div>
{/if}

<style>
	.nvr-page {
		display: flex;
		flex-direction: column;
		gap: var(--gap);
		min-width: 0;
	}
	.nvr-page.live {
		gap: var(--space-2-5);
	}
	.privacy {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2) var(--space-3);
		padding: var(--space-2-5) var(--space-4);
		border: 1px solid var(--border);
		border-radius: 10px;
		background: var(--surface);
		font-size: var(--text-base);
	}
	.privacy div {
		display: flex;
		flex-direction: column;
		gap: 2px;
		flex: 1 1 18rem;
		min-width: 0;
		overflow-wrap: anywhere;
	}
	.privacy span {
		color: var(--muted);
	}
</style>
