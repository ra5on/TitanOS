<script lang="ts">
	import { poll } from '#lib/poll.ts';
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { api, type StorageStatus } from '#lib/api.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import PageHead from '#lib/components/PageHead.svelte';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import NavigationGuard from '#lib/operations/NavigationGuard.svelte';
	import { formatBytes, formatDateTime } from '#lib/format.ts';
	import { operationError } from '#lib/operations.ts';
	type Result = { at: string; snapshot: string; target: string; verified: boolean; error: string };
	type Job = {
		job: {
			id: string;
			applications?: { kind: string; name: string; method: string }[];
			destination: { type: string };
		};
		state: { running: boolean; phase: string; applicationRestore?: Result };
	};
	type Snapshot = { id: string; time: string; bytes: number; paths: string[] };
	type Plan = {
		snapshot: Snapshot;
		targetVolume: string;
		freeBytes: number;
		currentSelection: boolean;
		capturedApplications?: {
			kind: string;
			name: string;
			method: string;
			identity: string;
			running: boolean;
		}[];
		applications: { kind: string; name: string; method: string }[];
	};
	let ready = $state(false);
	let jobs = $state<Job[]>([]),
		volumes = $state<StorageStatus['volumes']>([]),
		snapshots = $state<Snapshot[]>([]);
	let job = $state(''),
		snapshot = $state(''),
		volume = $state(''),
		plan = $state<Plan | null>(null),
		target = $state(''),
		watching = $state(false),
		busy = $state(false),
		error = $state(''),
		result = $state<Result | null>(null);
	async function load() {
		try {
			const [backups, storage] = await Promise.all([
				api.get<{ jobs: Job[] }>('/backups'),
				api.get<StorageStatus>('/storage'),
			]);
			jobs = backups.jobs.filter(
				(j) =>
					j.job.applications?.length && !['usb-copy', 'smb-copy'].includes(j.job.destination.type),
			);
			volumes = storage.volumes.filter((v) => v.mounted);
			if (watching) {
				const current = jobs.find((j) => j.job.id === job);
				if (current && !current.state.running) {
					watching = false;
					result = current.state.applicationRestore ?? null;
					if (!result || result.target !== target) error = t('applications.unknown');
				}
			}
		} catch (e) {
			error = operationError(e, t);
		} finally {
			ready = true;
		}
	}
	async function choose() {
		snapshots = [];
		snapshot = '';
		plan = null;
		error = '';
		if (!job) return;
		busy = true;
		try {
			snapshots = (
				await api.get<{ snapshots: Snapshot[] }>(`/backups/${encodeURIComponent(job)}/snapshots`)
			).snapshots;
		} catch (e) {
			error = operationError(e, t);
		} finally {
			busy = false;
		}
	}
	async function preview() {
		busy = true;
		error = '';
		result = null;
		try {
			plan = await api.post<Plan>(`/backups/${encodeURIComponent(job)}/applications/preview`, {
				snapshot,
				volume,
			});
		} catch (e) {
			error = operationError(e, t);
		} finally {
			busy = false;
		}
	}
	async function restore() {
		if (!plan) return;
		busy = true;
		error = '';
		try {
			const started = await api.post<{ target: string }>(
				`/backups/${encodeURIComponent(job)}/applications/restore`,
				{ snapshot: plan.snapshot.id, volume: plan.targetVolume },
			);
			target = started.target;
			watching = true;
			plan = null;
			await load();
		} catch (e) {
			error = operationError(e, t);
		} finally {
			busy = false;
		}
	}
	function targetLink() {
		const found = volumes.find((v) => target.startsWith(`/mnt/${v.name}/`));
		if (!found) return '/files';
		return (
			'/files?' +
			new URLSearchParams({
				root: 'volume/' + found.name,
				path: target.slice(`/mnt/${found.name}/`.length),
			})
		);
	}

	onMount(() => {
		const requested = page.url.searchParams.get('job');
		load().then(async () => {
			if (!requested) return;
			job = requested;
			const previous = jobs.find((j) => j.job.id === requested)?.state.applicationRestore;
			if (previous) {
				result = previous;
				target = previous.target;
			} else await choose();
		});
		return poll(load, { interval: 5000, immediate: false, skip: () => !watching || busy });
	});
</script>

<NavigationGuard dirty={!!plan} />
<PageHead
	title={t('applications.title')}
	parent={{ href: '/settings/backups', label: t('safeguard.title') }}
	>{#snippet actions()}<a class="btn" href="/settings/backups/coverage">{t('coverage.title')}</a
		><button class="btn" disabled={busy} onclick={load}>{t('ops.refresh')}</button
		>{/snippet}</PageHead
>
<p class="muted">{t('applications.hint')}</p>
{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
{#if ready && !jobs.length && !error}<EmptyState
		title={t('applications.none')}
		text={t('applications.noneHint')}
		><a class="btn btn-primary" href="/settings/backups/data">{t('coverage.configure')}</a
		></EmptyState
	>{/if}
{#if ready && jobs.length && !watching && !result && !plan}<form
		class="tile panel form"
		onsubmit={(e) => {
			e.preventDefault();
			preview();
		}}
	>
		<label class="stack"
			><span>{t('applications.job')}</span><select
				class="field"
				bind:value={job}
				required
				disabled={busy}
				onchange={choose}
				><option value="">{t('ops.choose')}</option>{#each jobs as entry (entry.job.id)}<option
						value={entry.job.id}>{entry.job.id}</option
					>{/each}</select
			></label
		>
		<label class="stack"
			><span>{t('applications.snapshot')}</span><select
				class="field"
				bind:value={snapshot}
				required
				disabled={busy || !job}
				><option value="">{t('ops.choose')}</option>{#each snapshots as saved (saved.id)}<option
						value={saved.id}>{formatDateTime(new Date(saved.time))} · {saved.id}</option
					>{/each}</select
			></label
		>
		<label class="stack"
			><span>{t('applications.volume')}</span><select
				class="field"
				bind:value={volume}
				required
				disabled={busy}
				><option value="">{t('ops.choose')}</option>{#each volumes as v (v.name)}<option
						value={v.name}>{v.name}</option
					>{/each}</select
			></label
		>
		<button class="btn btn-primary" disabled={busy || !job || !snapshot || !volume}
			>{t('ops.preview')}</button
		>
	</form>{/if}
{#if plan}<section class="tile panel plan">
		<h2 class="tile-title">{t('tasks.restorePreview')}</h2>
		<p class="mono">{job} · {plan.snapshot.id} · {formatDateTime(new Date(plan.snapshot.time))}</p>
		<p>
			{t('applications.size')}: {plan.snapshot.bytes
				? formatBytes(plan.snapshot.bytes)
				: t('applications.unknownSize')} · {t('applications.free')}: {formatBytes(plan.freeBytes)}
		</p>
		<p>{t('applications.target', { volume: plan.targetVolume })}</p>
		<h3>{t('applications.paths')}</h3>
		<ul>
			{#each plan.snapshot.paths as path (path)}<li class="mono">{path}</li>{/each}
		</ul>
		<h3>
			{t(
				plan.currentSelection ? 'applications.currentSelection' : 'applications.capturedSelection',
			)}
		</h3>
		<ul>
			{#each plan.capturedApplications ?? plan.applications as app (`${app.kind}:${app.name}`)}<li>
					{app.name} · {app.kind} · {app.method}
				</li>{/each}
		</ul>
		<p class="notice-warn">{t('applications.reviewHint')}</p>
		{#if plan.currentSelection}<p class="notice-warn">
				{t('applications.currentSelectionHint')}
			</p>{/if}
		<div class="row-actions">
			<button class="btn" disabled={busy} onclick={() => (plan = null)}>{t('common.cancel')}</button
			><button class="btn btn-primary" disabled={busy} onclick={restore}
				>{t('applications.restoreCopy')}</button
			>
		</div>
	</section>{/if}
{#if watching}<section class="tile panel plan">
		<h2 class="tile-title">{t('applications.running')}</h2>
		<p class="mono">{target}</p>
		<p>{t('applications.background')}</p>
		<a href="/tools/tasks">{t('ops.openTasks')}</a>
	</section>{/if}
{#if result}<section class="tile panel plan">
		<h2 class="tile-title">{t(result.verified ? 'applications.done' : 'applications.failed')}</h2>
		<p class="mono">{result.target}</p>
		{#if result.error}<p class="notice-error">{result.error}</p>{/if}
		{#if result.verified}<ol>
				<li>{t('applications.reviewFiles')}</li>
				<li>{t('applications.reviewConfig')}</li>
				<li>{t('applications.testCopy')}</li>
			</ol>{/if}
		<div class="row-actions">
			{#if result.verified}<a class="btn btn-primary" href={targetLink()}
					>{t('applications.openTarget')}</a
				>{/if}
			<button
				class="btn"
				onclick={() => {
					result = null;
					target = '';
				}}>{t('applications.another')}</button
			>
		</div>
	</section>{/if}

<style>
	.form {
		flex-direction: row;
		flex-wrap: wrap;
		align-items: end;
	}
	.form .stack {
		flex: 1 1 200px;
	}
	.plan h3 {
		font-size: 0.875rem;
	}
	.plan :is(ul, ol) {
		margin: 0;
	}
	.plan li {
		margin: 8px 0;
		overflow-wrap: anywhere;
	}
	.mono {
		overflow-wrap: anywhere;
	}
</style>
