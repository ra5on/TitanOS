<script lang="ts">
	import Icon from '#lib/components/Icon.svelte';
	import { msg, iconFor, jobTitle } from './labels.ts';
	import type { Capability, Job, JobStatus } from './api.ts';
	let {
		jobs,
		caps,
		paused,
		select,
		toggle,
		pending,
	}: {
		jobs: Job[];
		caps: Capability[];
		paused: boolean;
		pending: boolean;
		select: (j: Job) => void;
		toggle: () => void;
	} = $props();
	const filters: ('all' | JobStatus)[] = [
		'all',
		'running',
		'waiting',
		'planned',
		'done',
		'paused',
		'failed',
		'canceled',
	];
	let filter = $state<(typeof filters)[number]>('all');
	const rows = $derived(jobs.filter((j) => filter === 'all' || j.status === filter).toReversed());
</script>

<div class="pa-pane">
	<div class="pa-title-row">
		<h1>{msg('jobs')}</h1>
		<button
			class="pa-icon"
			disabled={pending}
			aria-label={msg(paused ? 'continueAll' : 'pauseAll')}
			title={msg(paused ? 'continueAll' : 'pauseAll')}
			onclick={toggle}><Icon name={paused ? 'play' : 'pause'} /></button
		>
	</div>
	{#if paused}<p class="pa-status"><Icon name="pause" />{msg('allPaused')}</p>{/if}
	<div class="pa-filterbar" aria-label={msg('status')}>
		{#each filters as f (f)}<button aria-pressed={f === filter} onclick={() => (filter = f)}
				>{msg(f)}</button
			>{/each}
	</div>
	{#if !rows.length}<p class="pa-muted">{msg('noneJobs')}</p>{/if}
	<div class="pa-row-list">
		{#each rows as job (job.id)}<button class="pa-row" onclick={() => select(job)}
				><i><Icon name={iconFor(job.request.tool)} /></i><span
					><b>{jobTitle(job, caps)}</b><small
						>{msg(job.status)}{job.status === 'planned' && job.request.runAt
							? ' · ' + new Date(job.request.runAt).toLocaleString()
							: ''}</small
					></span
				>{#if job.status === 'waiting'}<Icon name="lock" />{/if}<Icon name="chevronRight" /></button
			>{/each}
	</div>
</div>
