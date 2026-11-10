<script lang="ts">
	// Settings › KI-Agent › Verbindung: the Ollama server (own or the
	// cluster's), the model with thinking level and context, speech and pictures.
	import Segments from '#lib/components/Segments.svelte';
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

<section class="tile ui-card" aria-labelledby="ag-conn">
	<div class="card-head"><h2 id="ag-conn">{t('agent.sectionConnection')}</h2></div>
	{#if clusterInfo?.member || settings.source === 'cluster'}
		<div class="set-row">
			<span class="set-label"
				>{t('agent.source')}<span class="set-hint">{t('agent.sourceHint')}</span></span
			>
			<div class="set-control">
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
			</div>
		</div>
	{/if}
	{#if settings.source === 'cluster'}
		<div class="set-row one">
			<p class={shareState === 'allowed' ? 'notice-ok' : 'notice-warn'}>
				{shareState
					? t(shareText[shareState], { node: clusterInfo?.adminNode ?? '' })
					: t('agent.shareNotAsked', { node: clusterInfo?.adminNode ?? '' })}
			</p>
		</div>
	{:else}
		<label class="set-row">
			<span class="set-label"
				>{t('agent.ollamaServer')}<span class="set-hint">{t('agent.ollamaHint')}</span></span
			>
			<span class="set-control">
				<input class="field" bind:value={settings.ollama} placeholder="http://192.168.1.5:11434" />
			</span>
		</label>
	{/if}
	{#if check}
		<div class="set-row one">
			<p class={check.reachable ? 'notice-ok' : 'notice-warn'}>
				{check.reachable
					? t('agent.checkOk', { version: check.version ?? '', models: check.models ?? 0 })
					: t('agent.checkFail', { error: check.error ?? '' })}
			</p>
		</div>
	{:else if status?.enabled}
		<div class="set-row one">
			<p class="set-hint state">
				<span class="dot {status.reachable ? 'ok' : 'error'}"></span>
				{status.reachable
					? t('agent.connected', {
							version: status.version ?? '',
							models: status.models?.length ?? 0,
						})
					: t('agent.notConnected', { error: status.error ?? '' })}
			</p>
		</div>
	{/if}
	{#if status && !status.volume}<div class="set-row one">
			<p class="notice-warn">{t('agent.noVolume')}</p>
		</div>{/if}
	{#if settings.source === 'cluster'}
		{#if shareState !== 'allowed'}
			<div class="card-foot">
				<button type="button" class="btn" onclick={askAdmin}
					>{t('agent.shareAsk', { node: clusterInfo?.adminNode ?? '' })}</button
				>
			</div>
		{/if}
	{:else}
		<div class="card-foot">
			<button type="button" class="btn" onclick={test}>{t('agent.check')}</button>
		</div>
	{/if}
</section>

{#if clusterInfo?.admin}
	<section class="tile ui-card" aria-labelledby="ag-nodes">
		<div class="card-head"><h2 id="ag-nodes">{t('agent.sectionNodes')}</h2></div>
		<div class="set-row one"><span class="set-hint">{t('agent.nodesIntro')}</span></div>
		{#each settings.shared ?? [] as sh (sh.node)}
			<div class="set-row">
				<span class="set-label"
					><strong>{sh.name}</strong><span class="set-hint"
						>{t('agent.nodeAsked', { date: new Date(sh.asked).toLocaleString() })}</span
					></span
				>
				<div class="set-control node-tools">
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
				</div>
			</div>
		{:else}
			<div class="set-row one"><span class="set-hint">{t('agent.nodesNone')}</span></div>
		{/each}
	</section>
{/if}

<section class="tile ui-card" aria-labelledby="ag-model">
	<div class="card-head"><h2 id="ag-model">{t('agent.sectionModel')}</h2></div>
	<label class="set-row">
		<span class="set-label"
			>{t('agent.model')}
			{#if chosen}
				<span class="set-hint caps">
					{t('agent.can')}
					{#each ['tools', 'thinking', 'vision'] as cap (cap)}
						<span class="chip {chosen.capabilities.includes(cap) ? 'ok' : ''}"
							>{chosen.capabilities.includes(cap) ? '✓' : '–'}
							{t(`agent.cap.${cap}` as MessageKey)}</span
						>
					{/each}
				</span>
			{/if}
			{#if others.length}<span class="set-hint"
					>{t('agent.modelsWithoutTools', {
						names: others.map((m) => m.name).join(', '),
					})}</span
				>{/if}</span
		>
		<span class="set-control">
			<select class="field" bind:value={settings.model}>
				<option value="">{t('agent.modelChoose')}</option>
				{#each models as m (m.name)}<option value={m.name}>{m.name} ({gb(m.size)})</option>{/each}
				{#if settings.model && !models.some((m) => m.name === settings!.model)}<option
						value={settings.model}>{settings.model}</option
					>{/if}
			</select>
		</span>
	</label>
	{#if chosen && chosen.levels.length}
		<div class="set-row">
			<span class="set-label"
				>{t('agent.thinkDefault')}<span class="set-hint">{t('agent.thinkHint')}</span></span
			>
			<div class="set-control">
				<Segments
					compact
					label={t('agent.thinkDefault')}
					value={settings.think || chosen.defaultLevel || chosen.levels[0]}
					options={chosen.levels.map((l) => ({ value: l, label: levelLabel(l, i18n.locale) }))}
					onchange={(v) => (settings!.think = v)}
				/>
			</div>
		</div>
	{/if}
	<label class="set-row">
		<span class="set-label"
			>{t('agent.context')}<span class="set-hint">
				{#if modelContext}{t('agent.contextModel', { tokens: tokens(modelContext) })}{/if}
				{#if runningContext}{t('agent.contextNow', {
						tokens: tokens(runningContext),
					})}{:else}{t('agent.contextUnknown')}{/if}
				{t('agent.contextHint')}
				{#if (settings.context ?? 0) > 65536}{t('agent.contextVram')}{/if}
			</span></span
		>
		<span class="set-control">
			<select class="field" bind:value={settings.context}>
				{#each contexts as c (c)}<option value={c}
						>{c === 0 ? t('agent.contextOllama') : tokens(c) + ' ' + t('agent.tokensWord')}</option
					>{/each}
			</select>
		</span>
	</label>
	<label class="set-row">
		<span class="set-label"
			>{t('agent.maxSteps')}<span class="set-hint">{t('agent.maxStepsHint')}</span></span
		>
		<span class="set-control">
			<input class="field narrow" type="number" min="1" max="100" bind:value={settings.maxSteps} />
		</span>
	</label>
	<label class="set-row">
		<span class="set-label"
			>{t('agent.maxMinutes')}<span class="set-hint">{t('agent.maxMinutesHint')}</span></span
		>
		<span class="set-control">
			<input
				class="field narrow"
				type="number"
				min="1"
				max="600"
				bind:value={settings.maxMinutes}
			/>
		</span>
	</label>
	<label class="set-row">
		<span class="set-label"
			>{t('agent.lazyTools')}<span class="set-hint">{t('agent.lazyToolsHint')}</span></span
		>
		<span class="set-control end"
			><input
				type="checkbox"
				class="switch"
				role="switch"
				checked={!settings.allTools}
				onchange={(e) => (settings!.allTools = !e.currentTarget.checked)}
			/></span
		>
	</label>
	<label class="set-row">
		<span class="set-label"
			>{t('agent.delegate')}<span class="set-hint">{t('agent.delegateHint')}</span></span
		>
		<span class="set-control end"
			><input
				type="checkbox"
				class="switch"
				role="switch"
				checked={!settings.noDelegate}
				onchange={(e) => (settings!.noDelegate = !e.currentTarget.checked)}
			/></span
		>
	</label>
	<label class="set-row">
		<span class="set-label"
			>{t('agent.keepAlive')}<span class="set-hint">{t('agent.keepAliveHint')}</span></span
		>
		<span class="set-control">
			<select class="field" bind:value={settings.keepAlive}>
				<option value="">{t('agent.keepAliveOllama')}</option>
				{#each ['5m', '30m', '2h', '-1'] as k (k)}<option value={k}
						>{t(`agent.keepAlive.${k}` as MessageKey)}</option
					>{/each}
			</select>
		</span>
	</label>
	{#if settings.source !== 'cluster'}<div class="set-row">
			<span class="set-label"
				><label for="ag-pull">{t('agent.pull')}</label><span class="set-hint"
					>{t('agent.pullHint')}</span
				>
				{#if pull?.model}
					<span class="set-hint" role="status"
						>{pull.model}: {pull.error ? pull.error : pull.status}{pull.total
							? ` · ${Math.round(((pull.completed ?? 0) / pull.total) * 100)} %`
							: ''}</span
					>
				{/if}</span
			>
			<div class="set-control">
				<input
					id="ag-pull"
					class="field grow"
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
			</div>
		</div>{/if}
</section>

<section class="tile ui-card" aria-labelledby="ag-media">
	<div class="card-head"><h2 id="ag-media">{t('agent.sectionMedia')}</h2></div>
	<div class="set-row one">
		<span class="set-label"
			>{t('agent.pictures')}
			{#if chosen?.capabilities.includes('vision')}
				<span class="set-hint">✓ {t('agent.picturesMain', { model: chosen.name })}</span>
			{:else}
				<span class="set-hint">{t('agent.picturesNone')}</span>
			{/if}
			{#if vram}<span class="set-hint"
					>{t('agent.vramNow', {
						gb: (vram / 1e9).toFixed(1).replace('.', i18n.locale === 'de' ? ',' : '.'),
					})}</span
				>{/if}
		</span>
	</div>
	<label class="set-row">
		<span class="set-label">{t('agent.visionOwn')}</span>
		<span class="set-control end"
			><input
				type="checkbox"
				class="switch"
				role="switch"
				checked={!!settings.vision?.enabled}
				onchange={(e) =>
					(settings!.vision = {
						...(settings!.vision ?? {}),
						enabled: e.currentTarget.checked,
					})}
			/></span
		>
	</label>
	{#if settings.vision?.enabled}
		<div class="set-row">
			<span class="set-label"
				><label for="ag-vision">{t('agent.visionModel')}</label><span class="set-hint"
					>{t('agent.visionHint')}</span
				></span
			>
			<div class="set-control">
				<select
					id="ag-vision"
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
			</div>
		</div>
	{/if}
	<label class="set-row">
		<span class="set-label"
			>{t('agent.voiceOn')}<span class="set-hint">{t('agent.voiceHint')}</span></span
		>
		<span class="set-control end"
			><input
				type="checkbox"
				class="switch"
				role="switch"
				checked={!!settings.voice?.enabled}
				onchange={(e) =>
					(settings!.voice = {
						...(settings!.voice ?? {}),
						enabled: e.currentTarget.checked,
					})}
			/></span
		>
	</label>
	{#if !settings.voice?.url}
		<div class="set-row" id="voice">
			<span class="set-label"
				>{t('agent.voice')}{#if !whisperApp}<span class="set-hint"
						>{t('agent.whisperSetupHint')}</span
					>{/if}</span
			>
			<div class="set-control">
				{#if whisperApp}
					<button type="button" class="btn btn-primary" onclick={useWhisperApp}
						>{t('agent.whisperUse', { name: whisperApp.name, port: whisperApp.port })}</button
					>
				{:else}
					<a class="btn btn-primary" href="/apps/install/titanos/whisper"
						>{t('agent.whisperSetup')}</a
					>
				{/if}
			</div>
		</div>
	{/if}
	{#if settings.voice?.enabled}
		<div class="set-row">
			<span class="set-label"><label for="ag-voice-url">{t('agent.voiceUrl')}</label></span>
			<div class="set-control">
				<input
					id="ag-voice-url"
					class="field grow"
					value={settings.voice?.url ?? ''}
					placeholder="http://192.168.1.5:8000"
					aria-label={t('agent.voiceUrl')}
					oninput={(e) =>
						(settings!.voice = { ...(settings!.voice ?? {}), url: e.currentTarget.value })}
				/>
				<button type="button" class="btn" onclick={checkVoice}>{t('agent.voiceCheck')}</button>
			</div>
		</div>
		<div class="set-row">
			<span class="set-label"><label for="ag-voice-model">{t('agent.voiceModel')}</label></span>
			<div class="set-control">
				<input
					id="ag-voice-model"
					class="field"
					value={settings.voice?.model ?? ''}
					placeholder="Systran/faster-whisper-large-v3"
					aria-label={t('agent.voiceModel')}
					oninput={(e) =>
						(settings!.voice = { ...(settings!.voice ?? {}), model: e.currentTarget.value })}
				/>
			</div>
		</div>
		{#if voiceCheck}
			<div class="set-row one">
				<p class={voiceCheck.reachable ? 'notice-ok' : 'notice-error'} role="status">
					{voiceCheck.reachable
						? t('agent.voiceOk', { models: (voiceCheck.models ?? []).join(', ') || '–' })
						: t('agent.voiceDown', { error: voiceCheck.error ?? '' })}
				</p>
			</div>
		{/if}
	{/if}
	<label class="set-row">
		<span class="set-label"
			>{t('agent.snapshots')}<span class="set-hint">{t('agent.snapshotsHint')}</span></span
		>
		<span class="set-control end"
			><input
				type="checkbox"
				class="switch"
				role="switch"
				checked={!settings.noSnapshots}
				onchange={(e) => (settings!.noSnapshots = !e.currentTarget.checked)}
			/></span
		>
	</label>
	<label class="set-row">
		<span class="set-label"
			>{t('agent.code')}<span class="set-hint">{t('agent.codeHint')}</span></span
		>
		<span class="set-control end"
			><input
				type="checkbox"
				class="switch"
				role="switch"
				checked={!!settings.code?.enabled}
				onchange={(e) =>
					(settings!.code = { ...(settings!.code ?? {}), enabled: e.currentTarget.checked })}
			/></span
		>
	</label>
	{#if settings.code?.enabled}
		<label class="set-row sub">
			<span class="set-label">{t('agent.codeTimeout')}</span>
			<span class="set-control">
				<input
					class="field narrow"
					type="number"
					min="1"
					max="600"
					value={settings.code.timeout ?? 60}
					oninput={(e) =>
						(settings!.code = { ...settings!.code, timeout: Number(e.currentTarget.value) })}
				/>
			</span>
		</label>
		<label class="set-row sub">
			<span class="set-label">{t('agent.codeMemory')}</span>
			<span class="set-control">
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
			</span>
		</label>
	{/if}
</section>

<style>
	/* PLAN 68: every tab in cards with rows "label (+ hint) left, control
	   right"; lists as rows; the card's own actions in its foot. */
	.set-row.one {
		grid-template-columns: minmax(0, 1fr);
	}
	.set-row.one > p {
		margin: 0;
	}
	/* The hint on the card's surface: a little toward the text for 4.5:1. */
	.set-hint {
		color: color-mix(in srgb, var(--muted) 85%, var(--text));
	}
	.set-control > .field {
		flex: 1 1 12rem;
		min-width: 0;
	}
	.set-control > .field.grow {
		flex: 1 1 14rem;
	}
	.set-control > .field.narrow,
	.narrow {
		flex: none;
		width: 100px;
	}
	.set-control .btn {
		max-width: 100%;
		white-space: normal;
		height: auto;
	}
	.set-row.sub {
		padding-left: 40px;
	}
	.node-tools {
		justify-content: flex-end;
	}
	.state {
		margin: 0;
	}
	.caps {
		display: flex;
		gap: var(--space-1-5);
		align-items: center;
		flex-wrap: wrap;
	}

	/* The phone: long choices on a line of their own. */
	@media (max-width: 639px) {
		.set-row.sub {
			padding-left: var(--space-4);
		}

		.node-tools {
			justify-content: flex-start;
		}
	}
</style>
