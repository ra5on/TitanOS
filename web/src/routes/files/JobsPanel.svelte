<script lang="ts">
	import { api, type CopyJob } from '#lib/api.ts';
	import { formatBytes } from '#lib/format.ts';
	import { t } from '#lib/i18n/index.svelte.ts';

	type Props = { /** Called when a job that was running has ended. */ onfinished: () => void; refresh: number };
	let { onfinished, refresh }: Props = $props();

	let jobs = $state<CopyJob[]>([]);
	let running = new Set<string>();

	async function poll() {
		try {
			const list = await api.get<CopyJob[]>('/files/jobs');
			let ended = false;
			for (const job of list) {
				if (job.state === 'running') running.add(job.id);
				else if (running.delete(job.id)) ended = true;
			}
			jobs = list.slice(0, 5);
			if (ended) onfinished();
		} catch {
			// The next look may work.
		}
	}

	$effect(() => {
		refresh;
		poll();
		const timer = setInterval(() => !document.hidden && poll(), 1000);
		return () => clearInterval(timer);
	});

	const percent = (job: CopyJob) => (job.bytes > 0 ? Math.min(100, Math.round((100 * job.copied) / job.bytes)) : job.state === 'done' ? 100 : 0);
	const cancel = (job: CopyJob) => api.post(`/files/jobs/${job.id}/cancel`).then(poll, poll);
</script>

{#if jobs.length > 0}
	<section class="tile jobs" aria-label={t('files.jobs')}>
		<h2 class="tile-title">{t('files.jobs')}</h2>
		{#each jobs as job (job.id)}
			<div class="job">
				<div class="line">
					<span class="what">{t(job.move ? 'files.jobMove' : 'files.jobCopy', { count: job.names.length })} · <span class="mono">{job.from} → {job.to}/{job.dir}</span></span>
					{#if job.state === 'running'}
						<button type="button" class="btn small" onclick={() => cancel(job)}>{t('common.cancel')}</button>
					{:else}
						<span class="chip">{t(`files.jobState.${job.state}` as 'files.jobState.done')}</span>
					{/if}
				</div>
				<progress value={percent(job)} max="100" aria-label={t('files.jobs')}></progress>
				<div class="muted small">
					{formatBytes(job.copied)} / {formatBytes(job.bytes)} · {t('files.jobFiles', { done: job.done, files: job.files })}{#if job.state === 'running' && job.current} · <span class="mono">{job.current}</span>{/if}
				</div>
				{#if job.error}<p class="notice-warn">{job.error}</p>{/if}
			</div>
		{/each}
	</section>
{/if}

<style>
	.jobs { margin-top: var(--gap); }
	.job { padding: 0.5rem 0; border-top: 1px solid var(--border); }
	.line { display: flex; justify-content: space-between; align-items: center; gap: 0.8rem; flex-wrap: wrap; }
	progress { width: 100%; height: 0.5rem; margin: 0.4rem 0 0.2rem; accent-color: var(--accent); }
	.small { font-size: 0.8rem; overflow-wrap: anywhere; }
	.what { overflow-wrap: anywhere; }
</style>
