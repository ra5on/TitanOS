<script lang="ts">
	// Where the assistant reaches this account: its own channels, which may
	// carry a short summary because nobody else reads them.
	import { api, ApiError } from '#lib/api.ts';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';
	import { formatDateTime } from '#lib/format.ts';

	type Channel = 'email' | 'ntfy' | 'telegram' | 'discord' | 'homeAssistant';
	type Event = 'confirmation' | 'done' | 'blocked' | 'agent';
	type Personal = {persistQuiet?:boolean;fallback?:string;
		language: 'de' | 'en';
		email: { enabled: boolean; to: string };
		ntfy: { enabled: boolean; url: string; token?: string };
		telegram: { enabled: boolean; token?: string; chatID: string };
		discord: { enabled: boolean; webhook?: string };
		homeAssistant: { enabled: boolean; service: string };
		events: Record<Event, boolean>;
		content: 'summary' | 'none';
		quiet: { enabled: boolean; from: string; to: string };
		origin: string;
	};
	type Answer = {delivery?:Record<string,{time:string;ok:boolean;attempts:number}>; settings: Personal; secretSet: Partial<Record<Channel, boolean>>; mailServer: boolean };

	const events: Event[] = ['confirmation', 'done', 'blocked', 'agent'];
	const channels: Channel[] = ['email', 'ntfy', 'telegram', 'discord', 'homeAssistant'];

	let delivery=$state<Answer['delivery']>({});
	let draft = $state<Personal | null>(null);
	let secretSet = $state<Partial<Record<Channel, boolean>>>({});
	let mailServer = $state(false);
	let haServices = $state<string[]>([]);
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
		draft = {
			...res.settings,
			ntfy: { ...res.settings.ntfy, token: '' },
			telegram: { ...res.settings.telegram, token: '' },
			discord: { ...res.settings.discord, webhook: '' },
			homeAssistant: res.settings.homeAssistant ?? { enabled: false, service: '' },
			quiet: { enabled: res.settings.quiet.enabled, from: res.settings.quiet.from || '22:00', to: res.settings.quiet.to || '07:00' }
		};
		secretSet = res.secretSet;
 delivery=res.delivery??{};
		mailServer = res.mailServer;
		clear = [];
	}

	$effect(() => {
		api.get<Answer>('/account/notifications').then(take, problem);
		api.get<{ services: string[] }>('/homeassistant/notify-services').then((res) => (haServices = res.services), () => (haServices = []));
	});

	async function store(): Promise<boolean> {
		if (!draft) return false;
		error = null;
		saved = false;
		try {
			// Links in messages lead back to the address this page was opened under.
			take(await api.put<Answer>('/account/notifications', { ...draft, origin: location.origin, clearSecrets: clear }));
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

	async function test(channel: Channel) {
		busy = channel;
		tested = null;
		if (await store()) {
			try {
				await api.post('/account/notifications/test', { channel: channel === 'homeAssistant' ? 'homeassistant' : channel });
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
	const secretHolder = (channel: Channel) => (secretSet[channel] && !clear.includes(channel) ? t('notify.secretStored') : '');
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

<section class="tile personal" id="personal-notifications" aria-labelledby="personal-title">
	<h2 class="tile-title" id="personal-title">{t('personalNotify.title')}</h2>
	<p class="muted">{t('personalNotify.intro')}</p>
	{#if error}<div class="notice-error" role="alert">{error.text}{#if error.detail}<pre>{error.detail}</pre>{/if}</div>{/if}
	{#if draft}
		<form onsubmit={save}>
			<fieldset>
				<legend>{t('personalNotify.events')}</legend>
				{#each events as event}
					<label class="check"><input type="checkbox" bind:checked={draft.events[event]} /><span>{t(`personalNotify.event.${event}`)}</span></label>
				{/each}
			</fieldset>
			<div class="row">
				<label class="narrow"><span>{t('personalNotify.content')}</span>
					<select class="field" bind:value={draft.content}>
						<option value="summary">{t('personalNotify.content.summary')}</option>
						<option value="none">{t('personalNotify.content.none')}</option>
					</select>
				</label>
				<label class="narrow"><span>{t('notify.language')}</span>
					<select class="field" bind:value={draft.language}>
						<option value="de">Deutsch</option>
						<option value="en">English</option>
					</select>
				</label>
			</div>
			<div class="row quiet">
				<label class="check"><input type="checkbox" bind:checked={draft.quiet.enabled} /><span>{t('personalNotify.quiet')}</span></label>
				<label class="tiny"><span>{t('personalNotify.quietFrom')}</span><input class="field mono" type="time" bind:value={draft.quiet.from} disabled={!draft.quiet.enabled} /></label>
				<label class="tiny"><span>{t('personalNotify.quietTo')}</span><input class="field mono" type="time" bind:value={draft.quiet.to} disabled={!draft.quiet.enabled} /></label>
			</div>
			<p class="muted small">{t('personalNotify.quietHint')}</p>

			{#each channels as channel}
				<div class="channel">
					<label class="check"><input type="checkbox" bind:checked={draft[channel].enabled} disabled={(channel === 'email' && !mailServer) || (channel === 'homeAssistant' && !haServices.length && !draft.homeAssistant.enabled)} /><span class="name">{t(channel === 'homeAssistant' ? 'notify.homeassistant' : `notify.${channel}`)}</span></label>
					{#if channel === 'email'}
						{#if !mailServer}<p class="muted small">{t('personalNotify.noMailServer')}</p>{/if}
						<div class="row">
							<label class="narrow"><span>{t('notify.to')}</span><input class="field mono" type="email" bind:value={draft.email.to} placeholder="me@example.org" spellcheck="false" required={draft.email.enabled} /></label>
						</div>
					{:else if channel === 'ntfy'}
						<div class="row">
							<label class="wide"><span>{t('notify.ntfyUrl')}</span><input class="field mono" type="url" bind:value={draft.ntfy.url} placeholder="https://ntfy.sh/…" spellcheck="false" required={draft.ntfy.enabled} /></label>
							<label class="narrow"><span>{t('notify.token')}</span>
								<input class="field mono" type="password" bind:value={draft.ntfy.token} autocomplete="new-password" placeholder={secretHolder('ntfy')} />
								{@render secretNote('ntfy')}
							</label>
						</div>
					{:else if channel === 'telegram'}
						<div class="row">
							<label class="wide"><span>{t('notify.botToken')}</span>
								<input class="field mono" type="password" bind:value={draft.telegram.token} autocomplete="new-password" placeholder={secretHolder('telegram')} required={draft.telegram.enabled && !secretSet.telegram} />
								{@render secretNote('telegram')}
							</label>
							<label class="narrow"><span>{t('notify.chatId')}</span><input class="field mono" type="text" bind:value={draft.telegram.chatID} spellcheck="false" required={draft.telegram.enabled} /></label>
						</div>
					{:else if channel === 'homeAssistant'}
						{#if !haServices.length}<p class="muted small">{t('notify.haNotSetUp')}</p>{/if}
						<div class="row">
							<label class="narrow"><span>{t('notify.haService')}</span>
								<select class="field mono" bind:value={draft.homeAssistant.service} required={draft.homeAssistant.enabled}>
									<option value=""></option>
									{#each new Set([...haServices, draft.homeAssistant.service].filter(Boolean)) as service}<option value={service}>{service}</option>{/each}
								</select>
							</label>
						</div>
					{:else}
						<div class="row">
							<label class="wide"><span>{t('notify.webhook')}</span>
								<input class="field mono" type="password" bind:value={draft.discord.webhook} autocomplete="new-password" placeholder={secretHolder('discord')} required={draft.discord.enabled && !secretSet.discord} />
								{@render secretNote('discord')}
							</label>
						</div>
					{/if}
					{#if draft[channel].enabled}{@render testRow(channel)}{/if}
				</div>
			{/each}

			<label class="check"><input type="checkbox" bind:checked={draft.persistQuiet}/>{t('personal.persistQuiet')}</label><p class="muted small">{t('personal.persistHint')}</p>
            <label>{t('personal.fallback')}<select class="field" bind:value={draft.fallback}><option value="">{t('personal.noFallback')}</option>{#each channels.filter(c=>draft?.[c].enabled) as channel}<option value={channel==='homeAssistant'?'homeassistant':channel}>{channel}</option>{/each}</select></label>
            <details><summary>{t('personal.delivery')}</summary>{#each Object.entries(delivery??{}) as [channel,status]}<p class={status.ok?'notice-ok':'notice-error'}>{channel}: {t(status.ok?'personal.delivered':'personal.failed')} · {formatDateTime(new Date(status.time))} ({status.attempts})</p>{/each}<button type="button" class="btn" onclick={async()=>{try{const response=await api.get<Answer>('/account/notifications');delivery=response.delivery??{}}catch(e){problem(e)}}}>{t('personal.refreshDelivery')}</button></details>
            <div class="actions">
				{#if saved}<p class="notice-ok" role="status">{t('notify.saved')}</p>{/if}
				<button type="submit" class="btn btn-primary" disabled={busy !== null}>{t(busy === 'save' ? 'notify.saving' : 'notify.save')}</button>
			</div>
		</form>
	{/if}
</section>

<style>
	.personal {
		display: flex;
		flex-direction: column;
		gap: 14px;
		padding: var(--tile-pad);
		margin-block: var(--gap);
		font-size: 0.875rem;
	}
	.personal > p {
		margin: 0;
	}
	form {
		display: flex;
		flex-direction: column;
		gap: 14px;
	}
	fieldset {
		display: flex;
		flex-wrap: wrap;
		gap: 10px 24px;
		margin: 0;
		padding: 0;
		border: 0;
	}
	legend {
		margin-bottom: 8px;
		color: var(--muted);
		font-size: 0.8125rem;
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
	.check {
		flex-direction: row;
		align-items: center;
		gap: 10px;
		font-size: 0.875rem;
	}
	.check > span {
		color: var(--text);
	}
	.name {
		font-weight: 600;
	}
	input[type='checkbox'] {
		width: 18px;
		height: 18px;
		margin: 0;
		flex: none;
	}
	.row {
		display: flex;
		flex-wrap: wrap;
		gap: 16px 24px;
	}
	.quiet {
		align-items: flex-end;
	}
	.narrow {
		flex: 0 1 320px;
		max-width: 320px;
	}
	.tiny {
		flex: 0 1 120px;
		max-width: 120px;
	}
	.wide {
		flex: 1 1 360px;
		max-width: 664px;
	}
	.small {
		margin: 0;
		font-size: 0.8125rem;
	}
	.channel {
		display: flex;
		flex-direction: column;
		gap: 10px;
		padding-top: 14px;
		border-top: 1px solid var(--border);
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
