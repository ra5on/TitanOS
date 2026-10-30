<script lang="ts">
	// Passkeys of this account: log in without password and code, with the
	// fingerprint, face or PIN of a phone, computer or security key.
	import { api } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { apiErrorText, i18n, t } from '#lib/i18n/index.svelte.ts';
	import { addPasskey, passkeysPossible, type Passkey } from '#lib/passkeys.ts';
	import { session } from '#lib/session.svelte.ts';

	type Answer = { passkeys: Passkey[]; rpId: string; usable: boolean };

	let state_ = $state<Answer | null>(null);
	let loadError = $state('');
	let adding = $state(false);
	let password = $state('');
	let code = $state('');
	let name = $state('');
	let busy = $state(false);
	let error = $state('');
	let removing = $state<Passkey | null>(null);

	const possible = passkeysPossible();
	const problem = (err: unknown) => {
		if (err instanceof DOMException)
			return t(err.name === 'NotAllowedError' ? 'passkeys.cancelled' : 'passkeys.browserError');
		if (err instanceof Error && err.message === 'cancelled') return t('passkeys.cancelled');
		return apiErrorText(err);
	};

	async function load() {
		loadError = '';
		try {
			state_ = await api.get<Answer>('/account/passkeys');
		} catch (err) {
			loadError = problem(err);
		}
	}
	$effect(() => {
		load();
	});

	function open() {
		password = code = error = '';
		name = defaultName();
		adding = true;
	}

	function defaultName() {
		const ua = navigator.userAgent;
		const os = /iPhone|iPad/.test(ua)
			? 'iPhone/iPad'
			: /Android/.test(ua)
				? 'Android'
				: /Mac/.test(ua)
					? 'Mac'
					: /Windows/.test(ua)
						? 'Windows'
						: /Linux/.test(ua)
							? 'Linux'
							: '';
		return os ? `${os}` : 'Passkey';
	}

	async function add(event: SubmitEvent) {
		event.preventDefault();
		busy = true;
		error = '';
		try {
			const passkeys = await addPasskey(password, code, name);
			if (state_) state_.passkeys = passkeys;
			adding = false;
		} catch (err) {
			error = problem(err);
		}
		busy = false;
	}

	async function remove(key: Passkey) {
		busy = true;
		error = '';
		try {
			const answer = await api.delete<{ passkeys: Passkey[] }>(
				`/account/passkeys/${encodeURIComponent(key.id)}`,
			);
			if (state_) state_.passkeys = answer.passkeys;
			removing = null;
		} catch (err) {
			error = problem(err);
		}
		busy = false;
	}

	const date = (value: string) =>
		new Intl.DateTimeFormat(i18n.locale, { dateStyle: 'medium' }).format(new Date(value));
</script>

<section class="tile" aria-labelledby="acc-passkeys">
	<h2 class="tile-title" id="acc-passkeys">{t('passkeys.title')}</h2>
	{#if !state_}
		{#if loadError}
			<div class="notice-error" role="alert">
				{loadError} <button type="button" class="btn" onclick={load}>{t('common.retry')}</button>
			</div>
		{:else}
			<p class="muted loading" role="status">{t('common.loading')}</p>
		{/if}
	{:else}
		<p class="muted">{t('passkeys.intro')}</p>
		{#each state_.passkeys as key (key.id)}
			<div class="key">
				<div>
					<span class="name">{key.name}</span>
					<span class="muted small"
						>{key.rpId} · {t('passkeys.added', { date: date(key.created) })}{key.synced
							? ` · ${t('passkeys.synced')}`
							: ''}</span
					>
				</div>
				<button type="button" class="btn" onclick={() => ((error = ''), (removing = key))}
					>{t('passkeys.remove')}</button
				>
			</div>
		{:else}
			<p class="muted">{t('passkeys.none')}</p>
		{/each}
		{#if !state_.usable || !possible}
			<p class="notice-warn">{t('passkeys.notHere')}</p>
		{:else}
			<div><button type="button" class="btn" onclick={open}>{t('passkeys.add')}</button></div>
		{/if}
		{#if error && !adding && !removing}<p class="notice-error" role="alert">{error}</p>{/if}
	{/if}
</section>

{#if adding}
	<Dialog title={t('passkeys.add')} locked={busy} onclose={() => (adding = false)}>
		<form onsubmit={add}>
			<p class="muted">{t('passkeys.addText', { host: state_?.rpId ?? '' })}</p>
			<label
				><span>{t('passkeys.name')}</span><input
					class="field"
					type="text"
					bind:value={name}
					maxlength="60"
				/></label
			>
			<label
				><span>{t('auth.password')}</span>
				<!-- svelte-ignore a11y_autofocus -->
				<input
					class="field"
					type="password"
					bind:value={password}
					autocomplete="current-password"
					required
					autofocus
				/></label
			>
			{#if session.data?.user?.twoFactor}
				<label
					><span>{t('auth.code')}</span><input
						class="field mono"
						type="text"
						bind:value={code}
						inputmode="numeric"
						autocomplete="one-time-code"
						spellcheck="false"
						required
					/></label
				>
			{/if}
			{#if error}<p class="notice-warn" role="alert">{error}</p>{/if}
			<div class="dialog-actions">
				<button type="button" class="btn" disabled={busy} onclick={() => (adding = false)}
					>{t('common.cancel')}</button
				>
				<button type="submit" class="btn btn-primary" disabled={busy}>{t('passkeys.create')}</button
				>
			</div>
		</form>
	</Dialog>
{/if}

{#if removing}
	{@const key = removing}
	<Dialog title={t('passkeys.removeTitle')} locked={busy} onclose={() => (removing = null)}>
		<p>{t('passkeys.removeText', { name: key.name })}</p>
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={busy} onclick={() => (removing = null)}
				>{t('common.cancel')}</button
			>
			<button type="button" class="btn btn-danger" disabled={busy} onclick={() => remove(key)}
				>{t('passkeys.remove')}</button
			>
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
	section p {
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
		gap: 2px;
		min-width: 0;
	}
	.name {
		font-weight: 500;
	}
	.small {
		font-size: 0.75rem;
		overflow-wrap: anywhere;
	}
	form {
		display: flex;
		flex-direction: column;
		gap: 14px;
	}
	form p {
		margin: 0;
	}
	label {
		display: flex;
		flex-direction: column;
		gap: 6px;
		font-size: 0.875rem;
	}
</style>
