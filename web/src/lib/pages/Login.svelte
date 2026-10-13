<script lang="ts">
	import { ApiError } from '#lib/api.ts';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';
	import { login, session } from '#lib/session.svelte.ts';
	import AuthCard from './AuthCard.svelte';

	let name = $state('');
	let password = $state('');
	let busy = $state(false);
	let error = $state('');

	async function submit(event: SubmitEvent) {
		event.preventDefault();
		busy = true;
		error = '';
		try {
			await login(name, password);
		} catch (err) {
			password = '';
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

<AuthCard title={t('auth.loginTitle')} hostname={session.data?.hostname}>
	<form onsubmit={submit}>
		<label>
			<span>{t('auth.name')}</span>
			<!-- svelte-ignore a11y_autofocus -->
			<input class="field" type="text" bind:value={name} autocomplete="username" autocapitalize="none" spellcheck="false" required autofocus />
		</label>
		<label>
			<span>{t('auth.password')}</span>
			<input class="field" type="password" bind:value={password} autocomplete="current-password" required />
		</label>
		{#if error}<p class="notice-warn" role="alert">{error}</p>{/if}
		<button class="btn btn-primary" type="submit" disabled={busy}>{t('auth.login')}</button>
	</form>
</AuthCard>

<style>
	form {
		display: flex;
		flex-direction: column;
		gap: 16px;
	}
	label {
		display: flex;
		flex-direction: column;
		gap: 6px;
		font-size: 0.875rem;
	}
</style>
