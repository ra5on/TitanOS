<script lang="ts">
	import BackendDetail from '#lib/components/BackendDetail.svelte';
	// One backup job as a flat surface like a row (PLAN 78-1): its state, what the last run did, and what
	// can be done with it. The page shows the tile of the chosen job.
	import DangerZone from '#lib/components/DangerZone.svelte';
	import { formatBytes, formatDateTime } from '#lib/format.ts';
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { revealTarget } from '#lib/reveal-target.ts';
	import BackupNotes from './BackupNotes.svelte';
	import { bandwidthText, plainCopy, when, where, type Job, type View } from './data-backups.ts';

	let {
		view,
		hidden,
		restoreOnly,
		pending,
		act,
		openRestore,
		showPassword,
		edit,
		onprobe,
		oncheck,
		onremove,
	}: {
		view: View;
		/** Only the chosen backup's tile is shown. */
		hidden: boolean;
		restoreOnly: boolean;
		/** A start or stop that was sent and not answered yet. */
		pending: string | undefined;
		act: (job: Job, action: 'run' | 'stop') => void;
		openRestore: (job: Job) => void;
		showPassword: (job: Job) => void;
		edit: (view: View) => void;
		onprobe: () => void;
		oncheck: () => void;
		onremove: () => void;
	} = $props();

	const job = $derived(view.job);
	const state = $derived(view.state);
	const next = $derived(view.next);
</script>

<section class="srow panel job" class:hidden-job={hidden} use:revealTarget id="backup-{job.id}">
	<div class="head">
		<div>
			<h2 class="name mono">{job.id}</h2>
			{#if job.destination.type === 'smb-copy'}<span class="chip warn">{t('backup.beta')}</span
				>{/if}
			<p class="muted">
				{[...job.sources, ...(job.applications ?? []).map((a) => a.name)].join(', ')} →
				<span class="mono">{where(job.destination)}</span>
			</p>
		</div>
		{#if state.running && state.queued}<span class="chip">{t('backup.queued')}</span
			>{:else if state.running}<span class="chip info"
				>{t(`backup.phase.${state.phase}` as 'backup.phase.backup')}</span
			>
		{:else if view.stateKnown === false || state.statusError}<span class="chip warn"
				>{t('tasks.state.unknown')}</span
			>
		{:else if state.lastError && state.lastError !== 'cancelled'}<span class="chip error"
				>{t('backup.failed')}</span
			>
		{:else if state.lastError === 'cancelled'}<span class="chip warn">{t('backup.cancelled')}</span>
		{:else if state.warning || state.problems?.length}<span class="chip warn"
				>{t('backup.withWarning')}</span
			>
		{:else if state.lastOK}<span class="chip ok">{t('backup.ok')}</span>
		{:else}<span class="chip">{t('backup.never')}</span>{/if}
	</div>
	{#if state.running}
		<div
			class="bar"
			role="progressbar"
			aria-valuemin="0"
			aria-valuemax="100"
			aria-valuenow={Math.round(state.percent)}
			aria-label={t(`backup.phase.${state.phase}` as 'backup.phase.backup')}
		>
			<span style:width="{state.percent}%"></span>
		</div>
		<p class="muted mono">
			{#if state.bytesTotal > 0}{state.percent.toFixed(0)} %{' · '}{formatBytes(state.bytesDone)} / {formatBytes(
					state.bytesTotal,
				)}{:else}{formatBytes(
					state.bytesDone,
				)}{/if}{#if job.bandwidth}{' · '}{(state.bandwidthLimit ?? 0) > 0
					? t('backup.bandwidthActive', { value: state.bandwidthLimit ?? 0 })
					: t('backup.bandwidthActiveNone')}{/if}
		</p>
	{/if}
	{#if view.stateKnown === false || state.statusError}<p class="notice-warn">
			{t('coverage.stateUnknown')}
		</p>{/if}
	<dl>
		<div>
			<dt>{t('backup.schedule')}</dt>
			<dd>{when(job)}</dd>
		</div>
		{#if next}<div>
				<dt>{t('backup.next')}</dt>
				<dd>{formatDateTime(new Date(next))}</dd>
			</div>
		{:else if job.enabled}<div>
				<dt>{t('backup.next')}</dt>
				<dd class="muted">{t('backup.nextUnknown')}</dd>
			</div>{/if}
		<div>
			<dt>{t('safeguard.lastRun')}</dt>
			<dd>{state.lastRun ? formatDateTime(new Date(state.lastRun)) : '–'}</dd>
		</div>
		<div>
			<dt>{t('backup.lastOK')}</dt>
			<dd>
				{state.lastOK
					? formatDateTime(new Date(state.lastOK))
					: '–'}{#if state.lastOK && state.bytes > 0}{' · '}{formatBytes(state.bytes)}{/if}
			</dd>
		</div>
		{#if job.bandwidth}<div>
				<dt>{t('backup.bandwidth')}</dt>
				<dd>{bandwidthText(job.bandwidth)}</dd>
			</div>{/if}
		<div>
			<dt>{t('backup.keeps')}</dt>
			<dd>
				{plainCopy(job.destination.type) && !job.copy?.dated
					? t(job.copy?.mirror ? 'backup.copyMirror' : 'backup.copyRetain')
					: job.keep.mode === 'rolling'
						? t('backup.keepsRolling')
						: job.keep.daily + job.keep.weekly + job.keep.monthly > 0
							? t('backup.keepsText', {
									daily: job.keep.daily,
									weekly: job.keep.weekly,
									monthly: job.keep.monthly,
								})
							: t('backup.keepsAll')}
			</dd>
		</div>
	</dl>
	{#if !state.running && state.lastError && state.lastError !== 'cancelled'}<div
			class="notice-error"
		>
			{t('backup.lastError')}
			{#if state.lastErrorCode}<p>{t(`error.${state.lastErrorCode}` as MessageKey)}</p>{/if}
			<BackendDetail text={state.lastError} />
		</div>{/if}
	{#if !state.running}<BackupNotes {state} id={job.id} onchoice={() => edit(view)} />{/if}
	{#if !state.running && state.skipped}<p class="muted">
			{t(state.problems?.length ? 'backup.skippedNote' : 'backup.skipped', { n: state.skipped })}
		</p>{/if}
	{#if !state.running && state.lastCheck}
		{#if state.checkError}<div class="notice-error">
				{t('backup.checkFailed')}
				<BackendDetail text={state.checkError} />
			</div>
		{:else}<p class="muted">
				{t('backup.checkOk', { at: formatDateTime(new Date(state.lastCheck)) })}
			</p>{/if}
	{/if}
	{#if state.probe}<p class={state.probe.error ? 'notice-warn' : 'muted'}>
			{t('ops.probe')} · {formatDateTime(new Date(state.probe.at))} · {t('backup.probeFiles', {
				count: state.probe.files,
			})} · {formatBytes(state.probe.bytes)}{#if state.probe.truncated}<span>
					· {t('ops.truncated')}</span
				>{/if}{#if state.probe.error}<span> · {t('backup.probeFailed')}</span><BackendDetail
					text={state.probe.error}
				/>{/if}
		</p>{/if}
	{#if !state.running && state.restoreError}<div class="notice-error">
			{t('backup.restoreFailed')}
			<BackendDetail text={state.restoreError} />
		</div>
	{:else if !state.running && state.restoredTo}<p class="notice-ok">
			{t('backup.restoredTo')} <span class="mono">{state.restoredTo}</span>
		</p>{/if}
	{#if pending}<p role="status">
			{t(pending === 'run' ? 'ui.starting' : 'ui.stopping')}
		</p>{/if}
	<div class="row">
		{#if state.running}
			<button type="button" class="btn" disabled={!!pending} onclick={() => act(job, 'stop')}
				>{t('backup.stop')}</button
			>
		{:else}
			{#if !restoreOnly}<button
					type="button"
					class="btn btn-primary"
					disabled={!!pending}
					onclick={() => act(job, 'run')}>{t('backup.run')}</button
				>{/if}
			{#if !plainCopy(job.destination.type)}<button
					type="button"
					class="btn"
					onclick={() => openRestore(job)}>{t('backup.restore')}</button
				>
				<button type="button" class="btn" onclick={onprobe}>{t('ops.probe')}</button>
				<button type="button" class="btn" onclick={oncheck}>{t('backup.check')}</button>
				<button type="button" class="btn" onclick={() => showPassword(job)}
					>{t('backup.showPassword')}</button
				>{:else}<span class="muted">{t('backup.copyRestore')}</span>{/if}
			{#if !restoreOnly}<button
					type="button"
					class="btn"
					aria-label={t('backup.editNamed', { name: job.id })}
					onclick={() => edit(view)}>{t('backup.edit')}</button
				>{/if}
		{/if}
	</div>
	{#if !state.running && !restoreOnly}
		<DangerZone hint={t('backup.removeHint')}>
			<button
				type="button"
				class="btn btn-danger"
				aria-label={t('backup.removeNamed', { name: job.id })}
				onclick={onremove}>{t('backup.remove')}</button
			>
		</DangerZone>
	{/if}
</section>

<style>
	/* The quiet surface of a row, with the layout of a panel. */
	section {
		display: flex;
		flex-direction: column;
		font-size: 0.875rem;
	}
	p {
		margin: 0;
	}
	.head {
		display: flex;
		align-items: flex-start;
		justify-content: space-between;
		gap: var(--space-3);
	}
	.name {
		font-size: 1rem;
		font-weight: 600;
	}
	.head p {
		margin-top: 2px;
		overflow-wrap: anywhere;
	}
	dl {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
		gap: var(--space-3) var(--space-5);
		margin: 0;
	}
	dt {
		color: var(--muted);
		font-size: 0.75rem;
	}
	dd {
		margin: 2px 0 0;
	}
	.row {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-3) 20px;
	}
	/* Only the chosen backup's tile is shown: it is the detail of the row. */
	.hidden-job {
		display: none;
	}
</style>
