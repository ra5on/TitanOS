<script lang="ts">
	// Restores one whole folder of a Btrfs snapshot: to its original place
	// (with a conflict rule) or as a new folder next to it. The preview runs
	// on every change of the options and binds the start to the snapshot it saw.
	import Dialog from '#lib/components/Dialog.svelte';
	import { api } from '#lib/api.ts';
	import { formatBytes } from '#lib/format.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { operationError } from '#lib/operations.ts';
	import {
		folderRestoreRequest,
		needsReplaceConsent,
		type FolderConflict,
		type FolderRestoreJob,
		type FolderRestoreMode,
		type FolderRestorePreview,
	} from '#lib/snapshot-restore.ts';

	let {
		volume,
		snapshot,
		path,
		onclose,
		onstarted,
	}: {
		volume: string;
		snapshot: string;
		path: string;
		onclose: () => void;
		onstarted: (job: FolderRestoreJob) => void;
	} = $props();

	let mode = $state<FolderRestoreMode>('original'),
		conflict = $state<FolderConflict>('skip'),
		preview = $state<FolderRestorePreview | null>(null),
		checking = $state(false),
		starting = $state(false),
		consent = $state(false),
		error = $state('');
	let request = 0;
	const base = $derived(
		`/storage/volumes/${encodeURIComponent(volume)}/snapshots/${encodeURIComponent(snapshot)}/folders`,
	);

	async function check() {
		const mine = ++request;
		preview = null;
		consent = false;
		checking = true;
		error = '';
		try {
			const result = await api.post<FolderRestorePreview>(
				`${base}/preview`,
				folderRestoreRequest(path, mode, conflict),
			);
			if (mine === request) preview = result;
		} catch (e) {
			if (mine === request) error = operationError(e, t);
		} finally {
			if (mine === request) checking = false;
		}
	}
	$effect(() => {
		mode;
		conflict;
		check();
	});

	async function start() {
		if (!preview || starting || (needsReplaceConsent(preview) && !consent)) return;
		starting = true;
		error = '';
		try {
			const job = await api.post<FolderRestoreJob>(
				`${base}/restore`,
				folderRestoreRequest(path, mode, conflict, preview.uuid),
			);
			onstarted(job);
		} catch (e) {
			error = operationError(e, t);
		} finally {
			starting = false;
		}
	}
</script>

<Dialog title={t('snapshotRestore.title')} locked={starting} {onclose}>
	<p class="mono">{volume} / {snapshot} / {path}</p>
	<fieldset>
		<legend>{t('snapshotRestore.where')}</legend>
		<label class="check"
			><input type="radio" bind:group={mode} value="original" disabled={starting} />{t(
				'snapshotRestore.original',
			)}</label
		>
		<label class="check"
			><input type="radio" bind:group={mode} value="copy" disabled={starting} />{t(
				'snapshotRestore.copy',
			)}</label
		>
	</fieldset>
	{#if mode === 'original'}<fieldset>
			<legend>{t('files.conflict')}</legend>
			{#each ['skip', 'rename', 'replace'] as const as rule (rule)}<label class="check"
					><input type="radio" bind:group={conflict} value={rule} disabled={starting} />{t(
						`files.conflict.${rule}`,
					)}</label
				>{/each}
			<p class="muted">{t('snapshotRestore.conflictHint')}</p>
		</fieldset>{/if}
	{#if checking}<p class="muted" role="status">{t('snapshotRestore.checking')}</p>{/if}
	{#if preview}<div class="summary" role="status">
			<p>
				{t('snapshotRestore.amount', {
					files: preview.files,
					folders: preview.folders,
					size: formatBytes(preview.bytes),
				})}
			</p>
			{#if preview.links}<p>{t('snapshotRestore.links', { count: preview.links })}</p>{/if}
			{#if preview.special}<p class="muted">
					{t('snapshotRestore.special', { count: preview.special })}
				</p>{/if}
			<p>{t('snapshotRestore.target', { path: `${volume}/${preview.target}` })}</p>
			{#if preview.mode === 'original'}<p>
					{t('snapshotRestore.conflicts', { count: preview.conflicts })}
				</p>{/if}
			{#if preview.partial}<p class="notice-warn">{t('snapshotRestore.partial')}</p>{/if}
			<p class="muted">{t('snapshotRestore.preserves')}</p>
			{#if needsReplaceConsent(preview)}<p class="notice-warn">
					{t('snapshotRestore.replaceWarning', { count: preview.conflicts })}
				</p>
				<label class="check"
					><input type="checkbox" bind:checked={consent} disabled={starting} />{t(
						'snapshotRestore.replaceConsent',
					)}</label
				>{/if}
		</div>{/if}
	{#if error}<p class="notice-error" role="alert">
			{error}
			<button type="button" class="btn" disabled={checking} onclick={check}
				>{t('snapshotRestore.again')}</button
			>
		</p>{/if}
	<div class="dialog-actions">
		<button type="button" class="btn" disabled={starting} onclick={onclose}
			>{t('common.cancel')}</button
		><button
			type="button"
			class="btn"
			class:btn-primary={!needsReplaceConsent(preview)}
			class:btn-warn={needsReplaceConsent(preview)}
			disabled={!preview || checking || starting || (needsReplaceConsent(preview) && !consent)}
			onclick={start}>{t('snapshotRestore.start')}</button
		>
	</div>
</Dialog>

<style>
	fieldset {
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		padding: 10px 12px;
		margin: 0 0 12px;
		display: flex;
		flex-direction: column;
		gap: 6px;
	}
	legend {
		font-size: var(--text-base);
		padding: 0 4px;
	}
	.check {
		display: flex;
		align-items: center;
		gap: 8px;
		font-size: var(--text-md);
	}
	.summary p,
	fieldset p {
		margin: 0 0 6px;
		overflow-wrap: anywhere;
	}
	.mono {
		overflow-wrap: anywhere;
	}
</style>
