<script lang="ts">
	// Backups of shares to another volume, another server or the cloud.
	import { api, ApiError } from '#lib/api.ts';
	import ConfirmDialog from '#lib/components/ConfirmDialog.svelte';
	import Dialog from '#lib/components/Dialog.svelte';
	import { formatBytes, formatDateTime } from '#lib/format.ts';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';

	type Kind = 'local' | 's3' | 'sftp' | 'rest';
	type Destination = {
		type: Kind;
		volume?: string;
		folder?: string;
		endpoint?: string;
		bucket?: string;
		prefix?: string;
		accessKey?: string;
		secretKey?: string;
		host?: string;
		port?: number;
		user?: string;
		path?: string;
		url?: string;
		username?: string;
		password?: string;
	};
	type Job = { id: string; sources: string[]; destination: Destination; enabled: boolean; every: 'daily' | 'weekly'; weekday: number; time: string; keep: { daily: number; weekly: number; monthly: number } };
	type State = { running: boolean; phase: string; percent: number; bytesDone: number; bytesTotal: number; lastRun: string | null; lastOK: string | null; lastError: string; warning: string; bytes: number; restoredTo: string };
	type View = { job: Job; secretSet: Record<string, boolean>; state: State };
	type Overview = { jobs: View[]; shares: { name: string; volume: string }[]; volumes: string[] | null };
	type Snapshot = { id: string; time: string; paths: string[]; bytes: number };

	let overview = $state<Overview | null>(null);
	let error = $state<{ text: string; detail: string } | null>(null);
	let busy = $state(false);
	// The job being created (isNew) or changed.
	let draft = $state<Job | null>(null);
	let isNew = $state(false);
	let secretSet = $state<Record<string, boolean>>({});
	let publicKey = $state('');
	let removing = $state<Job | null>(null);
	let restoring = $state<{ job: Job; snapshots: Snapshot[] | null; snapshot: string; volume: string; target: string } | null>(null);
	let secret = $state<{ job: Job; password: string; asking: boolean } | null>(null);

	function problem(err: unknown) {
		const code = err instanceof ApiError ? err.code : 'internal';
		error = { text: errorText(code), detail: ['invalid_backup_job', 'backup_job_failed'].includes(code) && err instanceof ApiError ? err.message.replace(/^backup: invalid job: /, '') : '' };
	}

	async function load() {
		try {
			overview = await api.get<Overview>('/backups');
		} catch (err) {
			if (!overview) problem(err);
		}
	}
	const active = $derived(overview?.jobs.some((view) => view.state.running) ?? false);
	$effect(() => {
		load();
		const timer = setInterval(() => !document.hidden && load(), active ? 2000 : 20_000);
		return () => clearInterval(timer);
	});

	const volumes = $derived(overview?.volumes ?? []);
	const WEEKDAYS = [1, 2, 3, 4, 5, 6, 0];

	function create() {
		error = null;
		isNew = true;
		secretSet = {};
		draft = { id: '', sources: [], destination: { type: 'local', volume: '', folder: '', port: 22 }, enabled: true, every: 'daily', weekday: 0, time: '02:30', keep: { daily: 7, weekly: 4, monthly: 6 } };
	}

	function edit(view: View) {
		error = null;
		isNew = false;
		secretSet = view.secretSet;
		draft = JSON.parse(JSON.stringify(view.job)) as Job;
		draft.destination = { port: 22, ...draft.destination, secretKey: '', password: '' };
		if (draft.destination.type === 'sftp') showKey();
	}

	function toggleSource(name: string) {
		if (!draft) return;
		draft.sources = draft.sources.includes(name) ? draft.sources.filter((source) => source !== name) : [...draft.sources, name];
	}

	async function showKey() {
		if (publicKey) return;
		try {
			publicKey = (await api.post<{ publicKey: string }>('/backups/ssh-key')).publicKey;
		} catch (err) {
			problem(err);
		}
	}

	function setKind(kind: Kind) {
		if (!draft) return;
		draft.destination.type = kind;
		if (kind === 'sftp') showKey();
	}

	// Only what belongs to the chosen kind of destination is sent.
	function destination(d: Destination): Destination {
		switch (d.type) {
			case 'local':
				return { type: 'local', volume: d.volume, folder: d.folder };
			case 's3':
				return { type: 's3', endpoint: d.endpoint?.trim(), bucket: d.bucket?.trim(), prefix: d.prefix?.trim(), accessKey: d.accessKey?.trim(), secretKey: d.secretKey };
			case 'sftp':
				return { type: 'sftp', host: d.host?.trim(), port: Number(d.port) || 22, user: d.user?.trim(), path: d.path?.trim() };
			default:
				return { type: 'rest', url: d.url?.trim(), username: d.username?.trim(), password: d.password };
		}
	}

	async function save(event: SubmitEvent) {
		event.preventDefault();
		if (!draft) return;
		busy = true;
		error = null;
		const job = { ...draft, id: draft.id.trim().toLowerCase(), weekday: Number(draft.weekday), destination: destination(draft.destination), keep: { daily: Number(draft.keep.daily) || 0, weekly: Number(draft.keep.weekly) || 0, monthly: Number(draft.keep.monthly) || 0 } };
		try {
			overview = isNew ? await api.post<Overview>('/backups', job) : await api.put<Overview>(`/backups/${job.id}`, job);
			const created = isNew ? job : null;
			draft = null;
			// Without its password a backup is worthless: show it right away.
			if (created) showPassword(created as Job);
		} catch (err) {
			problem(err);
		}
		busy = false;
	}

	async function act(job: Job, action: 'run' | 'stop') {
		error = null;
		try {
			overview = await api.post<Overview>(`/backups/${job.id}/${action}`);
		} catch (err) {
			problem(err);
		}
	}

	async function remove() {
		if (!removing) return;
		error = null;
		try {
			overview = await api.delete<Overview>(`/backups/${removing.id}`);
		} catch (err) {
			problem(err);
		}
		removing = null;
	}

	async function showPassword(job: Job) {
		error = null;
		try {
			const res = await api.get<{ password: string }>(`/backups/${job.id}/password`);
			secret = { job, password: res.password, asking: false };
		} catch (err) {
			if (err instanceof ApiError && err.code === 'confirm_required') secret = { job, password: '', asking: true };
			else problem(err);
		}
	}

	async function openRestore(job: Job) {
		error = null;
		restoring = { job, snapshots: null, snapshot: '', volume: volumes[0] ?? '', target: '' };
		try {
			const res = await api.get<{ snapshots: Snapshot[] }>(`/backups/${job.id}/snapshots`);
			if (restoring) {
				restoring.snapshots = res.snapshots;
				restoring.snapshot = res.snapshots[0]?.id ?? '';
			}
		} catch (err) {
			problem(err);
			restoring = null;
		}
	}

	async function restore(event: SubmitEvent) {
		event.preventDefault();
		if (!restoring) return;
		busy = true;
		error = null;
		try {
			const res = await api.post<{ target: string }>(`/backups/${restoring.job.id}/restore`, { snapshot: restoring.snapshot, volume: restoring.volume });
			restoring.target = res.target;
			load();
		} catch (err) {
			problem(err);
			restoring = null;
		}
		busy = false;
	}

	function where(d: Destination): string {
		switch (d.type) {
			case 'local':
				return `/mnt/${d.volume}/${d.folder}`;
			case 's3':
				return `${d.endpoint}/${d.bucket}${d.prefix ? '/' + d.prefix : ''}`;
			case 'sftp':
				return `${d.user}@${d.host}:${d.path}`;
			default:
				return d.url ?? '';
		}
	}

	function when(job: Job): string {
		if (!job.enabled) return t('backup.manual');
		return job.every === 'weekly' ? t('backup.whenWeekly', { day: t(`backup.day.${job.weekday}` as 'backup.day.0'), time: job.time }) : t('backup.whenDaily', { time: job.time });
	}
</script>

<div class="title">
	<p class="muted crumbs"><a href="/settings">{t('nav.settings')}</a> › {t('backup.title')}</p>
	<h1>{t('backup.title')}</h1>
	<p class="muted intro">{t('backup.intro')}</p>
</div>

{#if error && !draft}<div class="notice-error" role="alert">{error.text}{#if error.detail}<pre>{error.detail}</pre>{/if}</div>{/if}

{#if overview}
	{#each overview.jobs as view (view.job.id)}
		{@const job = view.job}
		{@const state = view.state}
		<section class="tile">
			<div class="head">
				<div>
					<h2 class="name mono">{job.id}</h2>
					<p class="muted">{job.sources.join(', ')} → <span class="mono">{where(job.destination)}</span></p>
				</div>
				{#if state.running}<span class="chip info">{t(`backup.phase.${state.phase}` as 'backup.phase.backup')}</span>
				{:else if state.lastError && state.lastError !== 'cancelled'}<span class="chip error">{t('backup.failed')}</span>
				{:else if state.warning}<span class="chip warn">{t('backup.withWarning')}</span>
				{:else if state.lastOK}<span class="chip ok">{t('backup.ok')}</span>
				{:else}<span class="chip">{t('backup.never')}</span>{/if}
			</div>
			{#if state.running}
				<div class="bar" role="progressbar" aria-valuemin="0" aria-valuemax="100" aria-valuenow={Math.round(state.percent)} aria-label={t(`backup.phase.${state.phase}` as 'backup.phase.backup')}><span style:width="{state.percent}%"></span></div>
				<p class="muted mono">{state.percent.toFixed(0)} %{#if state.bytesTotal > 0}{' · '}{formatBytes(state.bytesDone)} / {formatBytes(state.bytesTotal)}{/if}</p>
			{/if}
			<dl>
				<div><dt>{t('backup.schedule')}</dt><dd>{when(job)}</dd></div>
				<div><dt>{t('backup.lastOK')}</dt><dd>{state.lastOK ? formatDateTime(new Date(state.lastOK)) : '–'}{#if state.lastOK && state.bytes > 0}{' · '}{formatBytes(state.bytes)}{/if}</dd></div>
				<div><dt>{t('backup.keeps')}</dt><dd>{job.keep.daily + job.keep.weekly + job.keep.monthly > 0 ? t('backup.keepsText', { daily: job.keep.daily, weekly: job.keep.weekly, monthly: job.keep.monthly }) : t('backup.keepsAll')}</dd></div>
			</dl>
			{#if !state.running && state.lastError && state.lastError !== 'cancelled'}<div class="notice-error">{t('backup.lastError')}<pre>{state.lastError}</pre></div>{/if}
			{#if !state.running && state.warning}<div class="notice-warn">{t('backup.warning')}<pre>{state.warning}</pre></div>{/if}
			{#if !state.running && state.restoredTo}<p class="notice-ok">{t('backup.restoredTo')} <span class="mono">{state.restoredTo}</span></p>{/if}
			<div class="row">
				{#if state.running}
					<button type="button" class="btn" onclick={() => act(job, 'stop')}>{t('backup.stop')}</button>
				{:else}
					<button type="button" class="btn btn-primary" onclick={() => act(job, 'run')}>{t('backup.run')}</button>
					<button type="button" class="btn" onclick={() => openRestore(job)}>{t('backup.restore')}</button>
					<button type="button" class="btn" onclick={() => showPassword(job)}>{t('backup.showPassword')}</button>
					<button type="button" class="btn" onclick={() => edit(view)}>{t('backup.edit')}</button>
					<button type="button" class="btn" onclick={() => (removing = job)}>{t('backup.remove')}</button>
				{/if}
			</div>
		</section>
	{:else}
		{#if !draft}<p class="muted">{t('backup.none')}</p>{/if}
	{/each}

	{#if !draft}
		{#if overview.shares.length === 0}
			<p class="notice-warn">{t('backup.noShares')}</p>
		{:else}
			<div><button type="button" class="btn btn-primary" onclick={create}>{t('backup.add')}</button></div>
		{/if}
	{/if}
{/if}

{#if draft && overview}
	<form class="tile" onsubmit={save}>
		<h2 class="tile-title">{isNew ? t('backup.add') : t('backup.editTitle', { name: draft.id })}</h2>
		{#if isNew}
			<label class="narrow"><span>{t('backup.name')}</span>
				<input class="field mono" type="text" bind:value={draft.id} pattern="[a-z][a-z0-9\-]*" maxlength="31" autocapitalize="none" spellcheck="false" required />
				<small>{t('backup.nameHint')}</small>
			</label>
		{/if}
		<fieldset>
			<legend>{t('backup.sources')}</legend>
			<div class="row">
				{#each overview.shares as share (share.name)}
					<label class="check"><input type="checkbox" checked={draft.sources.includes(share.name)} onchange={() => toggleSource(share.name)} /><span class="mono">{share.name}</span></label>
				{/each}
			</div>
		</fieldset>
		<fieldset>
			<legend>{t('backup.destination')}</legend>
			<div class="segments" role="radiogroup" aria-label={t('backup.destination')}>
				{#each ['local', 's3', 'sftp', 'rest'] as const as kind (kind)}
					<button type="button" role="radio" aria-checked={draft.destination.type === kind} onclick={() => setKind(kind)}>{t(`backup.kind.${kind}`)}</button>
				{/each}
			</div>
			<p class="muted">{t(`backup.kindHint.${draft.destination.type}`)}</p>
			{#if draft.destination.type === 'local'}
				<div class="row">
					<label class="narrow"><span>{t('backup.volume')}</span>
						<select class="field" bind:value={draft.destination.volume} required>
							<option value="" disabled>–</option>
							{#each volumes as volume (volume)}<option value={volume}>{volume}</option>{/each}
						</select>
					</label>
					<label class="narrow"><span>{t('backup.folder')}</span><input class="field mono" type="text" bind:value={draft.destination.folder} placeholder={`backups/${draft.id || 'name'}`} spellcheck="false" /></label>
				</div>
			{:else if draft.destination.type === 's3'}
				<div class="row">
					<label class="wide"><span>{t('backup.endpoint')}</span><input class="field mono" type="url" bind:value={draft.destination.endpoint} placeholder="https://s3.eu-central-1.amazonaws.com" spellcheck="false" required /></label>
					<label class="narrow"><span>{t('backup.bucket')}</span><input class="field mono" type="text" bind:value={draft.destination.bucket} spellcheck="false" autocapitalize="none" required /></label>
					<label class="narrow"><span>{t('backup.prefix')}</span><input class="field mono" type="text" bind:value={draft.destination.prefix} spellcheck="false" /></label>
				</div>
				<div class="row">
					<label class="narrow"><span>{t('backup.accessKey')}</span><input class="field mono" type="text" bind:value={draft.destination.accessKey} autocomplete="off" spellcheck="false" required /></label>
					<label class="narrow"><span>{t('backup.secretKey')}</span><input class="field mono" type="password" bind:value={draft.destination.secretKey} autocomplete="new-password" placeholder={secretSet.secretKey ? t('notify.secretStored') : ''} required={!secretSet.secretKey} /></label>
				</div>
			{:else if draft.destination.type === 'sftp'}
				<div class="row">
					<label class="narrow"><span>{t('backup.host')}</span><input class="field mono" type="text" bind:value={draft.destination.host} spellcheck="false" autocapitalize="none" required /></label>
					<label class="tiny"><span>{t('notify.port')}</span><input class="field mono" type="number" min="1" max="65535" bind:value={draft.destination.port} required /></label>
					<label class="narrow"><span>{t('notify.username')}</span><input class="field mono" type="text" bind:value={draft.destination.user} spellcheck="false" autocapitalize="none" required /></label>
					<label class="narrow"><span>{t('backup.remotePath')}</span><input class="field mono" type="text" bind:value={draft.destination.path} placeholder="backups/nas" spellcheck="false" required /></label>
				</div>
				{#if publicKey}
					<label><span>{t('backup.publicKey')}</span><textarea class="field mono key" readonly rows="2" onfocus={(event) => event.currentTarget.select()}>{publicKey}</textarea></label>
					<p class="muted">{t('backup.publicKeyHint')}</p>
				{/if}
			{:else}
				<div class="row">
					<label class="wide"><span>{t('backup.restUrl')}</span><input class="field mono" type="url" bind:value={draft.destination.url} placeholder="https://backup.example.org:8000/nas" spellcheck="false" required /></label>
					<label class="narrow"><span>{t('notify.username')}</span><input class="field mono" type="text" bind:value={draft.destination.username} autocomplete="off" spellcheck="false" /></label>
					<label class="narrow"><span>{t('notify.password')}</span><input class="field mono" type="password" bind:value={draft.destination.password} autocomplete="new-password" placeholder={secretSet.password ? t('notify.secretStored') : ''} /></label>
				</div>
			{/if}
		</fieldset>
		<fieldset>
			<legend>{t('backup.schedule')}</legend>
			<label class="check"><input type="checkbox" bind:checked={draft.enabled} /><span>{t('backup.enabled')}</span></label>
			{#if draft.enabled}
				<div class="row">
					<label class="narrow"><span>{t('backup.every')}</span>
						<select class="field" bind:value={draft.every}>
							<option value="daily">{t('backup.every.daily')}</option>
							<option value="weekly">{t('backup.every.weekly')}</option>
						</select>
					</label>
					{#if draft.every === 'weekly'}
						<label class="narrow"><span>{t('backup.weekday')}</span>
							<select class="field" bind:value={draft.weekday}>
								{#each WEEKDAYS as day (day)}<option value={day}>{t(`backup.day.${day}` as 'backup.day.0')}</option>{/each}
							</select>
						</label>
					{/if}
					<label class="tiny"><span>{t('update.autoTime')}</span><input class="field mono" type="time" bind:value={draft.time} required /></label>
				</div>
				<p class="muted">{t('backup.catchUp')}</p>
			{/if}
		</fieldset>
		<fieldset>
			<legend>{t('backup.keeps')}</legend>
			<div class="row">
				<label class="tiny"><span>{t('backup.keepDaily')}</span><input class="field mono" type="number" min="0" max="999" bind:value={draft.keep.daily} /></label>
				<label class="tiny"><span>{t('backup.keepWeekly')}</span><input class="field mono" type="number" min="0" max="999" bind:value={draft.keep.weekly} /></label>
				<label class="tiny"><span>{t('backup.keepMonthly')}</span><input class="field mono" type="number" min="0" max="999" bind:value={draft.keep.monthly} /></label>
			</div>
			<p class="muted">{t('backup.keepHint')}</p>
		</fieldset>
		{#if error}<div class="notice-error" role="alert">{error.text}{#if error.detail}<pre>{error.detail}</pre>{/if}</div>{/if}
		<div class="actions">
			<button type="button" class="btn" onclick={() => ((draft = null), (error = null))} disabled={busy}>{t('common.cancel')}</button>
			<button type="submit" class="btn btn-primary" disabled={busy || draft.sources.length === 0}>{isNew ? t('backup.create') : t('notify.save')}</button>
		</div>
	</form>
{/if}

{#if removing}
	<Dialog title={t('backup.removeTitle', { name: removing.id })} onclose={() => (removing = null)}>
		<p>{t('backup.removeText')}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (removing = null)}>{t('common.cancel')}</button>
			<button type="button" class="btn btn-danger" onclick={remove}>{t('backup.remove')}</button>
		</div>
	</Dialog>
{/if}

{#if secret?.asking}
	<ConfirmDialog reason={t('backup.passwordReason')} onconfirmed={() => secret && showPassword(secret.job)} onclose={() => (secret = null)} />
{:else if secret}
	<Dialog title={t('backup.passwordTitle', { name: secret.job.id })} onclose={() => (secret = null)}>
		<p>{t('backup.passwordText')}</p>
		<input class="field mono" type="text" readonly value={secret.password} onfocus={(event) => event.currentTarget.select()} aria-label={t('backup.passwordTitle', { name: secret.job.id })} />
		<p class="notice-warn">{t('backup.passwordWarn')}</p>
		<div class="dialog-actions"><button type="button" class="btn btn-primary" onclick={() => (secret = null)}>{t('backup.passwordDone')}</button></div>
	</Dialog>
{/if}

{#if restoring}
	{@const r = restoring}
	<Dialog title={t('backup.restoreTitle', { name: r.job.id })} locked={busy} onclose={() => (restoring = null)}>
		{#if r.target}
			<p>{t('backup.restoreStarted')}</p>
			<p class="mono">{r.target}</p>
			<p class="muted">{t('backup.restoreWhere')}</p>
			<div class="dialog-actions"><button type="button" class="btn btn-primary" onclick={() => (restoring = null)}>{t('common.done')}</button></div>
		{:else if r.snapshots === null}
			<p class="muted">{t('backup.restoreLoading')}</p>
		{:else if r.snapshots.length === 0}
			<p class="muted">{t('backup.restoreNone')}</p>
			<div class="dialog-actions"><button type="button" class="btn" onclick={() => (restoring = null)}>{t('common.cancel')}</button></div>
		{:else}
			<form class="plain" onsubmit={restore}>
				<p class="muted">{t('backup.restoreText')}</p>
				<label><span>{t('backup.restoreWhich')}</span>
					<select class="field" bind:value={r.snapshot}>
						{#each r.snapshots as snapshot (snapshot.id)}<option value={snapshot.id}>{formatDateTime(new Date(snapshot.time))}{snapshot.bytes ? ` · ${formatBytes(snapshot.bytes)}` : ''}</option>{/each}
					</select>
				</label>
				<label><span>{t('backup.restoreVolume')}</span>
					<select class="field" bind:value={r.volume} required>
						{#each volumes as volume (volume)}<option value={volume}>{volume}</option>{/each}
					</select>
				</label>
				<div class="dialog-actions">
					<button type="button" class="btn" onclick={() => (restoring = null)} disabled={busy}>{t('common.cancel')}</button>
					<button type="submit" class="btn btn-primary" disabled={busy || !r.volume}>{t('backup.restoreDo')}</button>
				</div>
			</form>
		{/if}
	</Dialog>
{/if}

<style>
	.title {
		display: flex;
		flex-direction: column;
		gap: 4px;
	}
	.crumbs {
		font-size: 0.8125rem;
	}
	.intro {
		max-width: 60rem;
		font-size: 0.875rem;
	}
	h1 {
		font-size: 1.5rem;
		font-weight: 600;
	}
	section,
	form.tile {
		display: flex;
		flex-direction: column;
		gap: 14px;
		padding: 18px;
		font-size: 0.875rem;
	}
	form.plain {
		display: flex;
		flex-direction: column;
		gap: 14px;
		font-size: 0.875rem;
	}
	p {
		margin: 0;
	}
	.head {
		display: flex;
		align-items: flex-start;
		justify-content: space-between;
		gap: 12px;
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
		gap: 12px 24px;
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
		gap: 12px 20px;
	}
	fieldset {
		display: flex;
		flex-direction: column;
		gap: 12px;
		min-width: 0;
		margin: 0;
		padding: 0;
		border: 0;
	}
	legend {
		padding: 0;
		margin-bottom: 6px;
		font-size: 0.8125rem;
		font-weight: 600;
		letter-spacing: 0.04em;
		text-transform: uppercase;
		color: var(--muted);
	}
	label {
		display: flex;
		flex-direction: column;
		gap: 6px;
		font-size: 0.8125rem;
	}
	label > span {
		color: var(--muted);
	}
	small {
		font-size: 0.75rem;
		color: var(--muted);
	}
	.narrow {
		flex: 0 1 260px;
		max-width: 320px;
	}
	.tiny {
		flex: 0 1 120px;
		max-width: 140px;
	}
	.wide {
		flex: 1 1 360px;
		max-width: 560px;
	}
	label.check {
		flex-direction: row;
		align-items: center;
		gap: 8px;
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
	.segments {
		display: inline-flex;
		flex-wrap: wrap;
		align-self: flex-start;
		gap: 4px;
		padding: 3px;
		border: 1px solid var(--panel-border);
		border-radius: var(--radius-ctl);
	}
	.segments button {
		height: 2.25rem;
		padding: 0 14px;
		background: transparent;
		border: 0;
		border-radius: var(--radius-ctl);
		color: var(--text);
		font: inherit;
		font-size: 0.8125rem;
		cursor: pointer;
	}
	.segments button[aria-checked='true'] {
		background: var(--accent);
		color: var(--on-accent);
	}
	.key {
		height: auto;
		padding: 10px 12px;
		font-size: 0.75rem;
		resize: none;
	}
	.actions {
		display: flex;
		justify-content: flex-end;
		gap: 10px;
	}
	pre {
		margin: 6px 0 0;
		font-family: var(--font-mono);
		font-size: 0.75rem;
		white-space: pre-wrap;
		overflow-wrap: anywhere;
	}
</style>
