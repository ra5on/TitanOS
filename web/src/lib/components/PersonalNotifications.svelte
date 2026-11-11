<script lang="ts">
	// Where the NAS reaches this account (owners and administrators): its
	// own channels for the kinds of messages it chooses, with quiet hours.
	import { tick } from 'svelte';
	import { api, ApiError } from '#lib/api.ts';
	import SettingGroup from './settings/SettingGroup.svelte';
	import SettingRow from './settings/SettingRow.svelte';
	import SettingSub from './settings/SettingSub.svelte';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';
	import { formatDateTime } from '#lib/format.ts';

	type Channel = 'email' | 'ntfy' | 'telegram' | 'discord' | 'homeAssistant';
	type Event = 'storage' | 'backups' | 'updates' | 'system';
	type Personal = {
		persistQuiet?: boolean;
		fallback?: string;
		language: 'de' | 'en';
		email: { enabled: boolean; to: string };
		ntfy: { enabled: boolean; url: string; token?: string };
		telegram: { enabled: boolean; token?: string; chatID: string };
		discord: { enabled: boolean; webhook?: string };
		homeAssistant: { enabled: boolean; service: string; connection?: string };
		events: Record<Event, boolean>;
		content: 'summary' | 'none';
		quiet: { enabled: boolean; from: string; to: string };
		origin: string;
	};
	type Answer = {
		delivery?: Record<string, { time: string; ok: boolean; attempts: number }>;
		settings: Personal;
		secretSet: Partial<Record<Channel, boolean>>;
		mailServer: boolean;
	};

	const events: Event[] = ['storage', 'backups', 'updates', 'system'];
	const channels: Channel[] = ['email', 'ntfy', 'telegram', 'discord', 'homeAssistant'];

	let delivery = $state<Answer['delivery']>({});
	let draft = $state<Personal | null>(null);
	let secretSet = $state<Partial<Record<Channel, boolean>>>({});
	let mailServer = $state(false);
	let haServices = $state<string[]>([]);
	// Home Assistant connections to choose from (PLAN §45), by name.
	let haConns = $state<{ id: string; name: string }[]>([]);
	function loadHAServices(connection: string) {
		const query = connection ? `?connection=${encodeURIComponent(connection)}` : '';
		api
			.get<{
				services: string[];
				connections?: { id: string; name: string }[];
			}>(`/homeassistant/notify-services${query}`)
			.then(
				(res) => {
					// A list of another connection may have come first.
					if (connection === (draft?.homeAssistant.connection ?? '') || !draft)
						haServices = res.services;
					if (!connection) haConns = res.connections ?? [];
				},
				() => (haServices = []),
			);
	}
	let clear = $state<Channel[]>([]);
	let error = $state<{ text: string; detail: string } | null>(null);
	let saved = $state(false);
	let busy = $state<'save' | Channel | null>(null);
	let tested = $state<{ channel: Channel; ok: boolean; detail: string } | null>(null);

	function problem(err: unknown) {
		const code = err instanceof ApiError ? err.code : 'internal';
		error = {
			text: errorText(code),
			detail: code === 'invalid_notify_settings' && err instanceof ApiError ? err.message : '',
		};
	}

	function take(res: Answer) {
		draft = {
			...res.settings,
			ntfy: { ...res.settings.ntfy, token: '' },
			telegram: { ...res.settings.telegram, token: '' },
			discord: { ...res.settings.discord, webhook: '' },
			homeAssistant: res.settings.homeAssistant ?? { enabled: false, service: '' },
			quiet: {
				enabled: res.settings.quiet.enabled,
				from: res.settings.quiet.from || '22:00',
				to: res.settings.quiet.to || '07:00',
			},
		};
		secretSet = res.secretSet;
		delivery = res.delivery ?? {};
		mailServer = res.mailServer;
		clear = [];
		if (draft.homeAssistant.connection) loadHAServices(draft.homeAssistant.connection);
	}

	$effect(() => {
		api.get<Answer>('/account/notifications').then(take, problem);
		loadHAServices('');
	});

	async function store(): Promise<boolean> {
		if (!draft) return false;
		error = null;
		saved = false;
		try {
			// Links in messages lead back to the address this page was opened under.
			take(
				await api.put<Answer>('/account/notifications', {
					...draft,
					origin: location.origin,
					clearSecrets: clear,
				}),
			);
			return true;
		} catch (err) {
			problem(err);
			return false;
		}
	}

	// PLAN 78-5: the fields of a way are the parts of its row, and a closed
	// row has no fields the browser could check. Before saving, the rows of
	// the ways that are switched on open, so that a required field that is
	// still empty is named as before. (A test stores without that check, as
	// before.)
	let form = $state<HTMLFormElement>();
	let open = $state<Record<Channel | 'quiet', boolean>>({
		email: false,
		ntfy: false,
		telegram: false,
		discord: false,
		homeAssistant: false,
		quiet: false,
	});
	async function complete(): Promise<boolean> {
		if (!draft) return false;
		for (const channel of channels) if (draft[channel].enabled) open[channel] = true;
		await tick();
		return form?.reportValidity() ?? true;
	}

	async function save(event: SubmitEvent) {
		event.preventDefault();
		if (!(await complete())) return;
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
				await api.post('/account/notifications/test', {
					channel: channel === 'homeAssistant' ? 'homeassistant' : channel,
				});
				tested = { channel, ok: true, detail: '' };
			} catch (err) {
				tested = { channel, ok: false, detail: err instanceof ApiError ? err.message : '' };
			}
		}
		busy = null;
	}

	function toggleClear(channel: Channel) {
		clear = clear.includes(channel)
			? clear.filter((name) => name !== channel)
			: [...clear, channel];
	}
</script>

<!-- A stored secret never comes back: that one is stored stands below the
     field's name (not as a placeholder, which a narrow field would clip). -->
{#snippet secretNote(channel: Channel)}
	{#if secretSet[channel]}
		{#if !clear.includes(channel)}{t('notify.secretStored')}{/if}
		<button type="button" class="link" onclick={() => toggleClear(channel)}
			>{clear.includes(channel) ? t('notify.secretRemoved') : t('notify.secretRemove')}</button
		>
	{/if}
{/snippet}

{#snippet testRow(channel: Channel)}
	<div class="srow-block test">
		<button type="button" class="btn" disabled={busy !== null} onclick={() => test(channel)}
			>{t(busy === channel ? 'notify.testing' : 'notify.test')}</button
		>
		{#if tested?.channel === channel}
			{#if tested.ok}
				<p class="notice-ok" role="status">{t('notify.testOk')}</p>
			{:else}
				<div class="notice-error" role="alert">
					{t('notify.testFailed')}
					<pre>{tested.detail}</pre>
				</div>
			{/if}
		{/if}
	</div>
{/snippet}

{#snippet onOff(channel: Channel, d: Personal, disabled = false)}
	<input
		id="pn-{channel}-on"
		class="switch"
		type="checkbox"
		role="switch"
		checked={d[channel].enabled}
		{disabled}
		onchange={(e) => {
			d[channel].enabled = e.currentTarget.checked;
			if (e.currentTarget.checked) open[channel] = true;
		}}
	/>
{/snippet}

<!-- PLAN 78-5: what reaches the account, its ways and the delivery are groups
     of rows; a way's fields open inside its row. One form with one Save, as
     before. -->
<form id="personal-notifications" onsubmit={save} bind:this={form}>
	<SettingGroup title={t('personalNotify.title')} id="personal" desc={t('personalNotify.intro')}>
		{#if error}<div class="notice-error" role="alert">
				{error.text}{#if error.detail}<pre>{error.detail}</pre>{/if}
			</div>{/if}
		{#if draft}
			{@const d = draft}
			<SettingRow
				id="pn-events"
				icon="bell"
				title={t('personalNotify.events')}
				summary={t('personalNotify.eventsSummary', {
					count: events.filter((event) => d.events[event]).length,
					total: events.length,
				})}
			>
				{#each events as event}
					<SettingSub title={t(`personalNotify.event.${event}`)} for="pn-event-{event}">
						{#snippet control()}
							<input
								id="pn-event-{event}"
								class="switch"
								type="checkbox"
								role="switch"
								bind:checked={d.events[event]}
							/>
						{/snippet}
					</SettingSub>
				{/each}
			</SettingRow>
			<SettingRow icon="fileText" title={t('personalNotify.content')} for="pn-content">
				{#snippet control()}
					<select id="pn-content" class="field" bind:value={d.content}>
						<option value="summary">{t('personalNotify.content.summary')}</option>
						<option value="none">{t('personalNotify.content.none')}</option>
					</select>
				{/snippet}
			</SettingRow>
			<SettingRow icon="globe" title={t('notify.language')} for="pn-language">
				{#snippet control()}
					<select id="pn-language" class="field" bind:value={d.language}>
						<option value="de">Deutsch</option>
						<option value="en">English</option>
					</select>
				{/snippet}
			</SettingRow>
			{#snippet quietParts()}
				<SettingSub title={t('personalNotify.quietFrom')} for="pn-quiet-from">
					{#snippet control()}
						<input id="pn-quiet-from" class="field mono" type="time" bind:value={d.quiet.from} />
					{/snippet}
				</SettingSub>
				<SettingSub title={t('personalNotify.quietTo')} for="pn-quiet-to">
					{#snippet control()}
						<input id="pn-quiet-to" class="field mono" type="time" bind:value={d.quiet.to} />
					{/snippet}
				</SettingSub>
			{/snippet}
			<SettingRow
				icon="clock"
				title={t('personalNotify.quiet')}
				desc={t('personalNotify.quietHint')}
				for="pn-quiet"
				bind:open={open.quiet}
				children={d.quiet.enabled ? quietParts : undefined}
			>
				{#snippet control()}
					<input
						id="pn-quiet"
						class="switch"
						type="checkbox"
						role="switch"
						checked={d.quiet.enabled}
						onchange={(e) => {
							d.quiet.enabled = e.currentTarget.checked;
							if (d.quiet.enabled) open.quiet = true;
						}}
					/>
				{/snippet}
			</SettingRow>
		{/if}
	</SettingGroup>

	{#if draft}
		{@const d = draft}
		<SettingGroup title={t('personalNotify.channels')} id="personal-channels">
			<SettingRow
				id="pn-email"
				icon="send"
				title={t('notify.email')}
				for="pn-email-on"
				bind:open={open.email}
			>
				{#snippet control()}{@render onOff('email', d, !mailServer)}{/snippet}
				{#if !mailServer}<div class="srow-block">
						<p class="muted">{t('personalNotify.noMailServer')}</p>
					</div>{/if}
				<SettingSub title={t('notify.to')} for="pn-email-to">
					{#snippet control()}
						<input
							id="pn-email-to"
							class="field mono"
							type="email"
							bind:value={d.email.to}
							placeholder="me@example.org"
							spellcheck="false"
							required={d.email.enabled}
						/>
					{/snippet}
				</SettingSub>
				{#if d.email.enabled}{@render testRow('email')}{/if}
			</SettingRow>

			<SettingRow
				id="pn-ntfy"
				icon="bell"
				title={t('notify.ntfy')}
				for="pn-ntfy-on"
				bind:open={open.ntfy}
			>
				{#snippet control()}{@render onOff('ntfy', d)}{/snippet}
				<SettingSub title={t('notify.ntfyUrl')} for="pn-ntfy-url">
					{#snippet control()}
						<input
							id="pn-ntfy-url"
							class="field mono"
							type="url"
							bind:value={d.ntfy.url}
							placeholder="https://ntfy.sh/…"
							spellcheck="false"
							required={d.ntfy.enabled}
						/>
					{/snippet}
				</SettingSub>
				<SettingSub title={t('notify.token')} for="pn-ntfy-token">
					{#snippet description()}{@render secretNote('ntfy')}{/snippet}
					{#snippet control()}
						<input
							id="pn-ntfy-token"
							class="field mono"
							type="password"
							bind:value={d.ntfy.token}
							autocomplete="new-password"
						/>
					{/snippet}
				</SettingSub>
				{#if d.ntfy.enabled}{@render testRow('ntfy')}{/if}
			</SettingRow>

			<SettingRow
				id="pn-telegram"
				icon="send"
				title={t('notify.telegram')}
				for="pn-telegram-on"
				bind:open={open.telegram}
			>
				{#snippet control()}{@render onOff('telegram', d)}{/snippet}
				<SettingSub title={t('notify.botToken')} for="pn-telegram-token">
					{#snippet description()}{@render secretNote('telegram')}{/snippet}
					{#snippet control()}
						<input
							id="pn-telegram-token"
							class="field mono"
							type="password"
							bind:value={d.telegram.token}
							autocomplete="new-password"
							required={d.telegram.enabled && !secretSet.telegram}
						/>
					{/snippet}
				</SettingSub>
				<SettingSub title={t('notify.chatId')} for="pn-telegram-chat">
					{#snippet control()}
						<input
							id="pn-telegram-chat"
							class="field mono"
							type="text"
							bind:value={d.telegram.chatID}
							spellcheck="false"
							required={d.telegram.enabled}
						/>
					{/snippet}
				</SettingSub>
				{#if d.telegram.enabled}{@render testRow('telegram')}{/if}
			</SettingRow>

			<SettingRow
				id="pn-discord"
				icon="chat"
				title={t('notify.discord')}
				for="pn-discord-on"
				bind:open={open.discord}
			>
				{#snippet control()}{@render onOff('discord', d)}{/snippet}
				<SettingSub title={t('notify.webhook')} for="pn-discord-webhook">
					{#snippet description()}{@render secretNote('discord')}{/snippet}
					{#snippet control()}
						<input
							id="pn-discord-webhook"
							class="field mono"
							type="password"
							bind:value={d.discord.webhook}
							autocomplete="new-password"
							required={d.discord.enabled && !secretSet.discord}
						/>
					{/snippet}
				</SettingSub>
				{#if d.discord.enabled}{@render testRow('discord')}{/if}
			</SettingRow>

			<SettingRow
				id="pn-homeassistant"
				icon="home"
				title={t('notify.homeassistant')}
				for="pn-homeAssistant-on"
				bind:open={open.homeAssistant}
			>
				{#snippet control()}{@render onOff(
						'homeAssistant',
						d,
						!haServices.length && !d.homeAssistant.enabled,
					)}{/snippet}
				{#if !haServices.length}<div class="srow-block">
						<p class="muted">{t('notify.haNotSetUp')}</p>
					</div>{/if}
				{#if haConns.length > 1}
					<SettingSub title={t('notify.haConnection')} for="pn-ha-connection">
						{#snippet control()}
							<select
								id="pn-ha-connection"
								class="field"
								bind:value={d.homeAssistant.connection}
								onchange={() => loadHAServices(draft?.homeAssistant.connection ?? '')}
							>
								{#each haConns as c (c.id)}<option value={c.id === 'main' ? '' : c.id}
										>{c.name || t('ha.conn.first')}</option
									>{/each}
							</select>
						{/snippet}
					</SettingSub>
				{/if}
				<SettingSub title={t('notify.haService')} for="pn-ha-service">
					{#snippet control()}
						<select
							id="pn-ha-service"
							class="field mono"
							bind:value={d.homeAssistant.service}
							required={d.homeAssistant.enabled}
						>
							<option value=""></option>
							{#each new Set([...haServices, d.homeAssistant.service].filter(Boolean)) as service}<option
									value={service}>{service}</option
								>{/each}
						</select>
					{/snippet}
				</SettingSub>
				{#if d.homeAssistant.enabled}{@render testRow('homeAssistant')}{/if}
			</SettingRow>
		</SettingGroup>

		<SettingGroup title={t('personalNotify.delivery')} id="personal-delivery">
			<SettingRow
				icon="archive"
				title={t('personal.persistQuiet')}
				desc={t('personal.persistHint')}
				for="pn-persist"
			>
				{#snippet control()}
					<input
						id="pn-persist"
						class="switch"
						type="checkbox"
						role="switch"
						bind:checked={d.persistQuiet}
					/>
				{/snippet}
			</SettingRow>
			<SettingRow icon="refresh" title={t('personal.fallback')} for="pn-fallback">
				{#snippet control()}
					<select id="pn-fallback" class="field" bind:value={d.fallback}
						><option value="">{t('personal.noFallback')}</option
						>{#each channels.filter((c) => d[c].enabled) as channel}<option
								value={channel === 'homeAssistant' ? 'homeassistant' : channel}>{channel}</option
							>{/each}</select
					>
				{/snippet}
			</SettingRow>
			<SettingRow id="pn-delivery" icon="activity" title={t('personal.delivery')}>
				{#each Object.entries(delivery ?? {}) as [channel, status]}
					<div class="srow-block">
						<p class={status.ok ? 'notice-ok' : 'notice-error'}>
							{channel}: {t(status.ok ? 'personal.delivered' : 'personal.failed')} · {formatDateTime(
								new Date(status.time),
							)} ({status.attempts})
						</p>
					</div>
				{/each}
				<div class="srow-block actions">
					<button
						type="button"
						class="btn"
						onclick={async () => {
							try {
								const response = await api.get<Answer>('/account/notifications');
								delivery = response.delivery ?? {};
							} catch (e) {
								problem(e);
							}
						}}>{t('personal.refreshDelivery')}</button
					>
				</div>
			</SettingRow>
			<div class="row-actions">
				<button type="submit" class="btn btn-primary" disabled={busy !== null}
					>{t(busy === 'save' ? 'notify.saving' : 'notify.save')}</button
				>
				{#if saved}<p class="notice-ok" role="status">{t('notify.saved')}</p>{/if}
			</div>
		</SettingGroup>
	{/if}
</form>

<style>
	form {
		display: flex;
		flex-direction: column;
		gap: var(--gap);
	}
	.link {
		padding: 0;
		background: none;
		border: 0;
		color: var(--muted);
		font: inherit;
		text-decoration: underline;
		cursor: pointer;
	}
	.test {
		flex-direction: row;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2-5) var(--space-4);
	}
	.test > :global(.notice-ok),
	.test > :global(.notice-error) {
		margin: 0;
		flex: 1 1 280px;
	}
	.row-actions {
		margin-top: var(--space-2);
	}
	.row-actions p {
		margin: 0;
	}
	pre {
		margin: var(--space-1-5) 0 0;
		font-family: var(--font-mono);
		font-size: var(--text-sm);
		white-space: pre-wrap;
		overflow-wrap: anywhere;
	}
</style>
