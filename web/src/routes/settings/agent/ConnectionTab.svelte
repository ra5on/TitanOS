<script lang="ts">
	// Settings › KI-Agent › Verbindung: the Ollama server (own or the
	// cluster's), the model with thinking level and context, speech and pictures.
	import Segments from '#lib/components/Segments.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import { i18n, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import {
		gb,
		levelLabel,
		tokens,
		type ClusterInfo,
		type Settings,
		type ShareState,
		type Status,
	} from '#lib/agent.ts';

	let {
		settings = $bindable(),
		pullName = $bindable(),
		status,
		clusterInfo,
		shareState,
		check,
		pull,
		voiceCheck,
		whisperApp,
		askAdmin,
		setShare,
		startPull,
		test,
		checkVoice,
		useWhisperApp,
	}: {
		settings: Settings;
		pullName: string;
		status: Status | null;
		clusterInfo: ClusterInfo | null;
		shareState: ShareState | undefined;
		check: { reachable: boolean; version?: string; error?: string; models?: number } | null;
		pull: {
			model?: string;
			status?: string;
			completed?: number;
			total?: number;
			error?: string;
			running?: boolean;
		} | null;
		voiceCheck: { reachable: boolean; models?: string[]; error?: string } | null;
		whisperApp: { name: string; port: number } | null;
		askAdmin: () => void;
		setShare: (node: string, body: { allowed?: boolean; remove?: boolean }) => void;
		startPull: () => void;
		test: () => void;
		checkVoice: () => void;
		useWhisperApp: () => void;
	} = $props();

	const shareText: Record<ShareState, MessageKey> = {
		allowed: 'agent.shareAllowed',
		pending: 'agent.sharePending',
		off: 'agent.shareOff',
		unreachable: 'agent.shareUnreachable',
		old: 'agent.shareOld',
		nocluster: 'agent.shareNoCluster',
	};

	const models = $derived((status?.models ?? []).filter((m) => m.capabilities.includes('tools')));
	const others = $derived((status?.models ?? []).filter((m) => !m.capabilities.includes('tools')));
	const chosen = $derived(models.find((m) => m.name === settings?.model));
	// Speech and pictures (PLAN 49).
	const visionModels = $derived(
		(status?.models ?? []).filter((m) => m.capabilities.includes('vision')),
	);
	const vram = $derived((status?.running ?? []).reduce((sum, r) => sum + (r.vram ?? 0), 0));
	const runningContext = $derived(
		status?.running?.find((r) => r.name === settings?.model)?.contextLength ?? 0,
	);
	// PLAN 59a: up to 256k, but never more than the model can.
	const modelContext = $derived(
		status?.models?.find((m) => m.name === settings?.model)?.contextLength ?? 0,
	);
	const contexts = $derived(
		[0, 8192, 16384, 32768, 65536, 131072, 196608, 262144].filter(
			(c) => c === 0 || !modelContext || c <= modelContext || c === settings?.context,
		),
	);
</script>

<!-- PLAN 78-1: plain headings with one row per setting (they were cards). -->
<SettingGroup title={t('agent.sectionConnection')}>
	{#if clusterInfo?.member || settings.source === 'cluster'}
		<SettingRow icon="tailnet" title={t('agent.source')} desc={t('agent.sourceHint')}>
			{#snippet control()}
				<Segments
					compact
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
			{/snippet}
		</SettingRow>
	{/if}
	{#if settings.source === 'cluster'}
		<p class={shareState === 'allowed' ? 'notice-ok' : 'notice-warn'}>
			{shareState
				? t(shareText[shareState], { node: clusterInfo?.adminNode ?? '' })
				: t('agent.shareNotAsked', { node: clusterInfo?.adminNode ?? '' })}
		</p>
	{:else}
		<SettingRow
			icon="globe"
			title={t('agent.ollamaServer')}
			desc={t('agent.ollamaHint')}
			for="ag-ollama"
		>
			{#snippet control()}
				<input
					id="ag-ollama"
					class="field"
					bind:value={settings.ollama}
					placeholder="http://192.168.1.5:11434"
				/>
				<button type="button" class="btn" onclick={test}>{t('agent.check')}</button>
			{/snippet}
		</SettingRow>
	{/if}
	{#if check}
		<p class={check.reachable ? 'notice-ok' : 'notice-warn'}>
			{check.reachable
				? t('agent.checkOk', { version: check.version ?? '', models: check.models ?? 0 })
				: t('agent.checkFail', { error: check.error ?? '' })}
		</p>
	{:else if status?.enabled}
		<p class="muted state">
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
	{#if settings.source === 'cluster' && shareState !== 'allowed'}
		<div class="row-actions">
			<button type="button" class="btn" onclick={askAdmin}
				>{t('agent.shareAsk', { node: clusterInfo?.adminNode ?? '' })}</button
			>
		</div>
	{/if}
</SettingGroup>

{#if clusterInfo?.admin}
	<SettingGroup title={t('agent.sectionNodes')} desc={t('agent.nodesIntro')}>
		{#each settings.shared ?? [] as sh (sh.node)}
			<SettingRow
				icon="tailnet"
				title={sh.name}
				desc={t('agent.nodeAsked', { date: new Date(sh.asked).toLocaleString() })}
			>
				{#snippet control()}
					<span class="chip {sh.allowed ? 'ok' : 'warn'}"
						>{sh.allowed ? '✓ ' + t('agent.nodeAllowed') : t('agent.nodePending')}</span
					>
					<button
						type="button"
						class="btn {sh.allowed ? '' : 'btn-primary'}"
						onclick={() => setShare(sh.node, { allowed: !sh.allowed })}
						>{sh.allowed ? t('agent.nodeRevoke') : t('agent.nodeRelease')}</button
					>
					<button type="button" class="btn" onclick={() => setShare(sh.node, { remove: true })}
						>{t('agent.nodeRemove')}</button
					>
				{/snippet}
			</SettingRow>
		{:else}
			<p class="muted">{t('agent.nodesNone')}</p>
		{/each}
	</SettingGroup>
{/if}

<SettingGroup title={t('agent.sectionModel')}>
	<SettingRow icon="sparkle" title={t('agent.model')} for="ag-model">
		{#snippet description()}
			{#if chosen}
				<span class="caps">
					{t('agent.can')}
					{#each ['tools', 'thinking', 'vision'] as cap (cap)}
						<span class="chip {chosen.capabilities.includes(cap) ? 'ok' : ''}"
							>{chosen.capabilities.includes(cap) ? '✓' : '–'}
							{t(`agent.cap.${cap}` as MessageKey)}</span
						>
					{/each}
				</span>
			{/if}
			{#if others.length}<span class="line"
					>{t('agent.modelsWithoutTools', {
						names: others.map((m) => m.name).join(', '),
					})}</span
				>{/if}
		{/snippet}
		{#snippet control()}
			<select id="ag-model" class="field" bind:value={settings.model}>
				<option value="">{t('agent.modelChoose')}</option>
				{#each models as m (m.name)}<option value={m.name}>{m.name} ({gb(m.size)})</option>{/each}
				{#if settings.model && !models.some((m) => m.name === settings!.model)}<option
						value={settings.model}>{settings.model}</option
					>{/if}
			</select>
		{/snippet}
	</SettingRow>
	{#if chosen && chosen.levels.length}
		<SettingRow icon="bulb" title={t('agent.thinkDefault')} desc={t('agent.thinkHint')}>
			{#snippet control()}
				<Segments
					compact
					label={t('agent.thinkDefault')}
					value={settings.think || chosen.defaultLevel || chosen.levels[0]}
					options={chosen.levels.map((l) => ({ value: l, label: levelLabel(l, i18n.locale) }))}
					onchange={(v) => (settings!.think = v)}
				/>
			{/snippet}
		</SettingRow>
	{/if}
	<SettingRow icon="memory" title={t('agent.context')} for="ag-context">
		{#snippet description()}
			{#if modelContext}{t('agent.contextModel', { tokens: tokens(modelContext) })}{/if}
			{#if runningContext}{t('agent.contextNow', {
					tokens: tokens(runningContext),
				})}{:else}{t('agent.contextUnknown')}{/if}
			{t('agent.contextHint')}
			{#if (settings.context ?? 0) > 65536}{t('agent.contextVram')}{/if}
		{/snippet}
		{#snippet control()}
			<select id="ag-context" class="field" bind:value={settings.context}>
				{#each contexts as c (c)}<option value={c}
						>{c === 0 ? t('agent.contextOllama') : tokens(c) + ' ' + t('agent.tokensWord')}</option
					>{/each}
			</select>
		{/snippet}
	</SettingRow>
	<SettingRow icon="list" title={t('agent.maxSteps')} desc={t('agent.maxStepsHint')} for="ag-steps">
		{#snippet control()}
			<input
				id="ag-steps"
				class="field"
				type="number"
				min="1"
				max="100"
				bind:value={settings.maxSteps}
			/>
		{/snippet}
	</SettingRow>
	<SettingRow
		icon="clock"
		title={t('agent.maxMinutes')}
		desc={t('agent.maxMinutesHint')}
		for="ag-minutes"
	>
		{#snippet control()}
			<input
				id="ag-minutes"
				class="field"
				type="number"
				min="1"
				max="600"
				bind:value={settings.maxMinutes}
			/>
		{/snippet}
	</SettingRow>
	<SettingRow
		icon="tool"
		title={t('agent.lazyTools')}
		desc={t('agent.lazyToolsHint')}
		for="ag-lazy"
	>
		{#snippet control()}
			<input
				id="ag-lazy"
				type="checkbox"
				class="switch"
				role="switch"
				checked={!settings.allTools}
				onchange={(e) => (settings!.allTools = !e.currentTarget.checked)}
			/>
		{/snippet}
	</SettingRow>
	<SettingRow
		icon="users"
		title={t('agent.delegate')}
		desc={t('agent.delegateHint')}
		for="ag-delegate"
	>
		{#snippet control()}
			<input
				id="ag-delegate"
				type="checkbox"
				class="switch"
				role="switch"
				checked={!settings.noDelegate}
				onchange={(e) => (settings!.noDelegate = !e.currentTarget.checked)}
			/>
		{/snippet}
	</SettingRow>
	<SettingRow
		icon="pause"
		title={t('agent.keepAlive')}
		desc={t('agent.keepAliveHint')}
		for="ag-keep"
	>
		{#snippet control()}
			<select id="ag-keep" class="field" bind:value={settings.keepAlive}>
				<option value="">{t('agent.keepAliveOllama')}</option>
				{#each ['5m', '30m', '2h', '-1'] as k (k)}<option value={k}
						>{t(`agent.keepAlive.${k}` as MessageKey)}</option
					>{/each}
			</select>
		{/snippet}
	</SettingRow>
	{#if settings.source !== 'cluster'}
		<SettingRow icon="download" title={t('agent.pull')} for="ag-pull">
			{#snippet description()}
				{t('agent.pullHint')}
				{#if pull?.model}
					<span class="line" role="status"
						>{pull.model}: {pull.error ? pull.error : pull.status}{pull.total
							? ` · ${Math.round(((pull.completed ?? 0) / pull.total) * 100)} %`
							: ''}</span
					>
				{/if}
			{/snippet}
			{#snippet control()}
				<input id="ag-pull" class="field" bind:value={pullName} placeholder="qwen3:8b" />
				<button
					type="button"
					class="btn"
					disabled={pull?.running || !pullName.trim()}
					onclick={startPull}>{t('agent.pullStart')}</button
				>
			{/snippet}
		</SettingRow>
	{/if}
</SettingGroup>

<SettingGroup title={t('agent.sectionMedia')}>
	<SettingRow icon="image" title={t('agent.pictures')}>
		{#snippet description()}
			{#if chosen?.capabilities.includes('vision')}
				<span class="line">✓ {t('agent.picturesMain', { model: chosen.name })}</span>
			{:else}
				<span class="line">{t('agent.picturesNone')}</span>
			{/if}
			{#if vram}<span class="line"
					>{t('agent.vramNow', {
						gb: (vram / 1e9).toFixed(1).replace('.', i18n.locale === 'de' ? ',' : '.'),
					})}</span
				>{/if}
		{/snippet}
	</SettingRow>
	<SettingRow icon="eye" title={t('agent.visionOwn')} for="ag-vision-on">
		{#snippet control()}
			<input
				id="ag-vision-on"
				type="checkbox"
				class="switch"
				role="switch"
				checked={!!settings.vision?.enabled}
				onchange={(e) =>
					(settings!.vision = {
						...(settings!.vision ?? {}),
						enabled: e.currentTarget.checked,
					})}
			/>
		{/snippet}
	</SettingRow>
	{#if settings.vision?.enabled}
		<SettingRow
			icon="eye"
			title={t('agent.visionModel')}
			desc={t('agent.visionHint')}
			for="ag-vision"
		>
			{#snippet control()}
				<select
					id="ag-vision"
					class="field"
					value={settings.vision?.model ?? ''}
					onchange={(e) =>
						(settings!.vision = { ...(settings!.vision ?? {}), model: e.currentTarget.value })}
				>
					<option value="">{t('agent.modelChoose')}</option>
					{#each visionModels as m (m.name)}<option value={m.name}>{m.name} ({gb(m.size)})</option
						>{/each}
				</select>
			{/snippet}
		</SettingRow>
	{/if}
	<SettingRow icon="mic" title={t('agent.voiceOn')} desc={t('agent.voiceHint')} for="ag-voice-on">
		{#snippet control()}
			<input
				id="ag-voice-on"
				type="checkbox"
				class="switch"
				role="switch"
				checked={!!settings.voice?.enabled}
				onchange={(e) =>
					(settings!.voice = {
						...(settings!.voice ?? {}),
						enabled: e.currentTarget.checked,
					})}
			/>
		{/snippet}
	</SettingRow>
	{#if !settings.voice?.url}
		<SettingRow
			id="voice"
			icon="mic"
			title={t('agent.voice')}
			desc={whisperApp ? undefined : t('agent.whisperSetupHint')}
		>
			{#snippet control()}
				{#if whisperApp}
					<button type="button" class="btn btn-primary" onclick={useWhisperApp}
						>{t('agent.whisperUse', { name: whisperApp.name, port: whisperApp.port })}</button
					>
				{:else}
					<a class="btn btn-primary" href="/apps/install/titanos/whisper"
						>{t('agent.whisperSetup')}</a
					>
				{/if}
			{/snippet}
		</SettingRow>
	{/if}
	{#if settings.voice?.enabled}
		<SettingRow icon="globe" title={t('agent.voiceUrl')} for="ag-voice-url">
			{#snippet control()}
				<input
					id="ag-voice-url"
					class="field"
					value={settings.voice?.url ?? ''}
					placeholder="http://192.168.1.5:8000"
					oninput={(e) =>
						(settings!.voice = { ...(settings!.voice ?? {}), url: e.currentTarget.value })}
				/>
				<button type="button" class="btn" onclick={checkVoice}>{t('agent.voiceCheck')}</button>
			{/snippet}
		</SettingRow>
		<SettingRow icon="mic" title={t('agent.voiceModel')} for="ag-voice-model">
			{#snippet control()}
				<input
					id="ag-voice-model"
					class="field"
					value={settings.voice?.model ?? ''}
					placeholder="Systran/faster-whisper-large-v3"
					oninput={(e) =>
						(settings!.voice = { ...(settings!.voice ?? {}), model: e.currentTarget.value })}
				/>
			{/snippet}
		</SettingRow>
		{#if voiceCheck}
			<p class={voiceCheck.reachable ? 'notice-ok' : 'notice-error'} role="status">
				{voiceCheck.reachable
					? t('agent.voiceOk', { models: (voiceCheck.models ?? []).join(', ') || '–' })
					: t('agent.voiceDown', { error: voiceCheck.error ?? '' })}
			</p>
		{/if}
	{/if}
	<SettingRow
		icon="camera"
		title={t('agent.snapshots')}
		desc={t('agent.snapshotsHint')}
		for="ag-snapshots"
	>
		{#snippet control()}
			<input
				id="ag-snapshots"
				type="checkbox"
				class="switch"
				role="switch"
				checked={!settings.noSnapshots}
				onchange={(e) => (settings!.noSnapshots = !e.currentTarget.checked)}
			/>
		{/snippet}
	</SettingRow>
	<SettingRow icon="terminal" title={t('agent.code')} desc={t('agent.codeHint')} for="ag-code">
		{#snippet control()}
			<input
				id="ag-code"
				type="checkbox"
				class="switch"
				role="switch"
				checked={!!settings.code?.enabled}
				onchange={(e) =>
					(settings!.code = { ...(settings!.code ?? {}), enabled: e.currentTarget.checked })}
			/>
		{/snippet}
	</SettingRow>
	{#if settings.code?.enabled}
		<SettingRow icon="clock" title={t('agent.codeTimeout')} for="ag-code-timeout">
			{#snippet control()}
				<input
					id="ag-code-timeout"
					class="field"
					type="number"
					min="1"
					max="600"
					value={settings.code?.timeout ?? 60}
					oninput={(e) =>
						(settings!.code = { ...settings!.code, timeout: Number(e.currentTarget.value) })}
				/>
			{/snippet}
		</SettingRow>
		<SettingRow icon="memory" title={t('agent.codeMemory')} for="ag-code-memory">
			{#snippet control()}
				<input
					id="ag-code-memory"
					class="field"
					type="number"
					min="64"
					max="8192"
					step="64"
					value={settings.code?.memory ?? 512}
					oninput={(e) =>
						(settings!.code = { ...settings!.code, memory: Number(e.currentTarget.value) })}
				/>
			{/snippet}
		</SettingRow>
	{/if}
</SettingGroup>

<style>
	.state {
		margin: 0;
	}
	.caps {
		display: flex;
		gap: var(--space-1-5);
		align-items: center;
		flex-wrap: wrap;
	}
	.line {
		display: block;
	}
</style>
