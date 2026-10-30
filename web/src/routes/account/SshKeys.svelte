<script lang="ts">
	// The public keys this account may log in with over SSH.
	import ReconfirmFields, { reconfirmComplete } from '#lib/components/ReconfirmFields.svelte';
	import { api, ApiError } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import { system } from '#lib/system.svelte.ts';

	type Key = { id: string; type: string; fingerprint: string; comment: string };
	type Answer = { keys: Key[]; mayLogin: boolean; sshEnabled: boolean };

	let state_ = $state<Answer | null>(null);
	let adding = $state(false);
	let line = $state('');
	// A key opens another way into the account: password (and code) first.
	let password = $state('');
	let code = $state('');
	let error = $state('');
	let busy = $state(false);
	let removing = $state<Key | null>(null);
	let removeError = $state('');
	let loadError = $state('');

	async function load() {
		loadError = '';
		try {
			state_ = await api.get<Answer>('/account/ssh-keys');
		} catch (err) {
			loadError = apiErrorText(err);
		}
	}
	$effect(() => {
		load();
	});

	async function add(event: SubmitEvent) {
		event.preventDefault();
		busy = true;
		error = '';
		try {
			state_ = await api.post<Answer>('/account/ssh-keys', {
				key: line,
				password,
				code: code.trim(),
			});
			line = '';
			password = '';
			code = '';
			adding = false;
		} catch (err) {
			error = apiErrorText(err);
			// A used or wrong code cannot be sent again.
			if (err instanceof ApiError && err.code === 'invalid_code') code = '';
		}
		busy = false;
	}

	async function remove(key: Key) {
		if (busy) return;
		busy = true;
		removeError = '';
		try {
			state_ = await api.delete<Answer>(`/account/ssh-keys/${key.id}`);
			removing = null;
		} catch (err) {
			removeError = apiErrorText(err);
		}
		busy = false;
	}

	const host = $derived(system.info?.network?.ipv4.split('/')[0] || system.info?.hostname || 'nas');
</script>

<section class="tile" aria-labelledby="acc-ssh">
	<h2 class="tile-title" id="acc-ssh">{t('ssh.keys')}</h2>
	{#if !state_}
		{#if loadError}
			<div class="notice-error failed" role="alert">
				<span>{loadError}</span><button type="button" class="btn" onclick={load}
					>{t('common.retry')}</button
				>
			</div>
		{:else}
			<p class="muted loading" role="status">{t('common.loading')}</p>
		{/if}
	{:else}
		{#if !state_.mayLogin}
			<p class="muted">{t('ssh.onlyAdmins')}</p>
		{:else if !state_.sshEnabled}
			<p class="muted">{t('ssh.offHint')} <a href="/settings/access">{t('ssh.settingsLink')}</a></p>
		{:else}
			<p class="muted">
				{t('ssh.onHint')} <span class="mono">ssh {session.data?.user?.name}@{host}</span>
			</p>
		{/if}
		{#each state_.keys as key (key.id)}
			<div class="key">
				<div>
					<span class="name">{key.comment || key.type}</span>
					<span class="muted mono">{key.type} · {key.fingerprint}</span>
				</div>
				<button type="button" class="btn" onclick={() => ((removeError = ''), (removing = key))}
					>{t('ssh.remove')}</button
				>
			</div>
		{:else}
			<p class="muted">{t('ssh.noKeys')}</p>
		{/each}
		{#if error}<p class="notice-warn" role="alert">{error}</p>{/if}
		{#if adding}
			<form onsubmit={add}>
				<label
					><span class="muted">{t('ssh.keyLabel')}</span>
					<!-- svelte-ignore a11y_autofocus -->
					<textarea
						class="field mono"
						bind:value={line}
						rows="4"
						placeholder="ssh-ed25519 AAAA… name@host"
						spellcheck="false"
						autocapitalize="none"
						required
						autofocus></textarea>
				</label>
				<p class="muted small">{t('ssh.keyHint')}</p>
				<ReconfirmFields bind:password bind:code />
				<div class="actions">
					<button
						type="button"
						class="btn"
						onclick={() => ((adding = false), (error = ''), (password = ''), (code = ''))}
						disabled={busy}>{t('common.cancel')}</button
					>
					<button
						type="submit"
						class="btn btn-primary"
						disabled={busy || !reconfirmComplete(password, code)}>{t('ssh.add')}</button
					>
				</div>
			</form>
		{:else}
			<div>
				<button type="button" class="btn" onclick={() => (adding = true)}>{t('ssh.addKey')}</button>
			</div>
		{/if}
	{/if}
</section>
{#if state_}
	{#if removing}
		{@const key = removing}
		<Dialog title={t('ssh.removeTitle')} locked={busy} onclose={() => (removing = null)}>
			<p>{t('ssh.removeText', { name: key.comment || key.type })}</p>
			<p class="muted mono small" style:overflow-wrap="anywhere">{key.type} · {key.fingerprint}</p>
			{#if state_.keys.length === 1}<p class="notice-warn">{t('ssh.removeLast')}</p>{/if}
			{#if removeError}<p class="notice-error" role="alert">{removeError}</p>{/if}
			<div class="dialog-actions">
				<button type="button" class="btn" disabled={busy} onclick={() => (removing = null)}
					>{t('common.cancel')}</button
				>
				<button type="button" class="btn btn-danger" disabled={busy} onclick={() => remove(key)}
					>{t('ssh.remove')}</button
				>
			</div>
		</Dialog>
	{/if}
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
	.key div {
		display: flex;
		flex-direction: column;
		gap: 2px;
		min-width: 0;
	}
	.key .mono {
		font-size: 0.75rem;
		overflow-wrap: anywhere;
	}
	.name {
		font-weight: 600;
	}
	form,
	label {
		display: flex;
		flex-direction: column;
		gap: 8px;
	}
	textarea {
		height: auto;
		padding: 10px 12px;
		font-size: 0.75rem;
		resize: vertical;
	}
	.small {
		font-size: 0.75rem;
	}
	.failed {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: 10px;
	}
	.actions {
		display: flex;
		justify-content: flex-end;
		gap: 10px;
	}
</style>
