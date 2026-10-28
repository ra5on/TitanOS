<script lang="ts">
	import PushDevices from '#lib/components/PushDevices.svelte';
	import HealthReport from '#lib/components/HealthReport.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import { api, ApiError } from '#lib/api.ts';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';

	type Channel = 'email' | 'ntfy' | 'telegram' | 'discord' | 'homeassistant';
	type Settings = {
		language: 'de' | 'en';
		minSeverity: 'info' | 'warning' | 'error';
		email: { enabled: boolean; host: string; port: number; security: 'starttls' | 'tls' | 'none'; username: string; password?: string; from: string; to: string };
		ntfy: { enabled: boolean; url: string; token?: string };
		telegram: { enabled: boolean; token?: string; chatID: string };
		discord: { enabled: boolean; webhook?: string };
		homeAssistant: { enabled: boolean; service: string };
		digest: { enabled: boolean; time: string; errorsNow: boolean };
	};
	type Answer = { settings: Settings; secretSet: Record<Channel, boolean> };

	let draft = $state<Settings | null>(null);
	let secretSet = $state<Partial<Record<Channel, boolean>>>({});
	let haServices = $state<string[] | null>(null);
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
			discord: { ...res.settings.discord, webhook: '' },
			homeAssistant: res.settings.homeAssistant ?? { enabled: false, service: '' },
			digest: res.settings.digest?.time ? res.settings.digest : { enabled: false, time: '19:00', errorsNow: true }
		};
		secretSet = res.secretSet;
		clear = [];
	}

	function load() {
		error = null;
		api.get<Answer>('/notifications/settings').then(take, problem);
	}
	$effect(() => {
		load();
		api.get<{ services: string[] }>('/homeassistant/notify-services').then((res) => (haServices = res.services), () => (haServices = []));
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

<PageHead title={t('notify.settingsTitle')} parent={{ href: '/settings', label: t('nav.settings') }}>
	<p class="muted">{t('notify.intro')}</p>
</PageHead>
<p class="muted"><a href="/account#bell">{t('notify.personalHint')}</a></p>

{#if error}<div class="notice-error" role="alert">{error.text}{#if error.detail}<pre>{error.detail}</pre>{/if}{#if !draft} <button type="button" class="btn small" onclick={load}>{t('ui.retry')}</button>{/if}</div>{/if}

<PushDevices />
<HealthReport />

{#if draft}
	<form onsubmit={save}>
		<section class="tile panel">
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
			<label class="check"><input type="checkbox" bind:checked={draft.digest.enabled} /><span>{t('digest.enabled')}</span></label>
			{#if draft.digest.enabled}
				<div class="row">
					<label class="narrow"><span>{t('digest.time')}</span><input class="field" type="time" bind:value={draft.digest.time} /></label>
				</div>
				<label class="check"><input type="checkbox" bind:checked={draft.digest.errorsNow} /><span>{t('digest.errorsNow')}</span></label>
				<p class="muted small">{t('digest.hint')}</p>
			{/if}
		</section>

		<section class="tile panel">
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

		<section class="tile panel">
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

		<section class="tile panel">
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

		<section class="tile panel">
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

		<section class="tile panel">
			<div class="head">
				<h2 class="tile-title">{t('notify.homeassistant')}</h2>
				<label class="check"><input type="checkbox" bind:checked={draft.homeAssistant.enabled} disabled={!haServices?.length && !draft.homeAssistant.enabled} /><span>{t('notify.enabled')}</span></label>
			</div>
			<p class="muted">{t('notify.haHint')}</p>
			{#if haServices && haServices.length === 0}<p class="muted">{t('notify.haNotSetUp')} <a href="/settings/homeassistant">{t('ha.title')}</a></p>{/if}
			<div class="row">
				<label class="narrow"><span>{t('notify.haService')}</span>
					<select class="field mono" bind:value={draft.homeAssistant.service} required={draft.homeAssistant.enabled}>
						<option value=""></option>
						{#each new Set([...(haServices ?? []), draft.homeAssistant.service].filter(Boolean)) as service}<option value={service}>{service}</option>{/each}
					</select>
				</label>
			</div>
			{@render testRow('homeassistant')}
		</section>

		<div class="row-actions">
			<button type="submit" class="btn btn-primary" disabled={busy !== null}>{t(busy === 'save' ? 'notify.saving' : 'notify.save')}</button>
			{#if saved}<p class="notice-ok" role="status">{t('notify.saved')}</p>{/if}
		</div>
	</form>
{/if}

<style>
	form {
		display: flex;
		flex-direction: column;
		gap: var(--gap);
	}
	section {
		font-size: 0.875rem;
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
	.check {
		flex-direction: row;
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
	.row-actions {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 10px 16px;
	}
	.row-actions p {
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
