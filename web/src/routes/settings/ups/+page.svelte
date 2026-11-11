<script lang="ts">
	// The uninterruptible power supply: on battery the NAS shuts down
	// cleanly before it is empty.
	import { poll } from '#lib/poll.ts';
	import { api, ApiError } from '#lib/api.ts';
	import PageHead from '#lib/components/PageHead.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import SettingSub from '#lib/components/settings/SettingSub.svelte';
	import { formatDuration } from '#lib/format.ts';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';
	import { system } from '#lib/system.svelte.ts';

	type Mode = 'off' | 'local' | 'network';
	type Settings = {
		mode: Mode;
		driver?: string;
		port?: string;
		serve: boolean;
		remotePassword?: string;
		host?: string;
		name?: string;
		user?: string;
		password?: string;
		shutdownAfter: number;
	};
	type Status = {
		mode: Mode;
		connected: boolean;
		onBattery: boolean;
		lowBattery: boolean;
		raw: string;
		charge: number;
		runtime: number;
		load: number;
		model: string;
		error: string;
		since: string | null;
		shuttingDown: boolean;
	};
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
		error = {
			text: errorText(code),
			detail:
				code === 'invalid_ups_settings' && err instanceof ApiError
					? err.message.replace(/^ups: invalid setting: /, '')
					: '',
		};
	}

	function take(res: Answer, keepDraft = false) {
		answer = res;
		if (!draft || !keepDraft) {
			draft = {
				...res.settings,
				driver: res.settings.driver || 'usbhid-ups',
				port: res.settings.port || 'auto',
				password: '',
			};
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
	$effect(() => poll(load, { interval: 5000 }));

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
			take(
				await api.put<Answer>('/ups', {
					...draft,
					shutdownAfter: byTime ? Number(draft.shutdownAfter) || 1 : 0,
				}),
			);
			saved = true;
		} catch (err) {
			problem(err);
		}
		busy = false;
	}

	const usb = $derived(draft?.driver === 'usbhid-ups' || draft?.driver === 'blazer_usb');
	const host = $derived(system.info?.network?.ipv4.split('/')[0] || system.info?.hostname || '');
	const status = $derived(answer?.status);
	// The parts of the connection row stand open: they are what is set here.
	let modeOpen = $state(true);
	let whenOpen = $state(true);
</script>

<PageHead title={t('ups.title')}>
	<p class="muted">{t('ups.intro')}</p>
</PageHead>

{#if error}<div class="notice-error" role="alert">
		{error.text}{#if error.detail}<pre>{error.detail}</pre>{/if}
	</div>{/if}

{#if status && status.mode !== 'off'}
	<!-- PLAN 78-1: the state as rows with their value at the right. -->
	<SettingGroup title={t('ups.state')} id="ups-now">
		<SettingRow icon="battery" title={t('ups.model')} summary={status.model || '–'}>
			{#snippet description()}{#if !status.connected && status.error}<pre>{status.error}</pre>{/if}{/snippet}
			{#snippet control()}
				{#if !status.connected}<span class="chip warn">{t('ups.notConnected')}</span>
				{:else if status.lowBattery}<span class="chip error">{t('ups.low')}</span>
				{:else if status.onBattery}<span class="chip warn">{t('ups.onBattery')}</span>
				{:else}<span class="chip ok">{t('ups.online')}</span>{/if}
			{/snippet}
		</SettingRow>
		{#if status.connected}
			<SettingRow icon="zap" title={t('ups.charge')}>
				{#snippet control()}
					{#if status.charge >= 0}
						<div
							class="bar"
							role="meter"
							aria-valuemin="0"
							aria-valuemax="100"
							aria-valuenow={status.charge}
							aria-label={t('ups.charge')}
						>
							<span style:width="{status.charge}%"></span>
						</div>
					{/if}
					<span class="mono value">{status.charge >= 0 ? `${status.charge} %` : '–'}</span>
				{/snippet}
			</SettingRow>
			<SettingRow icon="clock" title={t('ups.runtime')}>
				{#snippet control()}<span class="value"
						>{status.runtime >= 0 ? formatDuration(status.runtime) : '–'}</span
					>{/snippet}
			</SettingRow>
			<SettingRow icon="activity" title={t('ups.load')}>
				{#snippet control()}<span class="mono value"
						>{status.load >= 0 ? `${status.load} %` : '–'}</span
					>{/snippet}
			</SettingRow>
			{#if status.shuttingDown}<p class="notice-error" role="alert">{t('ups.shuttingDown')}</p>{/if}
		{/if}
	</SettingGroup>
{/if}

{#if draft && answer}
	{@const d = draft}
	{@const a = answer}
	<!-- One form, one Save: the request carries all of it, and half-typed
	     text fields must not be stored. The fields of the chosen connection
	     are the parts of its row. -->
	{#snippet modeParts()}
		{#if d.mode === 'local'}
			<div class="srow-block">
				<div class="row">
					<button type="button" class="btn" disabled={busy} onclick={scan}>{t('ups.scan')}</button>
					{#if found && found.length === 0}<span class="muted">{t('ups.scanNone')}</span>{/if}
				</div>
				{#each found ?? [] as device (device.driver + device.port + device.product)}
					<div class="found">
						<span
							>{device.product || device.driver}
							<span class="muted mono">{device.driver} · {device.port}</span></span
						>
						<button type="button" class="btn small" onclick={() => use(device)}
							>{t('ups.use')}</button
						>
					</div>
				{/each}
			</div>
			<SettingSub title={t('ups.driver')} for="ups-driver">
				{#snippet control()}
					<select id="ups-driver" class="field mono" bind:value={d.driver}>
						{#each a.drivers as driver (driver)}<option value={driver}>{driver}</option>{/each}
					</select>
				{/snippet}
			</SettingSub>
			{#if !usb}
				<SettingSub title={t('ups.port')} for="ups-port">
					{#snippet control()}
						<input
							id="ups-port"
							class="field mono"
							type="text"
							bind:value={d.port}
							placeholder="/dev/ttyS0"
							spellcheck="false"
							required
						/>
					{/snippet}
				</SettingSub>
			{/if}
			<SettingSub title={t('ups.serve')} for="ups-serve">
				{#snippet control()}
					<input
						id="ups-serve"
						class="switch"
						type="checkbox"
						role="switch"
						bind:checked={d.serve}
					/>
				{/snippet}
			</SettingSub>
			{#if d.serve && a.settings.serve && a.settings.remotePassword}
				<div class="srow-block">
					<dl class="share">
						<div>
							<dt>{t('ups.serveHost')}</dt>
							<dd class="mono">{host}</dd>
						</div>
						<div>
							<dt>{t('ups.serveName')}</dt>
							<dd class="mono">ups</dd>
						</div>
						<div>
							<dt>{t('notify.username')}</dt>
							<dd class="mono">monitor</dd>
						</div>
						<div>
							<dt>{t('notify.password')}</dt>
							<dd class="mono">{a.settings.remotePassword}</dd>
						</div>
					</dl>
					<p class="muted">{t('ups.serveHint')}</p>
				</div>
			{:else if d.serve}
				<div class="srow-block"><p class="muted">{t('ups.serveAfterSave')}</p></div>
			{/if}
		{:else if d.mode === 'network'}
			<SettingSub title={t('ups.server')} for="ups-host">
				{#snippet control()}
					<input
						id="ups-host"
						class="field mono"
						type="text"
						bind:value={d.host}
						spellcheck="false"
						autocapitalize="none"
						required
					/>
				{/snippet}
			</SettingSub>
			<SettingSub title={t('ups.name')} for="ups-name">
				{#snippet control()}
					<input
						id="ups-name"
						class="field mono"
						type="text"
						bind:value={d.name}
						placeholder="ups"
						spellcheck="false"
						autocapitalize="none"
						required
					/>
				{/snippet}
			</SettingSub>
			<SettingSub title={t('notify.username')} for="ups-user">
				{#snippet control()}
					<input
						id="ups-user"
						class="field mono"
						type="text"
						bind:value={d.user}
						autocomplete="off"
						spellcheck="false"
						autocapitalize="none"
						required
					/>
				{/snippet}
			</SettingSub>
			<SettingSub
				title={t('notify.password')}
				for="ups-password"
				desc={a.passwordSet ? t('notify.secretStored') : undefined}
			>
				{#snippet control()}
					<input
						id="ups-password"
						class="field mono"
						type="password"
						bind:value={d.password}
						autocomplete="new-password"
						required={!a.passwordSet}
					/>
				{/snippet}
			</SettingSub>
		{/if}
	{/snippet}
	<form onsubmit={save}>
		<SettingGroup title={t('ups.settings')}>
			<SettingRow
				id="ups-mode"
				icon="battery"
				title={t('ups.connection')}
				for="ups-mode-select"
				desc={t(`ups.modeHint.${d.mode}`)}
				bind:open={modeOpen}
				children={d.mode === 'off' ? undefined : modeParts}
			>
				{#snippet control()}
					<select id="ups-mode-select" class="field" bind:value={d.mode}>
						{#each ['off', 'local', 'network'] as const as mode (mode)}<option value={mode}
								>{t(`ups.mode.${mode}`)}</option
							>{/each}
					</select>
				{/snippet}
			</SettingRow>
			{#if d.mode !== 'off'}
				<SettingRow
					id="ups-when"
					icon="power"
					title={t('ups.when')}
					desc={t('ups.whenHint')}
					summary={byTime
						? `${t('ups.whenTime')} ${Number(d.shutdownAfter) || 1} ${t('ups.minutes')}`
						: t('ups.low')}
					bind:open={whenOpen}
				>
					<div class="srow-block">
						<fieldset>
							<legend class="sr-only">{t('ups.when')}</legend>
							<label class="check"
								><input
									type="radio"
									name="when"
									checked={!byTime}
									onchange={() => (byTime = false)}
								/><span>{t('ups.whenLow')}</span></label
							>
							<label class="check wrap"
								><input
									type="radio"
									name="when"
									checked={byTime}
									onchange={() => (byTime = true)}
								/><span>{t('ups.whenTime')}</span>
								<input
									class="field mono minutes"
									type="number"
									min="1"
									max="240"
									bind:value={d.shutdownAfter}
									disabled={!byTime}
									aria-label={t('ups.minutes')}
								/><span>{t('ups.minutes')}</span>
							</label>
						</fieldset>
					</div>
				</SettingRow>
			{/if}
			<div class="row-actions">
				<button type="submit" class="btn btn-primary" disabled={busy}>{t('notify.save')}</button>
				{#if saved}<span class="muted" role="status">{t('notify.saved')}</span>{/if}
			</div>
		</SettingGroup>
	</form>
{/if}

<style>
	form {
		display: contents;
	}
	p {
		margin: 0;
	}
	.value {
		color: var(--text);
	}
	.bar {
		width: 9rem;
		max-width: 100%;
	}
	dl {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
		gap: var(--space-3) var(--space-5);
		margin: 0;
	}
	dt {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	dd {
		margin: 2px 0 0;
		overflow-wrap: anywhere;
	}
	.share {
		padding: var(--space-3) 14px;
		border: 1px solid var(--srow-line);
		border-radius: var(--radius-ctl);
	}
	.row,
	.row-actions {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2-5) var(--space-4);
	}
	.row-actions {
		margin-top: var(--space-2);
	}
	.found {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-2) 14px;
		padding: var(--space-2-5) var(--space-3);
		border: 1px solid var(--srow-line);
		border-radius: var(--radius-ctl);
	}
	.small {
		height: 2rem;
		padding: 0 var(--space-3);
		font-size: var(--text-base);
	}
	fieldset {
		display: flex;
		flex-direction: column;
		gap: var(--space-2-5);
		min-width: 0;
		padding: 0;
		border: 0;
	}
	label.check {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		font-size: var(--text-md);
	}
	label.check.wrap {
		flex-wrap: wrap;
	}
	.check input[type='radio'] {
		width: 18px;
		height: 18px;
		margin: 0;
		flex: none;
	}
	.check input.minutes {
		width: 6rem;
	}
	pre {
		margin: 0;
		font-family: var(--font-mono);
		font-size: var(--text-sm);
		white-space: pre-wrap;
	}
</style>
