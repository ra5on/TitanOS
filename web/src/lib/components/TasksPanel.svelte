<script lang="ts">
	import { poll } from '#lib/poll.ts';
	import { api, type CopyJob, type VMStatus } from '#lib/api.ts';
	import { session } from '#lib/session.svelte.ts';
	import { storage } from '#lib/storage.svelte.ts';
	import { uploads } from '#lib/uploads.svelte.ts';
	import { system } from '#lib/system.svelte.ts';
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { taskPercent } from '#lib/task-progress.ts';
	import { vmLabel } from '#lib/vm-name.ts';
	import type { MediaJob } from '#lib/media.ts';
	import Dialog from './Dialog.svelte';
	import Icon from './Icon.svelte';
	/**
	 * `badge`: only a small badge with the number while something runs (head
	 * line, sidebar status), nothing otherwise.
	 */
	let { compact = false, badge = false }: { compact?: boolean; badge?: boolean } = $props();

	type Task = {
		id: string;
		title: string;
		kind: MessageKey;
		href: string;
		percent?: number;
		attention?: boolean;
	};
	type BackupStatus = {
		jobs: {
			job: { id: string };
			state: { running: boolean; bytesDone: number; bytesTotal: number };
		}[];
	};
	type UpdateStatus = { progress: { phase: string; percent: number; file: string } };
	let open = $state(false);
	let tasks = $state<Task[]>([]);
	let failed = $state(false);
	let loaded = $state(false);
	const admin = $derived(['owner', 'admin'].includes(session.data?.user?.role ?? ''));
	$effect(() => {
		const privileged = admin;
		let alive = true;
		const cache = new Map<string, Task[]>();
		async function collect() {
			const sources: [string, () => Promise<Task[]>][] = [
				[
					'files',
					async () =>
						(await api.get<CopyJob[]>('/files/jobs', AbortSignal.timeout(10000)))
							.filter((job) => job.state === 'running')
							.map((job) => ({
								id: `file-${job.id}`,
								kind: job.operation === 'download' ? 'files.jobDownload' : 'ui.taskCopy',
								title: job.names.join(', '),
								href: '/files',
								percent: taskPercent(job.copied, job.bytes),
							})),
				],
			];
			if (privileged)
				sources.push(
					[
						'media',
						async () =>
							(await api.get<{ jobs: MediaJob[] }>('/vms/imports', AbortSignal.timeout(10000))).jobs
								.filter((job) => !['done', 'cancelled'].includes(job.phase))
								.map((job) => ({
									id: `media-${job.id}`,
									title: job.name,
									kind: 'media.title' as const,
									href: '/vms/media',
									percent: ['uploading', 'downloading'].includes(job.phase)
										? taskPercent(job.bytes, job.total)
										: undefined,
									attention: ['paused', 'failed', 'review'].includes(job.phase),
								})),
					],
					[
						'backups',
						async () =>
							(await api.get<BackupStatus>('/backups', AbortSignal.timeout(10000))).jobs
								.filter((view) => view.state.running)
								.map((view) => ({
									id: `backup-${view.job.id}`,
									title: view.job.id,
									kind: 'ui.taskBackup',
									href: `/settings/backups/data#backup-${encodeURIComponent(view.job.id)}`,
									percent: taskPercent(view.state.bytesDone, view.state.bytesTotal),
								})),
					],
					[
						'update',
						async () => {
							const { progress } = await api.get<UpdateStatus>(
								'/update/progress',
								AbortSignal.timeout(10000),
							);
							return ['download', 'switch'].includes(progress.phase)
								? [
										{
											id: 'system-update',
											title: progress.file,
											kind: 'ui.taskUpdate',
											href: '/settings/updates',
											percent:
												progress.phase === 'download'
													? taskPercent(progress.percent, 100)
													: undefined,
										},
									]
								: [];
						},
					],
					[
						'vms',
						async () =>
							(await api.get<VMStatus>('/vms', AbortSignal.timeout(10000))).vms
								.filter((vm) => vm.job && vm.job.state !== 'failed')
								.map((vm) => ({
									id: `vm-${vm.name}`,
									title: vmLabel(vm),
									kind: 'ui.taskVM',
									href: `/vms?vm=${encodeURIComponent(vm.name)}`,
								})),
					],
				);
			const results = await Promise.allSettled(
				sources.map(async ([key, read]) => [key, await read()] as const),
			);
			if (alive) {
				failed = results.some((result) => result.status === 'rejected');
				for (const result of results)
					if (result.status === 'fulfilled') cache.set(result.value[0], result.value[1]);
				tasks = [...cache.values()].flat();
				loaded = true;
			}
		}
		const stop = poll(collect, { interval: 5000 });
		return () => {
			alive = false;
			stop();
		};
	});
	const all = $derived([
		...tasks,
		...uploads.items
			.filter((item) => item.state !== 'done')
			.map((item) => ({
				id: `upload-${item.key}`,
				title: item.name,
				kind: 'ui.taskUpload' as const,
				href: '/files',
				percent: taskPercent(item.sent, item.file.size),
				attention: ['failed', 'paused', 'conflict', 'pressure'].includes(item.state),
			})),
		...(admin ? (storage.status?.volumes ?? []) : [])
			.filter((volume) => volume.sync)
			.map((volume) => ({
				id: `volume-${volume.name}`,
				title: volume.name,
				kind: 'ui.taskVolume' as const,
				href: `/storage#volume-${encodeURIComponent(volume.name)}`,
				percent: taskPercent(volume.sync!.percent, 100),
			})),
	] as Task[]);
</script>

{#if badge}
	{#if all.length > 0}<button
			class="tasks-badge"
			type="button"
			title={t('ui.tasksBadge', { count: all.length })}
			aria-label={t('ui.tasksBadge', { count: all.length })}
			onclick={() => (open = true)}
			><Icon name="activity" size={14} /><span class="count mono">{all.length}</span></button
		>{/if}
{:else}
	<div class="tasks-entry" class:compact>
		<button class="btn" type="button" onclick={() => (open = true)}
			>{t('ui.tasks')} <span class="mono">{loaded ? all.length : '…'}</span>{#if failed}<span
					aria-label={t('ui.tasksPartial')}>!</span
				>{/if}</button
		>
	</div>
{/if}
{#if open}
	<Dialog title={t('ui.tasks')} onclose={() => (open = false)}>
		<p class="muted">{t('ui.tasksHint')}</p>
		{#if failed || !system.reachable}<p class="notice-warn" role="status">
				{t('ui.tasksPartial')}
			</p>{/if}
		{#if !loaded}<p role="status">{t('ui.working')}</p>
		{:else if all.length === 0}<p>{t('ui.tasksEmpty')}</p>{/if}
		<ul>
			{#each all as task (task.id)}
				<li>
					<div><strong>{t(task.kind)}</strong><span class="mono">{task.title}</span></div>
					{#if task.attention}<span class="chip warn">{t('ui.taskAttention')}</span>{/if}
					<progress value={task.percent} max="100" aria-label={t(task.kind)}></progress>
					<span class="muted"
						>{task.percent === undefined
							? t('ui.progressUnknown')
							: `${Math.round(task.percent)} %`}</span
					>
					<a class="btn" href={task.href} onclick={() => (open = false)}>{t('ui.open')}</a>
				</li>
			{/each}
		</ul>
		<div class="dialog-actions">
			<button class="btn" type="button" onclick={() => (open = false)}>{t('common.done')}</button>
		</div>
	</Dialog>
{/if}

<style>
	.tasks-entry {
		display: flex;
		justify-content: flex-end;
	}
	.tasks-entry.compact {
		align-items: center;
		margin-right: 12px;
		padding-right: 16px;
		border-right: 1px solid var(--border);
	}
	.compact > .btn {
		height: 32px;
		padding: 0 4px;
		border: 0;
		background: transparent;
		font-size: var(--text-sm);
		color: var(--muted);
	}
	.tasks-badge {
		flex: none;
		display: inline-flex;
		align-items: center;
		gap: 4px;
		height: 28px;
		padding: 0 4px 0 6px;
		border: 1px solid color-mix(in srgb, var(--accent) 45%, var(--border));
		border-radius: 999px;
		background: color-mix(in srgb, var(--accent) 12%, transparent);
		color: var(--text);
		font: inherit;
		font-size: var(--text-sm);
		white-space: nowrap;
		cursor: pointer;
	}
	.tasks-badge :global(svg) {
		color: var(--accent);
	}
	.tasks-badge:hover {
		background: color-mix(in srgb, var(--accent) 20%, transparent);
	}
	.tasks-badge:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	.tasks-badge .count {
		min-width: 20px;
		height: 20px;
		padding: 0 5px;
		display: inline-flex;
		align-items: center;
		justify-content: center;
		border-radius: 999px;
		background: var(--accent);
		color: var(--on-accent);
		font-weight: 600;
	}
	@media (max-width: 760px) {
		.tasks-entry.compact {
			flex: 1 1 100%;
			justify-content: flex-start;
			margin-right: 0;
			padding-right: 0;
			border: 0;
		}
	}
	ul {
		list-style: none;
		padding: 0;
		margin: 12px 0;
		display: grid;
		gap: 16px;
		max-height: 55vh;
		overflow: auto;
	}
	li {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 8px;
		border-top: 1px solid var(--border);
		padding-top: 12px;
	}
	li > div {
		display: flex;
		flex-direction: column;
		gap: 4px;
		width: 100%;
		overflow-wrap: anywhere;
	}
	progress {
		width: 100%;
		accent-color: var(--accent);
	}
	li > a {
		margin-left: auto;
	}
</style>
