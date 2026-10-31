<script lang="ts">
	// First start: create the owner account. The wizard at /welcome
	// takes over from there (name of the NAS, time zone, what to do next).
	import { goto } from '$app/navigation';
	import { apiErrorText, errorText, t } from '#lib/i18n/index.svelte.ts';
	import { session, setupOwner } from '#lib/session.svelte.ts';
	import AuthCard from './AuthCard.svelte';
	import Restore from './Restore.svelte';

	// Same rules as the backend (internal/auth); it checks again.
	const MIN_PASSWORD_LENGTH = 10;
	const NAME = /^[a-z][a-z0-9_-]{0,31}$/;

	let name = $state('');
	let password = $state('');
	let repeat = $state('');
	let busy = $state(false);
	let error = $state('');

	async function submit(event: SubmitEvent) {
		event.preventDefault();
		error = '';
		const wanted = name.trim().toLowerCase();
		if (!NAME.test(wanted)) {
			error = errorText('invalid_name');
			return;
		}
		if ([...password].length < MIN_PASSWORD_LENGTH) {
			error = errorText('password_too_short');
			return;
		}
		if (password !== repeat) {
			error = t('auth.passwordMismatch');
			return;
		}
		busy = true;
		try {
			await setupOwner(wanted, password);
			await goto('/welcome');
		} catch (err) {
			error = apiErrorText(err);
		} finally {
			busy = false;
		}
	}
</script>

<AuthCard title={t('auth.setupTitle')} hostname={session.data?.hostname}>
	<p class="intro">{t('auth.setupIntro')}</p>
	<Restore />
	<form onsubmit={submit}>
		<label>
			<span>{t('auth.name')}</span>
			<!-- svelte-ignore a11y_autofocus -->
			<input
				class="field"
				type="text"
				bind:value={name}
				autocomplete="username"
				autocapitalize="none"
				spellcheck="false"
				maxlength="32"
				required
				autofocus
				aria-describedby="name-hint"
			/>
			<small id="name-hint">{t('auth.nameHint')}</small>
		</label>
		<label>
			<span>{t('auth.password')}</span>
			<input
				class="field"
				type="password"
				bind:value={password}
				autocomplete="new-password"
				required
				aria-describedby="password-hint"
			/>
			<small id="password-hint">{t('auth.passwordHint', { min: MIN_PASSWORD_LENGTH })}</small>
		</label>
		<label>
			<span>{t('auth.passwordRepeat')}</span>
			<input
				class="field"
				type="password"
				bind:value={repeat}
				autocomplete="new-password"
				required
			/>
		</label>
		{#if error}<p class="notice-warn" role="alert">{error}</p>{/if}
		<button class="btn btn-primary" type="submit" disabled={busy}>{t('auth.createOwner')}</button>
	</form>
</AuthCard>

<style>
	.intro {
		font-size: var(--text-md);
		color: var(--muted);
		text-align: center;
	}
	form {
		display: flex;
		flex-direction: column;
		gap: 16px;
	}
	label {
		display: flex;
		flex-direction: column;
		gap: 6px;
		font-size: var(--text-md);
	}
	small {
		font-size: var(--text-sm);
		color: var(--muted);
	}
</style>
