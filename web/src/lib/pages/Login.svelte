<script lang="ts">
	import { ApiError } from '#lib/api.ts';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';
	import { login, refreshSession, session } from '#lib/session.svelte.ts';
	import { loginWithPasskey, passkeysPossible } from '#lib/passkeys.ts';
	import type { LoginLook } from '#lib/login/look.ts';
	import AuthCard from './AuthCard.svelte';

	type Props = {
		/** Only shows the page (preview of Settings › Appearance): nothing is sent. */
		preview?: boolean;
		look?: LoginLook | null;
		imageURL?: string;
		hostname?: string;
	};
	let { preview = false, look, imageURL = '', hostname }: Props = $props();

	let name = $state('');
	let password = $state('');
	let busy = $state(false);
	let error = $state('');
	// Set once the password was right and the account asks for a second factor.
	let needsCode = $state(false);
	let code = $state('');
	// "Stay logged in" keeps the login over a restart of the NAS; the
	// choice is remembered in this browser.
	const REMEMBER_KEY = 'titanos.remember';
	let remember = $state(readRemember());
	function readRemember(): boolean {
		try {
			return localStorage.getItem(REMEMBER_KEY) === '1';
		} catch {
			return false;
		}
	}
	function keepChoice() {
		try {
			localStorage.setItem(REMEMBER_KEY, remember ? '1' : '0');
		} catch {
			// Private mode: the box just starts empty next time.
		}
	}

	const passkeys = passkeysPossible();

	async function withPasskey() {
		if (preview) return;
		busy = true;
		error = '';
		try {
			keepChoice();
			await loginWithPasskey(remember);
			await refreshSession();
		} catch (err) {
			if (err instanceof DOMException || (err instanceof Error && err.message === 'cancelled')) {
				error = t(err instanceof DOMException && err.name !== 'NotAllowedError' ? 'passkeys.browserError' : 'passkeys.cancelled');
			} else if (err instanceof ApiError && err.code === 'rate_limited') {
				error = t('auth.rateLimited', { seconds: err.retryAfter });
			} else {
				error = errorText(err instanceof ApiError ? err.code : 'internal');
			}
		} finally {
			busy = false;
		}
	}

	async function submit(event: SubmitEvent) {
		event.preventDefault();
		if (preview) return;
		busy = true;
		error = '';
		try {
			keepChoice();
			await login(name, password, code, remember);
		} catch (err) {
			if (err instanceof ApiError && err.code === 'second_factor_required') {
				needsCode = true;
				return;
			}
			if (err instanceof ApiError && err.code === 'invalid_code') {
				code = '';
				error = errorText(err.code);
				return;
			}
			password = '';
			code = '';
			needsCode = false;
			if (err instanceof ApiError && err.code === 'rate_limited') {
				error = t('auth.rateLimited', { seconds: err.retryAfter });
			} else {
				error = errorText(err instanceof ApiError ? err.code : 'internal');
			}
		} finally {
			busy = false;
		}
	}
</script>

<AuthCard title={t('auth.loginTitle')} hostname={hostname ?? session.data?.hostname} texts {look} {imageURL}>
	<form onsubmit={submit}>
		{#if needsCode}
			<p class="hint">{t('auth.codeHint')}</p>
			<label>
				<span>{t('auth.code')}</span>
				<!-- svelte-ignore a11y_autofocus -->
				<input class="field mono" type="text" bind:value={code} inputmode="numeric" autocomplete="one-time-code" spellcheck="false" required autofocus />
			</label>
		{:else}
		<label>
			<span>{t('auth.name')}</span>
			<!-- svelte-ignore a11y_autofocus -->
			<input class="field" type="text" bind:value={name} autocomplete="username" autocapitalize="none" spellcheck="false" required autofocus />
		</label>
		<label>
			<span>{t('auth.password')}</span>
			<input class="field" type="password" bind:value={password} autocomplete="current-password" required />
		</label>
		{/if}
		{#if !needsCode}
			<label class="remember"><input type="checkbox" bind:checked={remember} /><span>{t('auth.remember')}</span></label>
		{/if}
		{#if error}<p class="notice-warn" role="alert">{error}</p>{/if}
		<button class="btn btn-primary" type="submit" disabled={busy}>{t('auth.login')}</button>
		{#if passkeys && !needsCode}
			<button class="btn" type="button" disabled={busy} onclick={withPasskey}>{t('passkeys.login')}</button>
		{/if}
	</form>
</AuthCard>

<style>
	form {
		display: flex;
		flex-direction: column;
		gap: 16px;
	}
	.hint {
		font-size: 0.875rem;
	}
	label {
		display: flex;
		flex-direction: column;
		gap: 6px;
		font-size: 0.875rem;
	}
	label.remember {
		flex-direction: row;
		align-items: center;
		gap: 8px;
	}
</style>
