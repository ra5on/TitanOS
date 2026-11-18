<script lang="ts">
	// Settings › KI-Agent › Home Assistant › Assist (PLAN 91): the switch,
	// the profile of Home Assistant, the addresses for its Ollama
	// integration, the key, the last request – and what a question from
	// Home Assistant may use, area by area. The areas are saved with the
	// page; the key has its own routes and never travels with the settings.
	import Icon, { type IconName } from '#lib/components/Icon.svelte';
	import Dialog from '#lib/components/Dialog.svelte';
	import Segments from '#lib/components/Segments.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import { api } from '#lib/api.ts';
	import { formatDateTime } from '#lib/format.ts';
	import { apiErrorText, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import type { Settings } from '#lib/agent.ts';

	let { settings = $bindable() }: { settings: Settings } = $props();

	type Level = 'deny' | 'read' | 'change';
	type Info = {
		keySet: boolean;
		addresses: { kind: string; url: string }[];
		areas: string[];
		levels: Record<string, Level>;
		/** 1.11: calendar and contacts, deny or read only (assistData). */
		dataAreas?: string[];
		dataLevels?: Record<string, Level>;
		last: {
			at: string;
			from: string;
			ok: boolean;
			reason?: string;
			question?: string;
			millis?: number;
		} | null;
		memories?: number;
		chats?: number;
		/** S-23: the account Assist acts as, and what it agreed to itself. */
		actor?: string;
		isActor?: boolean;
		consent?: Record<string, boolean>;
		/** What the account on this page agreed to (it may not be the actor now). */
		myConsent?: Record<string, boolean>;
	};
	let info = $state<Info | null>(null);
	let error = $state('');
	let key = $state('');
	let copied = $state('');
	let confirmNew = $state(false);

	async function load() {
		try {
			info = await api.get<Info>('/agent/assist');
		} catch (e) {
			error = apiErrorText(e);
		}
	}
	$effect(() => {
		if (settings.homeassistant.assist) void load();
	});

	async function showKey() {
		if (key) return (key = '');
		try {
			key = (await api.get<{ key: string }>('/agent/assist/key')).key;
		} catch (e) {
			error = apiErrorText(e);
		}
	}
	async function newKey() {
		confirmNew = false;
		try {
			key = (await api.post<{ key: string }>('/agent/assist/key')).key;
			await load();
		} catch (e) {
			error = apiErrorText(e);
		}
	}
	async function copy(what: string, text: string) {
		try {
			await navigator.clipboard.writeText(text);
		} catch {
			// Plain http in the home network: an invisible field does it.
			const area = document.createElement('textarea');
			area.value = text;
			area.style.position = 'fixed';
			area.style.opacity = '0';
			document.body.appendChild(area);
			area.select();
			document.execCommand('copy');
			area.remove();
		}
		copied = what;
		setTimeout(() => copied === what && (copied = ''), 1800);
	}
	async function copyKey() {
		const value = key || (await api.get<{ key: string }>('/agent/assist/key')).key;
		await copy('key', value);
	}

	// ----- what Home Assistant may ask -----
	const icons: Record<string, IconName> = {
		home: 'home',
		system: 'activity',
		storage: 'disk',
		backups: 'archive',
		docker: 'box',
		vms: 'chip',
		files: 'folder',
		shares: 'folder',
		accounts: 'users',
		network: 'shield',
		updates: 'download',
		plugins: 'grid',
		web: 'globe',
		memory: 'bulb',
		plan: 'clock',
		code: 'zap',
		calendar: 'calendar',
		contacts: 'person',
	};
	// Areas without a reading side are only on or off.
	const onOff = ['web', 'plan', 'code'];
	const three = $derived([
		{ value: 'deny' as Level, label: t('agent.assist.deny') },
		{ value: 'read' as Level, label: t('agent.assist.read') },
		{ value: 'change' as Level, label: t('agent.assist.change') },
	]);
	const two = $derived([
		{ value: 'deny' as Level, label: t('agent.assist.deny') },
		{ value: 'change' as Level, label: t('agent.assist.allow') },
	]);
	const level = (area: string): Level =>
		(settings.homeassistant.assistAreas?.[area] as Level | undefined) ??
		info?.levels[area] ??
		'deny';
	function setLevel(area: string, value: Level) {
		settings.homeassistant.assistAreas = {
			...(settings.homeassistant.assistAreas ?? {}),
			[area]: value,
		};
	}
	// Calendar and contacts: never more than reading, in a field of their own.
	const readOnly = $derived([
		{ value: 'deny' as Level, label: t('agent.assist.deny') },
		{ value: 'read' as Level, label: t('agent.assist.read') },
	]);
	const dataLevel = (area: string): Level =>
		settings.homeassistant.assistData?.[area] === 'read'
			? 'read'
			: settings.homeassistant.assistData?.[area] === 'deny'
				? 'deny'
				: (info?.dataLevels?.[area] ?? 'deny');
	function setDataLevel(area: string, value: Level) {
		settings.homeassistant.assistData = {
			...(settings.homeassistant.assistData ?? {}),
			[area]: value === 'read' ? 'read' : 'deny',
		};
	}
	// Calendar and contacts hold only with the consent of the acting account,
	// given by that account itself on this page (S-23); never saved with
	// the page, never for someone else.
	let consentBusy = $state(false);
	const showConsent = $derived(
		!!info && (!!info.isActor || Object.values(info.myConsent ?? {}).some(Boolean)),
	);
	const waitText = (area: string) => {
		if (dataLevel(area) !== 'read' || info?.consent?.[area]) return '';
		return info?.isActor
			? t('agent.assist.consent.waitingMe')
			: t('agent.assist.consent.waiting', { account: info?.actor ?? '' });
	};
	const consentTitle = $derived<Record<string, string>>({
		calendar: t('agent.assist.consent.calendar'),
		contacts: t('agent.assist.consent.contacts'),
	});
	async function setConsent(area: string, value: boolean) {
		consentBusy = true;
		error = '';
		try {
			const next = { ...(info?.myConsent ?? {}), [area]: value };
			await api.put('/agent/assist/consent', next);
			await load();
		} catch (e) {
			error = apiErrorText(e);
		} finally {
			consentBusy = false;
		}
	}
	function denyAll() {
		settings.homeassistant.assistAreas = Object.fromEntries(
			(info?.areas ?? []).map((a) => [a, 'deny']),
		);
		if (info?.dataAreas?.length)
			settings.homeassistant.assistData = Object.fromEntries(
				info.dataAreas.map((a) => [a, 'deny']),
			);
	}
	function defaults() {
		settings.homeassistant.assistAreas = undefined;
		settings.homeassistant.assistData = undefined;
	}
	const kindText = (kind: string) =>
		t(
			(['lan', 'tailscale', 'netbird', 'ddns', 'domain'].includes(kind)
				? `agent.assist.addr.${kind}`
				: 'agent.assist.addr.domain') as MessageKey,
		);
	const lastText = $derived.by(() => {
		const l = info?.last;
		if (!l) return t('agent.assist.lastNone');
		const when = formatDateTime(new Date(l.at));
		if (!l.ok)
			return t('agent.assist.lastRefused', {
				when,
				from: l.from,
				reason: t(`agent.assist.reason.${l.reason === 'key' ? 'key' : 'address'}` as MessageKey),
			});
		const seconds = ((l.millis ?? 0) / 1000).toFixed(1).replace('.', ',');
		// The question only for the account Assist acts as (S-23).
		if (!l.question) return t('agent.assist.lastOkNoQuestion', { when, from: l.from, seconds });
		return t('agent.assist.lastOk', { when, from: l.from, question: l.question, seconds });
	});
</script>

<SettingGroup title={t('agent.haAssist')} desc={t('agent.assist.intro')}>
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	<SettingRow
		icon="mic"
		title={t('agent.assist.on')}
		desc={t('agent.assist.onDesc')}
		for="ag-ha-assist"
	>
		{#snippet control()}
			<input
				id="ag-ha-assist"
				type="checkbox"
				class="switch"
				role="switch"
				bind:checked={settings.homeassistant.assist}
			/>
		{/snippet}
	</SettingRow>
	{#if settings.homeassistant.assist}
		<SettingRow
			icon="users"
			title={t('agent.assist.profile')}
			desc={t('agent.assist.profileDesc')}
			summary={info
				? t('agent.assist.profileCount', { memories: info.memories ?? 0, chats: info.chats ?? 0 })
				: ''}
			href="/agent?profile=assist"
		/>
		<SettingRow
			id="assist-addresses"
			icon="external"
			title={t('agent.assist.addresses')}
			desc={t('agent.assist.addressesDesc')}
			open
		>
			{#each info?.addresses ?? [] as a (a.url)}
				<div class="srow-sub">
					<span class="adr"><code>{a.url}</code><small>{kindText(a.kind)}</small></span>
					<button type="button" class="btn" onclick={() => copy(a.url, a.url)}
						><Icon name={copied === a.url ? 'check' : 'copy'} size={15} />{copied === a.url
							? t('agent.copiedShort')
							: t('agent.copy')}</button
					>
				</div>
			{/each}
			<div class="srow-block">
				<p class="muted">{t('agent.assist.addressesHttps')}</p>
			</div>
		</SettingRow>
		<SettingRow
			icon="key"
			title={t('agent.assist.key')}
			desc={info?.keySet ? t('agent.assist.keyDesc') : t('agent.assist.keyNone')}
		>
			{#snippet control()}
				{#if info?.keySet}
					<code class="key">{key || t('agent.assist.keyHidden')}</code>
					<button type="button" class="btn" onclick={showKey}
						><Icon name="eye" size={15} />{key
							? t('agent.assist.keyHide')
							: t('agent.assist.keyShow')}</button
					>
					<button type="button" class="btn" onclick={copyKey}
						><Icon name={copied === 'key' ? 'check' : 'copy'} size={15} />{copied === 'key'
							? t('agent.copiedShort')
							: t('agent.copy')}</button
					>
					<button type="button" class="btn" onclick={() => (confirmNew = true)}
						><Icon name="refresh" size={15} />{t('agent.assist.keyNew')}</button
					>
				{:else}
					<button type="button" class="btn btn-primary" disabled={!info} onclick={newKey}
						>{t('agent.assist.keyMake')}</button
					>
				{/if}
			{/snippet}
		</SettingRow>
		<SettingRow icon="activity" title={t('agent.assist.last')} desc={lastText}>
			{#snippet control()}
				{#if info?.last}<span class="chip {info.last.ok ? 'ok' : 'warn'}"
						>{info.last.ok ? t('agent.assist.connected') : t('agent.assist.refused')}</span
					>{/if}
				<button
					type="button"
					class="btn"
					aria-label={t('ops.refresh')}
					title={t('ops.refresh')}
					onclick={load}><Icon name="refresh" size={15} /></button
				>
			{/snippet}
		</SettingRow>
		<SettingRow icon="info" title={t('agent.assist.setup')} desc={t('agent.assist.setupDesc')} />
	{/if}
</SettingGroup>

{#if settings.homeassistant.assist && info}
	<SettingGroup title={t('agent.assist.rights')} desc={t('agent.assist.rightsDesc')}>
		{#snippet tools()}
			<button type="button" class="btn" onclick={denyAll}>{t('agent.assist.denyAll')}</button>
			<button type="button" class="btn" onclick={defaults}>{t('agent.assist.defaults')}</button>
		{/snippet}
		{#each info.areas as area (area)}
			<SettingRow
				icon={icons[area] ?? 'box'}
				title={t(`agent.assist.area.${area}` as MessageKey)}
				desc={t(`agent.assist.areaDesc.${area}` as MessageKey)}
			>
				{#snippet control()}
					<Segments
						compact
						label={t(`agent.assist.area.${area}` as MessageKey)}
						value={onOff.includes(area) && level(area) === 'read' ? 'deny' : level(area)}
						options={onOff.includes(area) ? two : three}
						onchange={(v) => setLevel(area, v)}
					/>
				{/snippet}
			</SettingRow>
		{/each}
		{#each info.dataAreas ?? [] as area (area)}
			<SettingRow
				icon={icons[area] ?? 'box'}
				title={t(`agent.assist.area.${area}` as MessageKey)}
				desc={waitText(area)
					? `${t(`agent.assist.areaDesc.${area}` as MessageKey)} – ${waitText(area)}`
					: t(`agent.assist.areaDesc.${area}` as MessageKey)}
			>
				{#snippet control()}
					<Segments
						compact
						label={t(`agent.assist.area.${area}` as MessageKey)}
						value={dataLevel(area)}
						options={readOnly}
						onchange={(v) => setDataLevel(area, v)}
					/>
				{/snippet}
			</SettingRow>
		{/each}
	</SettingGroup>
	{#if showConsent}
		<SettingGroup
			title={t('agent.assist.consent.title')}
			desc={info.isActor
				? t('agent.assist.consent.desc')
				: t('agent.assist.consent.notActor', { actor: info.actor ?? '' })}
		>
			{#each info.dataAreas ?? [] as area (area)}
				<SettingRow
					icon={icons[area] ?? 'box'}
					title={consentTitle[area] ?? area}
					desc={info.myConsent?.[area]
						? t('agent.assist.consent.given')
						: t('agent.assist.consent.notGiven')}
					for="ag-consent-{area}"
				>
					{#snippet control()}
						<input
							id="ag-consent-{area}"
							type="checkbox"
							class="switch"
							role="switch"
							data-consent={area}
							disabled={consentBusy}
							checked={!!info?.myConsent?.[area]}
							onchange={(e) => setConsent(area, e.currentTarget.checked)}
						/>
					{/snippet}
				</SettingRow>
			{/each}
		</SettingGroup>
	{/if}
{/if}

{#if confirmNew}
	<Dialog title={t('agent.assist.keyNewTitle')} onclose={() => (confirmNew = false)}>
		<p>{t('agent.assist.keyNewText')}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (confirmNew = false)}
				>{t('common.cancel')}</button
			>
			<button type="button" class="btn btn-danger" onclick={newKey}
				>{t('agent.assist.keyNewDo')}</button
			>
		</div>
	</Dialog>
{/if}

<style>
	code {
		font-family: var(--font-mono, ui-monospace, monospace);
		font-size: 0.9em;
		padding: var(--space-1) var(--space-2);
		border-radius: var(--radius-small, 6px);
		background: var(--chip);
		overflow-wrap: anywhere;
	}
	.adr {
		flex: 1;
		min-width: 0;
		display: flex;
		flex-direction: column;
		align-items: flex-start;
		gap: var(--space-1);
	}
	.adr small {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.btn {
		gap: var(--space-1-5);
	}
</style>
