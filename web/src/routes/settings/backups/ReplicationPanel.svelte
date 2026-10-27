<script lang="ts">
	// Btrfs replication: send a volume to another TitanOS (sender side) and
	// copy what others sent here into a new folder (receiving side).
	import { api, ApiError } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { formatBytes, formatDateTime } from '#lib/format.ts';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';
	import { storage } from '#lib/storage.svelte.ts';

	type State = { running: boolean; bytes: number; lastRun: string | null; lastOK: string | null; lastError: string; snapshot: string; next: string | null };
	type Job = { id: string; name: string; enabled: boolean; volume: string; url: string; user: string; everyHours: number; keep: number; limitMiB?: number; state: State };
	type Replica = { source: string; snapshot: string };
	type Overview = { jobs: Job[]; received: Replica[]; receiveVolume: string };

	let overview = $state<Overview | null>(null);
	let open = $state(false);
	let loadError = $state('');
	let error = $state('');
	let busy = $state(false);
	let editing = $state<{ id: string; name: string; enabled: boolean; volume: string; code: string; everyHours: number; keep: number; limitMiB: number } | null>(null);
	let removing = $state<Job | null>(null);
	let restoring = $state<Replica | null>(null);
	let restored = $state('');

	const fail = (err: unknown) => errorText(err instanceof ApiError ? err.code : 'internal');
	const btrfsVolumes = $derived((storage.status?.volumes ?? []).filter((v) => v.filesystem === 'btrfs' && v.mounted).map((v) => v.name));

	async function load() {
		try {
			overview = await api.get<Overview>('/replication');
			loadError = '';
		} catch (err) {
			loadError = fail(err);
		}
	}

	$effect(() => {
		if (!open) return;
		load();
		const timer = setInterval(() => !document.hidden && load(), 10_000);
		return () => clearInterval(timer);
	});

	function add() {
		error = '';
		editing = { id: '', name: '', enabled: true, volume: btrfsVolumes[0] ?? '', code: '', everyHours: 24, keep: 14, limitMiB: 0 };
	}

	function edit(job: Job) {
		error = '';
		editing = { id: job.id, name: job.name, enabled: job.enabled, volume: job.volume, code: '', everyHours: job.everyHours, keep: job.keep, limitMiB: job.limitMiB ?? 0 };
	}

	async function save(event: SubmitEvent) {
		event.preventDefault();
		if (!editing || busy) return;
		busy = true;
		error = '';
		const { id, ...body } = editing;
		try {
			if (id) await api.put(`/replication/${id}`, body);
			else await api.post('/replication', body);
			editing = null;
			await load();
		} catch (err) {
			error = fail(err);
		}
		busy = false;
	}

	async function runNow(job: Job) {
		error = '';
		try {
			await api.post(`/replication/${job.id}/run`, {});
			await load();
		} catch (err) {
			error = fail(err);
		}
	}

	async function remove() {
		if (!removing || busy) return;
		busy = true;
		try {
			await api.delete(`/replication/${removing.id}`);
			removing = null;
			await load();
		} catch (err) {
			error = fail(err);
		}
		busy = false;
	}

	async function restore() {
		if (!restoring || busy) return;
		busy = true;
		error = '';
		try {
			const res = await api.post<{ path: string }>('/replication/received/restore', restoring);
			restored = res.path;
			restoring = null;
		} catch (err) {
			error = fail(err);
		}
		busy = false;
	}
</script>

<section class="tile replication">
	<div class="head">
		<h2 class="tile-title">{t('replication.title')}</h2>
		<button type="button" class="btn small" aria-expanded={open} onclick={() => (open = !open)}>{open ? t('backup.targetHide') : t('backup.targetShow')}</button>
	</div>
	<p class="muted">{t('replication.desc')}</p>
	{#if error && !editing}<p class="notice-error" role="alert">{error}</p>{/if}
	{#if loadError}<p class="notice-error" role="alert">{loadError} <button type="button" class="btn small" onclick={load}>{t('ui.retry')}</button></p>{/if}
	{#if restored}<p class="notice-ok" role="status">{t('replication.restored', { path: restored })}</p>{/if}
	{#if open && overview}
		<h3>{t('replication.jobs')}</h3>
		{#each overview.jobs as job (job.id)}
			<div class="job">
				<span class="mono name">{job.name}</span>
				<span class="muted">
					{job.volume} → {job.user}
					· {job.state.running ? t('replication.running', { size: formatBytes(job.state.bytes) }) : job.state.lastOK ? t('replication.lastOK', { time: formatDateTime(new Date(job.state.lastOK)) }) : t('replication.never')}
					{#if job.state.next && !job.state.running} · {t('replication.next', { time: formatDateTime(new Date(job.state.next)) })}{/if}
				</span>
				{#if job.state.lastError && !job.state.running}<span class="notice-error small">{job.state.lastError}</span>{/if}
				<span class="buttons">
					<button type="button" class="btn small" disabled={job.state.running} onclick={() => runNow(job)}>{t('replication.runNow')}</button>
					<button type="button" class="btn small" disabled={job.state.running} onclick={() => edit(job)}>{t('replication.edit')}</button>
					<button type="button" class="btn small" disabled={job.state.running} onclick={() => (removing = job)}>{t('replication.remove')}</button>
				</span>
			</div>
		{:else}
			<p class="muted">{t('replication.noJobs')}</p>
		{/each}
		<button type="button" class="btn" disabled={btrfsVolumes.length === 0} onclick={add}>{t('replication.add')}</button>
		{#if btrfsVolumes.length === 0}<p class="muted">{t('replication.needBtrfs')}</p>{/if}

		<h3>{t('replication.received')}</h3>
		{#if !overview.receiveVolume}
			<p class="muted">{t('replication.receiveHint')}</p>
		{:else}
			{#each overview.received as replica (replica.source + replica.snapshot)}
				<div class="job">
					<span class="mono name">{replica.source}</span>
					<span class="mono muted">{replica.snapshot}</span>
					<button type="button" class="btn small" onclick={() => ((restoring = replica), (error = ''))}>{t('replication.restore')}</button>
				</div>
			{:else}
				<p class="muted">{t('replication.noneReceived')}</p>
			{/each}
		{/if}
	{/if}
</section>

{#if editing}
	<Dialog title={t(editing.id ? 'replication.editTitle' : 'replication.addTitle')} locked={busy} onclose={() => (editing = null)}>
		<form class="form" onsubmit={save}>
			<label class="narrow"><span>{t('replication.name')}</span><input class="field mono" bind:value={editing.name} pattern="[A-Za-z][A-Za-z0-9\-]*" maxlength="31" required /></label>
			<label class="narrow"><span>{t('replication.volume')}</span>
				<select class="field" bind:value={editing.volume}>{#each btrfsVolumes as name (name)}<option value={name}>{name}</option>{/each}</select>
			</label>
			<label><span>{t('replication.code')}</span>
				<textarea class="field mono" rows="3" bind:value={editing.code} required={!editing.id} placeholder="titanos-backup:…"></textarea>
				<small class="muted">{t(editing.id ? 'replication.codeKeep' : 'replication.codeHint')}</small>
			</label>
			<div class="row">
				<label class="narrow"><span>{t('replication.everyHours')}</span><input class="field" type="number" min="1" max="168" bind:value={editing.everyHours} /></label>
				<label class="narrow"><span>{t('replication.keep')}</span><input class="field" type="number" min="2" max="365" bind:value={editing.keep} /></label>
				<label class="narrow"><span>{t('replication.limit')}</span><input class="field" type="number" min="0" max="10000" bind:value={editing.limitMiB} /></label>
			</div>
			<label class="check"><input type="checkbox" bind:checked={editing.enabled} /><span>{t('replication.enabled')}</span></label>
			<p class="muted">{t('replication.subvolumes')}</p>
			{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
			<div class="dialog-actions">
				<button type="button" class="btn" disabled={busy} onclick={() => (editing = null)}>{t('common.cancel')}</button>
				<button type="submit" class="btn btn-primary" disabled={busy}>{t('replication.save')}</button>
			</div>
		</form>
	</Dialog>
{/if}

{#if removing}
	<Dialog title={t('replication.removeTitle', { name: removing.name })} locked={busy} onclose={() => (removing = null)}>
		<p>{t('replication.removeText')}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={busy} onclick={() => (removing = null)}>{t('common.cancel')}</button>
			<button type="button" class="btn btn-warn" disabled={busy} onclick={remove}>{t('replication.remove')}</button>
		</div>
	</Dialog>
{/if}

{#if restoring}
	<Dialog title={t('replication.restoreTitle')} locked={busy} onclose={() => (restoring = null)}>
		<p>{t('replication.restoreText', { source: restoring.source, snapshot: restoring.snapshot })}</p>
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={busy} onclick={() => (restoring = null)}>{t('common.cancel')}</button>
			<button type="button" class="btn btn-primary" disabled={busy} onclick={restore}>{t('replication.restore')}</button>
		</div>
	</Dialog>
{/if}

<style>
	.replication { margin-bottom: var(--gap); padding: var(--tile-pad); overflow-wrap: anywhere; }
	.head { display: flex; flex-wrap: wrap; justify-content: space-between; align-items: center; gap: 1rem; }
	.job { display: flex; flex-wrap: wrap; align-items: center; gap: 0.5rem 1rem; padding: 0.4rem 0; border-top: 1px solid var(--border); }
	.job .name { min-width: 8rem; }
	.buttons { display: flex; gap: 0.4rem; margin-left: auto; }
	.form { display: grid; gap: 0.8rem; }
	.form label { display: grid; gap: 0.3rem; }
	.row { display: flex; flex-wrap: wrap; gap: 0.8rem 1.2rem; }
	.narrow { width: min(100%, 14rem); min-width: 0; }
	.narrow .field { width: 100%; }
	.check { display: flex !important; align-items: center; gap: 0.5rem; }
	.small { font-size: 0.8125rem; }
	h3 { margin: 1rem 0 0.3rem; font-size: 0.95rem; }
	textarea { width: 100%; box-sizing: border-box; }
</style>
