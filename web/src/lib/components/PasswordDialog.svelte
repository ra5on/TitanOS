<script lang="ts">
	// Changes a password: one's own (needs the current one) or, as an
	// administrator, someone else's.
	import { api, ApiError } from '#lib/api.ts';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';
	import Dialog from './Dialog.svelte';

	type Props = { name: string; self: boolean; onclose: () => void };
	let { name, self, onclose }: Props = $props();

	const MIN_PASSWORD_LENGTH = 10;

	let current = $state('');
	let password = $state('');
	let repeat = $state('');
	let working = $state(false);
	let error = $state('');

	async function submit(event: SubmitEvent) {
		event.preventDefault();
		error = '';
		if ([...password].length < MIN_PASSWORD_LENGTH) {
			error = errorText('password_too_short');
			return;
		}
		if (password !== repeat) {
			error = t('auth.passwordMismatch');
			return;
		}
		working = true;
		try {
			await api.post(`/users/${name}/password`, self ? { password, current } : { password });
			onclose();
		} catch (err) {
			error = errorText(err instanceof ApiError ? err.code : 'internal');
			working = false;
		}
	}
</script>

<Dialog title={t('users.passwordTitle', { name })} locked={working} {onclose}>
	<form onsubmit={submit}>
		{#if self}
			<label>
				<span>{t('users.currentPassword')}</span>
				<!-- svelte-ignore a11y_autofocus -->
				<input class="field" type="password" bind:value={current} autocomplete="current-password" required autofocus />
			</label>
		{/if}
		<label>
			<span>{t('users.newPassword')}</span>
			<input class="field" type="password" bind:value={password} autocomplete="new-password" required />
			<small>{t('auth.passwordHint', { min: MIN_PASSWORD_LENGTH })}</small>
		</label>
		<label>
			<span>{t('auth.passwordRepeat')}</span>
			<input class="field" type="password" bind:value={repeat} autocomplete="new-password" required />
		</label>
		<p class="muted note">{t('users.passwordNote')}</p>
		{#if error}<p class="notice-warn" role="alert">{error}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={onclose} disabled={working}>{t('common.cancel')}</button>
			<button type="submit" class="btn btn-primary" disabled={working}>{t('users.savePassword')}</button>
		</div>
	</form>
</Dialog>

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
	small,
	.note {
		font-size: 0.75rem;
		color: var(--muted);
	}
</style>
