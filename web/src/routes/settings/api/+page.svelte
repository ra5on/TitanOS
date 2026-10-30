<script lang="ts">
	import ReconfirmFields, { reconfirmComplete } from '#lib/components/ReconfirmFields.svelte';
	import { apiPath } from '#lib/cluster-node.ts';
	import { api, ApiError } from '#lib/api.ts';
	import PageHead from '#lib/components/PageHead.svelte';
	import Dialog from '#lib/components/Dialog.svelte';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import { apiErrorText, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { formatDate, formatDateTime } from '#lib/format.ts';
	import { session } from '#lib/session.svelte.ts';
	import {
		claudeCodeCommand,
		desktopBridgeConfig,
		desktopRemoteConfig,
		mcpEndpoint,
		mcpTokenScopes,
		type MCPStatus,
	} from '#lib/mcp.ts';
	type Token = { id: string; name: string; scopes: string[]; created: string; lastUsed?: string };
	const labels: Record<string, MessageKey> = {
		'system:read': 'api.scope.systemRead',
		'files:read': 'api.scope.filesRead',
		'files:write': 'api.scope.filesWrite',
		'docker:read': 'api.scope.dockerRead',
		'docker:write': 'api.scope.dockerWrite',
		'vms:read': 'api.scope.vmsRead',
		'vms:write': 'api.scope.vmsWrite',
		mcp: 'api.scope.mcp',
	};
	let tokens = $state<Token[]>([]),
		scopes = $state<string[]>([]),
		loading = $state(true),
		busy = $state(false),
		error = $state(''),
		// Errors of the open create/revoke dialog, shown inside it.
		dialogError = $state('');
	let creating = $state(false),
		name = $state(''),
		selected = $state<string[]>(['system:read']),
		secret = $state(''),
		copied = $state(false),
		revoking = $state<Token | null>(null),
		// Scopes of the token just created, for the snippet with its secret.
		createdScopes = $state<string[]>([]),
		// A new token opens another way into the account: password (and code) first.
		password = $state(''),
		code = $state('');
	let mcp = $state<MCPStatus | null>(null),
		mcpBusy = $state(false),
		mcpError = $state(''),
		copiedSnippet = $state('');
	const admin = $derived(['owner', 'admin'].includes(session.data?.user?.role ?? ''));
	const origin = typeof location === 'undefined' ? '' : location.origin;
	const endpoint = $derived(mcpEndpoint(origin, mcp?.path));
	const snippets = $derived([
		{ id: 'code', label: t('mcp.claudeCode'), text: claudeCodeCommand(endpoint) },
		{ id: 'remote', label: t('mcp.desktopRemote'), text: desktopRemoteConfig(endpoint) },
		{
			id: 'bridge',
			label: t('mcp.desktopBridge'),
			text: desktopBridgeConfig(origin, '/home/me/.config/titanos-token'),
		},
	]);
	async function loadMCP() {
		try {
			mcp = await api.get<MCPStatus>('/system/mcp');
		} catch (e) {
			mcpError = apiErrorText(e);
		}
	}
	async function setMCP(enabled: boolean) {
		mcpBusy = true;
		mcpError = '';
		try {
			await api.put('/system/mcp', { enabled });
			await loadMCP();
		} catch (e) {
			mcpError = apiErrorText(e);
		} finally {
			mcpBusy = false;
		}
	}
	async function copySnippet(id: string, text: string) {
		try {
			await navigator.clipboard.writeText(text);
			copiedSnippet = id;
		} catch {
			copiedSnippet = '';
		}
	}
	function startCreate(scopes: string[], label = '') {
		name = label;
		selected = scopes;
		creating = true;
		dialogError = '';
		password = '';
		code = '';
	}
	async function load() {
		try {
			const res = await api.get<{ tokens: Token[]; scopes: string[] }>('/account/tokens');
			tokens = res.tokens;
			scopes = res.scopes;
		} catch (e) {
			error = apiErrorText(e);
		} finally {
			loading = false;
		}
	}
	$effect(() => {
		void load();
		void loadMCP();
	});
	async function create(e: SubmitEvent) {
		e.preventDefault();
		busy = true;
		dialogError = '';
		try {
			const res = await api.post<{ secret: string }>('/account/tokens', {
				name,
				scopes: selected,
				password,
				code: code.trim(),
			});
			password = '';
			code = '';
			secret = res.secret;
			createdScopes = [...selected];
			copied = false;
			creating = false;
			await load();
		} catch (e) {
			dialogError = apiErrorText(e);
			// A used or wrong code cannot be sent again.
			if (e instanceof ApiError && e.code === 'invalid_code') code = '';
		} finally {
			busy = false;
		}
	}
	async function revoke() {
		if (!revoking) return;
		busy = true;
		dialogError = '';
		try {
			await api.delete(`/account/tokens/${encodeURIComponent(revoking.id)}`);
			revoking = null;
			await load();
		} catch (e) {
			dialogError = apiErrorText(e);
		} finally {
			busy = false;
		}
	}
	async function copy() {
		try {
			await navigator.clipboard.writeText(secret);
			copied = true;
		} catch {
			copied = false;
		}
	}
</script>

<PageHead title={t('api.title')} parent={{ href: '/settings', label: t('nav.settings') }}>
	<p class="muted">{t('api.intro')}</p>
	{#snippet actions()}<button class="btn btn-primary" onclick={() => startCreate(['system:read'])}
			>{t('api.create')}</button
		>{/snippet}
</PageHead>
{#if error}<div class="notice-error" role="alert">{error}</div>{/if}
{#if loading}<p class="loading" role="status">
		{t('common.loading')}
	</p>{:else if !tokens.length}<EmptyState title={t('api.empty')} />{:else}<section
		class="tile panel tokens"
		aria-labelledby="api-mine"
	>
		<h2 class="tile-title" id="api-mine">{t('api.mine')}</h2>
		{#each tokens as token (token.id)}<div class="token">
				<div>
					<strong>{token.name}</strong>
					<p class="muted">{token.scopes.map((s) => t(labels[s])).join(' · ')}</p>
					<small class="muted"
						>{t('api.created')}
						{formatDate(new Date(token.created))}{#if token.lastUsed}
							· {t('api.lastUsed')} {formatDateTime(new Date(token.lastUsed))}{/if}</small
					>
				</div>
				<button class="btn btn-danger" onclick={() => ((dialogError = ''), (revoking = token))}
					>{t('api.revoke')}</button
				>
			</div>{/each}
	</section>{/if}
<section class="tile panel" aria-labelledby="api-docs">
	<h2 class="tile-title" id="api-docs">{t('api.documentation')}</h2>
	<div class="row-actions">
		<a class="btn" href={apiPath('/openapi.json')} target="_blank" rel="noreferrer"
			>{t('api.openapi')}</a
		>
	</div>
	<p class="muted">{t('api.cliHint')}</p>
</section>
<section class="tile panel" id="api-mcp" aria-labelledby="api-mcp-title">
	<h2 class="tile-title" id="api-mcp-title">{t('mcp.title')}</h2>
	<p class="muted">{t('mcp.intro')}</p>
	{#if mcp}
		{#if admin}<label class="check"
				><input
					type="checkbox"
					checked={mcp.enabled}
					disabled={mcpBusy}
					onchange={(e) => setMCP(e.currentTarget.checked)}
				/><span>{t('mcp.enabled')}</span></label
			>{:else}<p>{t(mcp.enabled ? 'mcp.stateOn' : 'mcp.stateOff')}</p>
			<p class="muted">{t('mcp.adminOnly')}</p>{/if}
	{/if}
	{#if mcpError}<div class="notice-error" role="alert">{mcpError}</div>{/if}
	<div class="snippet">
		<span>{t('mcp.endpoint')}</span>
		<code class="mono">{endpoint}</code>
		<button class="btn" onclick={() => copySnippet('endpoint', endpoint)}
			>{t(copiedSnippet === 'endpoint' ? 'mcp.copied' : 'mcp.copy')}</button
		>
	</div>
	<div class="row-actions">
		<button class="btn" onclick={() => startCreate(mcpTokenScopes(admin), 'Claude')}
			>{t('mcp.createToken')}</button
		>
	</div>
	<p class="muted">{t('mcp.tokenHint')}</p>
	{#each snippets as snippet (snippet.id)}
		<div class="snippet block">
			<span>{snippet.label}</span>
			<pre class="mono">{snippet.text}</pre>
			<button class="btn" onclick={() => copySnippet(snippet.id, snippet.text)}
				>{t(copiedSnippet === snippet.id ? 'mcp.copied' : 'mcp.copy')}</button
			>
		</div>
	{/each}
	<p class="muted">{t('mcp.placeholder')}</p>
	<p class="muted">{t('mcp.bridgeHint')}</p>
	<p class="muted">{t('mcp.safety')}</p>
</section>
<p class="muted">{t('api.security')}</p>

{#if creating}<Dialog title={t('api.create')} locked={busy} onclose={() => (creating = false)}
		><form onsubmit={create}>
			<label class="field-row"
				><span>{t('api.name')}</span><input
					class="field"
					bind:value={name}
					required
					maxlength="80"
				/></label
			>
			<fieldset>
				<legend>{t('api.scopes')}</legend
				>{#each scopes.filter((s) => admin || s.startsWith('files:') || s === 'system:read' || s === 'mcp') as scope}<label
						class="check"
						><input type="checkbox" bind:group={selected} value={scope} /><span
							>{t(labels[scope])}</span
						></label
					>{/each}
			</fieldset>
			<p class="muted">{t('api.scopeHint')}</p>
			<ReconfirmFields bind:password bind:code />
			{#if dialogError}<div class="notice-error" role="alert">{dialogError}</div>{/if}
			<div class="dialog-actions">
				<button type="button" class="btn" disabled={busy} onclick={() => (creating = false)}
					>{t('common.cancel')}</button
				><button
					class="btn btn-primary"
					disabled={busy || !name.trim() || !selected.length || !reconfirmComplete(password, code)}
					>{t('api.create')}</button
				>
			</div>
		</form></Dialog
	>{/if}
{#if secret}<Dialog title={t('api.createdTitle')} onclose={() => (secret = '')}
		><p>{t('api.once')}</p>
		<label class="field-row"
			><span>{t('api.token')}</span><textarea
				class="field mono"
				readonly
				value={secret}
				rows="3"
				spellcheck="false"></textarea></label
		><button class="btn" onclick={copy}>{t(copied ? 'api.copied' : 'api.copy')}</button>
		{#if createdScopes.includes('mcp')}<div class="snippet block">
				<span>{t('mcp.withToken')}</span>
				<pre class="mono">{claudeCodeCommand(endpoint, secret)}</pre>
				<button
					class="btn"
					onclick={() => copySnippet('created', claudeCodeCommand(endpoint, secret))}
					>{t(copiedSnippet === 'created' ? 'mcp.copied' : 'mcp.copy')}</button
				>
			</div>{/if}
		<div class="dialog-actions">
			<button class="btn btn-primary" onclick={() => (secret = '')}>{t('common.close')}</button>
		</div></Dialog
	>{/if}
{#if revoking}<Dialog title={t('api.revoke')} locked={busy} onclose={() => (revoking = null)}
		><p>{t('api.revokeConfirm', { name: revoking.name })}</p>
		{#if dialogError}<div class="notice-error" role="alert">{dialogError}</div>{/if}
		<div class="dialog-actions">
			<button class="btn" disabled={busy} onclick={() => (revoking = null)}
				>{t('common.cancel')}</button
			><button class="btn btn-danger" disabled={busy} onclick={revoke}>{t('api.revoke')}</button>
		</div></Dialog
	>{/if}

<style>
	.tokens {
		gap: 0;
	}
	.tokens h2 {
		margin-bottom: 4px;
	}
	.token {
		display: flex;
		justify-content: space-between;
		align-items: center;
		gap: var(--gap);
		padding: calc(var(--row-pad) + 6px) 0;
		border-bottom: 1px solid var(--border);
	}
	.token:last-child {
		border: 0;
	}
	.token > div {
		min-width: 0;
	}
	.token strong,
	.token p {
		overflow-wrap: anywhere;
	}
	.token .btn {
		flex: none;
	}
	form,
	.field-row {
		display: grid;
		gap: var(--gap);
	}
	fieldset {
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		padding: var(--tile-pad);
	}
	.check {
		display: flex;
		gap: 10px;
		align-items: center;
		min-height: 44px;
	}
	.check input {
		width: 18px;
		height: 18px;
	}
	.dialog-actions {
		display: flex;
		gap: 8px;
		justify-content: flex-end;
		margin-top: var(--gap);
	}
	.snippet {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 8px var(--gap);
	}
	.snippet > span {
		font-weight: 600;
	}
	.snippet code {
		overflow-wrap: anywhere;
	}
	.snippet.block {
		display: grid;
		grid-template-columns: 1fr auto;
	}
	.snippet.block pre {
		grid-column: 1 / -1;
		margin: 0;
		padding: var(--tile-pad);
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		background: var(--track);
		overflow-x: auto;
		white-space: pre;
	}
	@media (max-width: 500px) {
		.token {
			align-items: flex-start;
			flex-wrap: wrap;
		}
	}
</style>
