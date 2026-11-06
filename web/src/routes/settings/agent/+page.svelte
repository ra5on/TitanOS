<script lang="ts">
	// Settings › KI-Agent (PLAN 44): the Ollama server, model, thinking level
	// and context as Ollama reports them, which accounts may use the agent,
	// named locations, what the agent may do (quick choice, areas, single
	// tools and folders with "Ohne Nachfrage / Nachfragen / Verboten"), its
	// own initiative and Home Assistant. Owners and administrators only.
	import PageHead from '#lib/components/PageHead.svelte';
	import Segments from '#lib/components/Segments.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import Tabs from '#lib/components/Tabs.svelte';
	import QrCode from '#lib/components/QrCode.svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { api, type DockerStatus, type FileRoot } from '#lib/api.ts';
	import { apiErrorText, i18n, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import {
		AREAS,
		areaMode,
		levelLabel,
		presetOf,
		tokens,
		toolMode,
		gb,
		type Mode,
		type Preset,
		type ClusterInfo,
		type Settings,
		type ShareState,
		type Status,
		type ToolView,
	} from '#lib/agent.ts';

	let settings = $state<Settings | null>(null);
	let status = $state<Status | null>(null);
	let tools = $state<ToolView[]>([]);
	let roots = $state<FileRoot[]>([]);
	let accounts = $state<{ name: string; role: string }[]>([]);
	let error = $state('');
	let saving = $state(false);
	let saved = $state(false);
	let check = $state<{
		reachable: boolean;
		version?: string;
		error?: string;
		models?: number;
	} | null>(null);
	let open = $state<Record<string, boolean>>({});
	// The agent across the cluster (PLAN 47).
	let clusterInfo = $state<ClusterInfo | null>(null);
	let shareState = $state<ShareState | undefined>();
	$effect(() => {
		api.get<ClusterInfo>('/agent/cluster').then(
			(c) => {
				clusterInfo = c;
				shareState = c.state;
			},
			() => (clusterInfo = null),
		);
	});
	async function askAdmin() {
		try {
			shareState = (await api.post<{ state: ShareState }>('/agent/cluster/request')).state;
		} catch (e) {
			error = apiErrorText(e);
		}
	}
	async function setShare(node: string, body: { allowed?: boolean; remove?: boolean }) {
		if (!settings) return;
		try {
			settings.shared = await api.put<NonNullable<Settings['shared']>>(
				`/agent/cluster/nodes/${encodeURIComponent(node)}`,
				body,
			);
		} catch (e) {
			error = apiErrorText(e);
		}
	}
	const shareText: Record<ShareState, MessageKey> = {
		allowed: 'agent.shareAllowed',
		pending: 'agent.sharePending',
		off: 'agent.shareOff',
		unreachable: 'agent.shareUnreachable',
		old: 'agent.shareOld',
		nocluster: 'agent.shareNoCluster',
	};
	let haNotify = $state('');
	let haRules = $state<{ key: string; mode: Mode }[]>([]);
	// Home Assistant: domains and entities for the agent's own release list.
	let haDomains = $state<string[]>([]);
	let haEntities = $state<{ id: string; name: string; area?: string; domain: string }[] | null>(
		null,
	);
	let haQuery = $state('');
	let haError = $state('');
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
	function chooseHA(id: string) {
		haConn = id;
		haEntities = null;
		haError = '';
	}
	async function loadHA() {
		haError = '';
		const query = haConn === 'main' ? '' : `?connection=${encodeURIComponent(haConn)}`;
		try {
			const s = await api.get<{ domains: string[] }>(`/homeassistant/settings${query}`);
			haDomains = s.domains ?? [];
			haEntities = (
				await api.get<{ entities: { id: string; name: string; area?: string; domain: string }[] }>(
					`/homeassistant/entities${query}`,
				)
			).entities;
		} catch (e) {
			haError = apiErrorText(e);
		}
	}
	function toggleRelease(kind: 'domains' | 'entities', value: string) {
		if (!settings) return;
		const ha = settings.homeassistant;
		const r = haConn === 'main' ? (ha.release ??= {}) : ((ha.releases ??= {})[haConn] ??= {});
		const list = new Set(r[kind] ?? []);
		if (list.has(value)) list.delete(value);
		else list.add(value);
		r[kind] = [...list].sort();
	}
	const haShown = $derived(
		(haEntities ?? [])
			.filter((e) => {
				const q = haQuery.trim().toLowerCase();
				return !q || (e.id + ' ' + e.name + ' ' + (e.area ?? '')).toLowerCase().includes(q);
			})
			.slice(0, 200),
	);

	async function load() {
		error = '';
		try {
			const s = await api.get<Settings>('/agent/settings');
			s.policy ??= { preset: 'standard' };
			s.homeassistant ??= {};
			settings = s;
			haNotify = (s.homeassistant.notify ?? []).join(', ');
			haRules = Object.entries(s.homeassistant.entities ?? {}).map(([key, mode]) => ({
				key,
				mode,
			}));
		} catch (e) {
			error = apiErrorText(e);
			return;
		}
		void Promise.all([
			api
				.get<Status>('/agent/status')
				.then((s) => (status = s))
				.catch(() => (status = null)),
			api
				.get<{ tools: ToolView[] }>('/agent/tools')
				.then((r) => (tools = r.tools))
				.catch(() => (tools = [])),
			api
				.get<FileRoot[]>('/files/roots')
				.then((r) => (roots = r.filter((x) => x.kind !== 'stick')))
				.catch(() => (roots = [])),
			api
				.get<{ name: string; role: string }[]>('/users')
				.then((r) => (accounts = r))
				.catch(() => (accounts = [])),
		]);
	}
	$effect(() => {
		void load();
	});

	async function save() {
		if (!settings) return;
		saving = true;
		saved = false;
		error = '';
		settings.homeassistant.notify = haNotify
			.split(',')
			.map((s) => s.trim())
			.filter(Boolean);
		settings.homeassistant.entities = Object.fromEntries(
			haRules.filter((r) => r.key.trim()).map((r) => [r.key.trim(), r.mode]),
		);
		try {
			settings = await api.put<Settings>('/agent/settings', $state.snapshot(settings));
			saved = true;
			void api.get<Status>('/agent/status').then((s) => (status = s));
			void api.get<{ tools: ToolView[] }>('/agent/tools').then((r) => (tools = r.tools));
		} catch (e) {
			error = apiErrorText(e);
		} finally {
			saving = false;
		}
	}

	let pullName = $state('');
	let pull = $state<{
		model?: string;
		status?: string;
		completed?: number;
		total?: number;
		error?: string;
		running?: boolean;
	} | null>(null);
	async function startPull() {
		try {
			await api.post('/agent/models/pull', { model: pullName.trim() });
			pull = { model: pullName.trim(), status: 'starting', running: true };
			const timer = setInterval(async () => {
				try {
					pull = await api.get<typeof pull>('/agent/models/pull');
					if (!pull?.running) {
						clearInterval(timer);
						void api.get<Status>('/agent/status').then((s) => (status = s));
					}
				} catch {
					clearInterval(timer);
				}
			}, 1500);
		} catch (e) {
			pull = { model: pullName, error: apiErrorText(e) };
		}
	}

	async function test() {
		check = null;
		try {
			check = await api.post<{
				reachable: boolean;
				version?: string;
				error?: string;
				models?: number;
			}>('/agent/check', { ollama: settings?.ollama ?? '' });
		} catch (e) {
			check = { reachable: false, error: apiErrorText(e) };
		}
	}

	const models = $derived((status?.models ?? []).filter((m) => m.capabilities.includes('tools')));
	const others = $derived((status?.models ?? []).filter((m) => !m.capabilities.includes('tools')));
	const chosen = $derived(models.find((m) => m.name === settings?.model));
	// Speech and pictures (PLAN 49).
	const visionModels = $derived(
		(status?.models ?? []).filter((m) => m.capabilities.includes('vision')),
	);
	const vram = $derived((status?.running ?? []).reduce((sum, r) => sum + (r.vram ?? 0), 0));
	let voiceCheck = $state<{ reachable: boolean; models?: string[]; error?: string } | null>(null);
	async function checkVoice() {
		voiceCheck = null;
		try {
			voiceCheck = await api.post<{ reachable: boolean; models?: string[]; error?: string }>(
				'/agent/voice/check',
				{ url: settings?.voice?.url ?? '' },
			);
		} catch (e) {
			voiceCheck = { reachable: false, error: apiErrorText(e) };
		}
	}
	const runningContext = $derived(
		status?.running?.find((r) => r.name === settings?.model)?.contextLength ?? 0,
	);

	const modeOptions = $derived<{ value: Mode; label: string }[]>([
		{ value: 'allow', label: t('agent.modeAllow') },
		{ value: 'ask', label: t('agent.modeAsk') },
		{ value: 'deny', label: t('agent.modeDeny') },
	]);
	const preset = $derived(settings ? presetOf(settings.policy) : 'standard');
	function setPreset(p: Preset | 'custom') {
		if (!settings || p === 'custom') return;
		settings.policy = { preset: p, folders: settings.policy.folders ?? [] };
	}
	function setArea(area: string, mode: Mode) {
		if (!settings) return;
		settings.policy.areas = { ...(settings.policy.areas ?? {}), [area]: mode };
		// Single tools of the area follow it again.
		const tl = { ...(settings.policy.tools ?? {}) };
		for (const tool of tools.filter((x) => x.area === area)) delete tl[tool.name];
		settings.policy.tools = tl;
	}
	function setTool(name: string, mode: Mode) {
		if (!settings) return;
		settings.policy.tools = { ...(settings.policy.tools ?? {}), [name]: mode };
	}
	const areaLabel = (a: string) => t(`agent.area.${a}` as MessageKey);
	const kindLabel: Record<string, MessageKey> = {
		read: 'agent.kindRead',
		create: 'agent.kindCreate',
		change: 'agent.kindChange',
	};
	const userAccounts = $derived(accounts.filter((a) => a.role !== 'owner'));
	const admins = $derived(accounts.filter((a) => a.role === 'owner' || a.role === 'admin'));
	function toggleUser(name: string, on: boolean) {
		if (!settings) return;
		const set = new Set(settings.users ?? []);
		if (on) set.add(name);
		else set.delete(name);
		settings.users = [...set];
	}
	// PLAN 59a: up to 256k, but never more than the model can.
	const modelContext = $derived(
		status?.models?.find((m) => m.name === settings?.model)?.contextLength ?? 0,
	);
	const contexts = $derived(
		[0, 8192, 16384, 32768, 65536, 131072, 196608, 262144].filter(
			(c) => c === 0 || !modelContext || c <= modelContext || c === settings?.context,
		),
	);

	// PLAN 53: the page in tabs instead of one long column.
	const TABS = ['overview', 'connection', 'rights', 'accounts', 'ha', 'initiative'] as const;
	type Tab = (typeof TABS)[number];
	const tabOf = (v: string | null): Tab => (TABS.includes(v as Tab) ? (v as Tab) : 'overview');
	let tab = $state<Tab>(tabOf(page.url.searchParams.get('tab')));
	$effect(() => {
		tab = tabOf(page.url.searchParams.get('tab'));
	});
	function choose(next: Tab) {
		tab = next;
		void goto(`/settings/agent?tab=${next}`, {
			replaceState: true,
		});
	}

	// The phone app: first the way to reach the NAS, then the QR code.
	type Address = { kind: string; url: string; https: boolean };
	let addresses = $state<Address[]>([]);
	let way = $state('');
	let own = $state('');
	let copied = $state(false);
	$effect(() => {
		api
			.get<{ addresses: Address[] }>('/agent/addresses')
			.then((r) => {
				addresses = r.addresses ?? [];
				way = (addresses.find((a) => a.https) ?? addresses[0])?.url ?? 'own';
			})
			.catch(() => (addresses = []));
	});
	const appUrl = $derived(
		way === 'own'
			? /^https?:\/\/\S+$/.test(own.trim())
				? own.trim().replace(/\/agent\/?$/, '') + '/agent'
				: ''
			: way,
	);
	async function copyUrl() {
		try {
			await navigator.clipboard.writeText(appUrl);
			copied = true;
			setTimeout(() => (copied = false), 1500);
		} catch {
			copied = false;
		}
	}

	// Whisper as an app (PLAN 53): a running Whisper app on this NAS is found
	// and taken over with one click; otherwise "Einrichten" opens the app.
	let whisperApp = $state<{ name: string; port: number } | null>(null);
	$effect(() => {
		api
			.get<DockerStatus>('/docker')
			.then((d) => {
				const c = (d.containers ?? []).find(
					(x) => x.state === 'running' && /speaches|faster-whisper|whisper/i.test(x.image),
				);
				const port = c?.ports.find((p) => p.protocol === 'tcp' && p.host)?.host;
				whisperApp = c && port ? { name: c.name, port } : null;
			})
			.catch(() => (whisperApp = null));
	});
	function useWhisperApp() {
		if (!settings || !whisperApp) return;
		settings.voice = {
			...(settings.voice ?? {}),
			enabled: true,
			url: `http://127.0.0.1:${whisperApp.port}`,
			// The model the Whisper app loads at its first start.
			model: settings.voice?.model || 'Systran/faster-whisper-large-v3',
		};
	}
</script>

<PageHead title={t('agent.settingsTitle')}>
	<p class="muted">{t('agent.settingsIntro')}</p>
</PageHead>

{#if error}<div class="notice-error" role="alert">{error}</div>{/if}

{#if settings}
	<Tabs
		tabs={TABS.map((id) => ({ id, label: t(`agent.tab.${id}` as MessageKey) }))}
		value={tab}
		label={t('agent.settingsTitle')}
		onchange={choose}
	/>
	{#if tab === 'overview'}
		<section class="tile panel hero" aria-labelledby="ag-hero">
			<div class="hero-row">
				<span class="hero-orb" aria-hidden="true"><Icon name="sparkle" size={26} /></span>
				<div class="hero-text">
					<h2 class="tile-title" id="ag-hero">{t('nav.agent')}</h2>
					<span class="muted"
						>{status?.reachable
							? t('agent.heroOn', { version: status.version ?? '', model: settings.model || '–' })
							: settings.enabled
								? t('agent.heroDown')
								: t('agent.heroOff')}</span
					>
				</div>
				<label class="check"
					><input type="checkbox" bind:checked={settings.enabled} /><span>{t('agent.enabled')}</span
					></label
				>
			</div>
		</section>
		<section class="tile panel" aria-labelledby="ag-phone">
			<h2 class="tile-title" id="ag-phone">{t('agent.phoneApp')}</h2>
			<p class="muted">{t('agent.phoneAppIntro')}</p>
			<div class="phone-app">
				<fieldset class="ways">
					<legend class="sr-only">{t('agent.phoneWay')}</legend>
					{#each addresses as a (a.url)}
						<label class="way" class:on={way === a.url}
							><input type="radio" name="way" value={a.url} bind:group={way} /><span
								><strong>{t(`agent.way.${a.kind}` as MessageKey)}</strong><small>{a.url}</small
								></span
							><span class="chip" class:ok={a.https}
								>{a.https ? t('agent.wayHttps') : t('agent.wayHttp')}</span
							></label
						>
					{/each}
					<label class="way" class:on={way === 'own'}
						><input type="radio" name="way" value="own" bind:group={way} /><span
							><strong>{t('agent.way.own')}</strong>
							{#if way === 'own'}<input
									class="field"
									bind:value={own}
									placeholder="https://nas.example.org"
									aria-label={t('agent.way.own')}
								/>{:else}<small>{t('agent.wayOwnHint')}</small>{/if}</span
						></label
					>
					<p class="muted small">{t('agent.phoneHttpsHint')}</p>
				</fieldset>
				<div class="qr">
					{#if appUrl}
						<QrCode text={appUrl} label={t('agent.phoneQr', { url: appUrl })} size={180} />
						<small class="mono">{appUrl.replace(/^https?:\/\//, '')}</small>
						<button type="button" class="btn" onclick={copyUrl}
							>{copied ? '✓ ' + t('agent.copied') : t('agent.copyLink')}</button
						>
					{:else}
						<p class="muted small">{t('agent.phoneNoUrl')}</p>
					{/if}
				</div>
			</div>
		</section>
	{/if}
	{#if tab === 'connection'}
		<section class="tile panel" aria-labelledby="ag-conn">
			<h2 class="tile-title" id="ag-conn">{t('agent.sectionConnection')}</h2>
			{#if clusterInfo?.member || settings.source === 'cluster'}
				<div class="stack">
					<span>{t('agent.source')}</span>
					<Segments
						label={t('agent.source')}
						value={settings.source ?? ''}
						options={[
							{ value: '', label: t('agent.sourceOwn') },
							{
								value: 'cluster',
								label: t('agent.sourceCluster', { node: clusterInfo?.adminNode ?? '' }),
							},
						]}
						onchange={(v) => (settings!.source = v)}
					/>
					<span class="muted">{t('agent.sourceHint')}</span>
				</div>
			{/if}
			{#if settings.source === 'cluster'}
				<div class="stack">
					<p class={shareState === 'allowed' ? 'notice-ok' : 'notice-warn'}>
						{shareState
							? t(shareText[shareState], { node: clusterInfo?.adminNode ?? '' })
							: t('agent.shareNotAsked', { node: clusterInfo?.adminNode ?? '' })}
					</p>
					{#if shareState !== 'allowed'}
						<span class="line"
							><button type="button" class="btn" onclick={askAdmin}
								>{t('agent.shareAsk', { node: clusterInfo?.adminNode ?? '' })}</button
							></span
						>
					{/if}
				</div>
			{:else}
				<label class="stack">
					<span>{t('agent.ollamaServer')}</span>
					<span class="line">
						<input
							class="field wide"
							bind:value={settings.ollama}
							placeholder="http://192.168.1.5:11434"
						/>
						<button type="button" class="btn" onclick={test}>{t('agent.check')}</button>
					</span>
					<span class="muted">{t('agent.ollamaHint')}</span>
				</label>
			{/if}
			{#if check}
				<p class={check.reachable ? 'notice-ok' : 'notice-warn'}>
					{check.reachable
						? t('agent.checkOk', { version: check.version ?? '', models: check.models ?? 0 })
						: t('agent.checkFail', { error: check.error ?? '' })}
				</p>
			{:else if status?.enabled}
				<p class="muted">
					<span class="dot {status.reachable ? 'ok' : 'error'}"></span>
					{status.reachable
						? t('agent.connected', {
								version: status.version ?? '',
								models: status.models?.length ?? 0,
							})
						: t('agent.notConnected', { error: status.error ?? '' })}
				</p>
			{/if}
			{#if status && !status.volume}<p class="notice-warn">{t('agent.noVolume')}</p>{/if}
		</section>

		{#if clusterInfo?.admin}
			<section class="tile panel" aria-labelledby="ag-nodes">
				<h2 class="tile-title" id="ag-nodes">{t('agent.sectionNodes')}</h2>
				<p class="muted">{t('agent.nodesIntro')}</p>
				{#if settings.shared?.length}
					<ul class="nodes">
						{#each settings.shared as sh (sh.node)}
							<li>
								<span class="node-name"
									><strong>{sh.name}</strong><small class="muted"
										>{t('agent.nodeAsked', { date: new Date(sh.asked).toLocaleString() })}</small
									></span
								>
								<span class="chip {sh.allowed ? 'ok' : 'warn'}"
									>{sh.allowed ? t('agent.nodeAllowed') : t('agent.nodePending')}</span
								>
								<button
									type="button"
									class="btn {sh.allowed ? '' : 'btn-primary'}"
									onclick={() => setShare(sh.node, { allowed: !sh.allowed })}
									>{sh.allowed ? t('agent.nodeRevoke') : t('agent.nodeRelease')}</button
								>
								<button
									type="button"
									class="btn"
									onclick={() => setShare(sh.node, { remove: true })}
									>{t('agent.nodeRemove')}</button
								>
							</li>
						{/each}
					</ul>
				{:else}
					<p class="muted">{t('agent.nodesNone')}</p>
				{/if}
			</section>
		{/if}

		<section class="tile panel" aria-labelledby="ag-model">
			<h2 class="tile-title" id="ag-model">{t('agent.sectionModel')}</h2>
			<label class="stack">
				<span>{t('agent.model')}</span>
				<select class="field" bind:value={settings.model}>
					<option value="">{t('agent.modelChoose')}</option>
					{#each models as m (m.name)}<option value={m.name}>{m.name} ({gb(m.size)})</option>{/each}
					{#if settings.model && !models.some((m) => m.name === settings!.model)}<option
							value={settings.model}>{settings.model}</option
						>{/if}
				</select>
				{#if chosen}
					<span class="muted caps">
						{t('agent.can')}
						{#each ['tools', 'thinking', 'vision'] as cap (cap)}
							<span class="chip {chosen.capabilities.includes(cap) ? 'ok' : ''}"
								>{chosen.capabilities.includes(cap) ? '✓' : '–'}
								{t(`agent.cap.${cap}` as MessageKey)}</span
							>
						{/each}
					</span>
				{/if}
				{#if others.length}<span class="muted"
						>{t('agent.modelsWithoutTools', { names: others.map((m) => m.name).join(', ') })}</span
					>{/if}
			</label>
			{#if chosen && chosen.levels.length}
				<div class="stack">
					<span>{t('agent.thinkDefault')}</span>
					<Segments
						label={t('agent.thinkDefault')}
						value={settings.think || chosen.defaultLevel || chosen.levels[0]}
						options={chosen.levels.map((l) => ({ value: l, label: levelLabel(l, i18n.locale) }))}
						onchange={(v) => (settings!.think = v)}
					/>
					<span class="muted">{t('agent.thinkHint')}</span>
				</div>
			{/if}
			<label class="stack">
				<span>{t('agent.context')}</span>
				<select class="field" bind:value={settings.context}>
					{#each contexts as c (c)}<option value={c}
							>{c === 0
								? t('agent.contextOllama')
								: tokens(c) + ' ' + t('agent.tokensWord')}</option
						>{/each}
				</select>
				<span class="muted">
					{#if modelContext}{t('agent.contextModel', { tokens: tokens(modelContext) })}{/if}
					{#if runningContext}{t('agent.contextNow', { tokens: tokens(runningContext) })}{:else}{t(
							'agent.contextUnknown',
						)}{/if}
					{t('agent.contextHint')}
					{#if (settings.context ?? 0) > 65536}{t('agent.contextVram')}{/if}
				</span>
			</label>
			<label class="stack">
				<span>{t('agent.maxSteps')}</span>
				<input
					class="field narrow"
					type="number"
					min="1"
					max="100"
					bind:value={settings.maxSteps}
				/>
				<span class="muted">{t('agent.maxStepsHint')}</span>
			</label>
			<label class="stack">
				<span>{t('agent.maxMinutes')}</span>
				<input
					class="field narrow"
					type="number"
					min="1"
					max="600"
					bind:value={settings.maxMinutes}
				/>
				<span class="muted">{t('agent.maxMinutesHint')}</span>
			</label>
			<label class="check"
				><input
					type="checkbox"
					checked={!settings.allTools}
					onchange={(e) => (settings!.allTools = !e.currentTarget.checked)}
				/><span>{t('agent.lazyTools')}</span></label
			>
			<span class="muted">{t('agent.lazyToolsHint')}</span>
			<label class="check"
				><input
					type="checkbox"
					checked={!settings.noDelegate}
					onchange={(e) => (settings!.noDelegate = !e.currentTarget.checked)}
				/><span>{t('agent.delegate')}</span></label
			>
			<span class="muted">{t('agent.delegateHint')}</span>
			<label class="stack">
				<span>{t('agent.keepAlive')}</span>
				<select class="field" bind:value={settings.keepAlive}>
					<option value="">{t('agent.keepAliveOllama')}</option>
					{#each ['5m', '30m', '2h', '-1'] as k (k)}<option value={k}
							>{t(`agent.keepAlive.${k}` as MessageKey)}</option
						>{/each}
				</select>
				<span class="muted">{t('agent.keepAliveHint')}</span>
			</label>
			{#if settings.source !== 'cluster'}<div class="stack">
					<span>{t('agent.pull')}</span>
					<span class="line">
						<input
							class="field"
							bind:value={pullName}
							placeholder="qwen3:8b"
							aria-label={t('agent.pull')}
						/>
						<button
							type="button"
							class="btn"
							disabled={pull?.running || !pullName.trim()}
							onclick={startPull}>{t('agent.pullStart')}</button
						>
					</span>
					{#if pull?.model}
						<span class="muted" role="status"
							>{pull.model}: {pull.error ? pull.error : pull.status}{pull.total
								? ` · ${Math.round(((pull.completed ?? 0) / pull.total) * 100)} %`
								: ''}</span
						>
					{/if}
					<span class="muted">{t('agent.pullHint')}</span>
				</div>{/if}
		</section>

		<section class="tile panel" aria-labelledby="ag-media">
			<h2 class="tile-title" id="ag-media">{t('agent.sectionMedia')}</h2>
			<div class="stack">
				<span>{t('agent.pictures')}</span>
				{#if chosen?.capabilities.includes('vision')}
					<span class="muted">✓ {t('agent.picturesMain', { model: chosen.name })}</span>
				{:else}
					<span class="muted">{t('agent.picturesNone')}</span>
				{/if}
				<label class="check"
					><input
						type="checkbox"
						checked={!!settings.vision?.enabled}
						onchange={(e) =>
							(settings!.vision = {
								...(settings!.vision ?? {}),
								enabled: e.currentTarget.checked,
							})}
					/><span>{t('agent.visionOwn')}</span></label
				>
				{#if settings.vision?.enabled}
					<select
						class="field"
						value={settings.vision?.model ?? ''}
						aria-label={t('agent.visionModel')}
						onchange={(e) =>
							(settings!.vision = { ...(settings!.vision ?? {}), model: e.currentTarget.value })}
					>
						<option value="">{t('agent.modelChoose')}</option>
						{#each visionModels as m (m.name)}<option value={m.name}>{m.name} ({gb(m.size)})</option
							>{/each}
					</select>
					<span class="muted">{t('agent.visionHint')}</span>
				{/if}
				{#if vram}<span class="muted"
						>{t('agent.vramNow', {
							gb: (vram / 1e9).toFixed(1).replace('.', i18n.locale === 'de' ? ',' : '.'),
						})}</span
					>{/if}
			</div>
			<div class="stack">
				<span>{t('agent.voice')}</span>
				<label class="check"
					><input
						type="checkbox"
						checked={!!settings.voice?.enabled}
						onchange={(e) =>
							(settings!.voice = { ...(settings!.voice ?? {}), enabled: e.currentTarget.checked })}
					/><span>{t('agent.voiceOn')}</span></label
				>
				{#if !settings.voice?.url}
					<span class="line" id="voice">
						{#if whisperApp}
							<button type="button" class="btn btn-primary" onclick={useWhisperApp}
								>{t('agent.whisperUse', { name: whisperApp.name, port: whisperApp.port })}</button
							>
						{:else}
							<a class="btn btn-primary" href="/apps/install/titanos/whisper"
								>{t('agent.whisperSetup')}</a
							>
							<span class="muted small">{t('agent.whisperSetupHint')}</span>
						{/if}
					</span>
				{/if}
				{#if settings.voice?.enabled}
					<span class="line">
						<input
							class="field"
							value={settings.voice?.url ?? ''}
							placeholder="http://192.168.1.5:8000"
							aria-label={t('agent.voiceUrl')}
							oninput={(e) =>
								(settings!.voice = { ...(settings!.voice ?? {}), url: e.currentTarget.value })}
						/>
						<button type="button" class="btn" onclick={checkVoice}>{t('agent.voiceCheck')}</button>
					</span>
					<input
						class="field"
						value={settings.voice?.model ?? ''}
						placeholder="Systran/faster-whisper-large-v3"
						aria-label={t('agent.voiceModel')}
						oninput={(e) =>
							(settings!.voice = { ...(settings!.voice ?? {}), model: e.currentTarget.value })}
					/>
					{#if voiceCheck}
						<span class={voiceCheck.reachable ? 'notice-ok' : 'notice-error'} role="status"
							>{voiceCheck.reachable
								? t('agent.voiceOk', { models: (voiceCheck.models ?? []).join(', ') || '–' })
								: t('agent.voiceDown', { error: voiceCheck.error ?? '' })}</span
						>
					{/if}
				{/if}
				<span class="muted">{t('agent.voiceHint')}</span>
			</div>
			<label class="check"
				><input
					type="checkbox"
					checked={!settings.noSnapshots}
					onchange={(e) => (settings!.noSnapshots = !e.currentTarget.checked)}
				/><span>{t('agent.snapshots')}</span></label
			>
			<span class="muted">{t('agent.snapshotsHint')}</span>
			<label class="check"
				><input
					type="checkbox"
					checked={!!settings.code?.enabled}
					onchange={(e) =>
						(settings!.code = { ...(settings!.code ?? {}), enabled: e.currentTarget.checked })}
				/><span>{t('agent.code')}</span></label
			>
			{#if settings.code?.enabled}
				<div class="code-limits">
					<label class="stack">
						<span>{t('agent.codeTimeout')}</span>
						<input
							class="field narrow"
							type="number"
							min="1"
							max="600"
							value={settings.code.timeout ?? 60}
							oninput={(e) =>
								(settings!.code = { ...settings!.code, timeout: Number(e.currentTarget.value) })}
						/>
					</label>
					<label class="stack">
						<span>{t('agent.codeMemory')}</span>
						<input
							class="field narrow"
							type="number"
							min="64"
							max="8192"
							step="64"
							value={settings.code.memory ?? 512}
							oninput={(e) =>
								(settings!.code = { ...settings!.code, memory: Number(e.currentTarget.value) })}
						/>
					</label>
				</div>
			{/if}
			<span class="muted">{t('agent.codeHint')}</span>
		</section>
	{/if}

	{#if tab === 'overview'}
		<section class="tile panel" aria-labelledby="ag-instruction">
			<h2 class="tile-title" id="ag-instruction">{t('agent.sectionInstruction')}</h2>
			<p class="muted">{t('agent.instructionIntro')}</p>
			<textarea
				class="field"
				rows="8"
				maxlength="8000"
				bind:value={settings.instruction}
				aria-labelledby="ag-instruction"></textarea>
			<div class="row-actions">
				{#if !settings.instruction?.trim()}<button
						type="button"
						class="btn"
						onclick={() => (settings!.instruction = t('agent.instructionTemplate'))}
						>{t('agent.instructionUseTemplate')}</button
					>{/if}
				<span class="muted">{t('agent.instructionHint')}</span>
			</div>
		</section>
	{/if}

	{#if tab === 'accounts'}
		<section class="tile panel" aria-labelledby="ag-users">
			<h2 class="tile-title" id="ag-users">{t('agent.sectionAccounts')}</h2>
			<p class="muted">{t('agent.accountsHint')}</p>
			{#each userAccounts as a (a.name)}
				<label class="check"
					><input
						type="checkbox"
						checked={settings.users?.includes(a.name)}
						onchange={(e) => toggleUser(a.name, e.currentTarget.checked)}
					/><span
						>{a.name}
						<span class="muted"
							>({t(a.role === 'admin' ? 'agent.roleAdmin' : 'agent.roleUser')})</span
						></span
					></label
				>
			{:else}
				<p class="muted">{t('agent.accountsNone')}</p>
			{/each}
		</section>

		<section class="tile panel" aria-labelledby="ag-loc">
			<h2 class="tile-title" id="ag-loc">{t('agent.sectionLocations')}</h2>
			<p class="muted">{t('agent.locationsHint')}</p>
			{#each settings.locations ?? [] as l, i (i)}
				<div class="loc">
					<input
						class="field"
						bind:value={l.name}
						placeholder={t('agent.locationName')}
						aria-label={t('agent.locationName')}
					/>
					<select class="field" bind:value={l.root} aria-label={t('agent.locationRoot')}>
						<option value="">{t('agent.locationRoot')}</option>
						{#each roots as r (r.id)}<option value={r.id}
								>{r.name} ({r.kind === 'share' ? t('agent.share') : t('agent.volume')})</option
							>{/each}
					</select>
					<input
						class="field"
						bind:value={l.path}
						placeholder={t('agent.locationPath')}
						aria-label={t('agent.locationPath')}
					/>
					<input
						class="field"
						bind:value={l.note}
						placeholder={t('agent.locationNote')}
						aria-label={t('agent.locationNote')}
					/>
					<button
						type="button"
						class="btn"
						aria-label={t('common.delete')}
						onclick={() => settings!.locations!.splice(i, 1)}
						><Icon name="trash" size={15} /></button
					>
				</div>
			{/each}
			<div class="row-actions">
				<button
					type="button"
					class="btn"
					onclick={() =>
						(settings!.locations = [
							...(settings!.locations ?? []),
							{ name: '', root: roots[0]?.id ?? '', path: '', note: '' },
						])}>{t('agent.locationAdd')}</button
				>
			</div>
		</section>
	{/if}

	{#if tab === 'rights'}
		<section class="tile panel" aria-labelledby="ag-perm">
			<h2 class="tile-title" id="ag-perm">{t('agent.sectionPermissions')}</h2>
			<div class="stack">
				<span>{t('agent.preset')}</span>
				<Segments
					label={t('agent.preset')}
					value={preset}
					options={[
						{ value: 'read', label: t('agent.presetRead') },
						{ value: 'standard', label: t('agent.presetStandard') },
						{ value: 'all', label: t('agent.presetAll') },
						{ value: 'custom', label: t('agent.presetCustom') },
					]}
					onchange={(v) => setPreset(v as Preset | 'custom')}
				/>
				<span class="muted">{t('agent.presetHint')}</span>
			</div>
			<div class="areas">
				{#each AREAS as area (area)}
					{@const list = tools.filter((x) => x.area === area)}
					{#if list.length}
						{@const m = areaMode(settings.policy, area, list)}
						<div class="area">
							<button
								type="button"
								class="expand"
								aria-expanded={!!open[area]}
								onclick={() => (open[area] = !open[area])}
							>
								<Icon name={open[area] ? 'chevronDown' : 'chevronRight'} size={15} />
								<span
									><strong>{areaLabel(area)}</strong><small class="muted"
										>{t('agent.toolCount', { count: list.length })}{m === 'mixed'
											? ' · ' + t('agent.mixed')
											: ''}</small
									></span
								>
							</button>
							<Segments
								label={areaLabel(area)}
								value={m === 'mixed' ? ('' as Mode) : m}
								options={modeOptions}
								onchange={(v) => setArea(area, v)}
							/>
						</div>
						{#if open[area]}
							{#each list as tool (tool.name)}
								<div class="area tool">
									<span class="tname"
										><code>{tool.name}</code>
										<span class="chip {tool.dangerous ? 'warn' : ''}"
											>{t(kindLabel[tool.kind] ?? 'agent.kindChange')}</span
										></span
									>
									<Segments
										label={tool.name}
										value={toolMode(settings.policy, tool)}
										options={modeOptions}
										onchange={(v) => setTool(tool.name, v)}
									/>
								</div>
							{/each}
						{/if}
					{/if}
				{/each}
			</div>
			<div class="stack">
				<span>{t('agent.folderRules')}</span>
				{#each settings.policy.folders ?? [] as f, i (i)}
					<div class="rule">
						<select class="field" bind:value={f.root} aria-label={t('agent.locationRoot')}>
							{#each roots as r (r.id)}<option value={r.id}>{r.name}</option>{/each}
						</select>
						<input
							class="field"
							bind:value={f.path}
							placeholder={t('agent.locationPath')}
							aria-label={t('agent.locationPath')}
						/>
						<Segments label={t('agent.folderRules')} bind:value={f.mode} options={modeOptions} />
						<button
							type="button"
							class="btn"
							aria-label={t('common.delete')}
							onclick={() => settings!.policy.folders!.splice(i, 1)}
							><Icon name="trash" size={15} /></button
						>
					</div>
				{/each}
				<span class="line"
					><button
						type="button"
						class="btn"
						onclick={() =>
							(settings!.policy.folders = [
								...(settings!.policy.folders ?? []),
								{ root: roots[0]?.id ?? '', path: '', mode: 'deny' },
							])}>{t('agent.folderAdd')}</button
					></span
				>
				<span class="muted">{t('agent.folderHint')}</span>
			</div>
			<p class="muted">{t('agent.alwaysDenied')}</p>
		</section>
	{/if}

	{#if tab === 'initiative'}
		<section class="tile panel" aria-labelledby="ag-react">
			<h2 class="tile-title" id="ag-react">{t('agent.sectionInitiative')}</h2>
			<label class="check"
				><input type="checkbox" bind:checked={settings.react} /><span>{t('agent.react')}</span
				></label
			>
			<p class="muted">{t('agent.reactHint')}</p>
			<label class="check"
				><input type="checkbox" bind:checked={settings.weeklyReport} /><span
					>{t('agent.weeklyReport')}</span
				></label
			>
			<label class="stack">
				<span>{t('agent.reactAs')}</span>
				<select class="field" bind:value={settings.reactAs}>
					<option value="">{t('agent.firstOwner')}</option>
					{#each admins as a (a.name)}<option value={a.name}>{a.name}</option>{/each}
				</select>
			</label>
		</section>
	{/if}

	{#if tab === 'ha'}
		<section class="tile panel" aria-labelledby="ag-ha">
			<h2 class="tile-title" id="ag-ha">{t('agent.sectionHA')}</h2>
			<p class="muted">{t('agent.haIntro')}</p>
			<div class="stack">
				<span>{t('agent.haRelease')}</span>
				<span class="muted">{t('agent.haReleaseHint')}</span>
				{#if haConns.length > 1}<label class="narrow"
						><span>{t('notify.haConnection')}</span>
						<select
							class="field"
							value={haConn}
							onchange={(e) => chooseHA((e.currentTarget as HTMLSelectElement).value)}
						>
							{#each haConns as c (c.id)}<option value={c.id}>{c.name || t('ha.conn.first')}</option
								>{/each}
						</select></label
					>{/if}
				{#if haEntities === null}
					<span class="line"
						><button type="button" class="btn" onclick={loadHA}>{t('agent.haLoad')}</button>
						<span class="muted"
							>{t('agent.haReleased', {
								domains: haRelease?.domains?.length ?? 0,
								entities: haRelease?.entities?.length ?? 0,
							})}</span
						></span
					>
					{#if haError}<p class="notice-warn">{haError}</p>{/if}
				{:else}
					<fieldset class="domains">
						<legend>{t('agent.haDomains')}</legend>
						{#each haDomains as d (d)}
							<label class="check"
								><input
									type="checkbox"
									checked={haRelease?.domains?.includes(d)}
									onchange={() => toggleRelease('domains', d)}
								/><span>{d}</span></label
							>
						{/each}
					</fieldset>
					<input
						class="field"
						type="search"
						bind:value={haQuery}
						placeholder={t('agent.haSearch')}
						aria-label={t('agent.haSearch')}
					/>
					<div class="entities">
						{#each haShown as e (e.id)}
							{@const viaDomain = haRelease?.domains?.includes(e.domain)}
							<label class="check"
								><input
									type="checkbox"
									checked={viaDomain || haRelease?.entities?.includes(e.id)}
									disabled={viaDomain}
									onchange={() => toggleRelease('entities', e.id)}
								/><span
									>{e.name} <code>{e.id}</code>{#if e.area}<span class="muted">
											· {e.area}</span
										>{/if}</span
								></label
							>
						{:else}
							<p class="muted">{t('agent.haNoEntities')}</p>
						{/each}
					</div>
				{/if}
			</div>
			<div class="stack">
				<span>{t('agent.haRules')}</span>
				{#each haRules as r, i (i)}
					<div class="rule ha">
						<input
							class="field"
							bind:value={r.key}
							placeholder="domain:lock · entity:light.flur · area:kueche"
							aria-label={t('agent.haRules')}
						/>
						<Segments label={t('agent.haRules')} bind:value={r.mode} options={modeOptions} />
						<button
							type="button"
							class="btn"
							aria-label={t('common.delete')}
							onclick={() => haRules.splice(i, 1)}><Icon name="trash" size={15} /></button
						>
					</div>
				{/each}
				<span class="line"
					><button
						type="button"
						class="btn"
						onclick={() => haRules.push({ key: 'domain:', mode: 'ask' })}
						>{t('agent.haRuleAdd')}</button
					></span
				>
				<span class="muted">{t('agent.haRulesHint')}</span>
			</div>
			<label class="stack">
				<span>{t('agent.haNotify')}</span>
				<input class="field" bind:value={haNotify} placeholder="notify.mobile_app_iphone" />
				<span class="muted">{t('agent.haNotifyHint')}</span>
			</label>
			<label class="check"
				><input type="checkbox" bind:checked={settings.homeassistant.assist} /><span
					>{t('agent.haAssist')}</span
				></label
			>
			<p class="muted">{t('agent.haAssistHint')}</p>
			{#if settings.homeassistant.assist}
				<label class="stack">
					<span>{t('agent.haAssistAs')}</span>
					<select class="field" bind:value={settings.homeassistant.assistAs}>
						<option value="">{t('agent.firstOwner')}</option>
						{#each admins as a (a.name)}<option value={a.name}>{a.name}</option>{/each}
					</select>
				</label>
				<p class="notice-ok">
					{t('agent.haAssistSetup', {
						url: typeof location === 'undefined' ? '' : location.origin + '/agent-ollama',
					})}
				</p>
			{/if}
		</section>
	{/if}

	<div class="row-actions sticky">
		<button type="button" class="btn btn-primary" disabled={saving} onclick={save}
			>{t('common.save')}</button
		>
		{#if saved}<span class="muted" role="status">✓ {t('agent.saved')}</span>{/if}
		<a class="btn" href="/agent">{t('agent.openAgent')}</a>
	</div>
{/if}

<style>
	.code-limits {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-3);
	}
	.hero {
		background: linear-gradient(
			120deg,
			color-mix(in srgb, var(--accent) 16%, transparent),
			transparent 60%
		);
	}
	.hero-row {
		display: flex;
		align-items: center;
		gap: var(--space-3);
		flex-wrap: wrap;
	}
	.hero-orb {
		width: 48px;
		height: 48px;
		border-radius: 15px;
		display: grid;
		place-items: center;
		flex: none;
		color: var(--on-accent);
		background: var(--accent);
	}
	.hero-text {
		flex: 1;
		min-width: 12rem;
		display: flex;
		flex-direction: column;
	}
	.hero-text .tile-title {
		margin: 0;
	}
	.phone-app {
		display: grid;
		grid-template-columns: minmax(0, 1fr) auto;
		gap: var(--space-4);
		align-items: start;
	}
	@media (max-width: 760px) {
		.phone-app {
			grid-template-columns: minmax(0, 1fr);
		}
	}
	.ways {
		border: 0;
		margin: 0;
		padding: 0;
		min-width: 0;
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}
	.way {
		display: flex;
		align-items: center;
		gap: var(--space-3);
		padding: var(--space-2) var(--space-3);
		border: 1px solid var(--border);
		border-radius: var(--radius-md, 10px);
		cursor: pointer;
	}
	.way.on {
		border-color: var(--accent);
		background: color-mix(in srgb, var(--accent) 10%, transparent);
	}
	.way > span:first-of-type {
		flex: 1;
		min-width: 0;
		display: flex;
		flex-direction: column;
		gap: 2px;
	}
	.way small {
		color: var(--muted);
		font-family: var(--font-mono, monospace);
		overflow-wrap: anywhere;
	}
	.way .chip.ok {
		color: var(--success, #34d399);
	}
	.qr {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-3);
		border: 1px solid var(--border);
		border-radius: var(--radius-md, 10px);
		min-width: 13rem;
	}
	.qr .mono {
		font-family: var(--font-mono, monospace);
		color: var(--muted);
		overflow-wrap: anywhere;
		text-align: center;
	}
	.line {
		display: flex;
		gap: var(--space-2);
		align-items: center;
		flex-wrap: wrap;
	}
	.line .btn {
		max-width: 100%;
		white-space: normal;
		height: auto;
	}
	.wide {
		flex: 1;
		min-width: 240px;
	}
	.narrow {
		width: 100px;
	}
	.caps {
		display: flex;
		gap: 6px;
		align-items: center;
		flex-wrap: wrap;
	}
	.loc {
		display: grid;
		grid-template-columns:
			minmax(120px, 1fr) minmax(120px, 1fr) minmax(120px, 1fr) minmax(120px, 1.2fr)
			auto;
		gap: var(--space-2);
		align-items: center;
		margin-bottom: var(--space-2);
	}
	.rule {
		display: grid;
		grid-template-columns: minmax(140px, 1fr) minmax(140px, 1.4fr) auto auto;
		gap: var(--space-2);
		align-items: center;
		margin-bottom: var(--space-2);
	}
	.rule :global(.segments) {
		flex-wrap: nowrap;
	}
	.rule.ha {
		grid-template-columns: minmax(200px, 1fr) auto auto;
	}
	.domains {
		display: flex;
		flex-wrap: wrap;
		gap: 4px 16px;
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		padding: var(--space-2) var(--space-3);
	}
	.entities {
		max-height: 320px;
		overflow-y: auto;
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		padding: var(--space-2) var(--space-3);
		display: flex;
		flex-direction: column;
		gap: 2px;
	}
	.areas {
		display: flex;
		flex-direction: column;
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		overflow: hidden;
		margin: var(--space-3) 0;
	}
	.area {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-3);
		padding: 8px 12px;
		border-bottom: 1px solid var(--border);
		flex-wrap: wrap;
	}
	.area:last-child {
		border-bottom: 0;
	}
	.area.tool {
		padding-left: 36px;
		background: color-mix(in srgb, var(--text) 2.5%, transparent);
	}
	.expand {
		display: flex;
		gap: 8px;
		align-items: center;
		border: 0;
		background: none;
		color: var(--text);
		font: inherit;
		cursor: pointer;
		text-align: left;
		padding: 0;
	}
	.expand span {
		display: flex;
		flex-direction: column;
	}
	.tname {
		display: flex;
		gap: 8px;
		align-items: center;
		flex-wrap: wrap;
	}
	.sticky {
		position: sticky;
		bottom: var(--fab-clear, 0px);
		padding: var(--space-3) 0;
		background: linear-gradient(transparent, var(--bg) 30%);
		display: flex;
		gap: var(--space-2);
		align-items: center;
	}
	@media (max-width: 760px) {
		.loc,
		.rule,
		.rule.ha {
			grid-template-columns: 1fr;
			border-bottom: 1px solid var(--border);
			padding-bottom: var(--space-2);
		}
	}
	.nodes {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-direction: column;
	}
	.nodes li {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		flex-wrap: wrap;
		padding: var(--space-2) 0;
		border-top: 1px solid var(--border);
	}
	.node-name {
		flex: 1;
		min-width: 10rem;
		display: flex;
		flex-direction: column;
	}
</style>
