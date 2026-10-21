<script lang="ts">
	// Keys for agents that read the state of the NAS over MCP. See docs/AGENT-API.md.
	import { api, ApiError } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { formatDateTime } from '#lib/format.ts';
	import { errorText, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { system } from '#lib/system.svelte.ts';

	type Key = {
		id: string;
		name: string;
		scopes: string[];
		created: string;
		expires: string | null;
		expired: boolean;
		networks: string[];
		lastUsed: string | null;
		lastIP: string;
	};
	type Answer = { enabled: boolean; keys: Key[]; scopes: string[]; fingerprint: string };

	let state_ = $state<Answer | null>(null);
	let error = $state('');
	let busy = $state(false);
	let adding = $state(false);
	let name = $state('');
	let scopes = $state<string[]>(['status']);
	let expiresDays = $state(90);
	let networks = $state('');
	let created = $state<{ token: string; key: Key } | null>(null);
	let copied = $state(false);
	let removing = $state<Key | null>(null);

	const problem = (err: unknown) => errorText(err instanceof ApiError ? err.code : 'internal');

	$effect(() => {
		api.get<Answer>('/account/api-keys').then((res) => (state_ = res), (err) => (error = problem(err)));
	});

	const host = $derived(system.info?.network?.ipv4.split('/')[0] || system.info?.hostname || 'nas');
	const address = $derived(`https://${host}/api/v1/mcp`);
	// The home network the NAS sits in, as a suggestion for "allowed networks".
	const homeNetwork = $derived.by(() => {
		const cidr = system.info?.network?.ipv4;
		const match = cidr?.match(/^(\d+)\.(\d+)\.(\d+)\.(\d+)\/(\d+)$/);
		if (!match) return '';
		const bits = Number(match[5]);
		const value = match.slice(1, 5).reduce((sum, part) => sum * 256 + Number(part), 0);
		const masked = bits === 0 ? 0 : (value >>> (32 - bits)) * 2 ** (32 - bits);
		return `${[24, 16, 8, 0].map((shift) => Math.floor(masked / 2 ** shift) % 256).join('.')}/${bits}`;
	});
	const fingerprint = $derived(state_?.fingerprint.match(/.{1,2}/g)?.join(':') ?? '');
	const clientConfig = $derived(
		created ? JSON.stringify({ mcpServers: { titanos: { type: 'http', url: address, headers: { Authorization: `Bearer ${created.token}` } } } }, null, 2) : ''
	);

	function suggest(network: string) {
		const list = networks.split(/[\s,]+/).filter(Boolean);
		if (!list.includes(network)) networks = [...list, network].join(', ');
	}

	function toggle(scope: string, on: boolean) {
		scopes = on ? [...scopes, scope] : scopes.filter((s) => s !== scope);
	}

	function reset() {
		adding = false;
		name = '';
		scopes = ['status'];
		expiresDays = 90;
		networks = '';
		error = '';
	}

	async function add(event: SubmitEvent) {
		event.preventDefault();
		busy = true;
		error = '';
		try {
			const res = await api.post<{ token: string; key: Key; keys: Answer }>('/account/api-keys', {
				name: name.trim(),
				scopes,
				expiresDays,
				networks: networks.split(/[\s,]+/).filter(Boolean)
			});
			state_ = res.keys;
			created = { token: res.token, key: res.key };
			copied = false;
			reset();
		} catch (err) {
			error = problem(err);
		}
		busy = false;
	}

	async function remove() {
		if (!removing) return;
		busy = true;
		error = '';
		try {
			state_ = await api.delete<Answer>(`/account/api-keys/${removing.id}`);
			removing = null;
		} catch (err) {
			error = problem(err);
		}
		busy = false;
	}

	async function copy(text: string) {
		try {
			await navigator.clipboard.writeText(text);
			copied = true;
		} catch {
			// The key is shown and can be selected by hand.
		}
	}

	const scopeLabel = (scope: string) => t(`apiKeys.scope.${scope}` as MessageKey);
</script>

{#if state_}
	<section class="tile" aria-labelledby="acc-api">
		<h2 class="tile-title" id="acc-api">{t('apiKeys.title')}</h2>
		<p class="muted">{t('apiKeys.text')}</p>
		{#if !state_.enabled}
			<p class="notice-warn">{t('apiKeys.off')} <a href="/settings/access">{t('ssh.settingsLink')}</a></p>
		{/if}
		{#each state_.keys as key (key.id)}
			<div class="key">
				<div>
					<span class="name">{key.name}{#if key.expired} <span class="chip warn">{t('apiKeys.expired')}</span>{/if}</span>
					<span class="chips">{#each key.scopes as scope (scope)}<span class="chip">{scopeLabel(scope)}</span>{/each}</span>
					<span class="muted small">
						{t('apiKeys.created', { time: formatDateTime(new Date(key.created)) })} ·
						{key.expires ? t('apiKeys.expires', { time: formatDateTime(new Date(key.expires)) }) : t('apiKeys.never')}
						{#if key.networks.length} · <span class="mono">{key.networks.join(', ')}</span>{/if}
					</span>
					<span class="muted small">
						{#if key.lastUsed}{t('apiKeys.lastUsed', { time: formatDateTime(new Date(key.lastUsed)) })} · <span class="mono">{key.lastIP}</span>{:else}{t('apiKeys.notUsed')}{/if}
					</span>
				</div>
				<button type="button" class="btn" onclick={() => (removing = key)}>{t('ssh.remove')}</button>
			</div>
		{:else}
			<p class="muted">{t('apiKeys.none')}</p>
		{/each}
		{#if error && !removing}<p class="notice-warn" role="alert">{error}</p>{/if}
		{#if adding}
			<form onsubmit={add}>
				<label><span class="muted">{t('apiKeys.name')}</span>
					<!-- svelte-ignore a11y_autofocus -->
					<input class="field" bind:value={name} maxlength="40" placeholder="ollama-agent" required autofocus />
				</label>
				<fieldset>
					<legend class="muted">{t('apiKeys.scopes')}</legend>
					{#each state_.scopes as scope (scope)}
						<label class="check">
							<input type="checkbox" checked={scopes.includes(scope)} onchange={(event) => toggle(scope, event.currentTarget.checked)} />
							<span><span class="scope">{scopeLabel(scope)}</span> <span class="muted small">{t(`apiKeys.scopeText.${scope}` as MessageKey)}</span></span>
						</label>
					{/each}
					<p class="muted small">{t('apiKeys.readOnly')}</p>
				</fieldset>
				<label><span class="muted">{t('apiKeys.expiry')}</span>
					<select class="field" bind:value={expiresDays}>
						<option value={30}>{t('apiKeys.days', { days: 30 })}</option>
						<option value={90}>{t('apiKeys.days', { days: 90 })}</option>
						<option value={365}>{t('apiKeys.year')}</option>
						<option value={0}>{t('apiKeys.never')}</option>
					</select>
				</label>
				<label><span class="muted">{t('apiKeys.networks')}</span>
					<input class="field mono" bind:value={networks} placeholder="172.16.0.0/12, 192.168.1.0/24" spellcheck="false" autocapitalize="none" />
				</label>
				<div class="suggest">
					<button type="button" class="btn" onclick={() => suggest('172.16.0.0/12')}>{t('apiKeys.suggestDocker')}</button>
					{#if homeNetwork}<button type="button" class="btn" onclick={() => suggest(homeNetwork)}>{t('apiKeys.suggestHome', { network: homeNetwork })}</button>{/if}
				</div>
				<p class="muted small">{t('apiKeys.networksHint')}</p>
				<div class="actions">
					<button type="button" class="btn" onclick={reset} disabled={busy}>{t('common.cancel')}</button>
					<button type="submit" class="btn btn-primary" disabled={busy || scopes.length === 0}>{t('apiKeys.create')}</button>
				</div>
			</form>
		{:else}
			<div><button type="button" class="btn" onclick={() => (adding = true)}>{t('apiKeys.add')}</button></div>
		{/if}
	</section>
{/if}

{#if created}
	<Dialog title={t('apiKeys.createdTitle', { name: created.key.name })} locked wide onclose={() => (created = null)}>
		<div class="created">
			<p class="notice-warn">{t('apiKeys.once')}</p>
			<div class="token">
				<code class="mono">{created.token}</code>
				<button type="button" class="btn" onclick={() => created && copy(created.token)}>{copied ? t('apiKeys.copied') : t('apiKeys.copy')}</button>
			</div>
			<dl>
				<div><dt>{t('apiKeys.address')}</dt><dd class="mono">{address}</dd></div>
				<div><dt>{t('apiKeys.header')}</dt><dd class="mono">Authorization: Bearer …</dd></div>
				{#if fingerprint}<div class="wide"><dt>{t('access.certFingerprint')}</dt><dd class="mono">{fingerprint}</dd></div>{/if}
			</dl>
			<p class="muted small">{t('apiKeys.clientHint')}</p>
			<pre class="mono">{clientConfig}</pre>
			<p class="muted small">{t('apiKeys.certHint')}</p>
		</div>
		<div class="dialog-actions"><button type="button" class="btn btn-primary" onclick={() => (created = null)}>{t('apiKeys.done')}</button></div>
	</Dialog>
{/if}

{#if removing}
	<Dialog title={t('apiKeys.removeTitle', { name: removing.name })} onclose={() => ((removing = null), (error = ''))}>
		<p class="text">{t('apiKeys.removeText')}</p>
		{#if error}<p class="notice-warn" role="alert">{error}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => ((removing = null), (error = ''))}>{t('common.cancel')}</button>
			<button type="button" class="btn btn-danger" disabled={busy} onclick={remove}>{t('ssh.remove')}</button>
		</div>
	</Dialog>
{/if}

<style>
	section {
		display: flex;
		flex-direction: column;
		gap: 12px;
		padding: 18px;
		font-size: 0.875rem;
	}
	p {
		margin: 0;
	}
	.key {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: 10px 16px;
		padding: 10px 12px;
		border: 1px solid var(--panel-border);
		border-radius: var(--radius-ctl);
	}
	.key > div {
		display: flex;
		flex-direction: column;
		gap: 4px;
		min-width: 0;
	}
	.name {
		font-weight: 600;
	}
	.chips {
		display: flex;
		flex-wrap: wrap;
		gap: 4px;
	}
	.small {
		font-size: 0.75rem;
	}
	.mono {
		overflow-wrap: anywhere;
	}
	form,
	label,
	fieldset {
		display: flex;
		flex-direction: column;
		gap: 8px;
	}
	fieldset {
		margin: 0;
		padding: 0;
		border: 0;
	}
	legend {
		padding: 0 0 8px;
	}
	label.check {
		flex-direction: row;
		align-items: flex-start;
		gap: 10px;
	}
	.check input {
		width: 18px;
		height: 18px;
		margin: 1px 0 0;
		flex: none;
	}
	.scope {
		font-weight: 600;
	}
	.suggest {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
	}
	.actions {
		display: flex;
		justify-content: flex-end;
		gap: 10px;
	}
	.created {
		display: flex;
		flex-direction: column;
		gap: 12px;
		font-size: 0.875rem;
	}
	.token {
		display: flex;
		align-items: center;
		gap: 10px;
		padding: 10px 12px;
		border: 1px solid var(--panel-border);
		border-radius: var(--radius-ctl);
	}
	.token code {
		flex: 1;
		min-width: 0;
		font-size: 0.8125rem;
		overflow-wrap: anywhere;
		user-select: all;
	}
	dl {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
		gap: 12px 24px;
		margin: 0;
	}
	.wide {
		grid-column: 1 / -1;
	}
	dt {
		color: var(--muted);
		font-size: 0.75rem;
	}
	dd {
		margin: 2px 0 0;
		overflow-wrap: anywhere;
	}
	pre {
		margin: 0;
		padding: 10px 12px;
		background: var(--chip);
		border-radius: var(--radius-ctl);
		font-size: 0.75rem;
		white-space: pre-wrap;
		overflow-wrap: anywhere;
	}
	.text {
		margin: 0 0 16px;
	}
</style>
