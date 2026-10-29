<script lang="ts">
	// Storage for Home Assistant: a share and an account of its own that Home
	// Assistant OS mounts as network storage (backups, media or a share for
	// add-ons). TitanOS can enter it there itself and watches the backups.
	import { api, ApiError } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { formatDateTime } from '#lib/format.ts';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';

	type Usage = 'backup' | 'media' | 'share';
	type Setup = {
		share: string;
		account: string;
		usage: Usage;
		mount: string;
		server: string;
		created: string;
		maxAgeDays: number;
	};
	type Backup = {
		checked: string;
		latest?: string;
		name?: string;
		error?: string;
		overdue: boolean;
	};
	type Status = {
		setup: Setup | null;
		volumes: string[];
		homeAssistant: boolean;
		server: string;
		backup?: Backup;
	};
	type Result = { setup: Setup; password: string; registered?: boolean; registerError?: string };

	let status = $state<Status | null>(null);
	let loadError = $state('');
	let error = $state('');
	let busy = $state('');
	let result = $state<Result | null>(null);
	let confirm = $state<'register' | 'forget' | null>(null);
	let copied = $state(false);
	let form = $state({
		volume: '',
		share: 'homeassistant',
		account: 'homeassistant',
		usage: 'backup' as Usage,
		quotaGiB: '',
		register: true,
	});
	let maxAge = $state(3);

	const usages: Usage[] = ['backup', 'media', 'share'];
	const ages = [0, 1, 2, 3, 7, 14, 30];

	function failure(err: unknown) {
		return errorText(err instanceof ApiError ? err.code : 'internal');
	}
	function take(next: Status) {
		status = next;
		if (!form.volume) form.volume = next.volumes[0] ?? '';
		if (!next.homeAssistant) form.register = false;
		if (next.setup) maxAge = next.setup.maxAgeDays;
	}
	async function load() {
		loadError = '';
		try {
			take(await api.get<Status>('/homeassistant/storage'));
		} catch (err) {
			loadError = failure(err);
		}
	}
	$effect(() => {
		void load();
	});

	async function run<T>(name: string, call: () => Promise<T>): Promise<T | null> {
		if (busy) return null;
		busy = name;
		error = '';
		try {
			return await call();
		} catch (err) {
			error = failure(err);
			return null;
		} finally {
			busy = '';
		}
	}

	async function setUp(event: SubmitEvent) {
		event.preventDefault();
		const quota = Number(form.quotaGiB) || 0;
		const made = await run('setup', () =>
			api.post<Result>('/homeassistant/storage', {
				...form,
				quotaGiB: quota,
				register: form.register && !!status?.homeAssistant,
			}),
		);
		if (made) {
			result = made;
			copied = false;
			await load();
		}
	}
	async function register() {
		confirm = null;
		const made = await run('register', () =>
			api.post<Result>('/homeassistant/storage/register', {}),
		);
		if (made) {
			result = made;
			copied = false;
			await load();
		}
	}
	async function forget() {
		confirm = null;
		const next = await run('forget', () => api.delete<Status>('/homeassistant/storage'));
		if (next) {
			result = null;
			take(next);
		}
	}
	async function saveAge() {
		const next = await run('age', () =>
			api.put<Status>('/homeassistant/storage', { maxAgeDays: maxAge }),
		);
		if (next) take(next);
	}
	async function check() {
		const next = await run('check', () => api.post<Status>('/homeassistant/storage/check', {}));
		if (next) take(next);
	}
	async function copy(text: string) {
		try {
			await navigator.clipboard.writeText(text);
			copied = true;
		} catch {
			copied = false;
		}
	}
	function registerProblem(text: string) {
		return text === 'no_supervisor' ? t('hastore.noSupervisor') : text;
	}
</script>

<section class="tile panel" id="storage" aria-labelledby="hastore-title">
	<h2 class="tile-title" id="hastore-title">{t('hastore.title')}</h2>
	<p class="muted hint">{t('hastore.intro')}</p>

	{#if loadError}
		<div class="notice-error" role="alert">
			{loadError}
			<button class="btn small" type="button" onclick={load}>{t('common.retry')}</button>
		</div>
	{:else if !status}
		<p class="muted" role="status">{t('common.loading')}</p>
	{:else if !status.setup}
		{#if !status.volumes.length}
			<p class="notice-warn">{t('hastore.noVolume')}</p>
		{:else}
			<form class="setup" onsubmit={setUp}>
				<div class="grid">
					<label class="stack"
						><span>{t('hastore.volume')}</span>
						<select class="field" bind:value={form.volume}>
							{#each status.volumes as volume (volume)}<option value={volume}>{volume}</option
								>{/each}
						</select></label
					>
					<label class="stack"
						><span>{t('hastore.share')}</span><input
							class="field mono"
							bind:value={form.share}
							spellcheck="false"
							autocapitalize="off"
						/></label
					>
					<label class="stack"
						><span>{t('hastore.account')}</span><input
							class="field mono"
							bind:value={form.account}
							spellcheck="false"
							autocapitalize="off"
						/></label
					>
					<label class="stack"
						><span>{t('hastore.quota')}</span><input
							class="field"
							type="number"
							min="0"
							step="1"
							inputmode="numeric"
							placeholder={t('hastore.quotaNone')}
							bind:value={form.quotaGiB}
						/></label
					>
				</div>
				<fieldset class="usage">
					<legend>{t('hastore.usage')}</legend>
					{#each usages as usage (usage)}
						<label class="check"
							><input
								type="radio"
								name="hastore-usage"
								value={usage}
								bind:group={form.usage}
							/><span
								><strong>{t(`hastore.usage.${usage}`)}</strong>
								<span class="muted">{t(`hastore.usageHint.${usage}`)}</span></span
							></label
						>
					{/each}
				</fieldset>
				<label class="check"
					><input
						type="checkbox"
						bind:checked={form.register}
						disabled={!status.homeAssistant}
					/><span>{t('hastore.register')}</span></label
				>
				<p class="muted hint">
					{t(status.homeAssistant ? 'hastore.registerHint' : 'hastore.registerOff')}
				</p>
				{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
				<div class="row-actions">
					<button
						class="btn btn-primary"
						disabled={!!busy || !form.volume || !form.share || !form.account}
						>{t(busy === 'setup' ? 'hastore.settingUp' : 'hastore.setUp')}</button
					>
				</div>
			</form>
		{/if}
	{:else}
		{@const setup = status.setup}
		<dl class="values">
			<dt>{t('hastore.share')}</dt>
			<dd class="mono">{setup.share}</dd>
			<dt>{t('hastore.account')}</dt>
			<dd class="mono">{setup.account}</dd>
			<dt>{t('hastore.usage')}</dt>
			<dd>{t(`hastore.usage.${setup.usage}`)}</dd>
			<dt>{t('hastore.server')}</dt>
			<dd class="mono">{setup.server || status.server}</dd>
			<dt>{t('hastore.mount')}</dt>
			<dd class="mono">{setup.mount}</dd>
		</dl>

		{#if setup.usage === 'backup'}
			<div class="divided backups">
				<h3>{t('hastore.backups')}</h3>
				{#if status.backup?.error}
					<p class="muted hint">{t('hastore.backupUnknown')}</p>
				{:else if status.backup?.latest}
					<p class:notice-warn={status.backup.overdue}>
						{t('hastore.backupLatest', {
							name: status.backup.name ?? '',
							date: formatDateTime(new Date(status.backup.latest)),
						})}
					</p>
				{:else if status.backup}
					<p class:notice-warn={status.backup.overdue} class:muted={!status.backup.overdue}>
						{t('hastore.backupNone')}
					</p>
				{/if}
				<div class="row-actions">
					<label class="stack age"
						><span>{t('hastore.maxAge')}</span>
						<select class="field" bind:value={maxAge}>
							{#each ages as days (days)}<option value={days}
									>{days ? t('hastore.days', { count: days }) : t('hastore.off')}</option
								>{/each}
						</select></label
					>
					<button
						class="btn"
						type="button"
						disabled={!!busy || maxAge === setup.maxAgeDays}
						onclick={saveAge}>{t('common.save')}</button
					>
					<button
						class="btn"
						type="button"
						disabled={!!busy || !status.homeAssistant}
						onclick={check}>{t(busy === 'check' ? 'hastore.checking' : 'hastore.check')}</button
					>
				</div>
				<p class="muted hint">{t('hastore.maxAgeHint')}</p>
			</div>
		{/if}

		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<div class="row-actions divided">
			<button
				class="btn"
				type="button"
				disabled={!!busy || !status.homeAssistant}
				onclick={() => (confirm = 'register')}>{t('hastore.registerAgain')}</button
			>
			<button class="btn" type="button" disabled={!!busy} onclick={() => (confirm = 'forget')}
				>{t('hastore.forget')}</button
			>
		</div>
	{/if}
</section>

{#if result}
	<Dialog title={t('hastore.resultTitle')} onclose={() => (result = null)}>
		{#if result.registered}
			<p class="notice-ok" role="status">{t('hastore.registered', { name: result.setup.mount })}</p>
		{:else if result.registerError}
			<p class="notice-warn" role="alert">
				{t('hastore.registerFailed', { error: registerProblem(result.registerError) })}
			</p>
		{/if}
		<p>{t(result.registered ? 'hastore.valuesKept' : 'hastore.valuesHowTo')}</p>
		<dl class="values">
			<dt>{t('hastore.mount')}</dt>
			<dd class="mono">{result.setup.mount}</dd>
			<dt>{t('hastore.usage')}</dt>
			<dd>{t(`hastore.usage.${result.setup.usage}`)}</dd>
			<dt>{t('hastore.server')}</dt>
			<dd class="mono">{result.setup.server}</dd>
			<dt>{t('hastore.protocol')}</dt>
			<dd>Samba/Windows (CIFS)</dd>
			<dt>{t('hastore.share')}</dt>
			<dd class="mono">{result.setup.share}</dd>
			<dt>{t('hastore.account')}</dt>
			<dd class="mono">{result.setup.account}</dd>
			<dt>{t('hastore.password')}</dt>
			<dd class="password">
				<span class="mono">{result.password}</span>
				<button class="btn small" type="button" onclick={() => copy(result?.password ?? '')}
					>{t(copied ? 'hastore.copied' : 'hastore.copy')}</button
				>
			</dd>
		</dl>
		<p class="notice-warn">{t('hastore.passwordOnce')}</p>
		<div class="dialog-actions">
			<button type="button" class="btn btn-primary" onclick={() => (result = null)}
				>{t('common.close')}</button
			>
		</div>
	</Dialog>
{/if}

{#if confirm && status?.setup}
	<Dialog
		title={t(confirm === 'register' ? 'hastore.registerAgain' : 'hastore.forget')}
		locked={!!busy}
		onclose={() => (confirm = null)}
	>
		<p>
			{t(confirm === 'register' ? 'hastore.registerAgainText' : 'hastore.forgetText', {
				share: status.setup.share,
				account: status.setup.account,
			})}
		</p>
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={!!busy} onclick={() => (confirm = null)}
				>{t('common.cancel')}</button
			>
			<button
				type="button"
				class="btn btn-warn"
				disabled={!!busy}
				onclick={confirm === 'register' ? register : forget}
				>{t(confirm === 'register' ? 'hastore.registerAgain' : 'hastore.forget')}</button
			>
		</div>
	</Dialog>
{/if}

<style>
	.setup {
		display: flex;
		flex-direction: column;
		gap: 12px;
	}
	.grid {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(12rem, 1fr));
		gap: 12px;
	}
	.usage {
		display: flex;
		flex-direction: column;
		gap: 8px;
		margin: 0;
		padding: 0;
		border: 0;
	}
	.usage legend {
		margin-bottom: 6px;
		font-size: 0.875rem;
	}
	.hint {
		font-size: 0.875rem;
	}
	.values {
		display: grid;
		grid-template-columns: max-content minmax(0, 1fr);
		gap: 6px 16px;
		margin: 0;
		font-size: 0.875rem;
	}
	.values dt {
		color: var(--muted);
	}
	.values dd {
		margin: 0;
		overflow-wrap: anywhere;
	}
	.password {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 8px;
	}
	.backups h3 {
		margin: 0 0 6px;
		font-size: 0.9375rem;
	}
	.backups p {
		margin: 0 0 8px;
	}
	.backups .row-actions {
		align-items: flex-end;
	}
	.age select {
		width: auto;
		min-width: 9rem;
	}
</style>
