<script lang="ts">
	import { api, ApiError } from '#lib/api.ts';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';

	type Channel = 'email' | 'ntfy' | 'telegram' | 'discord';
	type Settings = {
		language: 'de' | 'en';
		minSeverity: 'info' | 'warning' | 'error';
		email: { enabled: boolean; host: string; port: number; security: 'starttls' | 'tls' | 'none'; username: string; password?: string; from: string; to: string };
		ntfy: { enabled: boolean; url: string; token?: string };
		telegram: { enabled: boolean; token?: string; chatID: string };
		discord: { enabled: boolean; webhook?: string };
	};
	type Answer = { settings: Settings; secretSet: Record<Channel, boolean> };

	let draft = $state<Settings | null>(null);
	let secretSet = $state<Record<Channel, boolean>>({ email: false, ntfy: false, telegram: false, discord: false });
	// Stored secrets that go away with the next save.
	let clear = $state<Channel[]>([]);
	let error = $state<{ text: string; detail: string } | null>(null);
	let saved = $state(false);
	let busy = $state<'save' | Channel | null>(null);
	let tested = $state<{ channel: Channel; ok: boolean; detail: string } | null>(null);

	function problem(err: unknown) {
		const code = err instanceof ApiError ? err.code : 'internal';
		error = { text: errorText(code), detail: code === 'invalid_notify_settings' && err instanceof ApiError ? err.message : '' };
	}

	function take(res: Answer) {
		// Secrets never come back from the NAS; their fields start empty.
		draft = {
			...res.settings,
			email: { ...res.settings.email, port: res.settings.email.port || 587, security: res.settings.email.security || 'starttls', password: '' },
			ntfy: { ...res.settings.ntfy, token: '' },
			telegram: { ...res.settings.telegram, token: '' },
			discord: { ...res.settings.discord, webhook: '' }
		};
		secretSet = res.secretSet;
		clear = [];
	}

	$effect(() => {
		api.get<Answer>('/notifications/settings').then(take, problem);
	});

	async function store(): Promise<boolean> {
		if (!draft) return false;
		error = null;
		saved = false;
		try {
			take(await api.put<Answer>('/notifications/settings', { ...draft, email: { ...draft.email, port: Number(draft.email.port) || 0 }, clearSecrets: clear }));
			return true;
		} catch (err) {
			problem(err);
			return false;
		}
	}

	async function save(event: SubmitEvent) {
		event.preventDefault();
		busy = 'save';
		tested = null;
		saved = await store();
		busy = null;
	}

	// A test uses what is stored, so that it tests what will be used.
	async function test(channel: Channel) {
		busy = channel;
		tested = null;
		if (await store()) {
			try {
				await api.post('/notifications/test', { channel });
				tested = { channel, ok: true, detail: '' };
			} catch (err) {
				tested = { channel, ok: false, detail: err instanceof ApiError ? err.message : '' };
			}
		}
		busy = null;
	}

	function toggleClear(channel: Channel) {
		clear = clear.includes(channel) ? clear.filter((name) => name !== channel) : [...clear, channel];
	}
</script>

{#snippet secretNote(channel: Channel)}
	{#if secretSet[channel]}
		<button type="button" class="link" onclick={() => toggleClear(channel)}>{clear.includes(channel) ? t('notify.secretRemoved') : t('notify.secretRemove')}</button>
	{/if}
{/snippet}

{#snippet testRow(channel: Channel)}
	<div class="test">
		<button type="button" class="btn" disabled={busy !== null} onclick={() => test(channel)}>{t(busy === channel ? 'notify.testing' : 'notify.test')}</button>
		{#if tested?.channel === channel}
			{#if tested.ok}
				<p class="notice-ok" role="status">{t('notify.testOk')}</p>
			{:else}
				<div class="notice-error" role="alert">{t('notify.testFailed')}<pre>{tested.detail}</pre></div>
			{/if}
		{/if}
	</div>
{/snippet}

<div class="title">
	<p class="muted crumbs"><a href="/settings">{t('nav.settings')}</a> › {t('notify.settingsTitle')}</p>
	<h1>{t('notify.settingsTitle')}</h1>
	<p class="muted intro">{t('notify.intro')}</p>
</div>

{#if error}<div class="notice-error" role="alert">{error.text}{#if error.detail}<pre>{error.detail}</pre>{/if}</div>{/if}

{#if draft}
	<form onsubmit={save}>
		<section class="tile">
			<h2 class="tile-title">{t('notify.general')}</h2>
			<div class="row">
				<label class="narrow"><span>{t('notify.minSeverity')}</span>
					<select class="field" bind:value={draft.minSeverity}>
						<option value="info">{t('notify.min.info')}</option>
						<option value="warning">{t('notify.min.warning')}</option>
						<option value="error">{t('notify.min.error')}</option>
					</select>
				</label>
				<label class="narrow"><span>{t('notify.language')}</span>
					<select class="field" bind:value={draft.language}>
						<option value="de">Deutsch</option>
						<option value="en">English</option>
					</select>
				</label>
			</div>
		</section>

		<section class="tile">
			<div class="head">
				<h2 class="tile-title">{t('notify.email')}</h2>
				<label class="check"><input type="checkbox" bind:checked={draft.email.enabled} /><span>{t('notify.enabled')}</span></label>
			</div>
			<p class="muted">{t('notify.emailHint')}</p>
			<div class="row">
				<label class="narrow"><span>{t('notify.host')}</span><input class="field mono" type="text" bind:value={draft.email.host} placeholder="smtp.example.org" spellcheck="false" autocapitalize="none" required={draft.email.enabled} /></label>
				<label class="tiny"><span>{t('notify.port')}</span><input class="field mono" type="number" min="1" max="65535" bind:value={draft.email.port} required={draft.email.enabled} /></label>
				<label class="narrow"><span>{t('notify.security')}</span>
					<select class="field" bind:value={draft.email.security}>
						<option value="starttls">{t('notify.security.starttls')}</option>
						<option value="tls">{t('notify.security.tls')}</option>
						<option value="none">{t('notify.security.none')}</option>
					</select>
				</label>
			</div>
			{#if draft.email.security === 'none'}<p class="notice-warn">{t('notify.securityNone')}</p>{/if}
			<div class="row">
				<label class="narrow"><span>{t('notify.username')}</span><input class="field mono" type="text" bind:value={draft.email.username} autocomplete="off" spellcheck="false" autocapitalize="none" /></label>
				<label class="narrow"><span>{t('notify.password')}</span>
					<input class="field mono" type="password" bind:value={draft.email.password} autocomplete="new-password" placeholder={secretSet.email && !clear.includes('email') ? t('notify.secretStored') : ''} />
					{@render secretNote('email')}
				</label>
			</div>
			<div class="row">
				<label class="narrow"><span>{t('notify.from')}</span><input class="field mono" type="text" bind:value={draft.email.from} placeholder="nas@example.org" spellcheck="false" required={draft.email.enabled} /></label>
				<label class="narrow"><span>{t('notify.to')}</span><input class="field mono" type="text" bind:value={draft.email.to} placeholder="me@example.org" spellcheck="false" required={draft.email.enabled} /></label>
			</div>
			{@render testRow('email')}
		</section>

		<section class="tile">
			<div class="head">
				<h2 class="tile-title">{t('notify.ntfy')}</h2>
				<label class="check"><input type="checkbox" bind:checked={draft.ntfy.enabled} /><span>{t('notify.enabled')}</span></label>
			</div>
			<p class="muted">{t('notify.ntfyHint')}</p>
			<div class="row">
				<label class="wide"><span>{t('notify.ntfyUrl')}</span><input class="field mono" type="url" bind:value={draft.ntfy.url} placeholder="https://ntfy.sh/…" spellcheck="false" required={draft.ntfy.enabled} /></label>
				<label class="narrow"><span>{t('notify.token')}</span>
					<input class="field mono" type="password" bind:value={draft.ntfy.token} autocomplete="new-password" placeholder={secretSet.ntfy && !clear.includes('ntfy') ? t('notify.secretStored') : ''} />
					{@render secretNote('ntfy')}
				</label>
			</div>
			{@render testRow('ntfy')}
		</section>

		<section class="tile">
			<div class="head">
				<h2 class="tile-title">{t('notify.telegram')}</h2>
				<label class="check"><input type="checkbox" bind:checked={draft.telegram.enabled} /><span>{t('notify.enabled')}</span></label>
			</div>
			<p class="muted">{t('notify.telegramHint')}</p>
			<div class="row">
				<label class="wide"><span>{t('notify.botToken')}</span>
					<input class="field mono" type="password" bind:value={draft.telegram.token} autocomplete="new-password" placeholder={secretSet.telegram && !clear.includes('telegram') ? t('notify.secretStored') : ''} required={draft.telegram.enabled && !secretSet.telegram} />
					{@render secretNote('telegram')}
				</label>
				<label class="narrow"><span>{t('notify.chatId')}</span><input class="field mono" type="text" bind:value={draft.telegram.chatID} spellcheck="false" required={draft.telegram.enabled} /></label>
			</div>
			{@render testRow('telegram')}
		</section>

		<section class="tile">
			<div class="head">
				<h2 class="tile-title">{t('notify.discord')}</h2>
				<label class="check"><input type="checkbox" bind:checked={draft.discord.enabled} /><span>{t('notify.enabled')}</span></label>
			</div>
			<p class="muted">{t('notify.discordHint')}</p>
			<div class="row">
				<label class="wide"><span>{t('notify.webhook')}</span>
					<input class="field mono" type="password" bind:value={draft.discord.webhook} autocomplete="new-password" placeholder={secretSet.discord && !clear.includes('discord') ? t('notify.secretStored') : ''} required={draft.discord.enabled && !secretSet.discord} />
					{@render secretNote('discord')}
				</label>
			</div>
			{@render testRow('discord')}
		</section>

		<div class="actions">
			{#if saved}<p class="notice-ok" role="status">{t('notify.saved')}</p>{/if}
			<button type="submit" class="btn btn-primary" disabled={busy !== null}>{t(busy === 'save' ? 'notify.saving' : 'notify.save')}</button>
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
	form {
		display: flex;
		flex-direction: column;
		gap: var(--gap);
	}
	section {
		display: flex;
		flex-direction: column;
		gap: 14px;
		padding: 18px;
		font-size: 0.875rem;
	}
	section > p {
		margin: 0;
	}
	.head {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: 8px 16px;
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
		flex: 0 1 320px;
		max-width: 320px;
	}
	.tiny {
		flex: 0 1 110px;
		max-width: 110px;
	}
	.wide {
		flex: 1 1 360px;
		max-width: 664px;
	}
	.row {
		display: flex;
		flex-wrap: wrap;
		gap: 16px 24px;
	}
	input[type='checkbox'] {
		width: 18px;
		height: 18px;
		margin: 0;
		flex: none;
	}
	.check {
		flex-direction: row;
		align-items: center;
		gap: 10px;
		font-size: 0.875rem;
	}
	.check > span {
		color: var(--text);
	}
	.link {
		align-self: flex-start;
		padding: 0;
		background: none;
		border: 0;
		color: var(--muted);
		font: inherit;
		font-size: 0.75rem;
		text-decoration: underline;
		cursor: pointer;
	}
	.test {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 10px 16px;
	}
	.test > :global(.notice-ok),
	.test > :global(.notice-error) {
		margin: 0;
		flex: 1 1 280px;
	}
	.actions {
		display: flex;
		align-items: center;
		justify-content: flex-end;
		gap: 16px;
	}
	.actions p {
		margin: 0;
	}
	pre {
		margin: 6px 0 0;
		font-family: var(--font-mono);
		font-size: 0.75rem;
		white-space: pre-wrap;
		overflow-wrap: anywhere;
	}
</style>
