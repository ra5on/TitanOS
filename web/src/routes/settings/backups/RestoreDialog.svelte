<script lang="ts">
	// Restore a saved state of a backup job onto a volume: everything, or
	// chosen files. The NAS answers with the folder it restores into.
	import { untrack } from 'svelte';
	import { api } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { formatBytes, formatDateTime } from '#lib/format.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import SnapshotFiles from './SnapshotFiles.svelte';

	type Snapshot = { id: string; time: string; paths: string[]; bytes: number };

	let {
		job,
		volumes,
		onclose,
		onfailed,
		onstarted,
	}: {
		job: string;
		volumes: string[];
		onclose: () => void;
		/** The saved states could not be read; the page shows why and closes the dialog. */
		onfailed: (err: unknown) => void;
		onstarted: () => void;
	} = $props();

	let snapshots = $state<Snapshot[] | null>(null);
	let snapshot = $state('');
	let volume = $state('');
	let target = $state('');
	let mode = $state<'all' | 'files'>('all');
	let paths = $state<string[]>([]);
	let busy = $state(false);
	let error = $state('');

	// Each job opened has its number; a list of saved states that arrives
	// late belongs to an earlier one and is dropped.
	let request = 0;
	$effect(() => {
		const id = job;
		const mine = ++request;
		snapshots = null;
		snapshot = '';
		// The volume list refreshes while the dialog is open; only the opening picks one.
		volume = untrack(() => volumes[0] ?? '');
		target = '';
		mode = 'all';
		paths = [];
		error = '';
		api.get<{ snapshots: Snapshot[] }>(`/backups/${id}/snapshots`).then(
			(res) => {
				if (mine !== request) return;
				snapshots = res.snapshots;
				snapshot = res.snapshots[0]?.id ?? '';
			},
			(err) => {
				if (mine === request) onfailed(err);
			},
		);
		return () => request++;
	});

	async function restore(event: SubmitEvent) {
		event.preventDefault();
		if (busy) return;
		busy = true;
		error = '';
		try {
			const res = await api.post<{ target: string }>(`/backups/${job}/restore`, {
				snapshot,
				volume,
				paths: mode === 'files' ? paths : undefined,
			});
			target = res.target;
			onstarted();
		} catch (err) {
			// The dialog stays with what was chosen; the reason is shown in it.
			error = apiErrorText(err);
		}
		busy = false;
	}
</script>

<Dialog title={t('backup.restoreTitle', { name: job })} locked={busy} {onclose}>
	{#if target}
		<p>{t('backup.restoreStarted')}</p>
		<p class="mono">{target}</p>
		<p class="muted">{t('backup.restoreWhere')}</p>
		<div class="dialog-actions">
			<button type="button" class="btn btn-primary" onclick={onclose}>{t('common.done')}</button>
		</div>
	{:else if snapshots === null}
		<p class="muted">{t('backup.restoreLoading')}</p>
	{:else if snapshots.length === 0}
		<p class="muted">{t('backup.restoreNone')}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={onclose}>{t('common.cancel')}</button>
		</div>
	{:else}
		<form class="plain" onsubmit={restore}>
			<p class="muted">{t('backup.restoreText')}</p>
			<label
				><span>{t('backup.restoreWhich')}</span>
				<select class="field" bind:value={snapshot}>
					{#each snapshots as saved (saved.id)}<option value={saved.id}
							>{formatDateTime(new Date(saved.time))}{saved.bytes
								? ` · ${formatBytes(saved.bytes)}`
								: ''}</option
						>{/each}
				</select>
			</label>
			<fieldset class="mode">
				<label class="check"
					><input type="radio" bind:group={mode} value="all" /><span>{t('backup.restoreAll')}</span
					></label
				>
				<label class="check"
					><input type="radio" bind:group={mode} value="files" /><span
						>{t('backup.restoreFiles')}</span
					></label
				>
			</fieldset>
			{#if mode === 'files'}
				<SnapshotFiles
					{job}
					{snapshot}
					start={snapshots.find((saved) => saved.id === snapshot)?.paths?.[0] ?? '/'}
					bind:selected={paths}
				/>
			{/if}
			<label
				><span>{t('backup.restoreVolume')}</span>
				<select class="field" bind:value={volume} required>
					{#each volumes as name (name)}<option value={name}>{name}</option>{/each}
				</select>
			</label>
			{#if volumes.length === 0}<p class="notice-warn">{t('backup.noVolume')}</p>{/if}
			{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
			<div class="dialog-actions">
				<button type="button" class="btn" onclick={onclose} disabled={busy}
					>{t('common.cancel')}</button
				>
				<button
					type="submit"
					class="btn btn-primary"
					disabled={busy || !volume || (mode === 'files' && paths.length === 0)}
					>{t('backup.restoreDo')}</button
				>
			</div>
		</form>
	{/if}
</Dialog>

<style>
	p {
		margin: 0;
	}
	form.plain {
		display: flex;
		flex-direction: column;
		gap: 14px;
		font-size: 0.875rem;
	}
	label {
		display: flex;
		flex-direction: column;
		gap: var(--space-1-5);
		font-size: 0.8125rem;
	}
	label > span {
		color: var(--muted);
	}
	label.check {
		flex-direction: row;
		align-items: center;
		gap: var(--space-2);
		font-size: 0.875rem;
	}
	.check > span {
		color: var(--text);
	}
	.check input {
		width: 18px;
		height: 18px;
		margin: 0;
	}
	fieldset {
		gap: var(--space-3);
	}
	.mode {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2) 20px;
		margin: 0;
		padding: 0;
		border: 0;
	}
	.mode .check {
		flex-direction: row;
		align-items: center;
		gap: var(--space-2);
	}
</style>
