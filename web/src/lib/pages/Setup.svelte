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
	let show = $state(false);
	let busy = $state(false);
	// A problem with one field is shown under it; anything else above the button.
	let problems = $state({ name: '', password: '', repeat: '' });
	let error = $state('');
	let nameField = $state<HTMLInputElement>();
	let passwordField = $state<HTMLInputElement>();
	let repeatField = $state<HTMLInputElement>();

	async function submit(event: SubmitEvent) {
		event.preventDefault();
		error = '';
		const wanted = name.trim().toLowerCase();
		problems = {
			name: NAME.test(wanted) ? '' : errorText('invalid_name'),
			password:
				[...password].length < MIN_PASSWORD_LENGTH
					? t('auth.passwordTooShort', { min: MIN_PASSWORD_LENGTH })
					: '',
			repeat: password !== repeat ? t('auth.passwordMismatch') : '',
		};
		// The first field with a problem gets the focus.
		const first = problems.name
			? nameField
			: problems.password
				? passwordField
				: problems.repeat
					? repeatField
					: undefined;
		if (first) {
			first.focus();
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
	<Restore>
		<form onsubmit={submit} novalidate>
			<p class="intro">{t('auth.setupIntro')}</p>
			<label>
				<span>{t('auth.name')}</span>
				<!-- svelte-ignore a11y_autofocus -->
				<input
					bind:this={nameField}
					class="field"
					type="text"
					bind:value={name}
					oninput={() => (problems.name = '')}
					autocomplete="username"
					autocapitalize="none"
					spellcheck="false"
					maxlength="32"
					required
					autofocus
					aria-invalid={problems.name !== ''}
					aria-describedby={problems.name ? 'name-problem' : 'name-hint'}
				/>
				<!-- The problem names the same rules as the hint; it takes the hint's place. -->
				{#if problems.name}<small class="problem" id="name-problem">{problems.name}</small
					>{:else}<small id="name-hint">{t('auth.nameHint')}</small>{/if}
			</label>
			<label>
				<span>{t('auth.password')}</span>
				<input
					bind:this={passwordField}
					class="field"
					type={show ? 'text' : 'password'}
					bind:value={password}
					oninput={() => (problems.password = '')}
					autocomplete="new-password"
					required
					aria-invalid={problems.password !== ''}
					aria-describedby={problems.password ? 'password-problem password-hint' : 'password-hint'}
				/>
				{#if problems.password}<small class="problem" id="password-problem"
						>{problems.password}</small
					>{/if}
				<small id="password-hint">{t('auth.passwordHint', { min: MIN_PASSWORD_LENGTH })}</small>
			</label>
			<label>
				<span>{t('auth.passwordRepeat')}</span>
				<input
					bind:this={repeatField}
					class="field"
					type={show ? 'text' : 'password'}
					bind:value={repeat}
					oninput={() => (problems.repeat = '')}
					autocomplete="new-password"
					required
					aria-invalid={problems.repeat !== ''}
					aria-describedby={problems.repeat ? 'repeat-problem' : undefined}
				/>
				{#if problems.repeat}<small class="problem" id="repeat-problem">{problems.repeat}</small
					>{/if}
			</label>
			<label class="check"
				><input type="checkbox" bind:checked={show} />{t('auth.showPasswords')}</label
			>
			{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
			<button class="btn btn-primary" type="submit" disabled={busy}
				>{t(busy ? 'auth.creatingOwner' : 'auth.createOwner')}</button
			>
		</form>
	</Restore>
</AuthCard>

<style>
	.intro {
		margin: 0;
		font-size: 0.9375rem;
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
		font-size: 0.875rem;
	}
	small {
		font-size: 0.75rem;
		color: var(--muted);
	}
	.problem {
		color: var(--error-text);
		font-weight: 500;
	}
	.field[aria-invalid='true'] {
		border-color: var(--status-error);
	}
	.check {
		flex-direction: row;
		align-items: center;
		gap: 8px;
		margin-top: -4px;
		font-size: 0.8125rem;
		color: var(--muted);
		cursor: pointer;
	}
</style>
