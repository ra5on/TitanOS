<script lang="ts">
	// The addresses of a share for each system, to copy.
	import type { Share } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { shareAddresses } from './shares-page.ts';

	type Props = {
		share: Share;
		host: string;
		/** The .local name, when the NAS announces one; shown as a second way. */
		mdnsHost: string;
		onclose: () => void;
	};
	let { share, host, mdnsHost, onclose }: Props = $props();

	let copied = $state(''),
		copyFailed = $state('');
	let disposed = false;
	$effect(() => () => {
		disposed = true;
	});

	async function copy(value: string, code: Element | null) {
		copyFailed = '';
		try {
			await navigator.clipboard.writeText(value);
			copied = value;
			setTimeout(() => {
				if (!disposed) copied = '';
			}, 1500);
		} catch {
			// No clipboard (plain http, refused): select the address for copying by hand.
			copied = '';
			copyFailed = value;
			if (code) getSelection()?.selectAllChildren(code);
		}
	}
</script>

<Dialog title={t('shares.connectTitle', { name: share.name })} {onclose}>
	{#each [host, mdnsHost].filter(Boolean) as via, index (via)}
		{#if mdnsHost}<h3 class="via">
				{t(index === 0 ? 'shares.viaAddress' : 'shares.viaName')}
			</h3>{/if}
		{#each shareAddresses(share, via) as address (address.label)}<div class="address">
				<span>{address.label}</span><code class="mono">{address.value}</code><button
					class="btn"
					onclick={(event) => copy(address.value, event.currentTarget.previousElementSibling)}
					>{t(copied === address.value ? 'shares.copied' : 'shares.copy')}</button
				>
				{#if copyFailed === address.value}<p class="notice-warn copy-failed" role="alert">
						{t('shares.copyFailed')}
					</p>{/if}
			</div>{/each}
	{/each}
	{#if !share.smb && !share.nfs.enabled}<p>{t('shares.noProtocol')}</p>{/if}
	<div class="dialog-actions">
		<button class="btn" onclick={onclose}>{t('common.done')}</button>
	</div>
</Dialog>

<style>
	.address {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
		align-items: center;
		font-size: 0.8125rem;
	}
	.address > span,
	.copy-failed {
		width: 100%;
	}
	.via {
		margin: 4px 0 0;
		font-size: 0.8125rem;
		font-weight: 600;
	}
	.address code {
		flex: 1;
		min-width: 0;
		overflow-wrap: anywhere;
		padding: 10px;
		background: var(--chip);
		border-radius: var(--radius-ctl);
		user-select: all;
	}
</style>
