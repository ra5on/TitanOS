<script lang="ts">
	// Settings › KI-Agent (PLAN 44): the Ollama server, model, thinking level
	// and context as Ollama reports them, which accounts may use the agent,
	// named locations, what the agent may do (quick choice, areas, single
	// tools and folders with "Ohne Nachfrage / Nachfragen / Verboten"), its
	// own initiative and Home Assistant. Owners and administrators only.
	import PageHead from '#lib/components/PageHead.svelte';
	import Segments from '#lib/components/Segments.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import DesktopIcon from '#lib/desktop/DesktopIcon.svelte';
	import QrCode from '#lib/components/QrCode.svelte';
	import AgentVolume from '#lib/components/agent/AgentVolume.svelte';
	import FeaturesPanel from '#lib/components/agent/features/FeaturesPanel.svelte';
	import AccountsTab from './AccountsTab.svelte';
	import ConnectionTab from './ConnectionTab.svelte';
	import HomeAssistantTab from './HomeAssistantTab.svelte';
	import RightsTab from './RightsTab.svelte';
	import { inWindow } from '#lib/desktop/frame.ts';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { api, type DockerStatus, type FileRoot } from '#lib/api.ts';
	import { apiErrorText, i18n, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import {
		type Mode,
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
	let haNotify = $state('');
	let haRules = $state<{ key: string; mode: Mode }[]>([]);

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
			base = current();
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

	// PLAN 73: the save bar shows only when something was changed.
	let base = $state('');
	const current = () =>
		JSON.stringify([$state.snapshot(settings), haNotify, $state.snapshot(haRules)]);
	const dirty = $derived(!!settings && base !== '' && current() !== base);

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
			base = current();
			void api.get<Status>('/agent/status').then((s) => (status = s));
			void api.get<{ tools: ToolView[] }>('/agent/tools').then((r) => (tools = r.tools));
		} catch (e) {
			error = apiErrorText(e);
		} finally {
			saving = false;
		}
	}

	// PLAN 58: switching the agent on asks where its data goes, once.
	let choosingVolume = $state(false);
	function switchOn(box: HTMLInputElement) {
		if (!settings) return;
		if (box.checked && !settings.volume) {
			box.checked = false;
			choosingVolume = true;
			return;
		}
		settings.enabled = box.checked;
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

	const modeOptions = $derived<{ value: Mode; label: string }[]>([
		{ value: 'allow', label: t('agent.modeAllow') },
		{ value: 'ask', label: t('agent.modeAsk') },
		{ value: 'deny', label: t('agent.modeDeny') },
	]);
	// PLAN 68: areas that are rarely changed wait behind "weitere Bereiche" (RightsTab).
	let allAreas = $state(false);
	const admins = $derived(accounts.filter((a) => a.role === 'owner' || a.role === 'admin'));

	// PLAN 53: the page in tabs instead of one long column.
	// PLAN 73 (draft 06): the overview lists the areas with a summary each;
	// an area opens with "‹ Übersicht" above instead of a row of tabs.
	const TABS = [
		'overview',
		'connection',
		'rights',
		'accounts',
		'phone',
		'ha',
		'initiative',
		'instruction',
		'features',
	] as const;
	type Tab = (typeof TABS)[number];
	const tabOf = (v: string | null): Tab => (TABS.includes(v as Tab) ? (v as Tab) : 'overview');
	let tab = $state<Tab>(tabOf(page.url.searchParams.get('tab')));
	$effect(() => {
		tab = tabOf(page.url.searchParams.get('tab'));
	});
	function summary(id: Tab): string {
		if (!settings) return '';
		switch (id) {
			case 'connection':
				return settings.model || '–';
			case 'rights':
				return t(
					({
						read: 'agent.presetRead',
						standard: 'agent.presetStandard',
						all: 'agent.presetAll',
					}[settings.policy.preset as string] ?? 'agent.presetCustom') as MessageKey,
				);
			case 'accounts':
				return t('agent.summaryAccounts', { count: (settings.users ?? []).length });
			case 'phone':
				return appUrl.startsWith('https:') ? 'HTTPS' : appUrl ? 'HTTP' : '';
			case 'ha':
				return t(settings.homeassistant.assist ? 'agent.summaryOn' : 'agent.summaryOff');
			case 'instruction':
				return settings.instruction?.trim() ? t('agent.summarySet') : '';
			default:
				return '';
		}
	}
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

{#if inWindow}
	<!-- In the window the tabs come first, as in every program (PLAN 68). -->
	<PageHead title={t('agent.settingsTitle')} />
{:else}
	<PageHead title={t('agent.settingsTitle')}>
		<p class="muted">{t('agent.settingsIntro')}</p>
	</PageHead>
{/if}

{#if error}<div class="notice-error" role="alert">{error}</div>{/if}

{#if settings}
	{#if tab !== 'overview'}
		<nav class="back-row" aria-label={t('agent.settingsTitle')}>
			<button type="button" class="link" onclick={() => choose('overview')}
				>‹ {t('agent.tab.overview')}</button
			><span aria-hidden="true">·</span><strong>{t(`agent.tab.${tab}` as MessageKey)}</strong>
		</nav>
	{/if}
	{#if tab === 'features'}
		<!-- PLAN 68: one card per group of functions, each function a row. -->
		<FeaturesPanel admin={true} heading={t('agent.f.title')} />
	{:else if tab === 'overview'}
		<section class="tile ui-card" aria-labelledby="ag-hero">
			<div class="set-row">
				<div class="set-label hero-label">
					<DesktopIcon glyph="agent" gradient="violet" size={36} name={t('nav.agent')} />
					<span class="hero-text">
						<strong id="ag-hero"
							>{t(settings.enabled ? 'agent.heroStateOn' : 'agent.heroStateOff')}</strong
						>
						<span class="set-hint"
							>{status?.reachable
								? t('agent.heroOn', { version: status.version ?? '', model: settings.model || '–' })
								: settings.enabled
									? t('agent.heroDown')
									: t('agent.heroOff')}</span
						>
					</span>
				</div>
				<div class="set-control end">
					<label class="switch-label"
						><input
							aria-label={t('agent.enabled')}
							type="checkbox"
							class="switch"
							role="switch"
							checked={settings.enabled}
							onchange={(e) => switchOn(e.currentTarget)}
						/></label
					>
				</div>
			</div>
		</section>
		<AgentVolume
			current={settings.volume ?? ''}
			bind:choosing={choosingVolume}
			onchosen={(v) => {
				settings!.volume = v;
				settings!.enabled = true;
			}}
			onmoved={(v) => (settings!.volume = v)}
		/>
		<section class="tile ui-card areas" aria-labelledby="ag-areas">
			<div class="card-head"><h2 id="ag-areas">{t('agent.areasTitle')}</h2></div>
			{#each TABS.filter((x) => x !== 'overview') as id (id)}
				<button type="button" class="set-row area" onclick={() => choose(id)}>
					<span class="set-label"
						>{t(`agent.tab.${id}` as MessageKey)}<span class="set-hint"
							>{t(`agent.areaHint.${id}` as MessageKey)}</span
						></span
					>
					<span class="set-control end summary"
						>{summary(id)}<Icon name="chevronRight" size={16} /></span
					>
				</button>
			{/each}
		</section>
	{/if}
	{#if tab === 'phone'}
		<section class="tile ui-card" aria-labelledby="ag-phone">
			<div class="card-head"><h2 id="ag-phone">{t('agent.phoneApp')}</h2></div>
			<div class="set-row one">
				<span class="set-hint">{t('agent.phoneAppIntro')}</span>
			</div>
			<fieldset class="ways">
				<legend class="sr-only">{t('agent.phoneWay')}</legend>
				{#each addresses as a (a.url)}
					<label class="set-row way" class:on={way === a.url}
						><input type="radio" name="way" value={a.url} bind:group={way} /><span class="set-label"
							><strong>{t(`agent.way.${a.kind}` as MessageKey)}</strong><small class="set-hint mono"
								>{a.url}</small
							></span
						><span class="chip" class:ok={a.https}
							>{a.https ? '✓ ' + t('agent.wayHttps') : t('agent.wayHttp')}</span
						></label
					>
				{/each}
				<label class="set-row way" class:on={way === 'own'}
					><input type="radio" name="way" value="own" bind:group={way} /><span class="set-label"
						><strong>{t('agent.way.own')}</strong>
						{#if way === 'own'}<input
								class="field"
								bind:value={own}
								placeholder="https://nas.example.org"
								aria-label={t('agent.way.own')}
							/>{:else}<small class="set-hint">{t('agent.wayOwnHint')}</small>{/if}</span
					></label
				>
			</fieldset>
			<div class="set-row qr-row">
				<span class="set-label"
					>{t('agent.phoneQrTitle')}<span class="set-hint">{t('agent.phoneHttpsHint')}</span></span
				>
				<div class="set-control qr">
					{#if appUrl}
						<QrCode text={appUrl} label={t('agent.phoneQr', { url: appUrl })} size={180} />
						<small class="mono">{appUrl.replace(/^https?:\/\//, '')}</small>
					{:else}
						<p class="set-hint">{t('agent.phoneNoUrl')}</p>
					{/if}
				</div>
			</div>
			{#if appUrl}
				<div class="card-foot">
					<button type="button" class="btn" onclick={copyUrl}
						>{copied ? '✓ ' + t('agent.copied') : t('agent.copyLink')}</button
					>
				</div>
			{/if}
		</section>
	{/if}
	{#if tab === 'connection'}
		<ConnectionTab
			bind:settings={() => settings as Settings, (v) => (settings = v)}
			bind:pullName
			{status}
			{clusterInfo}
			{shareState}
			{check}
			{pull}
			{voiceCheck}
			{whisperApp}
			{askAdmin}
			{setShare}
			{startPull}
			{test}
			{checkVoice}
			{useWhisperApp}
		/>
	{/if}

	{#if tab === 'instruction'}
		<section class="tile ui-card" aria-labelledby="ag-instruction">
			<div class="card-head"><h2 id="ag-instruction">{t('agent.sectionInstruction')}</h2></div>
			<div class="set-row one">
				<span class="set-hint">{t('agent.instructionIntro')}</span>
			</div>
			<div class="card-body">
				<textarea
					class="field"
					rows="8"
					maxlength="8000"
					bind:value={settings.instruction}
					aria-labelledby="ag-instruction"></textarea>
			</div>
			<div class="card-foot">
				<span class="grow">{t('agent.instructionHint')}</span>
				{#if !settings.instruction?.trim()}<button
						type="button"
						class="btn"
						onclick={() => (settings!.instruction = t('agent.instructionTemplate'))}
						>{t('agent.instructionUseTemplate')}</button
					>{/if}
			</div>
		</section>
	{/if}

	{#if tab === 'accounts'}
		<AccountsTab
			bind:settings={() => settings as Settings, (v) => (settings = v)}
			{accounts}
			{roots}
		/>
	{/if}

	{#if tab === 'rights'}
		<RightsTab
			bind:settings={() => settings as Settings, (v) => (settings = v)}
			bind:allAreas
			bind:open
			{tools}
			{roots}
			{modeOptions}
		/>
	{/if}

	{#if tab === 'initiative'}
		<section class="tile ui-card" aria-labelledby="ag-react">
			<div class="card-head"><h2 id="ag-react">{t('agent.sectionInitiative')}</h2></div>
			<label class="set-row">
				<span class="set-label"
					>{t('agent.react')}<span class="set-hint">{t('agent.reactHint')}</span></span
				>
				<span class="set-control end"
					><input
						type="checkbox"
						class="switch"
						role="switch"
						bind:checked={settings.react}
					/></span
				>
			</label>
			<label class="set-row">
				<span class="set-label">{t('agent.weeklyReport')}</span>
				<span class="set-control end"
					><input
						type="checkbox"
						class="switch"
						role="switch"
						bind:checked={settings.weeklyReport}
					/></span
				>
			</label>
			<label class="set-row">
				<span class="set-label">{t('agent.reactAs')}</span>
				<span class="set-control">
					<select class="field" bind:value={settings.reactAs}>
						<option value="">{t('agent.firstOwner')}</option>
						{#each admins as a (a.name)}<option value={a.name}>{a.name}</option>{/each}
					</select>
				</span>
			</label>
		</section>
	{/if}

	{#if tab === 'ha'}
		<HomeAssistantTab
			bind:settings={() => settings as Settings, (v) => (settings = v)}
			bind:haNotify
			bind:haRules
			{admins}
			{modeOptions}
		/>
	{/if}

	{#if dirty || saving}
		<div class="row-actions sticky">
			<span class="grow muted">{t('agent.unsaved')}</span>
			<button type="button" class="btn" disabled={saving} onclick={() => void load()}
				>{t('common.cancel')}</button
			>
			<button type="button" class="btn btn-primary" disabled={saving} onclick={save}
				>{t('common.save')}</button
			>
		</div>
	{:else if saved}<p class="muted saved-note" role="status">✓ {t('agent.saved')}</p>{/if}
{/if}

<style>
	.back-row {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		font-size: var(--text-md);
	}
	.areas .area {
		all: unset;
		box-sizing: border-box;
		display: grid;
		grid-template-columns: minmax(0, 1fr) auto;
		align-items: center;
		gap: var(--space-3);
		width: 100%;
		padding: 11px var(--space-4);
		border-top: 1px solid var(--panel-border, var(--border));
		cursor: pointer;
	}
	.areas .area:hover,
	.areas .area:focus-visible {
		background: color-mix(in srgb, var(--text) 5%, transparent);
	}
	.areas .set-label {
		display: flex;
		flex-direction: column;
		gap: 2px;
	}
	.summary {
		display: inline-flex;
		align-items: center;
		gap: var(--space-2);
		color: var(--muted);
	}
	.row-actions .grow {
		flex: 1;
	}
	.saved-note {
		margin: 0;
	}
	/* PLAN 68: every tab in cards with rows "label (+ hint) left, control
	   right"; lists as rows; the card's own actions in its foot. */
	.set-row.one {
		grid-template-columns: minmax(0, 1fr);
	}

	/* The hint on the card's surface: a little toward the text for 4.5:1. */
	.set-hint {
		color: color-mix(in srgb, var(--muted) 85%, var(--text));
	}
	.set-control > .field {
		flex: 1 1 12rem;
		min-width: 0;
	}

	.mono {
		font-family: var(--font-mono, monospace);
		overflow-wrap: anywhere;
	}
	/* The ways to the NAS: a radio list as rows. */
	.ways {
		border: 0;
		margin: 0;
		padding: 0;
		min-width: 0;
		position: relative;
	}
	.way {
		display: flex;
		align-items: center;
		gap: var(--space-3);
		cursor: pointer;
	}
	.way > .set-label {
		flex: 1 1 auto;
	}
	.way > input[type='radio'] {
		flex: none;
		accent-color: var(--accent);
	}
	.way > .set-label > .field {
		margin-top: var(--space-1);
	}
	.way.on {
		background: color-mix(in srgb, var(--accent) 8%, transparent);
	}
	.qr-row {
		align-items: start;
	}
	.qr {
		flex-direction: column;
		align-items: center;
	}
	.qr .mono {
		color: color-mix(in srgb, var(--muted) 85%, var(--text));
		text-align: center;
	}
	.qr p {
		margin: 0;
	}
	.card-foot > .grow {
		color: color-mix(in srgb, var(--muted) 85%, var(--text));
	}
	textarea.field {
		padding: 0.6rem 0.75rem;
		resize: vertical;
	}
	/* PLAN 68: an area is a row; the label opens its tools. */

	.area .set-control :global(.segments) {
		flex-wrap: nowrap;
	}
	.area {
		padding-top: 9px;
		padding-bottom: 9px;
	}

	.hero-label {
		flex-direction: row;
		align-items: center;
		gap: var(--space-3);
	}
	.hero-text {
		display: flex;
		flex-direction: column;
		gap: 2px;
		min-width: 0;
	}
	.hero-text strong {
		font-weight: 600;
	}
	.switch-label {
		display: inline-flex;
		align-items: center;
		gap: var(--space-2-5);
		cursor: pointer;
	}

	/* The phone: long choices on a line of their own. */
	@media (max-width: 639px) {
		.area {
			grid-template-columns: minmax(0, 1fr) !important;
		}
		.area .set-control {
			justify-content: flex-start;
		}
	}

	.sticky {
		position: sticky;
		bottom: 0;
		z-index: 2;
		margin-top: var(--space-3);
		padding: var(--space-3) 0;
		background: var(--bg);
		border-top: 1px solid var(--border);
		display: flex;
		gap: var(--space-2);
		align-items: center;
	}
</style>
