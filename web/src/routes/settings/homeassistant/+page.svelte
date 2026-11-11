<script lang="ts">
	// The connection to Home Assistant, storage for it on the NAS, sensors and
	// buttons over MQTT, and what MCP clients may switch. The token never
	// comes back from the NAS.
	import PageHead from '#lib/components/PageHead.svelte';
	import HAStorage from './HAStorage.svelte';
	import Dialog from '#lib/components/Dialog.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import SettingSub from '#lib/components/settings/SettingSub.svelte';
	import { tick } from 'svelte';
	import { api, ApiError } from '#lib/api.ts';
	import { errorText, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { formatDateTime } from '#lib/format.ts';

	type MQTTDevice = { group: string; id: string; name: string };
	type MQTT = {
		devices?: Record<string, string[]>;
		enabled: boolean;
		host: string;
		port: number;
		username: string;
		password?: string;
		tls: boolean;
		prefix: string;
		groups: string[];
		commands: string[];
		interval?: number;
		id?: string;
	};
	type Policy = {
		aliases?: string[];
		readOnly?: boolean;
		services: string[] | null;
		description?: string;
	};
	type Settings = {
		name?: string;
		policies?: Record<string, Policy>;
		enabled: boolean;
		url: string;
		token?: string;
		fingerprint: string;
		domains: string[];
		entities: string[];
		account?: string;
		mqtt: MQTT;
	};
	type Status = { watching: boolean; watchError: string; mqtt: boolean; mqttError: string };
	type Answer = {
		mqttDevices?: MQTTDevice[];
		diagnostics?: {
			lastConnected: string;
			lastDisconnected: string;
			evictedTransitions: number;
			watchedEntities: number;
		};
		settings: Settings;
		tokenSet: boolean;
		mqttPasswordSet: boolean;
		domains: string[];
		sensorGroups: string[];
		commandGroups: string[];
		status: Status;
	};
	type Entity = { id: string; name: string; area?: string; state: string; domain: string };

	// Domains whose calls need the ticked confirmation; the rest switch
	// after a normal confirmation, sensors are read only.
	const DANGER = [
		'cover',
		'lock',
		'alarm_control_panel',
		'vacuum',
		'button',
		'valve',
		'water_heater',
	];
	const READ_ONLY = [
		'sensor',
		'binary_sensor',
		'weather',
		'person',
		'device_tracker',
		'sun',
		'zone',
	];

	// Several connections (PLAN §45): the page edits the chosen one.
	type Connection = {
		id: string;
		name: string;
		url: string;
		enabled: boolean;
		active: boolean;
		mqtt: boolean;
	};
	let connections = $state<Connection[]>([]);
	let maxConnections = $state(8);
	let current = $state('main');
	let adding = $state(false);
	let newName = $state('');
	let removing = $state<Connection | null>(null);
	let listBusy = $state(false);
	const on = (path: string) =>
		current === 'main' ? path : `${path}?connection=${encodeURIComponent(current)}`;
	const label = (c: Connection) =>
		c.name || (c.id === 'main' ? t('ha.conn.first') : t('ha.conn.unnamed'));

	async function loadConnections() {
		try {
			const res = await api.get<{ connections: Connection[]; max: number }>(
				'/homeassistant/connections',
			);
			connections = res.connections ?? [];
			maxConnections = res.max || 8;
		} catch {
			connections = [];
		}
	}
	function choose(id: string, reload = false) {
		if (id === current && !reload) return;
		current = id;
		draft = null;
		entities = null;
		checked = null;
		checkError = '';
		saved = false;
		error = null;
		api.get<Answer>(on('/homeassistant/settings')).then(take, problem);
	}
	async function addConnection(event: SubmitEvent) {
		event.preventDefault();
		listBusy = true;
		try {
			const res = await api.post<{ id: string; connections: Connection[] }>(
				'/homeassistant/connections',
				{ name: newName.trim() },
			);
			connections = res.connections;
			adding = false;
			newName = '';
			choose(res.id);
		} catch (err) {
			problem(err);
		}
		listBusy = false;
	}
	async function removeConnection() {
		if (!removing) return;
		listBusy = true;
		try {
			const res = await api.delete<{ connections: Connection[] }>(
				`/homeassistant/connections/${encodeURIComponent(removing.id)}`,
			);
			connections = res.connections;
			if (current === removing.id) choose('main', true);
			removing = null;
		} catch (err) {
			problem(err);
		}
		listBusy = false;
	}

	let draft = $state<Settings | null>(null);
	let tokenSet = $state(false);
	let mqttPasswordSet = $state(false);
	let clear = $state<string[]>([]);
	let domains = $state<string[]>([]);
	let mqttDevices = $state<MQTTDevice[]>([]);
	let sensorGroups = $state<string[]>([]);
	let commandGroups = $state<string[]>([]);
	let status = $state<Status | null>(null);
	let accounts = $state<{ name: string; role: string }[]>([]);
	let error = $state<{ text: string; detail: string } | null>(null);
	let saved = $state(false);
	let busy = $state<'save' | 'check' | 'entities' | null>(null);
	let checked = $state<{ version: string; location: string } | null>(null);
	let checkError = $state('');
	let entities = $state<Entity[] | null>(null);
	let search = $state('');
	let diagnostics = $state<Answer['diagnostics']>();
	function policy(id: string): Policy {
		return draft?.policies?.[id] ?? { services: null };
	}
	function setPolicy(id: string, update: Partial<Policy>) {
		if (draft) draft.policies = { ...draft.policies, [id]: { ...policy(id), ...update } };
	}

	function problem(err: unknown) {
		const code = err instanceof ApiError ? err.code : 'internal';
		error = {
			text: errorText(code),
			detail: err instanceof ApiError && code !== 'internal' ? err.message : '',
		};
	}

	// The interval titand works with, not the one being typed.
	let sentEvery = $state(30);

	function take(res: Answer) {
		sentEvery = res.settings.mqtt.interval || 30;
		// A connection without an address shows its fields right away.
		if (!res.settings.url) open.connection = true;
		draft = {
			...res.settings,
			token: '',
			fingerprint: res.settings.fingerprint ?? '',
			account: res.settings.account ?? '',
			mqtt: { ...res.settings.mqtt, password: '' },
		};
		tokenSet = res.tokenSet;
		mqttPasswordSet = res.mqttPasswordSet;
		domains = res.domains;
		sensorGroups = res.sensorGroups;
		mqttDevices = res.mqttDevices ?? [];
		commandGroups = res.commandGroups;
		status = res.status;
		diagnostics = res.diagnostics;
		clear = [];
	}

	$effect(() => {
		api.get<Answer>(on('/homeassistant/settings')).then(take, problem);
		void loadConnections();
		api.get<{ name: string; role: string }[]>('/users').then(
			(list) => (accounts = list.filter((a) => a.role === 'owner' || a.role === 'admin')),
			() => {},
		);
	});

	async function store(): Promise<boolean> {
		if (!draft) return false;
		error = null;
		saved = false;
		try {
			take(
				await api.put<Answer>(on('/homeassistant/settings'), {
					...draft,
					mqtt: {
						...draft.mqtt,
						port: Number(draft.mqtt.port) || 0,
						interval: Number(draft.mqtt.interval) || 0,
					},
					clearSecrets: clear,
				}),
			);
			void loadConnections();
			return true;
		} catch (err) {
			problem(err);
			return false;
		}
	}

	// PLAN 78-1: the fields are the parts of rows, and a closed row has no
	// fields the browser could check. Before storing, the rows that are
	// switched on open, so that a required field that is still empty is named
	// as before.
	let form = $state<HTMLFormElement>();
	let open = $state({ connection: false, mqtt: false });
	async function complete(): Promise<boolean> {
		if (!draft) return false;
		if (draft.enabled) open.connection = true;
		if (draft.mqtt.enabled) open.mqtt = true;
		await tick();
		return form?.reportValidity() ?? true;
	}

	async function save(event: SubmitEvent) {
		event.preventDefault();
		if (!(await complete())) return;
		busy = 'save';
		saved = await store();
		busy = null;
	}

	async function check() {
		busy = 'check';
		checked = null;
		checkError = '';
		if (await store()) {
			try {
				checked = await api.post<{ version: string; location: string }>(
					on('/homeassistant/check'),
					{},
				);
			} catch (err) {
				checkError =
					err instanceof ApiError ? `${t('ha.checkFailed')} ${err.message}` : errorText('internal');
			}
		}
		busy = null;
	}

	async function loadEntities() {
		busy = 'entities';
		try {
			entities = (await api.get<{ entities: Entity[] }>(on('/homeassistant/entities'))).entities;
		} catch (err) {
			problem(err);
		}
		busy = null;
	}

	function toggleClear(name: string) {
		clear = clear.includes(name) ? clear.filter((n) => n !== name) : [...clear, name];
	}

	function toggle(list: string[], value: string): string[] {
		return list.includes(value) ? list.filter((v) => v !== value) : [...list, value].sort();
	}

	const shown = $derived.by(() => {
		if (!entities || !draft) return [];
		const q = search.trim().toLowerCase();
		const known = new Set(domains);
		return entities
			.filter(
				(e) =>
					known.has(e.domain) &&
					(!q || `${e.id} ${e.name} ${e.area ?? ''}`.toLowerCase().includes(q)),
			)
			.slice(0, 200);
	});
	const level = (domain: string) =>
		READ_ONLY.includes(domain) ? 'read' : DANGER.includes(domain) ? 'danger' : 'normal';
</script>

<PageHead title={t('ha.title')}>
	<p class="muted">{t('ha.intro')}</p>
</PageHead>

{#if error}<div class="notice-error" role="alert">
		{error.text}{#if error.detail}<pre>{error.detail}</pre>{/if}
	</div>{/if}

<!-- A stored secret never comes back: that one is stored stands below the
     field's name (not as a placeholder, which a narrow field would clip). -->
{#snippet secretNote(name: string, set: boolean)}
	{#if set}
		{#if !clear.includes(name)}{t('notify.secretStored')}{/if}
		<button type="button" class="link" onclick={() => toggleClear(name)}
			>{clear.includes(name) ? t('notify.secretRemoved') : t('notify.secretRemove')}</button
		>
	{/if}
{/snippet}

<!-- PLAN 78-1: every connection is a row; a click on it chooses the one the
     rows below work on. -->
{#if connections.length}
	<SettingGroup title={t('ha.conn.title')} id="connections">
		{#snippet tools()}
			<button
				type="button"
				class="btn small"
				disabled={listBusy || connections.length >= maxConnections}
				onclick={() => (adding = true)}>{t('ha.conn.add')}</button
			>
		{/snippet}
		{#each connections as c (c.id)}
			<div class="srow" class:chosen={c.id === current}>
				<div class="srow-head">
					<span class="srow-ico" aria-hidden="true"><Icon name="home" size={19} /></span>
					<button
						type="button"
						class="conn srow-text"
						aria-current={c.id === current ? 'true' : undefined}
						onclick={() => choose(c.id)}
					>
						<span class="srow-title conn-name">{label(c)}</span>
						<span class="srow-desc mono">{c.url || t('ha.conn.noUrl')}</span>
					</button>
					<div class="srow-ctl">
						<span class="chip" class:ok={c.active}
							>{t(c.active ? 'ha.conn.on' : c.url ? 'ha.conn.off' : 'ha.conn.empty')}{c.mqtt
								? ' · MQTT'
								: ''}</span
						>
						{#if c.id !== 'main'}
							<button
								type="button"
								class="btn small"
								disabled={listBusy}
								onclick={() => (removing = c)}>{t('ha.conn.remove')}</button
							>
						{/if}
					</div>
				</div>
			</div>
		{/each}
		{#if connections.length > 1}<p class="muted">{t('ha.conn.hint')}</p>{/if}
	</SettingGroup>
{/if}

{#if draft}
	{@const d = draft}
	{@const chosen = connections.find((c) => c.id === current)}
	<!-- The form of the page: the fields of the connection and the broker
	     belong to it by their `form` attribute, as before (they are parts of
	     rows now, a closed row has none). -->
	<form id="ha-form" hidden onsubmit={save} bind:this={form}></form>
	<!-- Order decided 31.10.2026: connection, storage, sensors and buttons
	     (MQTT), devices for MCP. Each is a row that opens (PLAN 78-1). -->
	<SettingGroup title={connections.length > 1 && chosen ? label(chosen) : undefined}>
		<SettingRow
			id="connection"
			icon="home"
			title={t('ha.connection')}
			for="ha-enabled"
			bind:open={open.connection}
		>
			{#snippet description()}<span class="mono">{d.url || t('ha.conn.noUrl')}</span>{/snippet}
			{#snippet control()}
				<input
					id="ha-enabled"
					class="switch"
					type="checkbox"
					role="switch"
					checked={d.enabled}
					onchange={(e) => {
						d.enabled = e.currentTarget.checked;
						if (d.enabled) open.connection = true;
					}}
				/>
			{/snippet}
			<SettingSub title={t('ha.conn.name')} for="ha-name">
				{#snippet control()}
					<input
						id="ha-name"
						class="field"
						type="text"
						maxlength="40"
						form="ha-form"
						bind:value={d.name}
						placeholder={current === 'main' ? t('ha.conn.first') : ''}
					/>
				{/snippet}
			</SettingSub>
			<SettingSub title={t('ha.url')} for="ha-url">
				{#snippet control()}
					<input
						id="ha-url"
						class="field mono"
						type="url"
						form="ha-form"
						bind:value={d.url}
						placeholder="http://homeassistant.local:8123"
						spellcheck="false"
						autocapitalize="none"
						required={d.enabled}
					/>
				{/snippet}
			</SettingSub>
			<SettingSub title={t('ha.token')} for="ha-token">
				{#snippet description()}
					{t('ha.tokenHint')}
					{@render secretNote('token', tokenSet)}
				{/snippet}
				{#snippet control()}
					<input
						id="ha-token"
						class="field mono"
						type="password"
						form="ha-form"
						bind:value={d.token}
						autocomplete="new-password"
						required={d.enabled && !tokenSet}
					/>
				{/snippet}
			</SettingSub>
			<SettingSub
				title={t('ha.fingerprintLabel')}
				desc="{t('ha.fingerprint')}: {t('ha.fingerprintHint')}"
				for="ha-fingerprint"
			>
				{#snippet control()}
					<input
						id="ha-fingerprint"
						class="field mono"
						type="text"
						form="ha-form"
						bind:value={d.fingerprint}
						placeholder="AB:CD:…"
						spellcheck="false"
					/>
				{/snippet}
			</SettingSub>
			<div class="srow-block test">
				<button type="button" class="btn" disabled={busy !== null} onclick={check}
					>{t(busy === 'check' ? 'ha.checking' : 'ha.check')}</button
				>
				{#if checked}<p class="notice-ok" role="status">
						{t('ha.checkOk', { version: checked.version, location: checked.location })}
					</p>{/if}
				{#if checkError}<div class="notice-error" role="alert">
						{t('ha.checkFailed')}
						<pre>{checkError}</pre>
					</div>{/if}
			</div>
		</SettingRow>

		{#if diagnostics}
			{@const diag = diagnostics}
			{@const connectedAt =
				diag.lastConnected && !diag.lastConnected.startsWith('0001-')
					? formatDateTime(new Date(diag.lastConnected))
					: '—'}
			<SettingRow
				id="diagnostics"
				icon="activity"
				title={t('ha.diagnostics')}
				summary="{t('ha.lastConnected')}: {connectedAt}"
			>
				<SettingSub title={t('ha.lastConnected')}>
					{#snippet control()}{connectedAt}{/snippet}
				</SettingSub>
				<SettingSub title={t('ha.lastDisconnected')}>
					{#snippet control()}{diag.lastDisconnected && !diag.lastDisconnected.startsWith('0001-')
							? formatDateTime(new Date(diag.lastDisconnected))
							: '—'}{/snippet}
				</SettingSub>
				<SettingSub title={t('ha.evicted')}>
					{#snippet control()}{diag.evictedTransitions}{/snippet}
				</SettingSub>
				<div class="srow-block"><p class="muted">{t('ha.gaps')}</p></div>
			</SettingRow>
		{/if}

		{#key current}<HAStorage connection={current} />{/key}

		<SettingRow
			id="mqtt"
			icon="activity"
			title={t('ha.mqtt')}
			desc={t('ha.mqttIntro')}
			for="ha-mqtt-enabled"
			bind:open={open.mqtt}
		>
			{#snippet control()}
				<!-- The state of the broker in words, also while the row is closed. -->
				{#if status?.mqtt}<span class="chip ok">{t('ha.conn.on')}</span
					>{:else if status?.mqttError}<span class="chip error">{t('ha.mqttNoConnection')}</span
					>{/if}
				<input
					id="ha-mqtt-enabled"
					class="switch"
					type="checkbox"
					role="switch"
					checked={d.mqtt.enabled}
					onchange={(e) => {
						d.mqtt.enabled = e.currentTarget.checked;
						if (d.mqtt.enabled) open.mqtt = true;
					}}
				/>
			{/snippet}
			<SettingSub title={t('ha.mqttHost')} for="ha-mqtt-host">
				{#snippet control()}
					<input
						id="ha-mqtt-host"
						class="field mono"
						type="text"
						bind:value={d.mqtt.host}
						placeholder="homeassistant.local"
						spellcheck="false"
						autocapitalize="none"
						required={d.mqtt.enabled}
						form="ha-form"
					/>
				{/snippet}
			</SettingSub>
			<SettingSub title={t('notify.port')} for="ha-mqtt-port">
				{#snippet control()}
					<input
						id="ha-mqtt-port"
						class="field mono"
						type="number"
						form="ha-form"
						min="1"
						max="65535"
						bind:value={d.mqtt.port}
					/>
				{/snippet}
			</SettingSub>
			<SettingSub title="TLS" for="ha-mqtt-tls">
				{#snippet control()}
					<input
						id="ha-mqtt-tls"
						class="switch"
						type="checkbox"
						role="switch"
						bind:checked={d.mqtt.tls}
					/>
				{/snippet}
			</SettingSub>
			<SettingSub title={t('notify.username')} for="ha-mqtt-user">
				{#snippet control()}
					<input
						id="ha-mqtt-user"
						class="field mono"
						type="text"
						bind:value={d.mqtt.username}
						autocomplete="off"
						spellcheck="false"
						autocapitalize="none"
					/>
				{/snippet}
			</SettingSub>
			<SettingSub title={t('notify.password')} for="ha-mqtt-password">
				{#snippet description()}{@render secretNote('mqtt', mqttPasswordSet)}{/snippet}
				{#snippet control()}
					<input
						id="ha-mqtt-password"
						class="field mono"
						type="password"
						bind:value={d.mqtt.password}
						autocomplete="new-password"
					/>
				{/snippet}
			</SettingSub>
			<SettingSub title={t('ha.mqttPrefix')} for="ha-mqtt-prefix">
				{#snippet control()}
					<input
						id="ha-mqtt-prefix"
						class="field mono"
						type="text"
						bind:value={d.mqtt.prefix}
						spellcheck="false"
					/>
				{/snippet}
			</SettingSub>
			<SettingSub
				title={t('ha.mqttInterval')}
				desc={t('ha.mqttIntervalHint')}
				for="ha-mqtt-interval"
			>
				{#snippet control()}
					<input
						id="ha-mqtt-interval"
						class="field"
						type="number"
						min="5"
						max="3600"
						step="1"
						placeholder="30"
						bind:value={d.mqtt.interval}
					/>
				{/snippet}
			</SettingSub>
			<div class="srow-block">
				<fieldset>
					<legend>{t('ha.sensorGroups')}</legend>
					{#each sensorGroups as group}
						<label class="check"
							><input
								type="checkbox"
								checked={d.mqtt.groups.includes(group)}
								onchange={() => (d.mqtt.groups = toggle(d.mqtt.groups, group))}
							/><span>{t(`ha.group.${group}` as MessageKey)}</span></label
						>
					{/each}
				</fieldset>
				{#each sensorGroups.filter((g) => d.mqtt.groups.includes(g)) as group}
					{@const devices = mqttDevices.filter((dev) => dev.group === group)}
					{#if !['system', 'work'].includes(group)}<details class="device-selection">
							<summary>{t(`ha.group.${group}` as MessageKey)} · {t('ha.deviceSelection')}</summary>
							<label class="check"
								><input
									type="checkbox"
									checked={!d.mqtt.devices || !(group in d.mqtt.devices)}
									onchange={(e) => {
										const next = { ...d.mqtt.devices };
										if (e.currentTarget.checked) delete next[group];
										else next[group] = devices.map((dev) => dev.id);
										d.mqtt.devices = next;
									}}
								/>{t('ha.allDevices')}</label
							>
							{#if d.mqtt.devices && group in d.mqtt.devices}
								{#each [...new Set( [...devices.map((dev) => dev.id), ...d.mqtt.devices[group]] )] as id}
									<label class="check"
										><input
											type="checkbox"
											checked={d.mqtt.devices[group].includes(id)}
											onchange={() =>
												(d.mqtt.devices = {
													...d.mqtt.devices,
													[group]: toggle(d.mqtt.devices![group], id),
												})}
										/>{devices.find((dev) => dev.id === id)?.name ?? id}</label
									>
								{/each}
							{/if}
						</details>{/if}
				{/each}
			</div>
			<SettingSub title={t('ha.accountLabel')} desc={t('ha.accountIntro')} for="ha-account">
				{#snippet control()}
					<select id="ha-account" class="field" bind:value={d.account}>
						<option value="">{t('ha.accountNone')}</option>
						{#each accounts as account (account.name)}<option value={account.name}
								>{account.name}</option
							>{/each}
					</select>
				{/snippet}
			</SettingSub>
			<div class="srow-block">
				<fieldset>
					<legend>{t('ha.commandGroups')}</legend>
					{#each commandGroups as group}
						<label class="check"
							><input
								type="checkbox"
								checked={d.mqtt.commands.includes(group)}
								onchange={() => (d.mqtt.commands = toggle(d.mqtt.commands, group))}
							/><span>{t(`ha.command.${group}` as MessageKey)}</span></label
						>
					{/each}
				</fieldset>
				{#if d.mqtt.commands.length}
					<p class="notice-warn">{t('ha.commandsWarning')}</p>
					{#if !d.account}<p class="notice-warn">{t('ha.commandsNoAccount')}</p>{/if}
				{/if}
			</div>
			{#if status?.mqtt || status?.mqttError}
				<div class="srow-block">
					{#if status?.mqtt}<p class="notice-ok" role="status">
							{t('ha.mqttConnected', { seconds: String(sentEvery) })}
						</p>{:else if status?.mqttError}<div class="notice-error" role="alert">
							{t('ha.mqttFailed')}
							<pre>{status.mqttError}</pre>
						</div>{/if}
				</div>
			{/if}
		</SettingRow>

		<SettingRow
			id="release"
			icon="shield"
			title={t('ha.release')}
			desc={t('ha.releaseIntro')}
			summary={t('ha.releaseSummary', { domains: d.domains.length, entities: d.entities.length })}
		>
			<div class="srow-block">
				<fieldset>
					<legend>{t('ha.domains')}</legend>
					{#each domains as domain}
						<label class="check domain"
							><input
								type="checkbox"
								checked={d.domains.includes(domain)}
								onchange={() => (d.domains = toggle(d.domains, domain))}
							/><span class="mono">{domain}</span><span
								class="chip {level(domain) === 'danger' ? 'warn' : 'info'}"
								>{t(`ha.level.${level(domain)}` as MessageKey)}</span
							></label
						>
					{/each}
				</fieldset>
			</div>
			<!-- The devices of Home Assistant are a tool of their own: a list to
			     search, with the rights of each device. -->
			<div class="srow-block entities">
				<div class="row end">
					<button
						type="button"
						class="btn"
						disabled={busy !== null || !tokenSet}
						onclick={loadEntities}>{t(entities ? 'ha.reloadEntities' : 'ha.loadEntities')}</button
					>
					{#if entities}<label class="narrow"
							><span>{t('ha.searchEntities')}</span><input
								class="field"
								type="search"
								bind:value={search}
							/></label
						>{/if}
				</div>
				{#if d.entities.length}<p class="muted small">
						{t('ha.singleCount', { count: d.entities.length })}
					</p>{/if}
				{#if entities}
					<ul>
						{#each shown as entity (entity.id)}
							{@const viaDomain = d.domains.includes(entity.domain)}
							<li>
								<label class="check"
									><input
										type="checkbox"
										checked={viaDomain || d.entities.includes(entity.id)}
										disabled={viaDomain}
										onchange={() => (d.entities = toggle(d.entities, entity.id))}
									/>
									<span
										><span class="name">{entity.name}</span>
										<span class="mono muted">{entity.id}</span>{#if entity.area}<span class="muted"
												>&nbsp;· {entity.area}</span
											>{/if}</span
									>
								</label>
								<span class="mono muted">{entity.state}</span>
								<details>
									<summary>{t('ha.policy')}</summary>
									<label
										>{t('ha.aliases')}<input
											class="field"
											value={(policy(entity.id).aliases ?? []).join(', ')}
											onchange={(e) =>
												setPolicy(entity.id, {
													aliases: e.currentTarget.value
														.split(',')
														.map((v) => v.trim())
														.filter(Boolean),
												})}
										/></label
									>
									<label class="check"
										><input
											type="checkbox"
											checked={policy(entity.id).readOnly ?? false}
											onchange={(e) => setPolicy(entity.id, { readOnly: e.currentTarget.checked })}
										/>{t('ha.readOnly')}</label
									>
									<label
										>{t('ha.services')}<input
											class="field mono"
											value={policy(entity.id).services?.join(', ') ?? ''}
											onchange={(e) =>
												setPolicy(entity.id, {
													services: e.currentTarget.value.trim()
														? e.currentTarget.value
																.split(',')
																.map((v) => v.trim())
																.filter(Boolean)
														: null,
												})}
										/></label
									>
									<label
										>{t('ha.description')}<input
											class="field"
											maxlength="1000"
											value={policy(entity.id).description ?? ''}
											onchange={(e) => setPolicy(entity.id, { description: e.currentTarget.value })}
										/></label
									>
								</details>
							</li>
						{:else}
							<li class="muted">{t('ha.noEntities')}</li>
						{/each}
					</ul>
				{/if}
			</div>
		</SettingRow>

		<div class="row-actions">
			<button type="submit" form="ha-form" class="btn btn-primary" disabled={busy !== null}
				>{t(busy === 'save' ? 'notify.saving' : 'notify.save')}</button
			>
			{#if saved}<p class="notice-ok" role="status">{t('notify.saved')}</p>{/if}
		</div>
	</SettingGroup>
{/if}

{#if adding}
	<Dialog title={t('ha.conn.add')} locked={listBusy} onclose={() => (adding = false)}>
		<form onsubmit={addConnection}>
			<label class="wide"
				><span>{t('ha.conn.name')}</span><input
					class="field"
					type="text"
					maxlength="40"
					bind:value={newName}
					placeholder={t('ha.conn.namePlaceholder')}
				/></label
			>
			<p class="muted">{t('ha.conn.addHint')}</p>
			<div class="dialog-actions">
				<button type="button" class="btn" disabled={listBusy} onclick={() => (adding = false)}
					>{t('common.cancel')}</button
				>
				<button class="btn btn-primary" disabled={listBusy}>{t('ha.conn.addButton')}</button>
			</div>
		</form>
	</Dialog>
{/if}

{#if removing}
	<Dialog title={t('ha.conn.removeTitle')} locked={listBusy} onclose={() => (removing = null)}>
		<p>{t('ha.conn.removeText', { name: label(removing) })}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={listBusy} onclick={() => (removing = null)}
				>{t('common.cancel')}</button
			>
			<button type="button" class="btn btn-danger" disabled={listBusy} onclick={removeConnection}
				>{t('ha.conn.remove')}</button
			>
		</div>
	</Dialog>
{/if}

<style>
	/* A connection is chosen by a click on its name (the row's text). */
	.conn {
		padding: var(--space-1) var(--space-2);
		margin: calc(-1 * var(--space-1)) calc(-1 * var(--space-2));
		border: 0;
		border-radius: var(--radius-small);
		background: none;
		color: inherit;
		font: inherit;
		text-align: start;
		cursor: pointer;
	}
	.conn:hover,
	.conn:focus-visible {
		background: var(--chip);
	}
	.chosen {
		box-shadow: inset 3px 0 0 var(--accent);
	}
	.chosen .conn-name::after {
		content: ' ✓';
	}
	label {
		display: flex;
		flex-direction: column;
		gap: var(--space-1-5);
		font-size: var(--text-base);
	}
	label > span {
		color: var(--muted);
	}
	.row {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-4) var(--space-5);
	}
	.end {
		align-items: flex-end;
	}
	.wide {
		flex: 1 1 360px;
		max-width: 664px;
	}
	.narrow {
		flex: 0 1 320px;
		max-width: 320px;
	}
	.small {
		margin: 0;
		font-size: var(--text-base);
	}
	.check {
		flex-direction: row;
		align-items: center;
		gap: var(--space-2-5);
		font-size: var(--text-md);
	}
	.check > span {
		color: var(--text);
	}
	input[type='checkbox']:not(.switch) {
		width: 18px;
		height: 18px;
		margin: 0;
		flex: none;
	}
	fieldset {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(min(100%, 230px), 1fr));
		gap: var(--space-2-5) var(--space-5);
		margin: 0;
		padding: 0;
		border: 0;
	}
	legend {
		grid-column: 1 / -1;
		margin-bottom: var(--space-2);
		color: var(--muted);
		font-size: var(--text-base);
	}
	.domain .chip {
		font-size: var(--text-xs);
	}
	details {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}
	summary {
		cursor: pointer;
		color: var(--muted);
	}
	.entities {
		gap: var(--space-2-5);
	}
	ul {
		margin: 0;
		padding: 0;
		list-style: none;
		max-height: 28rem;
		overflow: auto;
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
	}
	li {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-3);
		padding: var(--space-2) var(--space-3);
		border-bottom: 1px solid var(--border);
	}
	li:last-child {
		border-bottom: 0;
	}
	li .check {
		min-width: 0;
	}
	li .check > span {
		overflow-wrap: anywhere;
	}
	.name {
		font-weight: 600;
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
