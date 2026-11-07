<script lang="ts">
	// The connection to Home Assistant, storage for it on the NAS, sensors and
	// buttons over MQTT, and what MCP clients may switch. The token never
	// comes back from the NAS.
	import PageHead from '#lib/components/PageHead.svelte';
	import HAStorage from './HAStorage.svelte';
	import Dialog from '#lib/components/Dialog.svelte';
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

	async function save(event: SubmitEvent) {
		event.preventDefault();
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
				checkError = err instanceof ApiError ? err.message : errorText('internal');
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

{#if connections.length}
	<section class="tile panel conns" id="connections" aria-labelledby="ha-conns">
		<div class="head">
			<h2 class="tile-title" id="ha-conns">{t('ha.conn.title')}</h2>
			<button
				type="button"
				class="btn small"
				disabled={listBusy || connections.length >= maxConnections}
				onclick={() => (adding = true)}>{t('ha.conn.add')}</button
			>
		</div>
		<ul class="conn-list">
			{#each connections as c (c.id)}
				<li class:chosen={c.id === current}>
					<button
						type="button"
						class="conn"
						aria-current={c.id === current ? 'true' : undefined}
						onclick={() => choose(c.id)}
					>
						<span class="conn-name">{label(c)}</span>
						<span class="muted mono">{c.url || t('ha.conn.noUrl')}</span>
					</button>
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
				</li>
			{/each}
		</ul>
		{#if connections.length > 1}<p class="muted">{t('ha.conn.hint')}</p>{/if}
	</section>
{/if}

{#if draft}
	<div class="ha-page">
		<form id="ha-form" class="ha-form" onsubmit={save}>
			<section class="tile panel" id="connection">
				<div class="head">
					<h2 class="tile-title">{t('ha.connection')}</h2>
					<label class="check"
						><input type="checkbox" bind:checked={draft.enabled} /><span>{t('notify.enabled')}</span
						></label
					>
				</div>
				<div class="row">
					<label class="wide"
						><span>{t('ha.conn.name')}</span><input
							class="field"
							type="text"
							maxlength="40"
							bind:value={draft.name}
							placeholder={current === 'main' ? t('ha.conn.first') : ''}
						/></label
					>
				</div>
				<div class="row">
					<label class="wide"
						><span>{t('ha.url')}</span><input
							class="field mono"
							type="url"
							bind:value={draft.url}
							placeholder="http://homeassistant.local:8123"
							spellcheck="false"
							autocapitalize="none"
							required={draft.enabled}
						/></label
					>
				</div>
				<div class="row">
					<label class="wide"
						><span>{t('ha.token')}</span>
						<input
							class="field mono"
							type="password"
							bind:value={draft.token}
							autocomplete="new-password"
							placeholder={tokenSet && !clear.includes('token') ? t('notify.secretStored') : ''}
							required={draft.enabled && !tokenSet}
						/>
						{#if tokenSet}<button type="button" class="link" onclick={() => toggleClear('token')}
								>{clear.includes('token')
									? t('notify.secretRemoved')
									: t('notify.secretRemove')}</button
							>{/if}
					</label>
				</div>
				<p class="muted small">{t('ha.tokenHint')}</p>
				<details>
					<summary>{t('ha.fingerprint')}</summary>
					<label class="wide"
						><span>{t('ha.fingerprintLabel')}</span><input
							class="field mono"
							type="text"
							bind:value={draft.fingerprint}
							placeholder="AB:CD:…"
							spellcheck="false"
						/></label
					>
					<p class="muted small">{t('ha.fingerprintHint')}</p>
				</details>
				<div class="test">
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
			</section>
		</form>
		{#if diagnostics}<section class="tile panel">
				<h2 class="tile-title">{t('ha.diagnostics')}</h2>
				<p>
					{t('ha.lastConnected')}: {diagnostics.lastConnected &&
					!diagnostics.lastConnected.startsWith('0001-')
						? formatDateTime(new Date(diagnostics.lastConnected))
						: '—'}
				</p>
				<p>
					{t('ha.lastDisconnected')}: {diagnostics.lastDisconnected &&
					!diagnostics.lastDisconnected.startsWith('0001-')
						? formatDateTime(new Date(diagnostics.lastDisconnected))
						: '—'}
				</p>
				<p>{t('ha.evicted')}: {diagnostics.evictedTransitions}</p>
				<p class="muted">{t('ha.gaps')}</p>
			</section>{/if}
		{#key current}<HAStorage connection={current} />{/key}
		<section class="tile panel" id="mqtt">
			<div class="head">
				<h2 class="tile-title">{t('ha.mqtt')}</h2>
				<label class="check"
					><input type="checkbox" bind:checked={draft.mqtt.enabled} /><span
						>{t('notify.enabled')}</span
					></label
				>
			</div>
			<p class="muted">{t('ha.mqttIntro')}</p>
			<div class="row">
				<label class="narrow"
					><span>{t('ha.mqttHost')}</span><input
						class="field mono"
						type="text"
						bind:value={draft.mqtt.host}
						placeholder="homeassistant.local"
						spellcheck="false"
						autocapitalize="none"
						required={draft.mqtt.enabled}
						form="ha-form"
					/></label
				>
				<label class="tiny"
					><span>{t('notify.port')}</span><input
						class="field mono"
						type="number"
						form="ha-form"
						min="1"
						max="65535"
						bind:value={draft.mqtt.port}
					/></label
				>
				<label class="check tls"
					><input type="checkbox" bind:checked={draft.mqtt.tls} /><span>TLS</span></label
				>
			</div>
			<div class="row">
				<label class="narrow"
					><span>{t('notify.username')}</span><input
						class="field mono"
						type="text"
						bind:value={draft.mqtt.username}
						autocomplete="off"
						spellcheck="false"
						autocapitalize="none"
					/></label
				>
				<label class="narrow"
					><span>{t('notify.password')}</span>
					<input
						class="field mono"
						type="password"
						bind:value={draft.mqtt.password}
						autocomplete="new-password"
						placeholder={mqttPasswordSet && !clear.includes('mqtt') ? t('notify.secretStored') : ''}
					/>
					{#if mqttPasswordSet}<button
							type="button"
							class="link"
							onclick={() => toggleClear('mqtt')}
							>{clear.includes('mqtt')
								? t('notify.secretRemoved')
								: t('notify.secretRemove')}</button
						>{/if}
				</label>
				<label class="narrow"
					><span>{t('ha.mqttPrefix')}</span><input
						class="field mono"
						type="text"
						bind:value={draft.mqtt.prefix}
						spellcheck="false"
					/></label
				>
				<label class="narrow"
					><span>{t('ha.mqttInterval')}</span><input
						class="field"
						type="number"
						min="5"
						max="3600"
						step="1"
						placeholder="30"
						bind:value={draft.mqtt.interval}
						aria-describedby="ha-mqtt-interval-hint"
					/><small class="muted" id="ha-mqtt-interval-hint">{t('ha.mqttIntervalHint')}</small
					></label
				>
			</div>
			<fieldset>
				<legend>{t('ha.sensorGroups')}</legend>
				{#each sensorGroups as group}
					<label class="check"
						><input
							type="checkbox"
							checked={draft.mqtt.groups.includes(group)}
							onchange={() => (draft!.mqtt.groups = toggle(draft!.mqtt.groups, group))}
						/><span>{t(`ha.group.${group}` as MessageKey)}</span></label
					>
				{/each}
			</fieldset>
			{#each sensorGroups.filter((g) => draft?.mqtt.groups.includes(g)) as group}
				{@const devices = mqttDevices.filter((d) => d.group === group)}
				{#if !['system', 'work'].includes(group)}<details class="device-selection">
						<summary>{t(`ha.group.${group}` as MessageKey)} · {t('ha.deviceSelection')}</summary>
						<label class="check"
							><input
								type="checkbox"
								checked={!draft.mqtt.devices || !(group in draft.mqtt.devices)}
								onchange={(e) => {
									const next = { ...draft!.mqtt.devices };
									if (e.currentTarget.checked) delete next[group];
									else next[group] = devices.map((d) => d.id);
									draft!.mqtt.devices = next;
								}}
							/>{t('ha.allDevices')}</label
						>
						{#if draft.mqtt.devices && group in draft.mqtt.devices}
							{#each [...new Set( [...devices.map((d) => d.id), ...draft.mqtt.devices[group]] )] as id}
								<label class="check"
									><input
										type="checkbox"
										checked={draft.mqtt.devices[group].includes(id)}
										onchange={() =>
											(draft!.mqtt.devices = {
												...draft!.mqtt.devices,
												[group]: toggle(draft!.mqtt.devices![group], id),
											})}
									/>{devices.find((d) => d.id === id)?.name ?? id}</label
								>
							{/each}
						{/if}
					</details>{/if}
			{/each}

			<p class="muted small">{t('ha.accountIntro')}</p>
			<label class="account"
				><span>{t('ha.accountLabel')}</span>
				<select class="field" bind:value={draft.account}>
					<option value="">{t('ha.accountNone')}</option>
					{#each accounts as account (account.name)}<option value={account.name}
							>{account.name}</option
						>{/each}
				</select>
			</label>
			<fieldset>
				<legend>{t('ha.commandGroups')}</legend>
				{#each commandGroups as group}
					<label class="check"
						><input
							type="checkbox"
							checked={draft.mqtt.commands.includes(group)}
							onchange={() => (draft!.mqtt.commands = toggle(draft!.mqtt.commands, group))}
						/><span>{t(`ha.command.${group}` as MessageKey)}</span></label
					>
				{/each}
			</fieldset>
			{#if draft.mqtt.commands.length}
				<p class="notice-warn">{t('ha.commandsWarning')}</p>
				{#if !draft.account}<p class="notice-warn">{t('ha.commandsNoAccount')}</p>{/if}
			{/if}
			{#if status?.mqtt}<p class="notice-ok" role="status">
					{t('ha.mqttConnected', { seconds: String(sentEvery) })}
				</p>{:else if status?.mqttError}<div class="notice-error" role="alert">
					{t('ha.mqttFailed')}
					<pre>{status.mqttError}</pre>
				</div>{/if}
		</section>
		<section class="tile panel" id="release">
			<h2 class="tile-title">{t('ha.release')}</h2>
			<p class="muted">
				{t('ha.releaseIntro')}
			</p>
			<fieldset>
				<legend>{t('ha.domains')}</legend>
				{#each domains as domain}
					<label class="check domain"
						><input
							type="checkbox"
							checked={draft.domains.includes(domain)}
							onchange={() => (draft!.domains = toggle(draft!.domains, domain))}
						/><span class="mono">{domain}</span><span
							class="chip {level(domain) === 'danger' ? 'warn' : 'info'}"
							>{t(`ha.level.${level(domain)}` as MessageKey)}</span
						></label
					>
				{/each}
			</fieldset>
			<div class="entities">
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
				{#if draft.entities.length}<p class="muted small">
						{t('ha.singleCount', { count: draft.entities.length })}
					</p>{/if}
				{#if entities}
					<ul>
						{#each shown as entity (entity.id)}
							{@const viaDomain = draft.domains.includes(entity.domain)}
							<li>
								<label class="check"
									><input
										type="checkbox"
										checked={viaDomain || draft.entities.includes(entity.id)}
										disabled={viaDomain}
										onchange={() => (draft!.entities = toggle(draft!.entities, entity.id))}
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
		</section>
		<div class="actions">
			{#if saved}<p class="notice-ok" role="status">{t('notify.saved')}</p>{/if}
			<button type="submit" form="ha-form" class="btn btn-primary" disabled={busy !== null}
				>{t(busy === 'save' ? 'notify.saving' : 'notify.save')}</button
			>
		</div>
	</div>
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
	.conns {
		margin-bottom: var(--gap);
	}
	.conn-list {
		list-style: none;
		margin: var(--space-3) 0 0;
		padding: 0;
		display: flex;
		flex-direction: column;
	}
	.conn-list li {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2) var(--space-3);
		padding: var(--space-3) 0;
		border-top: 1px solid var(--border);
	}
	.conn-list li:first-child {
		border-top: 0;
	}
	.conn {
		flex: 1 1 14rem;
		min-width: 0;
		display: flex;
		flex-direction: column;
		gap: 2px;
		text-align: start;
		background: none;
		border: 0;
		padding: var(--space-1) var(--space-2);
		margin: calc(-1 * var(--space-1)) calc(-1 * var(--space-2));
		border-radius: var(--radius-small);
		color: inherit;
		font: inherit;
		cursor: pointer;
	}
	.conn .mono {
		overflow-wrap: anywhere;
	}
	.conn:hover,
	.conn:focus-visible {
		background: var(--chip);
	}
	.chosen .conn {
		box-shadow: inset 3px 0 0 var(--accent);
	}
	.chosen .conn-name::after {
		content: ' ✓';
	}
	.conn-name {
		font-weight: 600;
	}
	.ha-page,
	form {
		display: flex;
		flex-direction: column;
		gap: var(--gap);
	}
	/* Stacked under its hint, never stretched by the column (flex-basis
	   would become the height there). */
	.account {
		flex: none;
		width: min(100%, 320px);
	}
	section {
		font-size: var(--text-md);
	}
	.head {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-2) var(--space-4);
	}
	label {
		display: flex;
		flex-direction: column;
		gap: 6px;
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
	.tiny {
		flex: 0 1 110px;
		max-width: 110px;
	}
	.tls {
		align-self: flex-end;
		padding-bottom: 10px;
	}
	.small {
		margin: 0;
		font-size: var(--text-base);
	}
	.check {
		flex-direction: row;
		align-items: center;
		gap: 10px;
		font-size: var(--text-md);
	}
	.check > span {
		color: var(--text);
	}
	input[type='checkbox'] {
		width: 18px;
		height: 18px;
		margin: 0;
		flex: none;
	}
	fieldset {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(min(100%, 230px), 1fr));
		gap: 10px var(--space-5);
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
		display: flex;
		flex-direction: column;
		gap: 10px;
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
		align-self: flex-start;
		padding: 0;
		background: none;
		border: 0;
		color: var(--muted);
		font: inherit;
		font-size: var(--text-sm);
		text-decoration: underline;
		cursor: pointer;
	}
	.test {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 10px var(--space-4);
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
		gap: var(--space-4);
	}
	.actions p {
		margin: 0;
	}
	pre {
		margin: 6px 0 0;
		font-family: var(--font-mono);
		font-size: var(--text-sm);
		white-space: pre-wrap;
		overflow-wrap: anywhere;
	}
</style>
