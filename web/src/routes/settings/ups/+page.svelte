<script lang="ts">
	// The uninterruptible power supply: on battery the NAS shuts down
	// cleanly before it is empty.
	import { api, ApiError } from '#lib/api.ts';
	import { formatDuration } from '#lib/format.ts';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';
	import { system } from '#lib/system.svelte.ts';

	type Mode = 'off' | 'local' | 'network';
	type Settings = { mode: Mode; driver?: string; port?: string; serve: boolean; remotePassword?: string; host?: string; name?: string; user?: string; password?: string; shutdownAfter: number };
	type Status = { mode: Mode; connected: boolean; onBattery: boolean; lowBattery: boolean; raw: string; charge: number; runtime: number; load: number; model: string; error: string; since: string | null; shuttingDown: boolean };
	type Answer = { settings: Settings; passwordSet: boolean; status: Status; drivers: string[] };
	type Found = { driver: string; port: string; product: string };

	let answer = $state<Answer | null>(null);
	let draft = $state<Settings | null>(null);
	let byTime = $state(false);
	let error = $state<{ text: string; detail: string } | null>(null);
	let busy = $state(false);
	let found = $state<Found[] | null>(null);
	let saved = $state(false);

	function problem(err: unknown) {
		const code = err instanceof ApiError ? err.code : 'internal';
		error = { text: errorText(code), detail: code === 'invalid_ups_settings' && err instanceof ApiError ? err.message.replace(/^ups: invalid setting: /, '') : '' };
	}

	function take(res: Answer, keepDraft = false) {
		answer = res;
		if (!draft || !keepDraft) {
			draft = { ...res.settings, driver: res.settings.driver || 'usbhid-ups', port: res.settings.port || 'auto', password: '' };
			byTime = res.settings.shutdownAfter > 0;
			if (!byTime) draft.shutdownAfter = 5;
		}
	}

	async function load() {
		try {
			take(await api.get<Answer>('/ups'), true);
		} catch (err) {
			if (!answer) problem(err);
		}
	}
	$effect(() => {
		load();
		const timer = setInterval(() => !document.hidden && load(), 5000);
		return () => clearInterval(timer);
	});

	async function scan() {
		busy = true;
		error = null;
		try {
			found = (await api.post<{ found: Found[] }>('/ups/scan')).found;
		} catch (err) {
			problem(err);
		}
		busy = false;
	}

	function use(device: Found) {
		if (!draft) return;
		draft.driver = device.driver;
		draft.port = device.port;
	}

	async function save(event: SubmitEvent) {
		event.preventDefault();
		if (!draft) return;
		busy = true;
		error = null;
		saved = false;
		try {
			take(await api.put<Answer>('/ups', { ...draft, shutdownAfter: byTime ? Number(draft.shutdownAfter) || 1 : 0 }));
			saved = true;
		} catch (err) {
			problem(err);
		}
		busy = false;
	}

	const usb = $derived(draft?.driver === 'usbhid-ups' || draft?.driver === 'blazer_usb');
	const host = $derived(system.info?.network?.ipv4.split('/')[0] || system.info?.hostname || '');
	const status = $derived(answer?.status);
</script>

<div class="title">
	<p class="muted crumbs"><a href="/settings">{t('nav.settings')}</a> › {t('ups.title')}</p>
	<h1>{t('ups.title')}</h1>
	<p class="muted intro">{t('ups.intro')}</p>
</div>

{#if error}<div class="notice-error" role="alert">{error.text}{#if error.detail}<pre>{error.detail}</pre>{/if}</div>{/if}

{#if status && status.mode !== 'off'}
	<section class="tile" aria-labelledby="ups-now">
		<div class="head">
			<h2 class="tile-title" id="ups-now">{t('ups.state')}</h2>
			{#if !status.connected}<span class="chip warn">{t('ups.notConnected')}</span>
			{:else if status.lowBattery}<span class="chip error">{t('ups.low')}</span>
			{:else if status.onBattery}<span class="chip warn">{t('ups.onBattery')}</span>
			{:else}<span class="chip ok">{t('ups.online')}</span>{/if}
		</div>
		{#if status.connected}
			{#if status.charge >= 0}
				<div class="bar" role="meter" aria-valuemin="0" aria-valuemax="100" aria-valuenow={status.charge} aria-label={t('ups.charge')}><span style:width="{status.charge}%"></span></div>
			{/if}
			<dl>
				<div><dt>{t('ups.model')}</dt><dd>{status.model || '–'}</dd></div>
				<div><dt>{t('ups.charge')}</dt><dd class="mono">{status.charge >= 0 ? `${status.charge} %` : '–'}</dd></div>
				<div><dt>{t('ups.runtime')}</dt><dd>{status.runtime >= 0 ? formatDuration(status.runtime) : '–'}</dd></div>
				<div><dt>{t('ups.load')}</dt><dd class="mono">{status.load >= 0 ? `${status.load} %` : '–'}</dd></div>
			</dl>
			{#if status.shuttingDown}<p class="notice-error" role="alert">{t('ups.shuttingDown')}</p>{/if}
		{:else if status.error}
			<pre class="muted">{status.error}</pre>
		{/if}
	</section>
{/if}

{#if draft && answer}
	<form class="tile" onsubmit={save}>
		<h2 class="tile-title">{t('ups.settings')}</h2>
		<div class="segments" role="radiogroup" aria-label={t('ups.settings')}>
			{#each ['off', 'local', 'network'] as const as mode (mode)}
				<button type="button" role="radio" aria-checked={draft.mode === mode} onclick={() => draft && (draft.mode = mode)}>{t(`ups.mode.${mode}`)}</button>
			{/each}
		</div>
		<p class="muted">{t(`ups.modeHint.${draft.mode}`)}</p>

		{#if draft.mode === 'local'}
			<div class="row">
				<button type="button" class="btn" disabled={busy} onclick={scan}>{t('ups.scan')}</button>
				{#if found && found.length === 0}<span class="muted">{t('ups.scanNone')}</span>{/if}
			</div>
			{#each found ?? [] as device (device.driver + device.port + device.product)}
				<div class="found">
					<span>{device.product || device.driver} <span class="muted mono">{device.driver} · {device.port}</span></span>
					<button type="button" class="btn small" onclick={() => use(device)}>{t('ups.use')}</button>
				</div>
			{/each}
			<div class="row">
				<label class="narrow"><span>{t('ups.driver')}</span>
					<select class="field mono" bind:value={draft.driver}>
						{#each answer.drivers as driver (driver)}<option value={driver}>{driver}</option>{/each}
					</select>
				</label>
				{#if !usb}
					<label class="narrow"><span>{t('ups.port')}</span><input class="field mono" type="text" bind:value={draft.port} placeholder="/dev/ttyS0" spellcheck="false" required /></label>
				{/if}
			</div>
			<label class="check"><input type="checkbox" bind:checked={draft.serve} /><span>{t('ups.serve')}</span></label>
			{#if draft.serve && answer.settings.serve && answer.settings.remotePassword}
				<dl class="share">
					<div><dt>{t('ups.serveHost')}</dt><dd class="mono">{host}</dd></div>
					<div><dt>{t('ups.serveName')}</dt><dd class="mono">ups</dd></div>
					<div><dt>{t('notify.username')}</dt><dd class="mono">monitor</dd></div>
					<div><dt>{t('notify.password')}</dt><dd class="mono">{answer.settings.remotePassword}</dd></div>
				</dl>
				<p class="muted">{t('ups.serveHint')}</p>
			{:else if draft.serve}
				<p class="muted">{t('ups.serveAfterSave')}</p>
			{/if}
		{:else if draft.mode === 'network'}
			<div class="row">
				<label class="narrow"><span>{t('ups.server')}</span><input class="field mono" type="text" bind:value={draft.host} spellcheck="false" autocapitalize="none" required /></label>
				<label class="narrow"><span>{t('ups.name')}</span><input class="field mono" type="text" bind:value={draft.name} placeholder="ups" spellcheck="false" autocapitalize="none" required /></label>
			</div>
			<div class="row">
				<label class="narrow"><span>{t('notify.username')}</span><input class="field mono" type="text" bind:value={draft.user} autocomplete="off" spellcheck="false" autocapitalize="none" required /></label>
				<label class="narrow"><span>{t('notify.password')}</span><input class="field mono" type="password" bind:value={draft.password} autocomplete="new-password" placeholder={answer.passwordSet ? t('notify.secretStored') : ''} required={!answer.passwordSet} /></label>
			</div>
		{/if}

		{#if draft.mode !== 'off'}
			<fieldset>
				<legend>{t('ups.when')}</legend>
				<label class="check"><input type="radio" name="when" checked={!byTime} onchange={() => (byTime = false)} /><span>{t('ups.whenLow')}</span></label>
				<label class="check"><input type="radio" name="when" checked={byTime} onchange={() => (byTime = true)} /><span>{t('ups.whenTime')}</span>
					<input class="field mono minutes" type="number" min="1" max="240" bind:value={draft.shutdownAfter} disabled={!byTime} aria-label={t('ups.minutes')} /><span>{t('ups.minutes')}</span>
				</label>
				<p class="muted">{t('ups.whenHint')}</p>
			</fieldset>
		{/if}
		<div class="actions">
			{#if saved}<span class="muted" role="status">{t('notify.saved')}</span>{/if}
			<button type="submit" class="btn btn-primary" disabled={busy}>{t('notify.save')}</button>
		</div>
	</form>
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
	.head {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 12px;
	}
	dl {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
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
	.share {
		padding: 12px 14px;
		border: 1px solid var(--panel-border);
		border-radius: var(--radius-ctl);
	}
	.row {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 12px 20px;
	}
	.found {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: 8px 14px;
		padding: 10px 12px;
		border: 1px solid var(--panel-border);
		border-radius: var(--radius-ctl);
	}
	.small {
		height: 2rem;
		padding: 0 12px;
		font-size: 0.8125rem;
	}
	fieldset {
		display: flex;
		flex-direction: column;
		gap: 10px;
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
	.narrow {
		flex: 0 1 280px;
		max-width: 320px;
	}
	label.check {
		flex-direction: row;
		flex-wrap: wrap;
		align-items: center;
		gap: 8px;
		font-size: 0.875rem;
	}
	.check > span {
		color: var(--text);
	}
	.check input[type='checkbox'],
	.check input[type='radio'] {
		width: 18px;
		height: 18px;
		margin: 0;
	}
	.minutes {
		width: 6rem;
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
	.actions {
		display: flex;
		align-items: center;
		justify-content: flex-end;
		gap: 12px;
	}
	pre {
		margin: 0;
		font-family: var(--font-mono);
		font-size: 0.75rem;
		white-space: pre-wrap;
	}
</style>
