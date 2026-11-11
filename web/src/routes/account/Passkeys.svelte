<script lang="ts">
	// Passkeys of this account: log in without password and code, with the
	// fingerprint, face or PIN of a phone, computer or security key.
	import { api } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { apiErrorText, i18n, t } from '#lib/i18n/index.svelte.ts';
	import { addPasskey, passkeysPossible, type Passkey } from '#lib/passkeys.ts';
	import { session } from '#lib/session.svelte.ts';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import SettingSub from '#lib/components/settings/SettingSub.svelte';

	// PLAN 78-5: one row of "Mein Konto"; the keys are its parts. `open` lets
	// the page open the row (menu bar › Passkeys).
	let { open = $bindable(false) }: { open?: boolean } = $props();

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

	function open_() {
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

	const summary = $derived.by(() => {
		if (!state_) return loadError ? '' : t('common.loading');
		const count = state_.passkeys?.length ?? 0;
		return count === 0
			? t('passkeys.countNone')
			: count === 1
				? t('passkeys.countOne')
				: t('passkeys.count', { count });
	});

	const date = (value: string) =>
		new Intl.DateTimeFormat(i18n.locale, { dateStyle: 'medium' }).format(new Date(value));
</script>

<SettingRow
	id="acc-passkeys"
	icon="key"
	title={t('passkeys.title')}
	desc={t('passkeys.intro')}
	{summary}
	bind:open
>
	{#if !state_}
		<div class="srow-block">
			{#if loadError}
				<div class="notice-error" role="alert">
					{loadError}
					<button type="button" class="btn" onclick={load}>{t('common.retry')}</button>
				</div>
			{:else}
				<p class="muted loading" role="status">{t('common.loading')}</p>
			{/if}
		</div>
	{:else}
		{#each state_.passkeys as key (key.id)}
			<SettingSub
				title={key.name}
				desc="{key.rpId} · {t('passkeys.added', { date: date(key.created) })}{key.synced
					? ` · ${t('passkeys.synced')}`
					: ''}"
			>
				{#snippet control()}
					<button
						type="button"
						class="btn"
						aria-label="{t('passkeys.remove')}: {key.name}"
						onclick={() => ((error = ''), (removing = key))}>{t('passkeys.remove')}</button
					>
				{/snippet}
			</SettingSub>
		{:else}
			<div class="srow-block"><p class="muted">{t('passkeys.none')}</p></div>
		{/each}
		{#if !state_.usable || !possible}
			<div class="srow-block"><p class="notice-warn">{t('passkeys.notHere')}</p></div>
		{:else}
			<div class="srow-block actions">
				<button type="button" class="btn" onclick={open_}>{t('passkeys.add')}</button>
			</div>
		{/if}
		{#if error && !adding && !removing}<div class="srow-block">
				<p class="notice-error" role="alert">{error}</p>
			</div>{/if}
	{/if}
</SettingRow>

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
		gap: var(--space-1-5);
		font-size: var(--text-md);
	}
</style>
