<script lang="ts">
	import { api, ApiError, type Account, type Share } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import { storage } from '#lib/storage.svelte.ts';
	import { system } from '#lib/system.svelte.ts';
	import ShareDialog from './ShareDialog.svelte';

	const canAdminister = $derived(session.data?.user?.role === 'owner' || session.data?.user?.role === 'admin');
	const volumes = $derived((storage.status?.volumes ?? []).map((volume) => volume.name));
	// How clients reach the NAS: its address works everywhere, its name only with a name service.
	const host = $derived(system.info?.network?.ipv4.split('/')[0] || system.info?.hostname || 'nas');

	let shares = $state<Share[] | null>(null);
	let accounts = $state<Account[]>([]);
	let error = $state('');
	let dialog = $state<{ kind: 'create' } | { kind: 'edit' | 'delete'; share: Share } | null>(null);
	let connect = $state('');
	let copied = $state('');

	async function load() {
		try {
			shares = await api.get<Share[]>('/shares');
			if (canAdminister) accounts = await api.get<Account[]>('/users');
		} catch (err) {
			error = errorText(err instanceof ApiError ? err.code : 'internal');
		}
	}
	$effect(() => {
		load();
	});

	function done() {
		dialog = null;
		load();
	}

	async function remove(share: Share) {
		dialog = null;
		error = '';
		try {
			await api.delete(`/shares/${share.name}`);
		} catch (err) {
			error = errorText(err instanceof ApiError ? err.code : 'internal');
		}
		await load();
	}

	function addresses(share: Share): { label: string; value: string }[] {
		const list: { label: string; value: string }[] = [];
		if (share.smb) {
			list.push(
				{ label: t('shares.windows'), value: `\\\\${host}\\${share.name}` },
				{ label: t('shares.mac'), value: `smb://${host}/${share.name}` },
				{ label: t('shares.linux'), value: `smb://${host}/${share.name}` }
			);
		}
		if (share.nfs.enabled) list.push({ label: t('shares.nfs'), value: `mount -t nfs ${host}:/${share.name} /mnt/${share.name}` });
		return list;
	}

	async function copy(value: string) {
		try {
			await navigator.clipboard.writeText(value);
			copied = value;
			setTimeout(() => (copied = ''), 1500);
		} catch {
			// No clipboard access (plain HTTP): the address can still be selected by hand.
		}
	}

	function accessSummary(share: Share): string {
		const names = Object.entries(share.access)
			.sort(([a], [b]) => a.localeCompare(b))
			.map(([name, level]) => `${name} (${t(`share.${level}`)})`);
		if (share.guest) names.push(t('shares.guest'));
		return names.join(', ') || t('shares.accessNone');
	}
</script>

<div class="head">
	<div class="title">
		<h1>{t('shares.title')}</h1>
		{#if shares}<p class="muted">{shares.length === 1 ? t('shares.summaryOne') : t('shares.summary', { count: shares.length })}</p>{/if}
	</div>
	{#if canAdminister}
		<button type="button" class="btn btn-primary" onclick={() => (dialog = { kind: 'create' })} disabled={volumes.length === 0}>{t('shares.new')}</button>
	{/if}
</div>

{#if error}<p class="notice-error" role="alert">{error}</p>{/if}

<div class="list">
	{#each shares ?? [] as share (share.name)}
		<article class="tile" aria-label={share.name}>
			<div class="top">
				<span class="mono name">{share.name}</span>
				{#if share.smb}<span class="chip">SMB</span>{/if}
				{#if share.nfs.enabled}<span class="chip">NFS</span>{/if}
				{#if share.recycle.enabled}<span class="chip">{t('shares.recycleChip', { days: share.recycle.days })}</span>{/if}
				{#if share.timeMachine.enabled}<span class="chip">{t('shares.timeMachineChip')}</span>{/if}
				{#if share.smb && !share.browseable}<span class="chip">{t('shares.hiddenChip')}</span>{/if}
				{#if share.guest}<span class="chip warn">{t('shares.guest')}</span>{/if}
				{#if !share.available}<span class="chip error">{t('shares.unavailable')}</span>{/if}
				<span class="mono path">{share.path}</span>
			</div>
			<div class="row"><span class="label">{t('shares.access')}</span><span>{accessSummary(share)}</span></div>
			<div class="buttons">
				<button type="button" class="btn" aria-expanded={connect === share.name} onclick={() => (connect = connect === share.name ? '' : share.name)}>{t('shares.connect')}</button>
				{#if canAdminister}
					<button type="button" class="btn" onclick={() => (dialog = { kind: 'edit', share })}>{t('shares.edit')}</button>
					<button type="button" class="btn delete" onclick={() => (dialog = { kind: 'delete', share })}>{t('shares.delete')}</button>
				{/if}
			</div>
			{#if connect === share.name}
				<div class="connect">
					{#each addresses(share) as address (address.label)}
						<div class="address">
							<span class="label">{address.label}</span>
							<code class="mono">{address.value}</code>
							<button type="button" class="btn" aria-label="{t('shares.copy')}: {address.label}" onclick={() => copy(address.value)}>
								<Icon name={copied === address.value ? 'check' : 'copy'} size={16} />{copied === address.value ? t('shares.copied') : t('shares.copy')}
							</button>
						</div>
					{/each}
				</div>
			{/if}
		</article>
	{:else}
		{#if shares}
			<div class="tile empty">
				<p>{t('shares.empty')}</p>
				<p class="muted">{volumes.length === 0 ? t('shares.needVolume') : t('shares.emptyHint')}</p>
			</div>
		{/if}
	{/each}
</div>

{#if dialog?.kind === 'create'}
	<ShareDialog {volumes} {accounts} onclose={() => (dialog = null)} ondone={done} />
{:else if dialog?.kind === 'edit'}
	<ShareDialog share={dialog.share} {volumes} {accounts} onclose={() => (dialog = null)} ondone={done} />
{:else if dialog?.kind === 'delete'}
	{@const share = dialog.share}
	<Dialog title={t('share.deleteTitle', { name: share.name })} onclose={() => (dialog = null)}>
		<p class="text">{t('share.deleteText', { path: share.path })}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (dialog = null)}>{t('common.cancel')}</button>
			<button type="button" class="btn btn-warn" onclick={() => remove(share)}>{t('share.deleteSubmit')}</button>
		</div>
	</Dialog>
{/if}

<style>
	.head {
		display: flex;
		flex-wrap: wrap;
		align-items: flex-end;
		justify-content: space-between;
		gap: 12px;
	}
	.title {
		display: flex;
		flex-direction: column;
		gap: 4px;
		font-size: 0.875rem;
	}
	h1 {
		font-size: 1.5rem;
		font-weight: 600;
	}
	.list {
		display: flex;
		flex-direction: column;
		gap: var(--gap);
	}
	article {
		display: flex;
		flex-direction: column;
		gap: 14px;
		padding: 20px 22px;
	}
	.top {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 8px 10px;
	}
	.name {
		font-size: 1.125rem;
		font-weight: 500;
	}
	.path {
		margin-left: auto;
		font-size: 0.8125rem;
		color: var(--muted);
	}
	.row {
		display: flex;
		flex-wrap: wrap;
		gap: 4px 16px;
		font-size: 0.875rem;
	}
	.label {
		min-width: 5rem;
		font-size: 0.75rem;
		color: var(--muted);
		align-self: center;
	}
	.buttons {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
	}
	.buttons .btn {
		height: 2.5rem;
	}
	.delete {
		margin-left: auto;
	}
	.connect {
		display: flex;
		flex-direction: column;
		gap: 8px;
		padding-top: 14px;
		border-top: 1px solid var(--panel-border);
	}
	.address {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 8px 12px;
		font-size: 0.875rem;
	}
	code {
		flex: 1 1 260px;
		min-width: 0;
		padding: 8px 12px;
		background: var(--chip);
		border-radius: var(--radius-ctl);
		font-size: 0.8125rem;
		overflow-wrap: anywhere;
		user-select: all;
	}
	.address .btn {
		height: 2.25rem;
	}
	.empty {
		padding: 28px 22px;
		display: flex;
		flex-direction: column;
		gap: 4px;
		font-size: 0.875rem;
	}
	.text {
		font-size: 0.875rem;
	}
</style>
