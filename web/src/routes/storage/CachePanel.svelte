<script lang="ts">
	// SSD cache of an mdadm volume: off unless switched on; read cache or
	// read and write cache. Takes effect with the next start.
	import { api, type CacheMode, type Disk, type Volume } from '#lib/api.ts';
	import HelpTip from '#lib/components/HelpTip.svelte';
	import Dialog from '#lib/components/Dialog.svelte';
	import { formatDiskBytes } from '#lib/format.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import { refreshStorage, storage } from '#lib/storage.svelte.ts';
	import DiskPicker from './DiskPicker.svelte';
	import { isUsable } from './labels.ts';

	type Props = { volume: Volume };
	let { volume }: Props = $props();

	let dialog = $state<'enable' | 'mode' | 'disable' | null>(null);
	let mode = $state<CacheMode>('writethrough');
	let selected = $state<string[]>([]);
	let identity = $state<string | undefined>();
	let confirmed = $state(false);
	let busy = $state(false);
	let error = $state('');

	const cache = $derived(volume.cache);
	// SSDs first: a cache on a spinning disk gains nothing.
	const disks = $derived(
		(storage.status?.disks ?? []).filter((disk: Disk) => isUsable(disk) && disk.kind !== 'hdd'),
	);

	function open(kind: 'enable' | 'mode' | 'disable') {
		error = '';
		confirmed = false;
		selected = [];
		mode =
			kind === 'mode' && cache
				? cache.mode === 'writeback'
					? 'writethrough'
					: 'writeback'
				: 'writethrough';
		dialog = kind;
	}

	async function submit() {
		if (busy) return;
		busy = true;
		error = '';
		try {
			if (dialog === 'enable')
				await api.put(`/storage/volumes/${volume.name}/cache`, {
					disk: selected[0],
					mode,
					erase: true,
					identity,
				});
			else if (dialog === 'mode') await api.put(`/storage/volumes/${volume.name}/cache`, { mode });
			else await api.delete(`/storage/volumes/${volume.name}/cache`);
			dialog = null;
			refreshStorage();
		} catch (err) {
			error = apiErrorText(err);
		}
		busy = false;
	}
</script>

<div class="cache">
	<span class="label">{t('cache.title')}<HelpTip term="cache" /></span>
	{#if !cache}
		<span class="muted">{t('cache.off')}</span>
		<button type="button" class="btn small" onclick={() => open('enable')}
			>{t('cache.enable')}</button
		>
	{:else}
		<span>
			{t(cache.mode === 'writeback' ? 'cache.writeback' : 'cache.writethrough')} ·
			<span class="mono">{cache.disk}</span>
			{#if cache.removing}· {t('cache.removing', { dirty: cache.dirtyBlocks })}
			{:else if cache.missing}· <span class="error">{t('cache.missing')}</span>
			{:else if !cache.active}· {t('cache.pending')}
			{:else}· {t('cache.usage', {
					used: formatDiskBytes(cache.usedBlocks * cache.blockBytes),
					total: formatDiskBytes(cache.totalBlocks * cache.blockBytes),
				})}{#if cache.readHitRate >= 0}
					· {t('cache.hits', {
						percent: Math.round(cache.readHitRate * 100),
					})}{/if}{#if cache.mode === 'writeback'}
					· {t('cache.dirty', { count: cache.dirtyBlocks })}{/if}
			{/if}
		</span>
		{#if !cache.removing}
			<span class="buttons">
				<button type="button" class="btn small" onclick={() => open('mode')}
					>{t('cache.changeMode')}</button
				>
				<button type="button" class="btn small" onclick={() => open('disable')}
					>{t('cache.disable')}</button
				>
			</span>
		{/if}
	{/if}
</div>

{#if dialog}
	<Dialog
		title={t(`cache.${dialog}Title`, { volume: volume.name })}
		wide={dialog === 'enable'}
		locked={busy}
		onclose={() => (dialog = null)}
	>
		<div class="body">
			{#if dialog === 'disable'}
				<p>
					{t(
						cache?.mode === 'writeback' && cache.active
							? 'cache.disableWriteback'
							: 'cache.disableText',
					)}
				</p>
			{:else}
				{#if dialog === 'enable'}
					<p class="muted">{t('cache.intro')}</p>
					<DiskPicker
						{disks}
						bind:selected
						single
						name="cache-disk"
						onselect={(disk, checked) => (identity = checked ? disk.identity : undefined)}
					/>
				{/if}
				<div class="modes" role="radiogroup" aria-label={t('cache.mode')}>
					<label class="option"
						><input type="radio" name="cache-mode" value="writethrough" bind:group={mode} /><span
							><strong>{t('cache.writethrough')}</strong><small class="muted"
								>{t('cache.writethroughDesc')}</small
							></span
						></label
					>
					<label class="option"
						><input type="radio" name="cache-mode" value="writeback" bind:group={mode} /><span
							><strong>{t('cache.writeback')}</strong><small class="muted"
								>{t('cache.writebackDesc')}</small
							></span
						></label
					>
				</div>
				{#if mode === 'writeback'}<p class="notice-warn" role="alert">
						{t('cache.writebackWarning')}
					</p>{/if}
				{#if dialog === 'enable'}
					<p class="muted">{t('cache.afterRestart')}</p>
					{#if selected.length === 1}
						<label class="confirm"
							><input type="checkbox" bind:checked={confirmed} /><span
								>{t('addDisk.eraseConfirm', { disk: selected[0] })}</span
							></label
						>
					{/if}
				{/if}
			{/if}
			{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
			<div class="dialog-actions">
				<button type="button" class="btn" disabled={busy} onclick={() => (dialog = null)}
					>{t('common.cancel')}</button
				>
				<button
					type="button"
					class="btn {dialog === 'enable' ? 'btn-warn' : 'btn-primary'}"
					disabled={busy || (dialog === 'enable' && (selected.length !== 1 || !confirmed))}
					onclick={submit}>{t(`cache.${dialog}Submit`)}</button
				>
			</div>
		</div>
	</Dialog>
{/if}

<style>
	.cache {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 6px 12px;
		margin-top: 12px;
		font-size: var(--text-md);
	}
	.label {
		font-weight: 600;
	}
	.buttons {
		display: flex;
		gap: 6px;
		margin-left: auto;
	}
	.error {
		color: var(--status-error);
	}
	.body {
		display: flex;
		flex-direction: column;
		gap: 14px;
	}
	p {
		margin: 0;
		font-size: var(--text-md);
	}
	.modes {
		display: grid;
		gap: 8px;
	}
	.option {
		display: flex;
		gap: 10px;
		align-items: flex-start;
		padding: 10px 12px;
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		cursor: pointer;
	}
	.option small {
		display: block;
	}
	.confirm {
		display: flex;
		gap: 8px;
		align-items: flex-start;
	}
</style>
