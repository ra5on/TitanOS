<script lang="ts">
	import PushDevices from '#lib/components/PushDevices.svelte';
	import HealthReport from '#lib/components/HealthReport.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import SettingSub from '#lib/components/settings/SettingSub.svelte';
	import { tick } from 'svelte';
	import { api, ApiError } from '#lib/api.ts';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';

	type Channel = 'email' | 'ntfy' | 'telegram' | 'discord' | 'homeassistant';
	type Settings = {
		language: 'de' | 'en';
		minSeverity: 'info' | 'warning' | 'error';
		email: {
			enabled: boolean;
			host: string;
			port: number;
			security: 'starttls' | 'tls' | 'none';
			username: string;
			password?: string;
			from: string;
			to: string;
		};
		ntfy: { enabled: boolean; url: string; token?: string };
		telegram: { enabled: boolean; token?: string; chatID: string };
		discord: { enabled: boolean; webhook?: string };
		homeAssistant: { enabled: boolean; service: string; connection?: string };
		digest: { enabled: boolean; time: string; errorsNow: boolean };
	};
	type Answer = { settings: Settings; secretSet: Record<Channel, boolean> };

	let draft = $state<Settings | null>(null);
	let secretSet = $state<Partial<Record<Channel, boolean>>>({});
	let haServices = $state<string[] | null>(null);
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
	// Stored secrets that go away with the next save.
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
		// Secrets never come back from the NAS; their fields start empty.
		draft = {
			...res.settings,
			email: {
				...res.settings.email,
				port: res.settings.email.port || 587,
				security: res.settings.email.security || 'starttls',
				password: '',
			},
			ntfy: { ...res.settings.ntfy, token: '' },
			telegram: { ...res.settings.telegram, token: '' },
			discord: { ...res.settings.discord, webhook: '' },
			homeAssistant: res.settings.homeAssistant ?? { enabled: false, service: '' },
			digest: res.settings.digest?.time
				? res.settings.digest
				: { enabled: false, time: '19:00', errorsNow: true },
		};
		secretSet = res.secretSet;
		clear = [];
		if (draft.homeAssistant.connection) loadHAServices(draft.homeAssistant.connection);
	}

	function load() {
		error = null;
		api.get<Answer>('/notifications/settings').then(take, problem);
	}
	$effect(() => {
		load();
		loadHAServices('');
	});

	async function store(): Promise<boolean> {
		if (!draft) return false;
		error = null;
		saved = false;
		try {
			take(
				await api.put<Answer>('/notifications/settings', {
					...draft,
					email: { ...draft.email, port: Number(draft.email.port) || 0 },
					clearSecrets: clear,
				}),
			);
			return true;
		} catch (err) {
			problem(err);
			return false;
		}
	}

	// PLAN 78-1: the fields of a way are the parts of its row, and a closed
	// row has no fields the browser could check. Before storing, the rows of
	// the ways that are switched on open, so that a required field that is
	// still empty is named as before.
	let form = $state<HTMLFormElement>();
	let open = $state<Record<Channel | 'digest', boolean>>({
		email: false,
		ntfy: false,
		telegram: false,
		discord: false,
		homeassistant: false,
		digest: false,
	});
	async function complete(): Promise<boolean> {
		if (!draft) return false;
		if (draft.email.enabled) open.email = true;
		if (draft.ntfy.enabled) open.ntfy = true;
		if (draft.telegram.enabled) open.telegram = true;
		if (draft.discord.enabled) open.discord = true;
		if (draft.homeAssistant.enabled) open.homeassistant = true;
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

	// A test uses what is stored, so that it tests what will be used.
	async function test(channel: Channel) {
		if (!(await complete())) return;
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

{#snippet onOff(
	id: string,
	channel: Channel,
	get: () => boolean,
	set: (on: boolean) => void,
	disabled = false,
)}
	<input
		{id}
		class="switch"
		type="checkbox"
		role="switch"
		checked={get()}
		{disabled}
		onchange={(e) => {
			set(e.currentTarget.checked);
			if (e.currentTarget.checked) open[channel] = true;
		}}
	/>
{/snippet}

<PageHead title={t('notify.settingsTitle')}>
	<p class="muted">{t('notify.intro')}</p>
</PageHead>
<p class="muted"><a href="/account#bell">{t('notify.personalHint')}</a></p>

{#if error}<div class="notice-error" role="alert">
		{error.text}{#if error.detail}<pre>{error.detail}</pre>{/if}{#if !draft}
			<button type="button" class="btn small" onclick={load}>{t('ui.retry')}</button>{/if}
	</div>{/if}

<!-- PLAN 78-1: every way of a notification is a row; its fields open inside
     it. The ways and "Allgemein" are one form with one Save, as before. -->
<SettingGroup title={t('notify.groupDevices')}>
	<!-- The devices keep their look (the component also serves "Mein Konto"
	     and the agent); here they are the parts of a row. -->
	<SettingRow id="push" icon="bell" title={t('push.title')} desc={t('push.intro')}>
		<div class="srow-block"><PushDevices bare /></div>
	</SettingRow>
	<HealthReport />
</SettingGroup>

{#if draft}
	{@const d = draft}
	<form onsubmit={save} bind:this={form}>
		{#snippet digestParts()}
			<SettingSub title={t('digest.time')} for="digest-time">
				{#snippet control()}
					<input id="digest-time" class="field" type="time" bind:value={d.digest.time} />
				{/snippet}
			</SettingSub>
			<SettingSub title={t('digest.errorsNow')} for="digest-errors">
				{#snippet control()}
					<input
						id="digest-errors"
						class="switch"
						type="checkbox"
						role="switch"
						bind:checked={d.digest.errorsNow}
					/>
				{/snippet}
			</SettingSub>
			<div class="srow-block"><p class="muted">{t('digest.hint')}</p></div>
		{/snippet}
		<SettingGroup title={t('notify.general')}>
			<SettingRow icon="info" title={t('notify.minSeverity')} for="notify-min">
				{#snippet control()}
					<select id="notify-min" class="field" bind:value={d.minSeverity}>
						<option value="info">{t('notify.min.info')}</option>
						<option value="warning">{t('notify.min.warning')}</option>
						<option value="error">{t('notify.min.error')}</option>
					</select>
				{/snippet}
			</SettingRow>
			<SettingRow icon="globe" title={t('notify.language')} for="notify-language">
				{#snippet control()}
					<select id="notify-language" class="field" bind:value={d.language}>
						<option value="de">Deutsch</option>
						<option value="en">English</option>
					</select>
				{/snippet}
			</SettingRow>
			<SettingRow
				icon="clock"
				title={t('digest.enabled')}
				for="digest-enabled"
				bind:open={open.digest}
				children={d.digest.enabled ? digestParts : undefined}
			>
				{#snippet control()}
					<input
						id="digest-enabled"
						class="switch"
						type="checkbox"
						role="switch"
						checked={d.digest.enabled}
						onchange={(e) => {
							d.digest.enabled = e.currentTarget.checked;
							if (d.digest.enabled) open.digest = true;
						}}
					/>
				{/snippet}
			</SettingRow>
		</SettingGroup>

		<SettingGroup title={t('notify.channels')} id="notify-channels">
			<SettingRow
				id="notify-email"
				icon="send"
				title={t('notify.email')}
				desc={t('notify.emailHint')}
				for="notify-email-on"
				bind:open={open.email}
			>
				{#snippet control()}
					{@render onOff(
						'notify-email-on',
						'email',
						() => d.email.enabled,
						(on) => (d.email.enabled = on),
					)}
				{/snippet}
				<SettingSub title={t('notify.host')} for="notify-email-host">
					{#snippet control()}
						<input
							id="notify-email-host"
							class="field mono"
							type="text"
							bind:value={d.email.host}
							placeholder="smtp.example.org"
							spellcheck="false"
							autocapitalize="none"
							required={d.email.enabled}
						/>
					{/snippet}
				</SettingSub>
				<SettingSub title={t('notify.port')} for="notify-email-port">
					{#snippet control()}
						<input
							id="notify-email-port"
							class="field mono"
							type="number"
							min="1"
							max="65535"
							bind:value={d.email.port}
							required={d.email.enabled}
						/>
					{/snippet}
				</SettingSub>
				<SettingSub title={t('notify.security')} for="notify-email-security">
					{#snippet control()}
						<select id="notify-email-security" class="field" bind:value={d.email.security}>
							<option value="starttls">{t('notify.security.starttls')}</option>
							<option value="tls">{t('notify.security.tls')}</option>
							<option value="none">{t('notify.security.none')}</option>
						</select>
					{/snippet}
				</SettingSub>
				{#if d.email.security === 'none'}<div class="srow-block">
						<p class="notice-warn">{t('notify.securityNone')}</p>
					</div>{/if}
				<SettingSub title={t('notify.username')} for="notify-email-user">
					{#snippet control()}
						<input
							id="notify-email-user"
							class="field mono"
							type="text"
							bind:value={d.email.username}
							autocomplete="off"
							spellcheck="false"
							autocapitalize="none"
						/>
					{/snippet}
				</SettingSub>
				<SettingSub title={t('notify.password')} for="notify-email-password">
					{#snippet description()}{@render secretNote('email')}{/snippet}
					{#snippet control()}
						<input
							id="notify-email-password"
							class="field mono"
							type="password"
							bind:value={d.email.password}
							autocomplete="new-password"
						/>
					{/snippet}
				</SettingSub>
				<SettingSub title={t('notify.from')} for="notify-email-from">
					{#snippet control()}
						<input
							id="notify-email-from"
							class="field mono"
							type="text"
							bind:value={d.email.from}
							placeholder="nas@example.org"
							spellcheck="false"
							required={d.email.enabled}
						/>
					{/snippet}
				</SettingSub>
				<SettingSub title={t('notify.to')} for="notify-email-to">
					{#snippet control()}
						<input
							id="notify-email-to"
							class="field mono"
							type="text"
							bind:value={d.email.to}
							placeholder="me@example.org"
							spellcheck="false"
							required={d.email.enabled}
						/>
					{/snippet}
				</SettingSub>
				{@render testRow('email')}
			</SettingRow>

			<SettingRow
				id="notify-ntfy"
				icon="bell"
				title={t('notify.ntfy')}
				desc={t('notify.ntfyHint')}
				for="notify-ntfy-on"
				bind:open={open.ntfy}
			>
				{#snippet control()}
					{@render onOff(
						'notify-ntfy-on',
						'ntfy',
						() => d.ntfy.enabled,
						(on) => (d.ntfy.enabled = on),
					)}
				{/snippet}
				<SettingSub title={t('notify.ntfyUrl')} for="notify-ntfy-url">
					{#snippet control()}
						<input
							id="notify-ntfy-url"
							class="field mono"
							type="url"
							bind:value={d.ntfy.url}
							placeholder="https://ntfy.sh/…"
							spellcheck="false"
							required={d.ntfy.enabled}
						/>
					{/snippet}
				</SettingSub>
				<SettingSub title={t('notify.token')} for="notify-ntfy-token">
					{#snippet description()}{@render secretNote('ntfy')}{/snippet}
					{#snippet control()}
						<input
							id="notify-ntfy-token"
							class="field mono"
							type="password"
							bind:value={d.ntfy.token}
							autocomplete="new-password"
						/>
					{/snippet}
				</SettingSub>
				{@render testRow('ntfy')}
			</SettingRow>

			<SettingRow
				id="notify-telegram"
				icon="chat"
				title={t('notify.telegram')}
				desc={t('notify.telegramHint')}
				for="notify-telegram-on"
				bind:open={open.telegram}
			>
				{#snippet control()}
					{@render onOff(
						'notify-telegram-on',
						'telegram',
						() => d.telegram.enabled,
						(on) => (d.telegram.enabled = on),
					)}
				{/snippet}
				<SettingSub title={t('notify.botToken')} for="notify-telegram-token">
					{#snippet description()}{@render secretNote('telegram')}{/snippet}
					{#snippet control()}
						<input
							id="notify-telegram-token"
							class="field mono"
							type="password"
							bind:value={d.telegram.token}
							autocomplete="new-password"
							required={d.telegram.enabled && !secretSet.telegram}
						/>
					{/snippet}
				</SettingSub>
				<SettingSub title={t('notify.chatId')} for="notify-telegram-chat">
					{#snippet control()}
						<input
							id="notify-telegram-chat"
							class="field mono"
							type="text"
							bind:value={d.telegram.chatID}
							spellcheck="false"
							required={d.telegram.enabled}
						/>
					{/snippet}
				</SettingSub>
				{@render testRow('telegram')}
			</SettingRow>

			<SettingRow
				id="notify-discord"
				icon="chat"
				title={t('notify.discord')}
				desc={t('notify.discordHint')}
				for="notify-discord-on"
				bind:open={open.discord}
			>
				{#snippet control()}
					{@render onOff(
						'notify-discord-on',
						'discord',
						() => d.discord.enabled,
						(on) => (d.discord.enabled = on),
					)}
				{/snippet}
				<SettingSub title={t('notify.webhook')} for="notify-discord-webhook">
					{#snippet description()}{@render secretNote('discord')}{/snippet}
					{#snippet control()}
						<input
							id="notify-discord-webhook"
							class="field mono"
							type="password"
							bind:value={d.discord.webhook}
							autocomplete="new-password"
							required={d.discord.enabled && !secretSet.discord}
						/>
					{/snippet}
				</SettingSub>
				{@render testRow('discord')}
			</SettingRow>

			<SettingRow
				id="notify-homeassistant"
				icon="home"
				title={t('notify.homeassistant')}
				desc={t('notify.haHint')}
				for="notify-ha-on"
				bind:open={open.homeassistant}
			>
				{#snippet control()}
					{@render onOff(
						'notify-ha-on',
						'homeassistant',
						() => d.homeAssistant.enabled,
						(on) => (d.homeAssistant.enabled = on),
						!haServices?.length && !d.homeAssistant.enabled,
					)}
				{/snippet}
				{#if haServices && haServices.length === 0}<div class="srow-block">
						<p class="muted">
							{t('notify.haNotSetUp')} <a href="/settings/homeassistant">{t('ha.title')}</a>
						</p>
					</div>{/if}
				{#if haConns.length > 1}
					<SettingSub title={t('notify.haConnection')} for="notify-ha-connection">
						{#snippet control()}
							<select
								id="notify-ha-connection"
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
				<SettingSub title={t('notify.haService')} for="notify-ha-service">
					{#snippet control()}
						<select
							id="notify-ha-service"
							class="field mono"
							bind:value={d.homeAssistant.service}
							required={d.homeAssistant.enabled}
						>
							<option value=""></option>
							{#each new Set([...(haServices ?? []), d.homeAssistant.service].filter(Boolean)) as service (service)}<option
									value={service}>{service}</option
								>{/each}
						</select>
					{/snippet}
				</SettingSub>
				{@render testRow('homeassistant')}
			</SettingRow>

			<div class="row-actions">
				<button type="submit" class="btn btn-primary" disabled={busy !== null}
					>{t(busy === 'save' ? 'notify.saving' : 'notify.save')}</button
				>
				{#if saved}<p class="notice-ok" role="status">{t('notify.saved')}</p>{/if}
			</div>
		</SettingGroup>
	</form>
{/if}

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
