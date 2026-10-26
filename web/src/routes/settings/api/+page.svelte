<script lang="ts">
	import { api, ApiError } from '#lib/api.ts';
	import PageHead from '#lib/components/PageHead.svelte';
	import Dialog from '#lib/components/Dialog.svelte';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import { errorText, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	type Token = { id: string; name: string; scopes: string[]; created: string; lastUsed?: string };
	const labels: Record<string, MessageKey> = {
		'system:read': 'api.scope.systemRead',
		'files:read': 'api.scope.filesRead',
		'files:write': 'api.scope.filesWrite',
		'docker:read': 'api.scope.dockerRead',
		'docker:write': 'api.scope.dockerWrite',
		'vms:read': 'api.scope.vmsRead',
		'vms:write': 'api.scope.vmsWrite',
	};
	let tokens = $state<Token[]>([]),
		scopes = $state<string[]>([]),
		loading = $state(true),
		busy = $state(false),
		error = $state('');
	let creating = $state(false),
		name = $state(''),
		selected = $state<string[]>(['system:read']),
		secret = $state(''),
		copied = $state(false),
		revoking = $state<Token | null>(null);
	const admin = $derived(['owner', 'admin'].includes(session.data?.user?.role ?? ''));
	async function load() {
		try {
			const res = await api.get<{ tokens: Token[]; scopes: string[] }>('/account/tokens');
			tokens = res.tokens;
			scopes = res.scopes;
		} catch (e) {
			error = errorText(e instanceof ApiError ? e.code : 'internal');
		} finally {
			loading = false;
		}
	}
	$effect(() => {
		void load();
	});
	async function create(e: SubmitEvent) {
		e.preventDefault();
		busy = true;
		error = '';
		try {
			const res = await api.post<{ secret: string }>('/account/tokens', { name, scopes: selected });
			secret = res.secret;
			copied = false;
			creating = false;
			await load();
		} catch (e) {
			error = errorText(e instanceof ApiError ? e.code : 'internal');
		} finally {
			busy = false;
		}
	}
	async function revoke() {
		if (!revoking) return;
		busy = true;
		error = '';
		try {
			await api.delete(`/account/tokens/${encodeURIComponent(revoking.id)}`);
			revoking = null;
			await load();
		} catch (e) {
			error = errorText(e instanceof ApiError ? e.code : 'internal');
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
	{#snippet actions()}<button
			class="btn btn-primary"
			onclick={() => {
				name = '';
				selected = ['system:read'];
				creating = true;
				error = '';
			}}>{t('api.create')}</button
		>{/snippet}
</PageHead>
{#if error}<div class="notice-error" role="alert">{error}</div>{/if}
{#if loading}<p role="status">{t('common.loading')}</p>{:else if !tokens.length}<EmptyState
		title={t('api.empty')}
	/>{:else}<section class="tile tokens" aria-label={t('api.mine')}>
		<h2 class="tile-title">{t('api.mine')}</h2>
		{#each tokens as token (token.id)}<div class="token">
				<div>
					<strong>{token.name}</strong>
					<p class="muted">{token.scopes.map((s) => t(labels[s])).join(' · ')}</p>
					<small class="muted"
						>{t('api.created')}
						{new Date(token.created).toLocaleDateString()}{#if token.lastUsed}
							· {t('api.lastUsed')} {new Date(token.lastUsed).toLocaleString()}{/if}</small
					>
				</div>
				<button class="btn btn-danger" onclick={() => (revoking = token)}>{t('api.revoke')}</button>
			</div>{/each}
	</section>{/if}
<section class="tile docs">
	<h2 class="tile-title">{t('api.documentation')}</h2>
	<a class="btn" href="/api/v1/openapi.json" target="_blank" rel="noreferrer">{t('api.openapi')}</a>
	<p class="muted">{t('api.cliHint')}</p>
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
				>{#each scopes.filter((s) => admin || s.startsWith('files:') || s === 'system:read') as scope}<label
						class="check"
						><input type="checkbox" bind:group={selected} value={scope} /><span
							>{t(labels[scope])}</span
						></label
					>{/each}
			</fieldset>
			<p class="muted">{t('api.scopeHint')}</p>
			<div class="dialog-actions">
				<button type="button" class="btn" disabled={busy} onclick={() => (creating = false)}
					>{t('common.cancel')}</button
				><button class="btn btn-primary" disabled={busy || !name.trim() || !selected.length}
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
		<div class="dialog-actions">
			<button class="btn btn-primary" onclick={() => (secret = '')}>{t('common.close')}</button>
		</div></Dialog
	>{/if}
{#if revoking}<Dialog title={t('api.revoke')} locked={busy} onclose={() => (revoking = null)}
		><p>{t('api.revokeConfirm', { name: revoking.name })}</p>
		<div class="dialog-actions">
			<button class="btn" disabled={busy} onclick={() => (revoking = null)}
				>{t('common.cancel')}</button
			><button class="btn btn-danger" disabled={busy} onclick={revoke}>{t('api.revoke')}</button>
		</div></Dialog
	>{/if}

<style>
	.tokens,
	.docs {
		padding: var(--tile-pad);
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
	.docs {
		display: grid;
		gap: var(--gap);
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
	@media (max-width: 500px) {
		.token {
			align-items: flex-start;
			flex-wrap: wrap;
		}
	}
</style>
