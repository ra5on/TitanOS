<script lang="ts">
	// Deleting a share: only the share, or with its folder and data after
	// counting what is in it and what still uses it.
	import { api, ApiError, type Share } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { formatDiskBytes } from '#lib/format.ts';
	import { apiErrorText, i18n, t } from '#lib/i18n/index.svelte.ts';
	import {
		canDeleteData,
		usageKey,
		type ShareDataPreview,
		type ShareDeletion,
	} from '#lib/share-delete.ts';

	type Props = {
		share: Share;
		onclose: () => void;
		/** After the share is gone; `purged` when its data is being deleted too. */
		ondeleted: (purged: boolean) => Promise<void>;
	};
	let { share, onclose, ondeleted }: Props = $props();

	let deleting = $state(false),
		deleteData = $state(false),
		dataPreview = $state<ShareDataPreview | null>(null),
		previewing = $state(false),
		previewError = $state(''),
		deleteError = $state('');

	// Counting a large folder takes a while; the dialog stays usable meanwhile.
	let previewFor = 0;
	async function loadPreview() {
		const ticket = ++previewFor;
		previewing = true;
		previewError = '';
		dataPreview = null;
		try {
			const preview = await api.get<ShareDataPreview>(
				`/shares/${encodeURIComponent(share.name)}/delete-data`,
			);
			if (ticket === previewFor) dataPreview = preview;
		} catch (err) {
			if (ticket === previewFor) previewError = apiErrorText(err);
		} finally {
			if (ticket === previewFor) previewing = false;
		}
	}
	function toggleData() {
		if (deleteData) void loadPreview();
		else {
			previewFor++;
			previewing = false;
			dataPreview = null;
			previewError = '';
		}
	}
	async function remove() {
		deleting = true;
		deleteError = '';
		try {
			if (deleteData) {
				if (!dataPreview) return;
				await api.post<ShareDeletion>(`/shares/${encodeURIComponent(share.name)}/delete-data`, {
					path: dataPreview.path,
				});
			} else await api.delete(`/shares/${encodeURIComponent(share.name)}`);
			await ondeleted(deleteData);
		} catch (err) {
			deleteError = apiErrorText(err);
			// What stands in the way may have changed: show the current state.
			if (deleteData && err instanceof ApiError && err.status === 409) void loadPreview();
		} finally {
			deleting = false;
		}
	}
</script>

<Dialog title={t('share.deleteTitle', { name: share.name })} locked={deleting} {onclose}>
	{#if !deleteData}<p>{t('share.deleteText', { path: share.path })}</p>{/if}
	<label class="check"
		><input
			type="checkbox"
			bind:checked={deleteData}
			disabled={deleting}
			onchange={toggleData}
		/><span>{t('share.deleteData')}</span></label
	>
	{#if deleteData}
		<div class="purge" aria-live="polite">
			<p class="notice-warn">{t('share.deleteDataHint', { path: share.path })}</p>
			{#if previewing}<p class="muted">{t('share.deleteDataCounting')}</p>
			{:else if previewError}<p class="notice-error" role="alert">{previewError}</p>
			{:else if dataPreview}
				<p class="mono path">{dataPreview.path}</p>
				{#if !dataPreview.exists}<p>{t('share.deleteDataMissing', { path: dataPreview.path })}</p>
				{:else}<p>
						{t('share.deleteDataSummary', {
							files: dataPreview.files.toLocaleString(i18n.locale),
							dirs: dataPreview.dirs.toLocaleString(i18n.locale),
							size: formatDiskBytes(dataPreview.bytes),
						})}
					</p>
					{#if !dataPreview.complete}<p class="muted">
							{t('share.deleteDataPartial', { limit: dataPreview.limit ?? '' })}
						</p>{/if}
					{#if dataPreview.subvolume}<p>{t('share.deleteDataSubvolume')}</p>{/if}
				{/if}
				{#if dataPreview.blockers.length}
					<div class="notice-error" role="alert">
						<p>{t('share.deleteDataBlocked')}</p>
						<ul>
							{#each dataPreview.blockers as use, i (i)}<li>
									{t(usageKey(use.kind), { name: use.name ?? '', path: use.path ?? '' })}
								</li>{/each}
						</ul>
					</div>
				{/if}
				{#if dataPreview.warnings.length}
					<div class="notice-warn">
						<p>{t('share.deleteDataWarnings')}</p>
						<ul>
							{#each dataPreview.warnings as use, i (i)}<li>
									{t(usageKey(use.kind), { name: use.name ?? '', path: use.path ?? '' })}
								</li>{/each}
						</ul>
					</div>
				{/if}
			{/if}
			{#if !previewing && (previewError || dataPreview?.blockers.length)}<button
					class="btn"
					disabled={deleting}
					onclick={loadPreview}>{t('share.deleteDataRetry')}</button
				>{/if}
		</div>
	{/if}
	{#if deleteError}<p class="notice-error" role="alert">{deleteError}</p>{/if}
	<div class="dialog-actions">
		<button class="btn" disabled={deleting} onclick={onclose}>{t('common.cancel')}</button><button
			class="btn btn-warn"
			disabled={deleting || (deleteData && (previewing || !canDeleteData(dataPreview)))}
			onclick={remove}>{t(deleteData ? 'share.deleteDataSubmit' : 'share.deleteSubmit')}</button
		>
	</div>
</Dialog>

<style>
	.check {
		display: flex;
		gap: 8px;
		align-items: center;
		margin-top: 8px;
	}
	.purge {
		display: grid;
		gap: 8px;
		margin-top: 8px;
	}
	.purge p,
	.purge ul {
		margin: 0;
	}
	.purge ul {
		padding-left: 18px;
	}
	.purge .path {
		overflow-wrap: anywhere;
	}
	.purge .btn {
		justify-self: start;
	}
	.notice-error {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
		align-items: center;
	}
</style>
