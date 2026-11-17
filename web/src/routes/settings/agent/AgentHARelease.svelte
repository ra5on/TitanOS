<script lang="ts">
	// Settings › KI-Agent › Home Assistant: what the agent may do there and
	// which devices it sees, per connection (split from +page.svelte, PLAN
	// 75-12). Owner 15 Nov 2026: the rights were spread over three places and
	// nothing said that an empty list hides every device. So this page says
	// in two rows what is allowed (looking, switching – the tools of the area
	// "Home Assistant" under Rechte) and offers "Nichts / Auswahl / Alles"
	// for the devices; the lists load by themselves.
	// Owner 15 Nov 2026, later that day: "filigran einstellen – Schlösser
	// ansehen ja, schalten nein; Beleuchtung schalten ja; alles auflisten".
	// So every kind of device (domain) is one row: a switch for looking and
	// Ja / Nachfragen / Nein for switching (a rule "domain:<d>").
	import { api } from '#lib/api.ts';
	import { apiErrorText, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { toolMode, type Mode, type Settings, type ToolView } from '#lib/agent.ts';
	import Segments from '#lib/components/Segments.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';

	let {
		settings = $bindable(),
		tools,
		modeOptions,
		haRules = $bindable(),
	}: {
		settings: Settings;
		haRules: { key: string; mode: Mode }[];
		tools: ToolView[];
		modeOptions: { value: Mode; label: string }[];
	} = $props();

	// ----- what the agent may do: the tools of the area, by kind -----
	const homeTools = $derived(tools.filter((x) => x.area === 'home'));
	const looking = $derived(homeTools.filter((x) => x.kind === 'read'));
	const switching = $derived(homeTools.filter((x) => x.kind !== 'read'));
	function modeOf(list: ToolView[]): Mode | '' {
		const modes = new Set(list.map((x) => toolMode(settings.policy, x)));
		return modes.size === 1 ? [...modes][0] : '';
	}
	function setMode(list: ToolView[], mode: Mode) {
		const next = { ...(settings.policy.tools ?? {}) };
		for (const tool of list) next[tool.name] = mode;
		settings.policy.tools = next;
	}

	// ----- which devices: the agent's own release list -----
	let haDomains = $state<string[]>([]);
	type Entity = { id: string; name: string; area?: string; domain: string };
	let haEntities = $state<Entity[] | null>(null);
	let haQuery = $state('');
	let haError = $state('');
	let loading = $state(false);
	// Several Home Assistant connections (PLAN §45): one release list each.
	let haConn = $state('main');
	let haConns = $state<{ id: string; name: string }[]>([]);
	$effect(() => {
		api
			.get<{ connections: { id: string; name: string; active: boolean }[] }>(
				'/homeassistant/connections',
			)
			.then(
				(res) => (haConns = (res.connections ?? []).filter((c) => c.active || c.id === 'main')),
				() => (haConns = []),
			);
	});
	const haRelease = $derived(
		haConn === 'main'
			? settings?.homeassistant.release
			: settings?.homeassistant.releases?.[haConn],
	);
	type Scope = 'none' | 'some' | 'all';
	// "Auswahl" was chosen and nothing is picked yet.
	let picking = $state(false);
	const scope = $derived<Scope>(
		haRelease?.all
			? 'all'
			: picking || haRelease?.domains?.length || haRelease?.entities?.length
				? 'some'
				: 'none',
	);
	function release() {
		const ha = settings.homeassistant;
		return haConn === 'main' ? (ha.release ??= {}) : ((ha.releases ??= {})[haConn] ??= {});
	}
	function setScope(next: Scope) {
		const r = release();
		picking = next === 'some';
		if (next === 'all') r.all = true;
		else delete r.all;
		if (next === 'none') {
			r.domains = [];
			r.entities = [];
		}
	}
	function chooseHA(id: string) {
		haConn = id;
		haEntities = null;
		haError = '';
		picking = false;
	}
	async function loadHA() {
		haError = '';
		loading = true;
		const query = haConn === 'main' ? '' : `?connection=${encodeURIComponent(haConn)}`;
		try {
			const s = await api.get<{ domains: string[]; agentDomains?: string[] }>(
				`/homeassistant/settings${query}`,
			);
			// The agent can also be given kinds MCP cannot (1.09: automations).
			haDomains = s.agentDomains ?? s.domains ?? [];
			haEntities = (await api.get<{ entities: Entity[] }>(`/homeassistant/entities${query}`))
				.entities;
		} catch (e) {
			haError = apiErrorText(e);
		} finally {
			loading = false;
		}
	}
	// The lists come by themselves once a selection is wanted.
	$effect(() => {
		if (scope !== 'none' && haEntities === null && !loading && !haError) void loadHA();
	});
	function toggleRelease(kind: 'domains' | 'entities', value: string) {
		const r = release();
		const list = new Set(r[kind] ?? []);
		if (list.has(value)) list.delete(value);
		else list.add(value);
		r[kind] = [...list].sort();
	}
	const perDomain = $derived.by(() => {
		const out: Record<string, number> = {};
		for (const e of haEntities ?? []) out[e.domain] = (out[e.domain] ?? 0) + 1;
		return out;
	});
	// Only domains this Home Assistant has devices of. With "Alles" also
	// those TitanOS has no services for: they can only be looked at.
	// Automations, scripts and scenes always have their row (1.09, owner
	// 17 Nov 2026: "wieviele Automationen" found none, they had no row).
	const always = ['automation', 'scene', 'script'];
	const domainsShown = $derived([
		...haDomains.filter(
			(d) => perDomain[d] || haRelease?.domains?.includes(d) || always.includes(d),
		),
		...(scope === 'all'
			? Object.keys(perDomain)
					.filter((d) => !haDomains.includes(d))
					.sort()
			: []),
	]);
	// Domains without services worth calling (homeassistant.domainLevels).
	const readOnly = new Set([
		'sensor',
		'binary_sensor',
		'weather',
		'person',
		'device_tracker',
		'sun',
		'zone',
	]);
	const named = new Set([
		'alarm_control_panel',
		'automation',
		'binary_sensor',
		'button',
		'climate',
		'cover',
		'device_tracker',
		'fan',
		'humidifier',
		'input_boolean',
		'input_button',
		'input_number',
		'input_select',
		'light',
		'lock',
		'media_player',
		'person',
		'scene',
		'script',
		'sensor',
		'sun',
		'switch',
		'vacuum',
		'valve',
		'water_heater',
		'weather',
		'zone',
	]);
	const domainName = (d: string) => (named.has(d) ? t(`agent.haDomain.${d}` as MessageKey) : d);
	const sees = (d: string) => !!haRelease?.all || !!haRelease?.domains?.includes(d);
	const canSwitch = (d: string) => haDomains.includes(d) && !readOnly.has(d);
	// Without a rule a kind follows "Geräte schalten" above; locks, alarm
	// systems, automations, scripts and scenes ask at least (haAsksFirst).
	const asksFirst = ['lock', 'alarm_control_panel', 'automation', 'script', 'scene'];
	function switchMode(d: string): Mode {
		const rule = haRules.find((r) => r.key.trim() === `domain:${d}`);
		if (rule) return rule.mode;
		const base = (modeOf(switching) || 'ask') as Mode;
		return base === 'allow' && asksFirst.includes(d) ? 'ask' : base;
	}
	function setSwitch(d: string, mode: Mode) {
		const rule = haRules.find((r) => r.key.trim() === `domain:${d}`);
		if (rule) rule.mode = mode;
		else haRules.push({ key: `domain:${d}`, mode });
	}
	const switchOptions = $derived<{ value: Mode; label: string }[]>([
		{ value: 'allow', label: t('agent.haYes') },
		{ value: 'ask', label: t('agent.modeAsk') },
		{ value: 'deny', label: t('agent.haNo') },
	]);
	const haShown = $derived(
		(haEntities ?? [])
			.filter((e) => {
				const q = haQuery.trim().toLowerCase();
				return !q || (e.id + ' ' + e.name + ' ' + (e.area ?? '')).toLowerCase().includes(q);
			})
			.slice(0, 200),
	);
</script>

<SettingGroup title={t('agent.haRights')} desc={t('agent.haRightsHint')}>
	{#if !homeTools.length}
		<p class="notice-warn">
			{t('agent.haNoConnection')}
			<a href="/settings/homeassistant">{t('agent.haOpenConnection')}</a>
		</p>
	{:else}
		<SettingRow icon="eye" title={t('agent.haLook')} desc={t('agent.haLookHint')}>
			{#snippet control()}
				<Segments
					compact
					label={t('agent.haLook')}
					value={modeOf(looking) as Mode}
					options={modeOptions}
					onchange={(v) => setMode(looking, v)}
				/>
			{/snippet}
		</SettingRow>
		<SettingRow icon="power" title={t('agent.haSwitch')} desc={t('agent.haSwitchHint')}>
			{#snippet control()}
				<Segments
					compact
					label={t('agent.haSwitch')}
					value={modeOf(switching) as Mode}
					options={modeOptions}
					onchange={(v) => setMode(switching, v)}
				/>
			{/snippet}
		</SettingRow>
	{/if}
</SettingGroup>

<!-- PLAN 78-1: a plain heading; the domains and the devices are a tool of
     their own (a grid of switches, a list to search) on a row's surface. -->
<SettingGroup title={t('agent.haRelease')} desc={t('agent.haReleaseHint')}>
	{#snippet tools()}
		{#if haConns.length > 1}
			<label class="conn"
				><span>{t('notify.haConnection')}</span>
				<select
					class="field"
					value={haConn}
					onchange={(e) => chooseHA((e.currentTarget as HTMLSelectElement).value)}
				>
					{#each haConns as c (c.id)}<option value={c.id}>{c.name || t('ha.conn.first')}</option
						>{/each}
				</select></label
			>
		{/if}
	{/snippet}
	<SettingRow
		icon="home"
		title={t('agent.haScope')}
		desc={scope === 'all'
			? t('agent.haScopeAllHint')
			: scope === 'none'
				? t('agent.haScopeNoneHint')
				: t('agent.haReleased', {
						domains: haRelease?.domains?.length ?? 0,
						entities: haRelease?.entities?.length ?? 0,
					})}
	>
		{#snippet control()}
			<Segments
				compact
				label={t('agent.haScope')}
				value={scope}
				options={[
					{ value: 'none', label: t('agent.haScopeNone') },
					{ value: 'some', label: t('agent.haScopeSome') },
					{ value: 'all', label: t('agent.haScopeAll') },
				]}
				onchange={(v) => setScope(v as Scope)}
			/>
		{/snippet}
	</SettingRow>
	{#if scope === 'none'}<p class="notice-warn">{t('agent.haScopeNoneWarn')}</p>{/if}
	{#if scope !== 'none'}
		{#if haError}
			<p class="notice-warn">
				{haError}
				<button type="button" class="btn" onclick={loadHA}>{t('agent.haLoad')}</button>
			</p>
		{:else if haEntities === null}
			<p class="muted" role="status">{t('common.loading')}</p>
		{:else}
			<div class="srow">
				<div class="srow-block wide">
					<strong>{t('agent.haKinds')}</strong>
					<span class="muted small">{t('agent.haKindsHint')}</span>
				</div>
				<div class="kinds" id="ag-ha-kinds">
					{#each domainsShown as d (d)}
						<div class="kind" data-domain={d}>
							<span class="pick-text"
								>{domainName(d)}<span class="muted small"
									><code>{d}</code> · {t('agent.haDomainCount', { count: perDomain[d] ?? 0 })}</span
								></span
							>
							<span class="kind-ctl">
								<label class="see"
									><span>{t('agent.haSee')}</span><input
										type="checkbox"
										class="switch"
										role="switch"
										aria-label={`${domainName(d)}: ${t('agent.haSee')}`}
										checked={sees(d)}
										disabled={scope === 'all'}
										onchange={() => toggleRelease('domains', d)}
									/></label
								>
								{#if !canSwitch(d)}
									<span class="muted small only">{t('agent.haReadOnly')}</span>
								{:else}
									<Segments
										compact
										label={`${domainName(d)}: ${t('agent.haSwitchShort')}`}
										value={switchMode(d)}
										options={switchOptions}
										disabled={!sees(d)}
										onchange={(v) => setSwitch(d, v)}
									/>
								{/if}
							</span>
						</div>
					{:else}
						<p class="muted small none">{t('agent.haNoEntities')}</p>
					{/each}
				</div>
			</div>
			{#if scope === 'some'}
				<div class="srow">
					<div class="srow-block wide">
						<strong>{t('agent.haSingle')}</strong>
						<input
							class="field"
							type="search"
							bind:value={haQuery}
							placeholder={t('agent.haSearch')}
							aria-label={t('agent.haSearch')}
						/>
					</div>
					<div class="entities">
						{#each haShown as e (e.id)}
							{@const viaDomain = haRelease?.domains?.includes(e.domain)}
							<label class="pick"
								><span class="pick-text"
									>{e.name}<span class="muted small"
										><code>{e.id}</code>{e.area ? ' · ' + e.area : ''}</span
									></span
								><input
									type="checkbox"
									class="switch"
									role="switch"
									checked={viaDomain || haRelease?.entities?.includes(e.id)}
									disabled={viaDomain}
									onchange={() => toggleRelease('entities', e.id)}
								/></label
							>
						{:else}
							<p class="muted small none">{t('agent.haNoEntities')}</p>
						{/each}
					</div>
				</div>
			{/if}
		{/if}
	{/if}
</SettingGroup>

<style>
	/* Home Assistant: one row per kind of device, the devices in a scroll box. */
	.kinds {
		border-top: 1px solid var(--srow-line);
	}
	.kind {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-2) var(--space-4);
		padding: var(--space-2) var(--space-4);
		font-size: var(--text-md);
	}
	.kind + .kind {
		border-top: 1px solid var(--srow-line);
	}
	.kind-ctl {
		display: flex;
		align-items: center;
		justify-content: flex-end;
		gap: var(--space-4);
		margin-left: auto;
	}
	.see {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		font-size: var(--text-sm);
	}
	.only {
		min-width: 11rem;
		text-align: right;
	}
	.pick {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-3);
		min-height: 40px;
		padding: var(--space-1-5) 0;
		min-width: 0;
		font-size: var(--text-md);
	}
	.pick code {
		overflow-wrap: anywhere;
	}
	.pick-text {
		display: flex;
		flex-direction: column;
		gap: 2px;
		min-width: 0;
		overflow-wrap: anywhere;
	}
	.entities {
		max-height: 360px;
		overflow-y: auto;
		border-top: 1px solid var(--srow-line);
	}
	.entities > .pick {
		padding: var(--space-2) var(--space-4);
	}
	.entities > .pick + .pick {
		border-top: 1px solid var(--srow-line);
	}
	.none {
		margin: 0;
		padding: var(--space-3) var(--space-4);
	}
	.small {
		font-size: var(--text-sm);
	}
	.conn {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2);
		font-size: var(--text-base);
	}
	.conn .field {
		width: auto;
		min-width: 9rem;
	}
	@media (max-width: 760px) {
		.kind-ctl {
			width: 100%;
			justify-content: space-between;
		}
		.only {
			min-width: 0;
		}
	}
</style>
