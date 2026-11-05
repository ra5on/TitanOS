<script lang="ts">
	// Settings › KI-Agent (PLAN 44): the Ollama server, model, thinking level
	// and context as Ollama reports them, which accounts may use the agent,
	// named locations, what the agent may do (quick choice, areas, single
	// tools and folders with "Ohne Nachfrage / Nachfragen / Verboten"), its
	// own initiative and Home Assistant. Owners and administrators only.
	import PageHead from '#lib/components/PageHead.svelte';
	import Segments from '#lib/components/Segments.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import { api, type FileRoot } from '#lib/api.ts';
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
		type Settings,
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
	let haNotify = $state('');
	let haRules = $state<{ key: string; mode: Mode }[]>([]);
	// Home Assistant: domains and entities for the agent's own release list.
	let haDomains = $state<string[]>([]);
	let haEntities = $state<{ id: string; name: string; area?: string; domain: string }[] | null>(
		null,
	);
	let haQuery = $state('');
	let haError = $state('');
	async function loadHA() {
		haError = '';
		try {
			const s = await api.get<{ domains: string[] }>('/homeassistant/settings');
			haDomains = s.domains ?? [];
			haEntities = (
				await api.get<{ entities: { id: string; name: string; area?: string; domain: string }[] }>(
					'/homeassistant/entities',
				)
			).entities;
		} catch (e) {
			haError = apiErrorText(e);
		}
	}
	function toggleRelease(kind: 'domains' | 'entities', value: string) {
		if (!settings) return;
		const r = (settings.homeassistant.release ??= {});
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
	const contexts = [0, 8192, 16384, 32768, 65536, 131072];
</script>

<PageHead title={t('agent.settingsTitle')}>
	<p class="muted">{t('agent.settingsIntro')}</p>
</PageHead>

{#if error}<div class="notice-error" role="alert">{error}</div>{/if}

{#if settings}
	<section class="tile panel" aria-labelledby="ag-conn">
		<h2 class="tile-title" id="ag-conn">{t('agent.sectionConnection')}</h2>
		<label class="check"
			><input type="checkbox" bind:checked={settings.enabled} /><span>{t('agent.enabled')}</span
			></label
		>
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
						>{c === 0 ? t('agent.contextOllama') : tokens(c) + ' ' + t('agent.tokensWord')}</option
					>{/each}
			</select>
			<span class="muted">
				{#if runningContext}{t('agent.contextNow', { tokens: tokens(runningContext) })}{:else}{t(
						'agent.contextUnknown',
					)}{/if}
				{t('agent.contextHint')}
			</span>
		</label>
		<label class="stack">
			<span>{t('agent.maxSteps')}</span>
			<input class="field narrow" type="number" min="1" max="100" bind:value={settings.maxSteps} />
			<span class="muted">{t('agent.maxStepsHint')}</span>
		</label>
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
		<div class="stack">
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
		</div>
	</section>

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
					<span class="muted">({t(a.role === 'admin' ? 'agent.roleAdmin' : 'agent.roleUser')})</span
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
					onclick={() => settings!.locations!.splice(i, 1)}><Icon name="trash" size={15} /></button
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

	<section class="tile panel" aria-labelledby="ag-react">
		<h2 class="tile-title" id="ag-react">{t('agent.sectionInitiative')}</h2>
		<label class="check"
			><input type="checkbox" bind:checked={settings.react} /><span>{t('agent.react')}</span></label
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

	<section class="tile panel" aria-labelledby="ag-ha">
		<h2 class="tile-title" id="ag-ha">{t('agent.sectionHA')}</h2>
		<p class="muted">{t('agent.haIntro')}</p>
		<div class="stack">
			<span>{t('agent.haRelease')}</span>
			<span class="muted">{t('agent.haReleaseHint')}</span>
			{#if haEntities === null}
				<span class="line"
					><button type="button" class="btn" onclick={loadHA}>{t('agent.haLoad')}</button>
					<span class="muted"
						>{t('agent.haReleased', {
							domains: settings.homeassistant.release?.domains?.length ?? 0,
							entities: settings.homeassistant.release?.entities?.length ?? 0,
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
								checked={settings.homeassistant.release?.domains?.includes(d)}
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
						{@const viaDomain = settings.homeassistant.release?.domains?.includes(e.domain)}
						<label class="check"
							><input
								type="checkbox"
								checked={viaDomain || settings.homeassistant.release?.entities?.includes(e.id)}
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

	<div class="row-actions sticky">
		<button type="button" class="btn btn-primary" disabled={saving} onclick={save}
			>{t('common.save')}</button
		>
		{#if saved}<span class="muted" role="status">✓ {t('agent.saved')}</span>{/if}
		<a class="btn" href="/agent">{t('agent.openAgent')}</a>
	</div>
{/if}

<style>
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
</style>
