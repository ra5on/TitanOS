<script lang="ts">
	// The public keys this account may log in with over SSH.
	import ReconfirmFields, { reconfirmComplete } from '#lib/components/ReconfirmFields.svelte';
	import { api, ApiError } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import { system } from '#lib/system.svelte.ts';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import SettingSub from '#lib/components/settings/SettingSub.svelte';

	// PLAN 78-5: one row of "Mein Konto"; the keys and the form for a new one
	// are its parts. `open` lets the page open the row (menu bar › SSH).
	let { open = $bindable(false) }: { open?: boolean } = $props();

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

	const summary = $derived.by(() => {
		if (!state_) return loadError ? '' : t('common.loading');
		const count = state_.keys?.length ?? 0;
		return count === 0
			? t('ssh.countNone')
			: count === 1
				? t('ssh.countOne')
				: t('ssh.count', { count });
	});

	const host = $derived(system.info?.network?.ipv4.split('/')[0] || system.info?.hostname || 'nas');
</script>

<SettingRow id="acc-ssh" icon="terminal" title={t('ssh.keys')} {summary} bind:open>
	{#snippet description()}
		{#if state_}
			{#if !state_.mayLogin}
				{t('ssh.onlyAdmins')}
			{:else if !state_.sshEnabled}
				{t('ssh.offHint')} <a href="/settings/access">{t('ssh.settingsLink')}</a>
			{:else}
				{t('ssh.onHint')} <span class="mono">ssh {session.data?.user?.name}@{host}</span>
			{/if}
		{/if}
	{/snippet}
	{#if !state_}
		<div class="srow-block">
			{#if loadError}
				<div class="notice-error failed" role="alert">
					<span>{loadError}</span><button type="button" class="btn" onclick={load}
						>{t('common.retry')}</button
					>
				</div>
			{:else}
				<p class="muted loading" role="status">{t('common.loading')}</p>
			{/if}
		</div>
	{:else}
		{#each state_.keys as key (key.id)}
			<SettingSub title={key.comment || key.type}>
				{#snippet description()}<span class="mono">{key.type} · {key.fingerprint}</span>{/snippet}
				{#snippet control()}
					<button
						type="button"
						class="btn"
						aria-label="{t('ssh.remove')}: {key.comment || key.type}"
						onclick={() => ((removeError = ''), (removing = key))}>{t('ssh.remove')}</button
					>
				{/snippet}
			</SettingSub>
		{:else}
			<div class="srow-block"><p class="muted">{t('ssh.noKeys')}</p></div>
		{/each}
		{#if error}<div class="srow-block">
				<p class="notice-warn" role="alert">{error}</p>
			</div>{/if}
		{#if adding}
			<form class="srow-block" onsubmit={add}>
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
			<div class="srow-block actions">
				<button type="button" class="btn" onclick={() => (adding = true)}>{t('ssh.addKey')}</button>
			</div>
		{/if}
	{/if}
</SettingRow>
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
	form,
	label {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}
	textarea {
		height: auto;
		padding: var(--space-2-5) var(--space-3);
		font-size: var(--text-sm);
		resize: vertical;
	}
	.small {
		font-size: var(--text-sm);
	}
	.failed {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-2-5);
	}
	form .actions {
		display: flex;
		flex-wrap: wrap;
		justify-content: flex-end;
		gap: var(--space-2-5);
	}
</style>
