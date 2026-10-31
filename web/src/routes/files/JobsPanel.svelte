<script lang="ts">
	async function forget(job: CopyJob) {
		if (retrying) return;
		retrying = job.id;
		pollRevision++;
		retryError = '';
		try {
			await api.delete(`/files/transfers/${job.id}`);
			notices = notices.filter((item) => item.id !== job.id);
			jobs = jobs.filter((item) => item.id !== job.id);
		} catch (err) {
			retryError = apiErrorText(err);
		} finally {
			retrying = '';
			await fetchJobs();
		}
	}
	async function clearFinished() {
		if (retrying) return;
		retrying = 'all';
		pollRevision++;
		retryError = '';
		try {
			await api.delete('/files/transfers');
			notices = [];
			jobs = jobs.filter((job) => job.state === 'running');
		} catch (err) {
			retryError = apiErrorText(err);
		} finally {
			retrying = '';
			await fetchJobs();
		}
	}
	import { poll } from '#lib/poll.ts';
	import { transferSample, transferProblem, type TransferSample } from '#lib/transfer-progress.ts';
	import { api, type CopyJob } from '#lib/api.ts';
	import { MB, formatBytes } from '#lib/format.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';

	type Props = {
		/** Called when a job that was running has ended. */ onfinished: () => void;
		refresh: number;
		canRetry: (job: CopyJob) => boolean;
		onopen: (root: string, path: string) => void;
	};
	let { onfinished, refresh, onopen, canRetry }: Props = $props();

	let jobs = $state<CopyJob[]>([]);
	let retryError = $state(''),
		retrying = $state(''),
		controlling = $state('');
	async function retry(job: CopyJob) {
		if (!canRetry(job)) return;
		retrying = job.id;
		pollRevision++;
		retryError = '';
		try {
			await api.post(`/files/jobs/${job.id}/retry`);
		} catch (err) {
			retryError = apiErrorText(err);
		} finally {
			retrying = '';
			await fetchJobs();
		}
	}
	async function control(job: CopyJob, change: Partial<CopyJob>) {
		if (controlling) return;
		controlling = job.id;
		retryError = '';
		try {
			await api.post(`/files/jobs/${job.id}/control`, {
				paused: job.paused ?? false,
				priority: job.priority ?? 0,
				rate: job.rate ?? 0,
				...change,
			});
			await fetchJobs();
		} catch (err) {
			retryError = apiErrorText(err);
		} finally {
			controlling = '';
		}
	}
	let samples = $state<Record<string, TransferSample>>({}),
		pollError = $state(false);
	let inFlight = false,
		refreshAfterPoll = false,
		pollRevision = 0;
	const activeJobs = $derived(jobs.filter((job) => job.state === 'running'));
	const totalKnown = $derived(activeJobs.every((job) => job.bytes > 0));
	const totalBytes = $derived(activeJobs.reduce((sum, job) => sum + job.bytes, 0));
	const totalCopied = $derived(activeJobs.reduce((sum, job) => sum + job.copied, 0));
	const overallSpeed = $derived(
		activeJobs.reduce((sum, job) => sum + (samples[job.id]?.speed ?? 0), 0),
	);
	let known = new Map<string, string>();
	let initialized = false;
	let notices = $state<CopyJob[]>([]);

	async function fetchJobs() {
		if (retrying) return;
		if (inFlight) {
			refreshAfterPoll = true;
			return;
		}
		inFlight = true;
		const revision = pollRevision;
		try {
			const list = await api.get<CopyJob[]>('/files/jobs');
			if (revision !== pollRevision) return;
			pollError = false;
			const now = performance.now();
			const next: Record<string, TransferSample> = {};
			for (const job of list)
				next[job.id] = transferSample(
					samples[job.id],
					job.copied,
					now,
					job.state === 'running' && !job.paused && !job.waiting,
				);
			samples = next;
			let ended = false;
			for (const job of list) {
				if (
					job.state !== 'running' &&
					(known.get(job.id) === 'running' || (initialized && !known.has(job.id)))
				) {
					ended = true;
					notices = [job, ...notices.filter((item) => item.id !== job.id)].slice(0, 5);
				}
				known.set(job.id, job.state);
			}
			initialized = true;
			jobs = [
				...list.filter((j) => j.state === 'running'),
				...list.filter((j) => j.state !== 'running'),
			];
			notices = notices.filter((item) => list.some((job) => job.id === item.id));
			if (ended) onfinished();
		} catch {
			if (revision !== pollRevision) return;
			pollError = true;
			samples = {};
		} finally {
			inFlight = false;
			if (refreshAfterPoll && !retrying) {
				refreshAfterPoll = false;
				void fetchJobs();
			}
		}
	}

	$effect(() => {
		refresh;
		return poll(fetchJobs, { interval: 1000 });
	});

	const percent = (job: CopyJob) =>
		job.bytes > 0
			? Math.min(100, Math.round((100 * job.copied) / job.bytes))
			: job.state === 'done'
				? 100
				: 0;
	const cancel = (job: CopyJob) =>
		api.post(`/files/jobs/${job.id}/cancel`).then(fetchJobs, fetchJobs);
</script>

{#each notices as notice (notice.id)}<div class="notice" role="status">
		<strong>{t(notice.state === 'done' ? 'files.operationDone' : 'files.operationFailed')}</strong>
		<span class="mono">{notice.to}/{notice.dir}</span>
		<button class="btn" onclick={() => onopen(notice.to, notice.dir)}
			>{t('files.openDestination')}</button
		>
		<button
			class="btn"
			aria-label={t('files.dismissCompletion')}
			onclick={() => (notices = notices.filter((item) => item.id !== notice.id))}>×</button
		>
	</div>{/each}
{#if jobs.length > 0}
	<section class="tile jobs" aria-label={t('files.jobs')}>
		{#if retryError}<p class="notice-error" role="alert">{retryError}</p>{/if}
		<div class="line">
			<h2 class="tile-title">{t('files.jobs')}</h2>
			{#if jobs.some((job) => job.state !== 'running')}<button
					class="btn"
					disabled={!!retrying}
					title={t('ops.forgetHint')}
					onclick={clearFinished}>{t('files.clearFinishedJobs')}</button
				>{/if}
		</div>
		{#if pollError}<p class="notice-warn" role="status">{t('files.jobsStale')}</p>{/if}
		{#if activeJobs.length}<div class="overall">
				<strong>{t('files.overallProgress', { count: activeJobs.length })}</strong><progress
					max="100"
					value={totalKnown && totalBytes > 0
						? Math.min(100, (100 * totalCopied) / totalBytes)
						: undefined}
					aria-label={t('files.overallProgress', { count: activeJobs.length })}
				></progress><span
					>{formatBytes(totalCopied)} / {totalKnown
						? formatBytes(totalBytes)
						: t('files.totalPending')}{#if overallSpeed > 0}
						· {formatBytes(overallSpeed)}/s{/if}</span
				>
			</div>{/if}
		<p class="muted small">{t('files.queueHint')}</p>
		{#each jobs as job (job.id)}
			<div class="job">
				<div class="line">
					<span class="what"
						>{t(
							job.operation === 'extract'
								? 'files.jobExtract'
								: job.operation === 'download'
									? 'files.jobDownload'
									: job.move
										? 'files.jobMove'
										: 'files.jobCopy',
							{ count: job.names.length },
						)} · <span class="mono">{job.from} → {job.to}/{job.dir}</span></span
					>
					{#if job.state === 'running'}
						<button type="button" class="btn small" onclick={() => cancel(job)}
							>{t('common.cancel')}</button
						>
					{:else}
						<span class="chip">{t(`files.jobState.${job.state}` as 'files.jobState.done')}</span>
					{/if}
				</div>
				{#if job.state === 'running' && !job.operation}<div class="line controls">
						<button
							class="btn small"
							disabled={!!controlling || (!!job.paused && !canRetry(job))}
							onclick={() => control(job, { paused: !job.paused })}
							>{t(job.paused ? 'files.transferResume' : 'files.transferPause')}</button
						>
						{#if job.waiting && !job.paused}<span class="chip">{t('files.transferWaiting')}</span
							>{/if}
						{#if job.paused}<span class="chip">{t('files.transferPaused')}</span>{/if}
						<label
							>{t('files.transferPriority')}
							<select
								class="field"
								aria-label={t('files.transferPriority')}
								disabled={!!controlling}
								value={String(job.priority ?? 0)}
								onchange={(e) => control(job, { priority: Number(e.currentTarget.value) })}
								><option value="0">{t('files.priorityNormal')}</option><option value="1"
									>{t('files.priorityHigh')}</option
								><option value="2">{t('files.priorityFirst')}</option></select
							></label
						>
						<label
							>{t('files.transferRate')}
							<input
								class="field"
								aria-label={t('files.transferRate')}
								disabled={!!controlling}
								type="number"
								min="0"
								max="1073"
								step="1"
								value={Math.round((job.rate ?? 0) / MB)}
								onchange={(e) => {
									if (e.currentTarget.checkValidity())
										control(job, { rate: Number(e.currentTarget.value) * MB });
								}}
							/></label
						>
					</div>{/if}
				<progress
					value={job.bytes > 0 || job.state !== 'running' ? percent(job) : undefined}
					max="100"
					aria-label={t('files.jobs')}
				></progress>
				<div class="muted small">
					{#if job.operation === 'download'}{formatBytes(job.copied)} / {job.bytes > 0
							? formatBytes(job.bytes)
							: t('files.urlSizeUnknown')} ·
						<span class="mono">{job.names[0]}</span>{:else}{formatBytes(job.copied)} / {formatBytes(
							job.bytes,
						)} · {t('files.jobFiles', {
							done: job.done,
							files: job.files,
						})}{/if}{#if job.state === 'running' && job.current}
						· <span class="mono">{job.current}</span>{/if}
				</div>
				{#if job.state === 'running' && !job.paused && !job.waiting}<p class="small muted">
						{#if samples[job.id]?.speed > 0}{formatBytes(
								samples[job.id].speed,
							)}/s{#if job.bytes > job.copied}
								· {t('files.remainingMinutes', {
									minutes: Math.max(
										1,
										Math.ceil((job.bytes - job.copied) / samples[job.id].speed / 60),
									),
								})}{/if}{:else}{t('files.speedPending')}{/if}
					</p>{/if}
				{#if job.error}<div class="notice-warn">
						<strong>{t(`files.transferProblem.${transferProblem(job.error)}`)}</strong
						>{#if job.current}<p class="mono">{job.current}</p>{/if}
						<details>
							<summary>{t('files.technicalDetails')}</summary>
							<p>{job.error}</p>
						</details>
					</div>{/if}
				{#if job.durable}<p class="muted small">{t('ops.durable')}</p>{/if}{#if job.blocked}<p
						class="notice-warn"
					>
						{t('ops.transferBlocked')}
					</p>{/if}
				{#if job.retryable}<button
						class="btn"
						disabled={retrying !== '' || !canRetry(job)}
						onclick={() => retry(job)}>{t('files.retryRemaining')}</button
					>
					<p class="muted small">{t('files.retryHint')}</p>{/if}
				{#if job.state !== 'running'}<button
						class="btn"
						disabled={!!retrying}
						title={t('ops.forgetHint')}
						onclick={() => forget(job)}>{t('ops.forget')}</button
					>{/if}
				{#if job.state !== 'running'}<button class="btn" onclick={() => onopen(job.to, job.dir)}
						>{t('files.openDestination')}</button
					>{/if}
			</div>
		{/each}
	</section>
{/if}

<style>
	.notice {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 10px;
		padding: 12px;
		margin-top: var(--gap);
		background: var(--chip);
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
	}
	.notice span {
		overflow-wrap: anywhere;
	}
	.jobs {
		margin-top: var(--gap);
		padding: var(--tile-pad);
	}
	.line.controls {
		justify-content: flex-start;
		margin-top: 0.4rem;
	}
	.controls label {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		font-size: var(--text-base);
	}
	.controls input {
		width: 6rem;
	}
	.job {
		padding: 0.5rem 0;
		border-top: 1px solid var(--border);
	}
	.line {
		display: flex;
		justify-content: space-between;
		align-items: center;
		gap: 0.8rem;
		flex-wrap: wrap;
	}
	progress {
		width: 100%;
		height: 0.5rem;
		margin: 0.4rem 0 0.2rem;
		accent-color: var(--accent);
	}
	.small {
		font-size: var(--text-sm);
		overflow-wrap: anywhere;
	}
	.what {
		overflow-wrap: anywhere;
	}
</style>
