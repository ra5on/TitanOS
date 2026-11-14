<script lang="ts">
	// Settings › Sprache (PLAN 92, draft Entwuerfe/92-sprache): speech to
	// text (faster-whisper) and text to speech (Piper) on this NAS, for Home
	// Assistant (Wyoming protocol) and the KI-Agent. The switch, the GPU or
	// the processor, the volume of the models, the models and voices to
	// download, make active and remove, a sample of a voice, trying the
	// microphone, the addresses for Home Assistant and who may reach them.
	import PageHead from '#lib/components/PageHead.svelte';
	import Dialog from '#lib/components/Dialog.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import Segments from '#lib/components/Segments.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import { api, apiFetch, responseError } from '#lib/api.ts';
	import { formatBytes, formatDateTime } from '#lib/format.ts';
	import { apiErrorText, i18n, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { record, toWav16k } from '#lib/speech-audio.ts';

	type Settings = {
		enabled: boolean;
		volume: string;
		device: string;
		whisperModel: string;
		language: string;
		voice: string;
		speed: number;
		reach: 'ha' | 'lan';
		agent: boolean;
	};
	type Service = {
		phase: 'off' | 'model' | 'pulling' | 'starting' | 'running' | 'error';
		message?: string;
		pullDone?: number;
		pullTotal?: number;
		port: number;
		listening: boolean;
		portError?: string;
		seen?: { at: string; from: string; ok: boolean };
		version: string;
	};
	type Job = { done: number; total: number; error?: string };
	type Page = {
		settings: Settings;
		status: { whisper: Service; piper: Service; available: boolean };
		jobs: Record<string, Job>;
		devices: {
			id: string;
			title: string;
			available: boolean;
			reason?: string;
			memoryUsedBytes?: number;
			memoryTotalBytes?: number;
		}[];
		models: {
			id: string;
			bytes: number;
			vramBytes?: number;
			recommended?: boolean;
			installed: boolean;
		}[];
		voices: {
			id: string;
			name: string;
			language: string;
			languageName: string;
			country: string;
			quality: string;
			speakers: number;
			bytes: number;
			installed: boolean;
		}[];
		voicesError?: string;
		volumes: string[];
		hosts: { kind: string; host: string }[];
	};

	let data = $state<Page | null>(null);
	let error = $state('');
	let busy = $state(false);
	let removing = $state<{ kind: 'model' | 'voice'; id: string; title: string } | null>(null);
	let voiceLanguage = $state(i18n.locale === 'de' ? 'de' : 'en');
	let heard = $state('');
	let recorder = $state<{ stop: () => Promise<Blob> } | null>(null);
	let hearing = $state(false);
	let sampling = $state('');
	let copied = $state('');
	let player: HTMLAudioElement | null = null;

	async function load() {
		try {
			data = await api.get<Page>('/speech');
		} catch (e) {
			error = apiErrorText(e);
		}
	}
	$effect(() => {
		void load();
	});
	// While something moves (a download, an image, a start) the page follows.
	const moving = $derived(
		!!data &&
			(Object.values(data.jobs).some((j) => !j.error) ||
				['pulling', 'starting'].includes(data.status.whisper.phase) ||
				['pulling', 'starting'].includes(data.status.piper.phase)),
	);
	$effect(() => {
		const timer = setInterval(load, moving ? 2000 : 15000);
		return () => clearInterval(timer);
	});

	async function save(change: Partial<Settings>) {
		if (!data) return;
		busy = true;
		error = '';
		try {
			data.settings = await api.put<Settings>('/speech/settings', { ...data.settings, ...change });
			await load();
		} catch (e) {
			error = apiErrorText(e);
			await load();
		} finally {
			busy = false;
		}
	}
	async function act(fn: () => Promise<unknown>) {
		error = '';
		try {
			await fn();
		} catch (e) {
			error = apiErrorText(e);
		}
		await load();
	}
	const download = (kind: 'model' | 'voice', id: string) =>
		act(() => api.post('/speech/download', { kind, id }));
	const cancel = (kind: 'model' | 'voice', id: string) =>
		act(async () => {
			const res = await apiFetch('/speech/download', {
				method: 'DELETE',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ kind, id }),
			});
			if (!res.ok) throw await responseError(res);
		});

	// ----- a voice's sample, trying the microphone -----
	async function sample(voice: string) {
		player?.pause();
		if (sampling === voice) return (sampling = '');
		sampling = voice;
		error = '';
		try {
			const res = await apiFetch('/speech/say', {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({
					text: t(voice.startsWith('de') ? 'speech.sampleDe' : 'speech.sampleEn'),
					voice,
				}),
			});
			if (!res.ok) throw await responseError(res);
			const audio = new Audio(URL.createObjectURL(await res.blob()));
			player = audio;
			audio.onended = audio.onerror = () => {
				URL.revokeObjectURL(audio.src);
				if (sampling === voice) sampling = '';
			};
			await audio.play();
		} catch (e) {
			error = apiErrorText(e);
			sampling = '';
		}
	}
	async function listen() {
		error = '';
		if (!recorder) {
			try {
				heard = '';
				recorder = await record();
			} catch {
				error = t('agent.voiceNoMic');
			}
			return;
		}
		const stop = recorder.stop;
		recorder = null;
		hearing = true;
		try {
			const wav = await toWav16k(await stop());
			const res = await apiFetch('/speech/hear', {
				method: 'POST',
				headers: { 'Content-Type': 'audio/wav' },
				body: wav,
			});
			if (!res.ok) throw await responseError(res);
			heard = ((await res.json()) as { text: string }).text || t('speech.heardNothing');
		} catch (e) {
			error = apiErrorText(e);
		} finally {
			hearing = false;
		}
	}
	async function copy(text: string) {
		try {
			await navigator.clipboard.writeText(text);
		} catch {
			const area = document.createElement('textarea');
			area.value = text;
			area.style.position = 'fixed';
			area.style.opacity = '0';
			document.body.appendChild(area);
			area.select();
			document.execCommand('copy');
			area.remove();
		}
		copied = text;
		setTimeout(() => copied === text && (copied = ''), 1800);
	}

	// ----- what the rows say -----
	const phaseText = (s: Service) =>
		s.phase === 'error'
			? ['volume', 'docker', 'device'].includes(s.message ?? '')
				? t(`speech.error.${s.message}` as MessageKey)
				: t('speech.phase.error')
			: s.phase === 'pulling' && s.pullTotal
				? t('speech.phase.pullingOf', {
						done: formatBytes(s.pullDone ?? 0),
						total: formatBytes(s.pullTotal),
					})
				: t(`speech.phase.${s.phase}` as MessageKey);
	const phaseTone = (s: Service) =>
		s.phase === 'running' ? 'ok' : s.phase === 'error' ? 'error' : s.phase === 'off' ? '' : 'info';
	const languages = $derived(
		[
			...new Map(
				(data?.voices ?? []).map((v) => [v.language.slice(0, 2), v.languageName]),
			).entries(),
		]
			.filter(([, name]) => name)
			.sort((a, b) => a[1].localeCompare(b[1])),
	);
	const shownVoices = $derived(
		(data?.voices ?? []).filter(
			(v) => v.installed || v.language.startsWith(voiceLanguage) || !v.language,
		),
	);
	const sttLanguages = ['auto', 'de', 'en', 'fr', 'es', 'it', 'nl', 'pl', 'pt', 'tr', 'ru', 'uk'];
	const languageName = (code: string) =>
		code === 'auto'
			? t('speech.languageAuto')
			: (new Intl.DisplayNames([i18n.locale], { type: 'language' }).of(code) ?? code);
	const deviceText = (d: Page['devices'][number]) => {
		if (!d.available) return t(d.reason === 'vm' ? 'speech.deviceVM' : 'speech.deviceDriver');
		if (d.memoryTotalBytes && d.memoryUsedBytes !== undefined)
			return t('speech.deviceMemory', {
				used: formatBytes(d.memoryUsedBytes),
				free: formatBytes(Math.max(0, d.memoryTotalBytes - d.memoryUsedBytes)),
				total: formatBytes(d.memoryTotalBytes),
			});
		return t('speech.deviceReady');
	};
	const seenText = (s: Service) =>
		s.portError
			? t('speech.portBusy', { port: s.port })
			: !s.seen
				? t('speech.seenNever')
				: t(s.seen.ok ? 'speech.seenOk' : 'speech.seenRefused', {
						when: formatDateTime(new Date(s.seen.at)),
						from: s.seen.from,
					});
	const qualityText = (q: string) => t(`speech.quality.${q}` as MessageKey);
</script>

{#snippet item(
	kind: 'model' | 'voice',
	id: string,
	title: string,
	installed: boolean,
	active: boolean,
)}
	{@const job = data?.jobs[`${kind}/${id}`]}
	{#if job && !job.error}
		<progress
			max={job.total || 1}
			value={job.done}
			aria-label={t('speech.downloading', { name: title })}
		></progress>
		<span class="pct">{job.total ? Math.floor((100 * job.done) / job.total) : 0} %</span>
		<button type="button" class="btn" onclick={() => cancel(kind, id)}>{t('common.cancel')}</button>
	{:else if active}
		<span class="chip ok">{t('speech.active')}</span>
	{:else if installed}
		<button
			type="button"
			class="btn"
			disabled={busy}
			onclick={() => save(kind === 'model' ? { whisperModel: id } : { voice: id })}
			>{t('speech.activate')}</button
		>
		<button
			type="button"
			class="btn icon"
			aria-label={t('speech.removeName', { name: title })}
			title={t('common.delete')}
			onclick={() => (removing = { kind, id, title })}><Icon name="trash" size={16} /></button
		>
	{:else}
		{#if job?.error}<span class="chip error" title={job.error}>{t('speech.downloadFailed')}</span
			>{/if}
		<button
			type="button"
			class="btn"
			disabled={!data?.settings.volume}
			onclick={() => download(kind, id)}
			><Icon name="download" size={15} />{job?.error
				? t('common.retry')
				: t('speech.download')}</button
		>
	{/if}
{/snippet}

<PageHead title={t('speech.title')}>
	<p class="muted">{t('speech.intro')}</p>
</PageHead>

{#if error}<div class="notice-error" role="alert">{error}</div>{/if}

{#if data}
	{@const s = data.settings}
	{#if !data.status.available}
		<p class="notice-warn">{t('speech.unavailable')}</p>
	{/if}

	<SettingGroup title={t('speech.service')}>
		<SettingRow
			icon="mic"
			title={t('speech.enable')}
			desc={s.volume ? t('speech.enableDesc') : t('speech.enableNoVolume')}
			for="speech-on"
		>
			{#snippet control()}
				{#if s.enabled}
					<span class="chip {phaseTone(data!.status.whisper)}"
						>{t('speech.stt')}: {phaseText(data!.status.whisper)}</span
					>
					<span class="chip {phaseTone(data!.status.piper)}"
						>{t('speech.tts')}: {phaseText(data!.status.piper)}</span
					>
				{/if}
				<input
					id="speech-on"
					type="checkbox"
					class="switch"
					role="switch"
					checked={s.enabled}
					disabled={busy || (!s.enabled && !s.volume)}
					onchange={(e) => save({ enabled: e.currentTarget.checked })}
				/>
			{/snippet}
		</SettingRow>
		{#each [data.status.whisper, data.status.piper] as svc, n (n)}
			{#if s.enabled && svc.phase === 'error' && !['volume', 'docker', 'device'].includes(svc.message ?? '')}
				<div class="srow">
					<div class="srow-head">
						<span class="srow-ico"><Icon name="info" size={19} /></span>
						<div class="srow-text">
							<span class="srow-title"
								>{t('speech.errorOf', {
									service: n === 0 ? t('speech.stt') : t('speech.tts'),
								})}</span
							>
							<pre class="log">{svc.message}</pre>
						</div>
					</div>
				</div>
			{/if}
		{/each}
		<SettingRow
			id="device"
			icon="chip"
			title={t('speech.device')}
			desc={t('speech.deviceDesc')}
			open
		>
			<div class="srow-block">
				<div class="choices" role="radiogroup" aria-label={t('speech.device')}>
					{#each data.devices as d (d.id)}
						<label class="choice" class:off={!d.available}>
							<input
								type="radio"
								name="speech-device"
								value={d.id}
								checked={s.device === d.id}
								disabled={busy || !d.available}
								onchange={() => save({ device: d.id })}
							/>
							<span class="adr"><strong>{d.title}</strong><small>{deviceText(d)}</small></span>
						</label>
					{/each}
					<label class="choice">
						<input
							type="radio"
							name="speech-device"
							value="cpu"
							checked={s.device === 'cpu'}
							disabled={busy}
							onchange={() => save({ device: 'cpu' })}
						/>
						<span class="adr"
							><strong>{t('speech.cpu')}</strong><small
								>{data.devices.length ? t('speech.cpuDesc') : t('speech.cpuOnly')}</small
							></span
						>
					</label>
				</div>
				<p class="muted note">{t('speech.deviceNote')}</p>
			</div>
		</SettingRow>
		<SettingRow
			icon="disk"
			title={t('speech.volume')}
			desc={t('speech.volumeDesc')}
			for="speech-volume"
		>
			{#snippet control()}
				<select
					id="speech-volume"
					class="field"
					value={s.volume}
					disabled={busy || s.enabled}
					title={s.enabled ? t('speech.volumeLocked') : undefined}
					onchange={(e) => save({ volume: e.currentTarget.value, whisperModel: '', voice: '' })}
				>
					{#if !s.volume}<option value="">{t('speech.volumeChoose')}</option>{/if}
					{#each data!.volumes as v (v)}<option value={v}>{v}</option>{/each}
					{#if s.volume && !data!.volumes.includes(s.volume)}<option value={s.volume}
							>{s.volume}</option
						>{/if}
				</select>
			{/snippet}
		</SettingRow>
	</SettingGroup>

	<SettingGroup title={t('speech.sttTitle')} desc={t('speech.sttDesc')}>
		{#each data.models as m (m.id)}
			<SettingRow
				icon={s.whisperModel === m.id ? 'check' : 'box'}
				title={m.id}
				desc={`${t(`speech.model.${m.id}` as MessageKey)} · ${formatBytes(m.bytes)}${m.vramBytes ? ' · ' + t('speech.vram', { size: formatBytes(m.vramBytes) }) : ''}`}
			>
				{#snippet badge()}{#if m.recommended}<span class="chip info">{t('speech.recommended')}</span
						>{/if}{/snippet}
				{#snippet control()}{@render item(
						'model',
						m.id,
						m.id,
						m.installed,
						s.whisperModel === m.id,
					)}{/snippet}
			</SettingRow>
		{/each}
		<SettingRow
			icon="globe"
			title={t('speech.language')}
			desc={t('speech.languageDesc')}
			for="speech-lang"
		>
			{#snippet control()}
				<select
					id="speech-lang"
					class="field"
					value={s.language}
					disabled={busy}
					onchange={(e) => save({ language: e.currentTarget.value })}
				>
					{#each sttLanguages as code (code)}<option value={code}>{languageName(code)}</option
						>{/each}
				</select>
			{/snippet}
		</SettingRow>
		<SettingRow
			icon="play"
			title={t('speech.try')}
			desc={heard ? `„${heard}“` : t('speech.tryDesc')}
		>
			{#snippet control()}
				<button
					type="button"
					class="btn"
					disabled={hearing || data!.status.whisper.phase !== 'running'}
					title={data!.status.whisper.phase !== 'running' ? t('speech.needsRunning') : undefined}
					onclick={listen}
					><Icon name={recorder ? 'stop' : 'mic'} size={15} />{hearing
						? t('speech.hearing')
						: recorder
							? t('speech.stop')
							: t('speech.record')}</button
				>
			{/snippet}
		</SettingRow>
	</SettingGroup>

	<SettingGroup title={t('speech.ttsTitle')} desc={t('speech.ttsDesc')}>
		{#snippet tools()}
			{#if languages.length}
				<select class="field" bind:value={voiceLanguage} aria-label={t('speech.voiceLanguage')}>
					{#each languages as [code, name] (code)}<option value={code}>{name}</option>{/each}
				</select>
			{/if}
		{/snippet}
		{#if data.voicesError}
			<p class="notice-warn">{t('speech.voicesError')}</p>
		{/if}
		{#each shownVoices as v (v.id)}
			<SettingRow
				icon={s.voice === v.id ? 'check' : 'volume'}
				title={v.quality ? `${v.name} (${qualityText(v.quality)})` : v.id}
				desc={v.languageName
					? `${v.languageName}${v.country ? ` (${v.country})` : ''}${v.speakers > 1 ? ' · ' + t('speech.speakers', { count: v.speakers }) : ''} · ${formatBytes(v.bytes)}`
					: v.id}
			>
				{#snippet control()}
					{#if v.installed}
						<button
							type="button"
							class="btn icon"
							aria-pressed={sampling === v.id}
							aria-label={t('speech.sample', { name: v.name })}
							title={data!.status.piper.phase !== 'running'
								? t('speech.needsRunning')
								: t('speech.sample', { name: v.name })}
							disabled={data!.status.piper.phase !== 'running'}
							onclick={() => sample(v.id)}
							><Icon name={sampling === v.id ? 'stop' : 'play'} size={16} /></button
						>
					{/if}
					{@render item('voice', v.id, v.name, v.installed, s.voice === v.id)}
				{/snippet}
			</SettingRow>
		{:else}
			<p class="muted">{t('speech.noVoices')}</p>
		{/each}
		<SettingRow
			icon="activity"
			title={t('speech.speed')}
			desc={t('speech.speedDesc')}
			for="speech-speed"
		>
			{#snippet control()}
				<input
					id="speech-speed"
					type="range"
					min="0.5"
					max="2"
					step="0.1"
					value={s.speed}
					disabled={busy}
					aria-valuetext={`${s.speed.toFixed(1)}×`}
					onchange={(e) => save({ speed: Number(e.currentTarget.value) })}
				/>
				<span class="pct">{s.speed.toFixed(1).replace('.', ',')}×</span>
			{/snippet}
		</SettingRow>
	</SettingGroup>

	<SettingGroup title={t('speech.use')}>
		<SettingRow id="ha" icon="home" title={t('speech.ha')} desc={t('speech.haDesc')} open>
			{#each [{ svc: data.status.whisper, title: t('speech.haStep1'), hint: t('speech.haStep1Hint') }, { svc: data.status.piper, title: t('speech.haStep2'), hint: t('speech.haStep2Hint') }] as step (step.svc.port)}
				<div class="srow-sub">
					<span class="adr">
						<strong>{step.title}</strong>
						<small>{step.hint}</small>
						{#each data.hosts.filter((h) => h.kind === 'lan') as h (h.host)}
							<span class="kv"
								><span>{t('speech.host')}</span><code>{h.host}</code><span>{t('speech.port')}</span
								><code>{step.svc.port}</code></span
							>
						{/each}
						<small>{seenText(step.svc)}</small>
					</span>
					{#each data.hosts.filter((h) => h.kind === 'lan') as h (h.host)}
						<button type="button" class="btn" onclick={() => copy(h.host)}
							><Icon name={copied === h.host ? 'check' : 'copy'} size={15} />{copied === h.host
								? t('agent.copiedShort')
								: t('speech.copyHost')}</button
						>
					{/each}
				</div>
			{/each}
			<div class="srow-sub">
				<span class="adr"
					><strong>{t('speech.haStep3')}</strong><small>{t('speech.haStep3Hint')}</small></span
				>
			</div>
			<div class="srow-sub">
				<span class="adr"
					><strong>{t('speech.otherHosts')}</strong><small
						>{t(
							'speech.otherHostsApp',
						)}{#each data.hosts.filter((h) => h.kind === 'tailscale') as h (h.host)}
							· {t('speech.otherHostsTailscale')} <code>{h.host}</code>{/each}</small
					></span
				>
			</div>
			<div class="srow-sub">
				<span class="adr"
					><strong>{t('speech.reach')}</strong><small>{t('speech.reachDesc')}</small></span
				>
				<Segments
					compact
					label={t('speech.reach')}
					value={s.reach}
					options={[
						{ value: 'ha', label: t('speech.reachHA') },
						{ value: 'lan', label: t('speech.reachLAN') },
					]}
					onchange={(v) => save({ reach: v })}
				/>
			</div>
		</SettingRow>
		<SettingRow
			icon="sparkle"
			title={t('speech.agent')}
			desc={t('speech.agentDesc')}
			for="speech-agent"
		>
			{#snippet control()}
				<input
					id="speech-agent"
					type="checkbox"
					class="switch"
					role="switch"
					checked={s.agent}
					disabled={busy}
					onchange={(e) => save({ agent: e.currentTarget.checked })}
				/>
			{/snippet}
		</SettingRow>
	</SettingGroup>
{:else if !error}
	<p class="muted">{t('agent.loading')}</p>
{/if}

{#if removing}
	{@const r = removing}
	<Dialog title={t('speech.removeTitle')} onclose={() => (removing = null)}>
		<p>{t('speech.removeText', { name: r.title })}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (removing = null)}
				>{t('common.cancel')}</button
			>
			<button
				type="button"
				class="btn btn-danger"
				onclick={() => {
					const path = `/speech/${r.kind}/${encodeURIComponent(r.id)}`;
					removing = null;
					void act(() => api.delete(path));
				}}>{t('common.delete')}</button
			>
		</div>
	</Dialog>
{/if}

<style>
	.btn {
		gap: var(--space-1-5);
	}
	.btn.icon {
		width: 36px;
		padding: 0;
		justify-content: center;
	}
	progress {
		width: 120px;
		accent-color: var(--accent);
	}
	.pct {
		min-width: 3.2rem;
		text-align: right;
	}
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
	.adr small,
	.note {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.kv {
		display: flex;
		align-items: center;
		flex-wrap: wrap;
		gap: var(--space-1-5) var(--space-2);
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.choices {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}
	.choice {
		display: flex;
		align-items: flex-start;
		gap: var(--space-2-5);
		min-height: 44px;
		cursor: pointer;
	}
	.choice.off {
		cursor: default;
	}
	.choice input {
		margin-top: var(--space-1);
	}
	.note {
		margin: var(--space-2) 0 0;
	}
	.log {
		margin: var(--space-1) 0 0;
		white-space: pre-wrap;
		overflow-wrap: anywhere;
		font-size: var(--text-sm);
		color: var(--muted);
	}
	@media (max-width: 760px) {
		.btn.icon {
			width: 44px;
			min-height: 44px;
		}
	}
</style>
