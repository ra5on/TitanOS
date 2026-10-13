<script lang="ts">
	// Updates: a new version goes into the other slot of the stick and
	// starts on trial; the one before stays as the fallback.
	import { api, ApiError } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import PowerDialog from '#lib/components/PowerDialog.svelte';
	import { formatBytes, formatDateTime } from '#lib/format.ts';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';

	type Settings = { channel: 'stable' | 'beta'; autoInstall: boolean; installTime: string };
	type Manifest = { version: string; channel: string; kernel: string; released: string; minVersion?: string; changelog: string; files: { size: number }[] };
	type Slot = { name: string; version: string; running: boolean; complete: boolean; next: boolean; triesLeft: number | null };
	type Status = {
		version: string;
		release: boolean;
		slots: Slot[];
		settings: Settings;
		canVerify: boolean;
		available: Manifest | null;
		lastCheck: string | null;
		checkError: string;
		progress: { phase: '' | 'download' | 'switch' | 'done' | 'failed'; file: string; percent: number; speedBytesPerSecond: number; error: string };
		pending: { kind: 'update' | 'rollback'; version: string; slot: string } | null;
		canRollback: boolean;
		unconfirmed: boolean;
	};

	let status = $state<Status | null>(null);
	let draft = $state<Settings | null>(null);
	let error = $state<{ text: string; detail: string } | null>(null);
	let busy = $state<'' | 'check' | 'save' | 'action'>('');
	let asking = $state<'' | 'install' | 'rollback'>('');
	let restarting = $state(false);
	let checked = $state(false);

	function problem(err: unknown) {
		const code = err instanceof ApiError ? err.code : 'internal';
		error = { text: errorText(code), detail: ['update_failed', 'update_too_old', 'update_no_space'].includes(code) && err instanceof ApiError ? err.message : '' };
	}

	function take(res: Status) {
		status = res;
		if (!draft) draft = { ...res.settings };
	}

	async function load() {
		try {
			take(await api.get<Status>('/update'));
		} catch (err) {
			if (!status) problem(err);
		}
	}

	const working = $derived(status?.progress.phase === 'download' || status?.progress.phase === 'switch');
	$effect(() => {
		load();
		const timer = setInterval(() => !document.hidden && load(), working ? 1500 : 15_000);
		return () => clearInterval(timer);
	});

	async function act(action: 'check' | 'install' | 'rollback' | 'cancel') {
		busy = action === 'check' ? 'check' : 'action';
		error = null;
		asking = '';
		checked = false;
		try {
			take(await api.post<Status>(`/update/${action}`));
			checked = action === 'check';
		} catch (err) {
			problem(err);
			load();
		}
		busy = '';
	}

	const changed = $derived(!!status && !!draft && JSON.stringify(draft) !== JSON.stringify(status.settings));
	async function save(event: SubmitEvent) {
		event.preventDefault();
		if (!draft) return;
		busy = 'save';
		error = null;
		try {
			status = await api.put<Status>('/update/settings', draft);
			draft = { ...status.settings };
		} catch (err) {
			problem(err);
		}
		busy = '';
	}

	const size = $derived(status?.available?.files.reduce((sum, file) => sum + file.size, 0) ?? 0);
	const other = $derived(status?.slots.find((slot) => !slot.running));

	function slotState(slot: Slot): string {
		if (slot.running) return t(slot.next ? 'update.slot.running' : 'update.slot.runningNotNext');
		if (!slot.complete) return t('update.slot.empty');
		if (slot.triesLeft === 0) return t('update.slot.bad');
		return t(slot.next ? 'update.slot.next' : 'update.slot.fallback');
	}
</script>

<div class="title">
	<p class="muted crumbs"><a href="/settings">{t('nav.settings')}</a> › {t('update.title')}</p>
	<h1>{t('update.title')}</h1>
</div>

{#if error}<div class="notice-error" role="alert">{error.text}{#if error.detail}<pre>{error.detail}</pre>{/if}</div>{/if}

{#if status && draft}
	<section class="tile" aria-labelledby="upd-now">
		<div class="head">
			<h2 class="tile-title" id="upd-now">{t('update.installed')}</h2>
			{#if !status.release}<span class="chip warn">{t('update.devBuild')}</span>{/if}
		</div>
		<p class="version mono">TitanOS {status.version}</p>

		{#if status.pending}
			<div class="notice-ok" role="status">
				{t(status.pending.kind === 'rollback' ? 'update.pendingRollback' : 'update.pendingUpdate', { version: status.pending.version })}
				<div class="row">
					<button type="button" class="btn btn-primary" onclick={() => (restarting = true)}>{t('update.restartNow')}</button>
					<button type="button" class="btn" disabled={busy !== ''} onclick={() => act('cancel')}>{t('update.cancel')}</button>
				</div>
			</div>
		{:else if status.unconfirmed}
			<p class="notice-warn" role="status">{t('update.unconfirmed')}</p>
		{/if}

		{#if !status.canVerify}
			<p class="notice-warn">{t('update.noKey')}</p>
		{:else if working}
			<div class="progress">
				<div class="bar" role="progressbar" aria-valuemin="0" aria-valuemax="100" aria-valuenow={Math.round(status.progress.percent)} aria-label={t('update.downloading')}><span style:width="{status.progress.percent}%"></span></div>
				<p class="muted">
					{#if status.progress.phase === 'download'}
						{t('update.downloading')}{' · '}<span class="mono">{status.progress.percent.toFixed(0)} % · {(status.progress.speedBytesPerSecond / 1e6).toFixed(1)} MB/s</span>
					{:else}{t('update.switching')}{/if}
				</p>
			</div>
		{:else if status.progress.phase === 'failed'}
			<div class="notice-error" role="alert">{t('update.failed')}<pre>{status.progress.error}</pre></div>
		{/if}

		{#if status.available && !status.pending && !working}
			<div class="available">
				<div class="head">
					<span class="name">TitanOS {status.available.version}</span>
					<span class="chip info">{t(status.available.channel === 'beta' ? 'update.channel.beta' : 'update.channel.stable')}</span>
				</div>
				<p class="muted">{t('update.availableMeta', { date: formatDateTime(new Date(status.available.released)), kernel: status.available.kernel, size: formatBytes(size) })}</p>
				{#if status.available.changelog}<pre class="changelog">{status.available.changelog}</pre>{/if}
				<div><button type="button" class="btn btn-primary" disabled={busy !== '' || status.unconfirmed} onclick={() => (asking = 'install')}>{t('update.install')}</button></div>
			</div>
		{:else if status.canVerify && !status.pending && !working}
			<div class="row">
				<button type="button" class="btn" disabled={busy !== ''} onclick={() => act('check')}>{t(busy === 'check' ? 'update.checking' : 'update.check')}</button>
				<span class="muted" role="status">
					{#if status.checkError}{t('update.checkFailed')}
					{:else if checked}{t('update.upToDate')}
					{:else if status.lastCheck}{t('update.lastCheck', { time: formatDateTime(new Date(status.lastCheck)) })}{/if}
				</span>
			</div>
			{#if status.checkError}<pre class="muted">{status.checkError}</pre>{/if}
		{/if}
	</section>

	<section class="tile" aria-labelledby="upd-slots">
		<h2 class="tile-title" id="upd-slots">{t('update.slots')}</h2>
		<p class="muted">{t('update.slotsText')}</p>
		<ul class="slots">
			{#each status.slots as slot (slot.name)}
				<li>
					<span class="mono name">{t('update.slot', { name: slot.name.toUpperCase() })}</span>
					<span class="mono">{slot.complete || slot.running ? slot.version || '–' : '–'}</span>
					<span class="grow muted">{slotState(slot)}{#if slot.triesLeft !== null && slot.triesLeft > 0}{' · '}{t('update.slot.tries', { count: slot.triesLeft })}{/if}</span>
				</li>
			{/each}
		</ul>
		{#if status.canRollback && !status.pending && other}
			<div><button type="button" class="btn" disabled={busy !== '' || working} onclick={() => (asking = 'rollback')}>{t('update.rollback', { version: other.version || '?' })}</button></div>
		{/if}
	</section>

	<form class="tile" onsubmit={save}>
		<h2 class="tile-title">{t('update.settings')}</h2>
		<label class="narrow"><span>{t('update.channel')}</span>
			<select class="field" bind:value={draft.channel}>
				<option value="stable">{t('update.channel.stable')}</option>
				<option value="beta">{t('update.channel.beta')}</option>
			</select>
		</label>
		{#if draft.channel === 'beta'}<p class="notice-warn">{t('update.betaHint')}</p>{/if}
		<label class="check"><input type="checkbox" bind:checked={draft.autoInstall} /><span>{t('update.auto')}</span></label>
		{#if draft.autoInstall}
			<label class="narrow"><span>{t('update.autoTime')}</span><input class="field mono" type="time" bind:value={draft.installTime} required /></label>
			<p class="muted">{t('update.autoHint')}</p>
		{:else}
			<p class="muted">{t('update.manualHint')}</p>
		{/if}
		<div class="actions"><button type="submit" class="btn btn-primary" disabled={!changed || busy !== ''}>{t('notify.save')}</button></div>
	</form>
{/if}

{#if asking === 'install' && status?.available}
	<Dialog title={t('update.confirmTitle', { version: status.available.version })} onclose={() => (asking = '')}>
		<p>{t('update.confirmText', { size: formatBytes(size) })}</p>
		<p class="muted">{t('update.confirmSafe')}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (asking = '')}>{t('common.cancel')}</button>
			<button type="button" class="btn btn-primary" onclick={() => act('install')}>{t('update.install')}</button>
		</div>
	</Dialog>
{/if}

{#if asking === 'rollback' && other}
	<Dialog title={t('update.rollbackTitle', { version: other.version || '?' })} onclose={() => (asking = '')}>
		<p>{t('update.rollbackText')}</p>
		<p class="notice-warn">{t('update.rollbackWarn')}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (asking = '')}>{t('common.cancel')}</button>
			<button type="button" class="btn btn-primary" onclick={() => act('rollback')}>{t('update.rollbackDo')}</button>
		</div>
	</Dialog>
{/if}

{#if restarting}
	<PowerDialog action="restart" onclose={() => (restarting = false)} />
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
	h1 {
		font-size: 1.5rem;
		font-weight: 600;
	}
	section,
	form {
		display: flex;
		flex-direction: column;
		gap: 14px;
		padding: 18px;
		font-size: 0.875rem;
	}
	p {
		margin: 0;
		max-width: 60rem;
	}
	.head,
	.row {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 10px 14px;
	}
	.head {
		justify-content: space-between;
	}
	.notice-ok .row {
		margin-top: 10px;
	}
	.version {
		font-size: 1.125rem;
	}
	.available {
		display: flex;
		flex-direction: column;
		gap: 10px;
		padding: 14px 16px;
		border: 1px solid color-mix(in srgb, var(--accent) 45%, transparent);
		border-radius: var(--radius-ctl);
		background: color-mix(in srgb, var(--accent) 8%, transparent);
	}
	.name {
		font-weight: 600;
	}
	.available .name {
		font-size: 1rem;
	}
	.changelog {
		max-height: 18rem;
		overflow-y: auto;
		padding: 10px 12px;
		background: var(--track);
		border-radius: var(--radius-ctl);
	}
	pre {
		margin: 0;
		font-family: var(--font-mono);
		font-size: 0.75rem;
		white-space: pre-wrap;
		overflow-wrap: anywhere;
	}
	.notice-error pre {
		margin-top: 6px;
	}
	.progress {
		display: flex;
		flex-direction: column;
		gap: 8px;
	}
	.slots {
		margin: 0;
		padding: 0;
		list-style: none;
		display: flex;
		flex-direction: column;
		gap: 8px;
	}
	.slots li {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 6px 18px;
		padding: 10px 12px;
		border: 1px solid var(--panel-border);
		border-radius: var(--radius-ctl);
	}
	.slots .name {
		min-width: 4.5rem;
	}
	.grow {
		flex: 1 1 200px;
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
	.narrow {
		max-width: 320px;
	}
	label.check {
		flex-direction: row;
		align-items: center;
		gap: 10px;
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
	.actions {
		display: flex;
		justify-content: flex-end;
	}
</style>
