<script lang="ts">
	// Settings › MCP server (decided 31.10.2026): status, how to connect a
	// client, the tools grouped by area with their switch (off, on, on with
	// dangerous calls) and the last calls from the activity log. Every account
	// opens it (PLAN 19b): users see the state, the endpoint, how
	// to connect and the token button; the switch, the tools and the last calls
	// stay with owners and administrators (the server checks the same).
	import type { IconName } from '#lib/components/Icon.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import SettingSub from '#lib/components/settings/SettingSub.svelte';
	import HelpTip from '#lib/components/HelpTip.svelte';
	import { api } from '#lib/api.ts';
	import { apiErrorText, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { formatDateTime } from '#lib/format.ts';
	import { session } from '#lib/session.svelte.ts';
	import {
		claudeCodeCommand,
		desktopBridgeConfig,
		desktopRemoteConfig,
		mcpEndpoint,
		type MCPStatus,
	} from '#lib/mcp.ts';

	type Mode = 'off' | 'on' | 'all';
	type Tool = {
		name: string;
		area: string;
		scope: string;
		kind: 'read' | 'change';
		dangerous: boolean;
		mode: Mode;
	};
	type Status = MCPStatus & { tools: Tool[] };
	type Call = { at: string; target?: string; ok: boolean; status?: number };

	const AREA_ICONS: Record<string, IconName> = {
		system: 'gear',
		storage: 'disk',
		shares: 'folder',
		files: 'file',
		docker: 'box',
		vms: 'monitor',
		backups: 'archive',
		network: 'globe',
		accounts: 'users',
		plugins: 'grid',
		updates: 'download',
		home: 'home',
	};
	const AREAS = [
		'system',
		'storage',
		'shares',
		'files',
		'docker',
		'vms',
		'backups',
		'network',
		'accounts',
		'plugins',
		'updates',
		'home',
	];

	let status = $state<Status | null>(null);
	let modes = $state<Record<string, Mode>>({});
	let calls = $state<Call[]>([]);
	let tokens = $state(0);
	let error = $state('');
	let saving = $state(false);
	let saved = $state(false);
	let tab = $state<'code' | 'remote' | 'bridge'>('code');
	let copied = $state('');

	const admin = $derived(['owner', 'admin'].includes(session.data?.user?.role ?? ''));
	const origin = typeof location === 'undefined' ? '' : location.origin;
	const endpoint = $derived(mcpEndpoint(origin, status?.path));
	const snippet = $derived(
		tab === 'code'
			? claudeCodeCommand(endpoint)
			: tab === 'remote'
				? desktopRemoteConfig(endpoint)
				: desktopBridgeConfig(origin, '/home/me/.config/titanos-token'),
	);
	const groups = $derived(
		AREAS.map((area) => ({
			area,
			tools: (status?.tools ?? [])
				.filter((tool) => (tool.area || 'system') === area)
				.sort((a, b) => a.name.localeCompare(b.name)),
		})).filter((group) => group.tools.length > 0),
	);
	const today = $derived(
		calls.filter((c) => new Date(c.at).toDateString() === new Date().toDateString()),
	);

	async function load() {
		try {
			status = await api.get<Status>('/system/mcp');
			modes = Object.fromEntries(status.tools.map((tool) => [tool.name, tool.mode]));
		} catch (e) {
			error = apiErrorText(e);
		}
		try {
			const res = await api.get<{ tokens: { scopes: string[] }[] }>('/account/tokens');
			tokens = res.tokens.filter((token) => token.scopes.includes('mcp')).length;
		} catch {
			tokens = 0;
		}
		if (!admin) return;
		try {
			const res = await api.get<{ entries: Call[] }>('/activity?via=mcp&days=1&limit=20');
			calls = res.entries;
		} catch {
			calls = [];
		}
	}
	$effect(() => {
		void load();
	});

	async function save(enabled = status?.enabled ?? false) {
		saving = true;
		saved = false;
		error = '';
		try {
			await api.put('/system/mcp', { enabled, tools: $state.snapshot(modes) });
			await load();
			saved = true;
		} catch (e) {
			error = apiErrorText(e);
		} finally {
			saving = false;
		}
	}
	function setMode(name: string, mode: Mode) {
		modes[name] = mode;
		void save();
	}
	function setGroup(tools: Tool[], on: boolean) {
		for (const tool of tools)
			modes[tool.name] = on ? (modes[tool.name] === 'all' ? 'all' : 'on') : 'off';
		void save();
	}
	function quick(choice: 'read' | 'standard' | 'all') {
		for (const tool of status?.tools ?? []) {
			modes[tool.name] =
				choice === 'read'
					? tool.kind === 'read'
						? 'on'
						: 'off'
					: choice === 'all' && tool.dangerous
						? 'all'
						: 'on';
		}
		void save();
	}
	async function copy(id: string, text: string) {
		try {
			await navigator.clipboard.writeText(text);
			copied = id;
		} catch {
			copied = '';
		}
	}
	const kind = (tool: Tool) => (tool.dangerous ? 'danger' : tool.kind);
	const kindLabel: Record<string, MessageKey> = {
		read: 'mcp.kindRead',
		change: 'mcp.kindChange',
		danger: 'mcp.kindDanger',
	};
	function groupLine(tools: Tool[]): string {
		const on = tools.filter((tool) => modes[tool.name] !== 'off').length;
		if (on === 0) return t('mcp.groupOff');
		const danger = tools.filter((tool) => modes[tool.name] === 'all').length;
		const line = t('mcp.groupState', { on, count: tools.length });
		return danger ? `${line} · ${t('mcp.groupDanger', { count: danger })}` : line;
	}
	const isStandard = $derived((status?.tools ?? []).every((tool) => modes[tool.name] === 'on'));
</script>

<PageHead title={t('mcp.pageTitle')}>
	<p class="muted">{t('mcp.pageIntro')}<HelpTip term="mcp" /></p>
</PageHead>

{#if error}<div class="notice-error" role="alert">{error}</div>{/if}

{#if status}
	{@const st = status}
	<!-- PLAN 78-1: the state as rows; the switch stores on change as before. -->
	<SettingGroup title={t('mcp.status')} id="mcp-status">
		<SettingRow
			icon="sparkle"
			title={admin ? t('mcp.enabled') : t('mcp.state')}
			for={admin ? 'mcp-enabled' : undefined}
		>
			{#snippet control()}
				<span class="chip {st.enabled ? 'ok' : ''}"
					>{st.enabled ? '● ' + t('mcp.ready') : '○ ' + t('mcp.off')}</span
				>
				{#if admin}<input
						id="mcp-enabled"
						class="switch"
						type="checkbox"
						role="switch"
						checked={st.enabled}
						disabled={saving}
						onchange={(e) => save(e.currentTarget.checked)}
					/>{/if}
			{/snippet}
		</SettingRow>
		{#if admin}
			<SettingRow icon="activity" title={t('mcp.callsToday')}>
				{#snippet control()}
					<span class="value"
						><strong>{today.length}</strong>{#if today.some((c) => !c.ok)}{' · '}{t('mcp.refused', {
								count: today.filter((c) => !c.ok).length,
							})}{/if}</span
					>
				{/snippet}
			</SettingRow>
		{/if}
		<SettingRow icon="key" title={t('mcp.tokens')}>
			{#snippet control()}<strong class="value">{tokens}</strong>{/snippet}
		</SettingRow>
		<SettingRow icon="globe" title={t('mcp.endpoint')} for="mcp-endpoint">
			{#snippet control()}
				<input id="mcp-endpoint" class="field mono" value={endpoint} readonly />
				<button type="button" class="btn" onclick={() => copy('endpoint', endpoint)}
					>{t(copied === 'endpoint' ? 'mcp.copied' : 'mcp.copy')}</button
				>
			{/snippet}
		</SettingRow>
	</SettingGroup>

	<!-- A tool of the page (tabs and the command to copy): it stands under its
	     heading without a box. -->
	<SettingGroup title={t('mcp.connect')} id="mcp-connect">
		<div class="connect">
			<div class="tabs" role="tablist">
				{#each [['code', 'mcp.claudeCode'], ['remote', 'mcp.desktopRemote'], ['bridge', 'mcp.desktopBridge']] as const as [id, label] (id)}
					<button
						type="button"
						role="tab"
						aria-selected={tab === id}
						class:active={tab === id}
						onclick={() => (tab = id)}>{t(label)}</button
					>
				{/each}
			</div>
			<pre class="mono snippet">{snippet}</pre>
			<div class="row-actions">
				<button type="button" class="btn" onclick={() => copy('snippet', snippet)}
					>{t(copied === 'snippet' ? 'mcp.copied' : 'mcp.copy')}</button
				>
				<a class="btn btn-primary" href="/settings/api?mcp=1">{t('mcp.createToken')}</a>
			</div>
			<p class="muted small">{t('mcp.tokenHint')}</p>
			<p class="muted small">{t('mcp.placeholder')}</p>
			{#if tab === 'bridge'}<p class="muted small">{t('mcp.bridgeHint')}</p>{/if}
		</div>
	</SettingGroup>

	{#if admin}
		<!-- Every area is a row: its switch turns all its tools on or off, the
		     row opens to the tools with their own choice. Each change stores at
		     once, as before. -->
		<SettingGroup title={t('mcp.tools')} id="mcp-tools" desc={t('mcp.toolsIntro')}>
			{#snippet tools()}
				<div class="quick" role="group" aria-label={t('mcp.quick')}>
					<span class="muted small">{t('mcp.quick')}</span>
					<button type="button" class="btn small" disabled={saving} onclick={() => quick('read')}
						>{t('mcp.quickRead')}</button
					>
					<button
						type="button"
						class="btn small"
						class:chosen={isStandard}
						disabled={saving}
						onclick={() => quick('standard')}>{t('mcp.quickStandard')}</button
					>
					<button type="button" class="btn small" disabled={saving} onclick={() => quick('all')}
						>{t('mcp.quickAll')}</button
					>
				</div>
			{/snippet}
			{#if saved}<p class="notice-ok" role="status">{t('mcp.saved')}</p>{/if}
			<div class="areas">
				{#each groups as group (group.area)}
					{@const on = group.tools.some((tool) => modes[tool.name] !== 'off')}
					<SettingRow
						id="mcp-area-{group.area}"
						icon={AREA_ICONS[group.area] ?? 'tool'}
						title={t(`mcp.area.${group.area}` as MessageKey)}
						summary={groupLine(group.tools)}
						for="mcp-area-{group.area}-on"
					>
						{#snippet control()}
							<input
								id="mcp-area-{group.area}-on"
								class="switch"
								type="checkbox"
								role="switch"
								checked={on}
								disabled={saving}
								onchange={(e) => setGroup(group.tools, e.currentTarget.checked)}
							/>
						{/snippet}
						{#each group.tools as tool (tool.name)}
							<SettingSub
								title={tool.name}
								desc={t(`mcp.tool.${tool.name}` as MessageKey)}
								for="mcp-tool-{tool.name}"
							>
								{#snippet control()}
									<span
										class="chip {kind(tool) === 'read'
											? 'info'
											: kind(tool) === 'danger'
												? 'warn'
												: ''}"
										>{kind(tool) === 'read' ? '◉' : kind(tool) === 'danger' ? '⚠' : '✎'}
										{t(kindLabel[kind(tool)])}</span
									>
									<select
										id="mcp-tool-{tool.name}"
										class="field"
										value={modes[tool.name]}
										disabled={saving}
										onchange={(e) => setMode(tool.name, e.currentTarget.value as Mode)}
									>
										<option value="off">{t('mcp.modeOff')}</option>
										<option value="on">{t('mcp.modeOn')}</option>
										{#if tool.dangerous}<option value="all">{t('mcp.modeAll')}</option>{/if}
									</select>
								{/snippet}
							</SettingSub>
						{/each}
					</SettingRow>
				{/each}
			</div>
		</SettingGroup>

		<!-- A log stays a table. -->
		<SettingGroup title={t('mcp.recent')} id="mcp-recent">
			{#if calls.length === 0}<p class="muted">{t('mcp.recentNone')}</p>{:else}
				<table class="calls">
					<tbody>
						{#each calls.slice(0, 10) as call, i (i)}
							<tr>
								<td class="mono">{formatDateTime(new Date(call.at))}</td>
								<td class="mono">{call.target ?? ''}</td>
								<td class="result"
									><span class="chip {call.ok ? 'ok' : 'error'}"
										>{call.ok ? 'ok' : (call.status ?? '–')}</span
									></td
								>
							</tr>
						{/each}
					</tbody>
				</table>
			{/if}
			<p><a href="/tools/activity">{t('mcp.recentAll')}</a></p>
		</SettingGroup>
	{/if}
{/if}

<style>
	.value {
		color: var(--text);
	}
	/* The address: as wide as the row allows, the button beside it. */
	#mcp-endpoint {
		width: 20rem;
		max-width: 100%;
	}
	.connect {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
		min-width: 0;
	}
	.connect p {
		margin: 0;
	}
	.tabs {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-1);
		border-bottom: 1px solid var(--border);
	}
	.tabs button {
		padding: var(--space-2) var(--space-3);
		border: 0;
		border-radius: var(--radius-ctl) var(--radius-ctl) 0 0;
		background: none;
		color: var(--muted);
		font: inherit;
		font-size: var(--text-md);
		cursor: pointer;
	}
	.tabs button.active {
		background: var(--chip);
		color: var(--text);
	}
	.snippet {
		margin: 0;
		padding: var(--space-3);
		overflow-x: auto;
		border-radius: var(--radius-ctl);
		background: var(--chip);
		font-size: var(--text-base);
		white-space: pre-wrap;
		word-break: break-all;
	}
	.quick {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-1-5);
	}
	.quick .chosen {
		border-color: var(--accent);
	}
	/* The rows stay direct parts of their group; a tool's name is its
	   technical name. */
	.areas {
		display: contents;
	}
	.areas :global(.srow-sub .srow-title) {
		font-family: var(--font-mono);
		font-size: var(--text-base);
	}
	.chip {
		white-space: nowrap;
	}
	.calls {
		width: 100%;
		border-collapse: collapse;
		font-size: var(--text-base);
	}
	.calls td {
		padding: var(--space-1-5) var(--space-2) var(--space-1-5) 0;
		border-top: 1px solid var(--border);
	}
	/* A long target wraps; date and result keep their words. */
	.calls td:nth-child(2) {
		overflow-wrap: anywhere;
	}
	.calls .result {
		text-align: right;
	}
</style>
