<script lang="ts">
	import { untrack } from 'svelte';
	import { api, type FileRoot } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { formatBytes } from '#lib/format.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';

	type Props = {
		/** A new folder, renaming the one selected entry, or deleting the selection. */
		dialog: { kind: 'mkdir' } | { kind: 'rename'; name: string } | { kind: 'delete' };
		root: FileRoot | undefined;
		rootId: string;
		dir: string;
		writable: boolean;
		selected: string[];
		/** Selected files and folders, for the summary of a deletion. */
		files: number;
		folders: number;
		bytes: number;
		/** Folders whose size is not in `bytes`; 0 once measured. */
		uncounted: number;
		busy: boolean;
		onbusy: (busy: boolean) => void;
		onclose: () => void;
		/** After the change; the page closes the dialog and loads the folder again. */
		ondone: () => Promise<void>;
	};
	let {
		dialog,
		root,
		rootId,
		dir,
		writable,
		selected,
		files,
		folders,
		bytes,
		uncounted,
		busy,
		onbusy,
		onclose,
		ondone,
	}: Props = $props();

	const join = (...parts: string[]) => parts.filter(Boolean).join('/');
	const kind = $derived(dialog.kind);
	// The page opens the dialog anew for each use, so these start from what it got.
	let typed = $state(untrack(() => (dialog.kind === 'rename' ? dialog.name : '')));
	let confirmed = $state(false);
	let permanent = $state(untrack(() => !root?.recycle));
	let error = $state('');

	// Enter submits only "new folder" and "rename"; deleting runs from its button alone.
	async function submit(event?: SubmitEvent) {
		event?.preventDefault();
		if (!writable || busy) return;
		if (event && dialog.kind === 'delete') return;
		error = '';
		onbusy(true);
		try {
			if (dialog.kind === 'mkdir') {
				await api.post('/files/mkdir', { root: rootId, path: join(dir, typed.trim()) });
			} else if (dialog.kind === 'rename') {
				await api.post('/files/rename', {
					root: rootId,
					from: join(dir, dialog.name),
					to: join(dir, typed.trim()),
				});
			} else {
				if (permanent && !confirmed) return;
				await api.post('/files/delete', {
					root: rootId,
					paths: selected.map((name) => join(dir, name)),
					permanent,
				});
			}
			await ondone();
		} catch (err) {
			error = apiErrorText(err);
		} finally {
			onbusy(false);
		}
	}
</script>

<Dialog
	title={dialog.kind === 'mkdir'
		? t('files.newFolderTitle')
		: dialog.kind === 'rename'
			? t('files.renameTitle', { name: dialog.name })
			: selected.length === 1
				? t('files.deleteTitleOne', { name: selected[0] })
				: t('files.deleteTitleMany', { count: selected.length })}
	locked={busy}
	{onclose}
>
	<form onsubmit={submit}>
		{#if kind === 'delete'}
			<div class="delete-summary">
				<p>
					{t('files.deleteSummary', { files, folders })} ·
					<span class="mono">{formatBytes(bytes)}</span>{#if uncounted}
						<span class="muted">({t('files.foldersNotCounted', { count: uncounted })})</span>{/if}
				</p>
				{#if selected.length > 1}
					<ul class="mono">
						{#each selected.slice(0, 10) as name (name)}<li>{name}</li>{/each}
					</ul>
					{#if selected.length > 10}<p class="muted">
							{t('files.deleteMore', { count: selected.length - 10 })}
						</p>{/if}
				{/if}
			</div>
			{#if root?.recycle}
				<label class="field-row"
					><span>{t('files.deleteAction')}</span>
					<select
						class="field"
						bind:value={permanent}
						disabled={busy}
						onchange={() => {
							confirmed = false;
						}}
					>
						<option value={false}>{t('files.moveToTrash')}</option>
						<option value={true}>{t('files.trashDeleteForever')}</option>
					</select>
				</label>
			{/if}
			<p class={permanent ? 'notice-error' : 'text'}>
				{t(
					permanent
						? root?.recycle
							? 'files.trashForeverText'
							: 'files.deletePermanent'
						: 'files.deleteRecycle',
				)}
			</p>
			{#if permanent}
				<label class="confirm"
					><input type="checkbox" bind:checked={confirmed} disabled={busy} /><span
						>{t('files.deleteConfirm')}</span
					></label
				>
			{/if}
		{:else}
			<label class="field-row">
				<span>{t(kind === 'mkdir' ? 'files.name' : 'files.newName')}</span>
				<!-- svelte-ignore a11y_autofocus -->
				<input
					class="field"
					type="text"
					bind:value={typed}
					maxlength="255"
					autocomplete="off"
					spellcheck="false"
					required
					autofocus
					readonly={busy}
				/>
			</label>
		{/if}
		{#if error}<p class="notice-warn" role="alert">{error}</p>{/if}
		<div class="dialog-actions">
			{#if busy}<span class="dialog-busy" role="status">{t('files.dialogBusy')}</span>{/if}
			<button type="button" class="btn" onclick={onclose} disabled={busy}
				>{t('common.cancel')}</button
			>
			{#if kind === 'delete'}
				<button
					type="button"
					class="btn {permanent ? 'btn-danger' : 'btn-warn'}"
					disabled={(permanent && !confirmed) || busy}
					onclick={() => submit()}
					>{t(permanent ? 'files.trashDeleteForever' : 'files.moveToTrash')}</button
				>
			{:else}
				<button type="submit" class="btn btn-primary" disabled={busy}
					>{t(kind === 'mkdir' ? 'files.create' : 'files.rename')}</button
				>
			{/if}
		</div>
	</form>
</Dialog>

<style>
	.delete-summary {
		display: flex;
		flex-direction: column;
		gap: 6px;
		font-size: 0.875rem;
	}
	.delete-summary ul {
		margin: 0;
		padding-left: 18px;
		font-size: 0.8125rem;
		max-height: 14rem;
		overflow-y: auto;
		overflow-wrap: anywhere;
	}
	.dialog-busy {
		order: -2;
		margin-right: auto;
		align-self: center;
		font-size: 0.8125rem;
		color: var(--muted);
	}
	form {
		display: flex;
		flex-direction: column;
		gap: 16px;
	}
	.field-row {
		display: flex;
		flex-direction: column;
		gap: 6px;
		font-size: 0.875rem;
	}
	.text {
		font-size: 0.875rem;
	}
</style>
