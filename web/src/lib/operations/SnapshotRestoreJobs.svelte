<script lang="ts">
	// The folder restores of this NAS with progress, cancel and dismiss.
	// Refreshes every two seconds while one runs and the page is visible.
	import { poll } from '#lib/poll.ts';
	import { onMount } from 'svelte';
	import { api } from '#lib/api.ts';
	import { formatBytes, formatBytesOf } from '#lib/format.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { operationError } from '#lib/operations.ts';
	import {
		folderRestoreProgress,
		restoredFolderLink,
		type FolderRestoreJob,
	} from '#lib/snapshot-restore.ts';

	let { jobs = $bindable([]) }: { jobs: FolderRestoreJob[] } = $props();
	let error = $state(''),
		busy = $state('');

	export async function load() {
		try {
			jobs = (await api.get<{ restores: FolderRestoreJob[] }>('/storage/snapshot-restores'))
				.restores;
			error = '';
		} catch (e) {
			error = operationError(e, t);
		}
	}
	onMount(() => {
		return poll(load, {
			interval: 2000,
			skip: () => !jobs.some((job) => job.state === 'running'),
		});
	});
	async function act(job: FolderRestoreJob, cancel: boolean) {
		busy = job.id;
		try {
			const path = `/storage/snapshot-restores/${encodeURIComponent(job.id)}`;
			if (cancel) await api.post(`${path}/cancel`, {});
			else await api.delete(path);
			await load();
		} catch (e) {
			error = operationError(e, t);
		} finally {
			busy = '';
		}
	}
</script>

{#if jobs.length || error}<section class="tile jobs" aria-label={t('snapshotRestore.jobs')}>
		<h2>{t('snapshotRestore.jobs')}</h2>
		{#if error}<p class="notice-error" role="alert">
				{error}
				<button type="button" class="btn" onclick={load}>{t('snapshotRestore.again')}</button>
			</p>{/if}
		<ul>
			{#each jobs as job (job.id)}
				{@const progress = folderRestoreProgress(job)}
				<li>
					<div class="head">
						<span class="mono">{job.volume} / {job.snapshot} / {job.path}</span>
						<span class="state">{t(`snapshotRestore.state.${job.state}`)}</span>
					</div>
					<p class="muted">
						{t(job.mode === 'copy' ? 'snapshotRestore.copy' : 'snapshotRestore.original')}
						{#if job.mode === 'original'}· {t(`files.conflict.${job.conflict}`)}{/if}
						· {t('snapshotRestore.target', { path: `${job.volume}/${job.target}` })}
					</p>
					{#if job.state === 'running'}{#if job.phase === 'counting'}<p class="muted">
								{t('snapshotRestore.counting')}
							</p>{:else}<progress max="100" value={progress ?? 0}></progress>
							<p class="muted">
								{t('snapshotRestore.progress', {
									files: job.files,
									total: job.totalFiles,
									size: formatBytesOf(job.bytes, job.totalBytes),
								})}
							</p>{/if}{:else}<p class="muted">
							{t('snapshotRestore.result', {
								copied: job.copied,
								skipped: job.skipped,
								replaced: job.replaced,
								renamed: job.renamed,
								size: formatBytes(job.bytes),
							})}
						</p>{/if}
					{#if job.error}<p class="notice-error">{job.error}</p>{/if}
					<div class="actions">
						{#if job.state === 'running'}<button
								type="button"
								class="btn"
								disabled={busy === job.id}
								onclick={() => act(job, true)}>{t('snapshotRestore.cancel')}</button
							>{:else}{#if job.state === 'done'}<a class="btn" href={restoredFolderLink(job)}
									>{t('snapshotRestore.open')}</a
								>{/if}<button
								type="button"
								class="btn"
								disabled={busy === job.id}
								onclick={() => act(job, false)}>{t('snapshotRestore.dismiss')}</button
							>{/if}
					</div>
				</li>
			{/each}
		</ul>
	</section>{/if}

<style>
	.jobs {
		padding: var(--tile-pad);
		margin-top: var(--gap);
	}
	h2 {
		margin: 0 0 12px;
		font-size: 1rem;
	}
	ul {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: 12px;
	}
	li {
		border-bottom: 1px solid var(--border);
		padding-bottom: 12px;
	}
	li:last-child {
		border-bottom: 0;
		padding-bottom: 0;
	}
	.head {
		display: flex;
		justify-content: space-between;
		gap: 12px;
		flex-wrap: wrap;
	}
	.mono,
	p {
		overflow-wrap: anywhere;
	}
	p {
		margin: 4px 0;
		font-size: 0.85rem;
	}
	progress {
		width: 100%;
	}
	.actions {
		display: flex;
		gap: 8px;
		flex-wrap: wrap;
		margin-top: 6px;
	}
</style>
