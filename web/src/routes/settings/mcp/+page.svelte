<script lang="ts">
	// Settings › MCP server (decided 31.10.2026): status, how to connect a
	// client, the tools grouped by area with their switch (off, on, on with
	// dangerous calls) and the last calls from the activity log.
	import PageHead from '#lib/components/PageHead.svelte';
	import HelpTip from '#lib/components/HelpTip.svelte';
	import { api } from '#lib/api.ts';
	import { apiErrorText, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { formatDateTime } from '#lib/format.ts';
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
	let open = $state<Record<string, boolean>>({});

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
	<section class="tile panel" aria-labelledby="mcp-status">
		<div class="head">
			<h2 class="tile-title" id="mcp-status">{t('mcp.status')}</h2>
			<label class="check"
				><input
					type="checkbox"
					checked={status.enabled}
					disabled={saving}
					onchange={(e) => save(e.currentTarget.checked)}
				/><span>{t('mcp.enabled')}</span></label
			>
		</div>
		<div class="facts">
			<div>
				<span class="muted">{t('mcp.state')}</span>
				<span class="chip {status.enabled ? 'ok' : ''}"
					>{status.enabled ? '● ' + t('mcp.ready') : '○ ' + t('mcp.off')}</span
				>
			</div>
			<div>
				<span class="muted">{t('mcp.callsToday')}</span>
				<span
					><strong>{today.length}</strong>{#if today.some((c) => !c.ok)}
						· {t('mcp.refused', { count: today.filter((c) => !c.ok).length })}{/if}</span
				>
			</div>
			<div>
				<span class="muted">{t('mcp.tokens')}</span>
				<strong>{tokens}</strong>
			</div>
			<label class="address">
				<span class="muted">{t('mcp.endpoint')}</span>
				<span class="copy-row"
					><input class="field mono" value={endpoint} readonly /><button
						type="button"
						class="btn"
						onclick={() => copy('endpoint', endpoint)}
						>{t(copied === 'endpoint' ? 'mcp.copied' : 'mcp.copy')}</button
					></span
				>
			</label>
		</div>
	</section>

	<section class="tile panel" aria-labelledby="mcp-connect">
		<h2 class="tile-title" id="mcp-connect">{t('mcp.connect')}</h2>
		<div class="tabs" role="tablist">
			{#each [['code', 'mcp.claudeCode'], ['remote', 'mcp.desktopRemote'], ['bridge', 'mcp.desktopBridge']] as const as [id, label]}
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
	</section>

	<section class="tile panel" aria-labelledby="mcp-tools">
		<div class="head">
			<h2 class="tile-title" id="mcp-tools">{t('mcp.tools')}</h2>
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
		</div>
		<p class="muted small">{t('mcp.toolsIntro')}</p>
		{#if saved}<p class="notice-ok" role="status">{t('mcp.saved')}</p>{/if}
		<div class="groups">
			{#each groups as group (group.area)}
				{@const on = group.tools.some((tool) => modes[tool.name] !== 'off')}
				<div class="group">
					<div class="group-head">
						<input
							type="checkbox"
							checked={on}
							disabled={saving}
							aria-label={t(`mcp.area.${group.area}` as MessageKey)}
							onchange={(e) => setGroup(group.tools, e.currentTarget.checked)}
						/>
						<button
							type="button"
							class="group-toggle"
							aria-expanded={!!open[group.area]}
							onclick={() => (open[group.area] = !open[group.area])}
						>
							<strong>{t(`mcp.area.${group.area}` as MessageKey)}</strong>
							<span class="muted small">{groupLine(group.tools)}</span>
							<span class="muted" aria-hidden="true">{open[group.area] ? '▾' : '▸'}</span>
						</button>
					</div>
					{#if open[group.area]}
						<div class="tools">
							{#each group.tools as tool (tool.name)}
								<div class="tool">
									<span class="mono name">{tool.name}</span>
									<span class="muted small">{t(`mcp.tool.${tool.name}` as MessageKey)}</span>
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
										class="field"
										value={modes[tool.name]}
										disabled={saving}
										aria-label={tool.name}
										onchange={(e) => setMode(tool.name, e.currentTarget.value as Mode)}
									>
										<option value="off">{t('mcp.modeOff')}</option>
										<option value="on">{t('mcp.modeOn')}</option>
										{#if tool.dangerous}<option value="all">{t('mcp.modeAll')}</option>{/if}
									</select>
								</div>
							{/each}
						</div>
					{/if}
				</div>
			{/each}
		</div>
	</section>

	<section class="tile panel" aria-labelledby="mcp-recent">
		<h2 class="tile-title" id="mcp-recent">{t('mcp.recent')}</h2>
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
		<a href="/tools/activity">{t('mcp.recentAll')}</a>
	</section>
{/if}

<style>
	.head {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: 8px 16px;
	}
	.head h2 {
		margin: 0;
	}
	.facts {
		display: flex;
		flex-wrap: wrap;
		gap: 12px 28px;
		font-size: var(--text-md);
	}
	.facts > div,
	.address {
		display: flex;
		flex-direction: column;
		gap: 4px;
	}
	.address {
		flex: 1;
		min-width: min(100%, 260px);
	}
	.copy-row {
		display: flex;
		gap: 8px;
	}
	.copy-row input {
		flex: 1;
		min-width: 0;
	}
	.tabs {
		display: flex;
		flex-wrap: wrap;
		gap: 4px;
		border-bottom: 1px solid var(--border);
	}
	.tabs button {
		padding: 8px 12px;
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
		padding: 12px;
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
		gap: 6px;
	}
	.quick .chosen {
		border-color: var(--accent);
	}
	.groups {
		display: grid;
		gap: 6px;
	}
	.group {
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		padding: 0 12px;
	}
	.group-head {
		display: flex;
		align-items: center;
		gap: 12px;
	}
	.group-head input {
		width: 18px;
		height: 18px;
		margin: 0;
	}
	.group-toggle {
		flex: 1;
		display: flex;
		align-items: center;
		gap: 12px;
		min-height: 44px;
		padding: 0;
		border: 0;
		background: none;
		color: var(--text);
		font: inherit;
		text-align: left;
		cursor: pointer;
	}
	.group-toggle strong {
		flex: 1;
	}
	.tools {
		display: grid;
		padding: 0 0 10px 30px;
	}
	.tool {
		display: grid;
		grid-template-columns: minmax(150px, 190px) 1fr auto 170px;
		gap: 12px;
		align-items: center;
		padding: 6px 0;
		border-top: 1px solid var(--border);
	}
	.tool .name {
		font-size: var(--text-base);
		overflow-wrap: anywhere;
	}
	.tool select {
		height: 32px;
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
		padding: 6px 8px 6px 0;
		border-top: 1px solid var(--border);
	}
	.calls .result {
		text-align: right;
	}
	@media (max-width: 760px) {
		.tools {
			padding-left: 0;
		}
		.tool {
			grid-template-columns: 1fr auto;
		}
		.tool .muted {
			grid-column: 1 / -1;
		}
	}
</style>
