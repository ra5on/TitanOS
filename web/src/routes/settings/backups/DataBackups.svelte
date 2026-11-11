<script lang="ts">
	import { poll } from '#lib/poll.ts';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import StatusBar from '#lib/components/explorer/StatusBar.svelte';
	import ProbeDialog from '#lib/operations/ProbeDialog.svelte';
	import RecoveryPanel from '#lib/operations/RecoveryPanel.svelte';
	let probing = $state('');
	import { revealTarget } from '#lib/reveal-target.ts';
	let { restoreOnly = false }: { restoreOnly?: boolean } = $props();
	// Backups of shares to another volume, another server or the cloud.
	import { api, ApiError } from '#lib/api.ts';
	import ConfirmDialog from '#lib/components/ConfirmDialog.svelte';
	import Dialog from '#lib/components/Dialog.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import { formatDateTime } from '#lib/format.ts';
	import { lastOf } from '#lib/components/KeepVersions.svelte';
	import {
		editableJob,
		newJob,
		plainCopy,
		stateChip,
		when,
		where,
		type Job,
		type Overview,
		type UsbDrive,
		type View,
	} from './data-backups.ts';
	import JobForm from './JobForm.svelte';
	import JobTile from './JobTile.svelte';
	import RestoreDialog from './RestoreDialog.svelte';
	import SmbTargets from './SmbTargets.svelte';
	import TargetPanel from './TargetPanel.svelte';
	import ReplicationPanel from './ReplicationPanel.svelte';
	import { apiErrorText, errorText, t } from '#lib/i18n/index.svelte.ts';

	let usbDrives = $state<UsbDrive[]>([]);
	let overview = $state<Overview | null>(null);
	let error = $state<{ text: string; detail: string } | null>(null);
	let busy = $state(false);
	let loadError = $state('');
	$effect(() => {
		if (feedback) {
			const timer = setTimeout(() => (feedback = ''), 5000);
			return () => clearTimeout(timer);
		}
	});
	let pending = $state<Record<string, string>>({});
	let feedback = $state('');
	let retry = $state<{ job: Job; action: 'run' | 'stop' } | null>(null);
	// The job being created (isNew) or changed; the form starts over each time one is opened.
	let draft = $state<Job | null>(null);
	let isNew = $state(false);
	let opened = $state(0);
	let secretSet = $state<Record<string, boolean>>({});
	let publicKey = $state('');
	let removing = $state<Job | null>(null);
	let restoring = $state<Job | null>(null);
	let checking = $state<{ job: Job; readData: number } | null>(null);
	let secret = $state<{ job: Job; password: string; asking: boolean } | null>(null);

	function problem(err: unknown) {
		const code = err instanceof ApiError ? err.code : 'internal';
		error = {
			text: errorText(code),
			detail:
				['invalid_backup_job', 'backup_job_failed'].includes(code) && err instanceof ApiError
					? err.message.replace(/^backup: invalid job: /, '')
					: '',
		};
	}

	// Answers can overtake each other: a look that started before a run was
	// accepted must not put back the state from before it.
	let generation = 0;
	function apply(next: Overview) {
		generation++;
		overview = next;
	}
	async function load() {
		const mine = ++generation;
		try {
			const next = await api.get<Overview>('/backups');
			if (mine !== generation) return;
			overview = next;
			loadError = '';
		} catch (err) {
			if (mine !== generation) return;
			loadError = apiErrorText(err);
		}
	}
	// USB drives are only needed by the form; a drive list that cannot be
	// read does not make the page look broken.
	let usbError = $state('');
	async function loadUSB() {
		usbError = '';
		try {
			usbDrives = await api.get<typeof usbDrives>('/usb');
		} catch (err) {
			usbError = apiErrorText(err);
		}
	}
	const active = $derived(overview?.jobs.some((view) => view.state.running) ?? false);
	$effect(() => {
		const fast = active;
		return poll(load, { interval: fast ? 2000 : 20_000 });
	});

	const volumes = $derived(overview?.volumes ?? []);

	function create() {
		error = null;
		isNew = true;
		secretSet = {};
		draft = newJob();
		opened++;
		loadUSB();
	}

	function edit(view: View) {
		error = null;
		isNew = false;
		secretSet = view.secretSet;
		draft = editableJob(view.job, lastOf);
		opened++;
		loadUSB();
		if (draft.destination.type === 'sftp') showKey();
	}
	async function showKey() {
		if (publicKey) return;
		try {
			publicKey = (await api.post<{ publicKey: string }>('/backups/ssh-key')).publicKey;
		} catch (err) {
			problem(err);
		}
	}

	function saved(next: Overview, created: Job | null) {
		apply(next);
		feedback = t('ui.saved');
		draft = null;
		// Without its password a backup is worthless: show it right away.
		if (created && !plainCopy(created.destination.type)) showPassword(created);
	}

	async function act(job: Job, action: 'run' | 'stop') {
		if (pending[job.id]) return;
		pending[job.id] = action;
		feedback = '';
		retry = null;
		error = null;
		try {
			apply(await api.post<Overview>(`/backups/${job.id}/${action}`));
			feedback = t('ui.backupAccepted', { name: job.id });
		} catch (err) {
			problem(err);
			retry = { job, action };
		} finally {
			delete pending[job.id];
		}
	}

	let removeError = $state('');
	async function remove() {
		if (!removing || busy) return;
		busy = true;
		removeError = '';
		try {
			apply(await api.delete<Overview>(`/backups/${removing.id}`));
			removing = null;
		} catch (err) {
			removeError = apiErrorText(err);
		}
		busy = false;
	}

	async function showPassword(job: Job) {
		error = null;
		const confirmed = secret?.asking === true;
		try {
			const res = await api.get<{ password: string }>(`/backups/${job.id}/password`);
			secret = { job, password: res.password, asking: false };
		} catch (err) {
			// Asked once; a second "confirm" answer or any other failure closes
			// the dialog instead of leaving it locked.
			if (!confirmed && err instanceof ApiError && err.code === 'confirm_required')
				secret = { job, password: '', asking: true };
			else {
				secret = null;
				problem(err);
			}
		}
	}
	let copiedPassword = $state(false);
	async function copyPassword() {
		if (!secret) return;
		try {
			await navigator.clipboard.writeText(secret.password);
			copiedPassword = true;
		} catch {
			// The password is shown and can be selected by hand.
		}
	}

	let dialogError = $state('');
	function openRestore(job: Job) {
		error = null;
		restoring = job;
	}

	async function check(event: SubmitEvent) {
		event.preventDefault();
		if (!checking || busy) return;
		busy = true;
		dialogError = '';
		try {
			await api.post(`/backups/${checking.job.id}/check`, { readData: Number(checking.readData) });
			checking = null;
			load();
		} catch (err) {
			dialogError = apiErrorText(err);
		}
		busy = false;
	}

	// Style W4 (owner 9.11.2026): the backups as a table; the chosen one's
	// tile is its detail. A link to #backup-<id> chooses that one.
	let picked = $state('');
	$effect(() => {
		const fromHash = () => {
			const m = /^#backup-(.+)$/.exec(location.hash);
			if (m) picked = decodeURIComponent(m[1]);
		};
		fromHash();
		addEventListener('hashchange', fromHash);
		return () => removeEventListener('hashchange', fromHash);
	});
	const chosenId = $derived(
		overview?.jobs.some((v) => v.job.id === picked) ? picked : (overview?.jobs[0]?.job.id ?? ''),
	);
	// PLAN 67-1: the menu bar of the desktop (Sicherung › Ablage). "?new=1"
	// comes from the menu on the overview: a new job opens once the jobs are here.
	import { registerMenuCommands } from '#lib/menu-commands.svelte.ts';
	import { page } from '$app/state';
	import { replaceState } from '$app/navigation';
	import { untrack } from 'svelte';
	const canCreate = () => !restoreOnly && !!overview && !draft && overview.shares.length > 0;
	$effect(() => {
		if (page.url.searchParams.get('new') !== '1' || !canCreate()) return;
		untrack(() => {
			try {
				replaceState(location.pathname + location.hash, page.state);
			} catch {
				// The address keeps the parameter; nothing opens twice (draft is set).
			}
			create();
		});
	});
	registerMenuCommands({
		'backup.new': { run: create, enabled: canCreate },
		'backup.now': {
			items: () =>
				restoreOnly
					? []
					: (overview?.jobs ?? [])
							.filter((v) => !v.state.running && !pending[v.job.id])
							.map((v) => ({ id: v.job.id, text: v.job.id })),
			run: (id) => {
				const view = overview?.jobs.find((v) => v.job.id === id);
				if (view) void act(view.job, 'run');
			},
		},
	});
</script>

{#if !restoreOnly}<PageHead title={t('safeguard.data')}>
		<p class="muted">{t('backup.intro')}</p>
	</PageHead>{:else}<h2 class="sgroup-title" id="restore-files" use:revealTarget>
		{t('safeguard.filesRestore')}
	</h2>{/if}

{#if error && !draft}<div class="notice-error" role="alert">
		{error.text}{#if error.detail}<pre>{error.detail}</pre>{/if}
	</div>{/if}

{#if loadError}<div class="notice-error" role="alert">
		{loadError} <button class="btn" onclick={load}>{t('ui.retry')}</button>
	</div>{:else if !overview}<p class="loading" role="status">{t('common.loading')}</p>{/if}
{#if feedback}<p class="notice-ok" role="status">{feedback}</p>{/if}
{#if retry && error}<button
		type="button"
		class="btn"
		onclick={() => retry && act(retry.job, retry.action)}>{t('ui.retry')}</button
	>{/if}

{#if overview}
	{#if !restoreOnly}<RecoveryPanel />{/if}
	<!-- PLAN 78-1: the targets are rows of a group; the jobs stay a table with
	     the chosen job's detail below it. -->
	{#if !restoreOnly}<div id="backup-targets" use:revealTarget>
			<SettingGroup title={t('safeguard.targets')}>
				<SmbTargets targets={overview.smbTargets} onchange={load} /><TargetPanel /><ReplicationPanel
				/>
			</SettingGroup>
		</div>{/if}
	{#if !restoreOnly}<h2 class="sgroup-title" id="backup-history" use:revealTarget>
			{t('safeguard.history')}
		</h2>{/if}
	{#if !restoreOnly && overview.jobs.length}
		<div class="table-wrap">
			<table class="data-table backup-table">
				<thead
					><tr
						><th>{t('backup.col.name')}</th><th>{t('backup.col.route')}</th><th
							>{t('backup.col.state')}</th
						><th>{t('backup.schedule')}</th><th>{t('safeguard.lastRun')}</th><th
							>{t('backup.next')}</th
						></tr
					></thead
				>
				<tbody>
					{#each overview.jobs as view (view.job.id)}
						{@const chip = stateChip(view)}
						<tr
							class:selected={view.job.id === chosenId}
							aria-selected={view.job.id === chosenId}
							onclick={() => (picked = view.job.id)}
						>
							<td
								><button type="button" class="link mono" onclick={() => (picked = view.job.id)}
									>{view.job.id}</button
								></td
							>
							<td class="route muted"
								>{[...view.job.sources, ...(view.job.applications ?? []).map((a) => a.name)].join(
									', ',
								)} → <span class="mono">{where(view.job.destination)}</span></td
							>
							<td><span class="chip {chip.tone}">{chip.text}</span></td>
							<td>{when(view.job)}</td>
							<td class="nowrap"
								>{view.state.lastRun ? formatDateTime(new Date(view.state.lastRun)) : '–'}</td
							>
							<td class="nowrap">{view.next ? formatDateTime(new Date(view.next)) : '–'}</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{/if}
	{#each overview.jobs as view (view.job.id)}
		<JobTile
			{view}
			hidden={!restoreOnly && view.job.id !== chosenId}
			{restoreOnly}
			pending={pending[view.job.id]}
			{act}
			{openRestore}
			{showPassword}
			{edit}
			onprobe={() => (probing = view.job.id)}
			oncheck={() => ((checking = { job: view.job, readData: 0 }), (dialogError = ''))}
			onremove={() => ((removing = view.job), (removeError = ''))}
		/>
	{:else}
		{#if !draft}<p class="muted">{t('backup.none')}</p>{/if}
	{/each}
	{#if !restoreOnly && overview.jobs.length}<StatusBar label={t('backup.statusBar')}>
			<span>{t('backup.statJobs', { count: overview.jobs.length })}</span>
			<span
				>{t('backup.statRunning')}
				<b>{overview.jobs.filter((v) => v.state.running).length}</b></span
			>
			<span
				class:warn={overview.jobs.some(
					(v) => v.state.lastError && v.state.lastError !== 'cancelled',
				)}
				>{t('backup.statFailed')}
				<b
					>{overview.jobs.filter((v) => v.state.lastError && v.state.lastError !== 'cancelled')
						.length}</b
				></span
			>
			{#if overview.jobs.some((v) => v.state.lastOK)}<span class="end"
					>{t('backup.statLastOk')}
					{formatDateTime(
						new Date(
							overview.jobs
								.map((v) => v.state.lastOK ?? '')
								.filter(Boolean)
								.sort()
								.at(-1) ?? '',
						),
					)}</span
				>{/if}
		</StatusBar>{/if}

	{#if !draft && !restoreOnly}
		{#if overview.shares.length === 0}
			<p class="notice-warn">{t('backup.noShares')}</p>
		{:else}
			<div>
				<button type="button" class="btn btn-primary" onclick={create}>{t('backup.add')}</button>
			</div>
		{/if}
	{/if}
{/if}

{#if draft && overview}
	<JobForm
		bind:draft={() => draft as Job, (v) => (draft = v)}
		bind:error
		bind:busy
		{opened}
		{isNew}
		{secretSet}
		{overview}
		{volumes}
		{usbDrives}
		{usbError}
		{loadUSB}
		{publicKey}
		{showKey}
		{problem}
		onsaved={saved}
		oncancel={() => ((draft = null), (error = null))}
	/>
{/if}

{#if removing}
	<Dialog
		title={t('backup.removeTitle', { name: removing.id })}
		locked={busy}
		onclose={() => (removing = null)}
	>
		<p>{t('backup.removeText')}</p>
		{#if removeError}<p class="notice-error" role="alert">{removeError}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={busy} onclick={() => (removing = null)}
				>{t('common.cancel')}</button
			>
			<button type="button" class="btn btn-danger" disabled={busy} onclick={remove}
				>{t('backup.remove')}</button
			>
		</div>
	</Dialog>
{/if}

{#if secret?.asking}
	<ConfirmDialog
		reason={t('backup.passwordReason')}
		onconfirmed={() => secret && showPassword(secret.job)}
		onclose={() => (secret = null)}
	/>
{:else if secret}
	<Dialog
		title={t('backup.passwordTitle', { name: secret.job.id })}
		onclose={() => ((secret = null), (copiedPassword = false))}
	>
		<p>{t('backup.passwordText')}</p>
		<input
			class="field mono"
			type="text"
			readonly
			value={secret.password}
			onfocus={(event) => event.currentTarget.select()}
			aria-label={t('backup.passwordTitle', { name: secret.job.id })}
		/>
		<p class="notice-warn">{t('backup.passwordWarn')}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={copyPassword}
				>{copiedPassword ? t('backup.targetCopied') : t('backup.targetCopy')}</button
			>
			<button type="button" class="btn btn-primary" onclick={() => (secret = null)}
				>{t('backup.passwordDone')}</button
			>
		</div>
	</Dialog>
{/if}

{#if restoring}
	<RestoreDialog
		job={restoring.id}
		{volumes}
		onclose={() => (restoring = null)}
		onfailed={(err) => {
			problem(err);
			restoring = null;
		}}
		onstarted={load}
	/>
{/if}

{#if checking}
	{@const c = checking}
	<Dialog
		title={t('backup.checkTitle', { name: c.job.id })}
		locked={busy}
		onclose={() => (checking = null)}
	>
		<form class="plain" onsubmit={check}>
			<p class="muted">{t('backup.checkText')}</p>
			<label
				><span>{t('backup.checkRead')}</span>
				<select class="field" bind:value={c.readData}>
					{#each [0, 5, 25, 100] as share}<option value={share}
							>{t(share === 0 ? 'backup.checkStructure' : 'backup.checkShare', { share })}</option
						>{/each}
				</select>
			</label>
			{#if dialogError}<p class="notice-error" role="alert">{dialogError}</p>{/if}
			<div class="dialog-actions">
				<button type="button" class="btn" onclick={() => (checking = null)} disabled={busy}
					>{t('common.cancel')}</button
				>
				<button type="submit" class="btn btn-primary" disabled={busy}>{t('backup.checkDo')}</button>
			</div>
		</form>
	</Dialog>
{/if}

{#if probing}<ProbeDialog
		job={probing}
		onclose={() => (probing = '')}
		onstarted={() => {
			probing = '';
			load();
		}}
	/>{/if}

<style>
	form.plain {
		display: flex;
		flex-direction: column;
		gap: 14px;
		font-size: 0.875rem;
	}
	p {
		margin: 0;
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
	.link {
		background: none;
		border: 0;
		color: inherit;
		cursor: pointer;
		padding: 0 0 0 var(--space-1);
	}
	pre {
		margin: var(--space-1-5) 0 0;
		font-family: var(--font-mono);
		font-size: 0.75rem;
		white-space: pre-wrap;
		overflow-wrap: anywhere;
	}
	.backup-table tbody tr {
		cursor: pointer;
	}
	.backup-table tr.selected td {
		background: color-mix(in srgb, var(--accent) 12%, transparent);
	}
	.backup-table .link {
		padding: 0;
		border: 0;
		background: none;
		color: var(--text);
		font-weight: 600;
		cursor: pointer;
	}
	.backup-table .route {
		overflow-wrap: anywhere;
	}
	.backup-table .nowrap {
		white-space: nowrap;
	}
	@media (max-width: 760px) {
		.backup-table thead {
			display: none;
		}
		.backup-table tr {
			display: grid;
			gap: 2px;
			padding: var(--space-2) 0;
			border-bottom: 1px solid var(--border);
		}
		.backup-table td {
			padding: 0 var(--space-2);
			border: 0;
		}
		.backup-table tr.selected {
			background: color-mix(in srgb, var(--accent) 12%, transparent);
		}
		.backup-table tr.selected td {
			background: none;
		}
	}
</style>
