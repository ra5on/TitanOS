<script lang="ts">
	// Asks for the password (and the code of the second factor) again
	// before something that gives away the secrets of the NAS.
	import { api } from '#lib/api.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import Dialog from './Dialog.svelte';

	type Props = { reason: string; onconfirmed: () => void; onclose: () => void };
	let { reason, onconfirmed, onclose }: Props = $props();

	let password = $state('');
	let code = $state('');
	let working = $state(false);
	let error = $state('');

	async function submit(event: SubmitEvent) {
		event.preventDefault();
		error = '';
		working = true;
		try {
			await api.post('/session/confirm', { password, code });
			onconfirmed();
		} catch (err) {
			error = apiErrorText(err);
			working = false;
		}
	}
</script>

<Dialog title={t('confirm.title')} locked={working} {onclose}>
	<form onsubmit={submit}>
		<p class="muted">{reason}</p>
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
			/>
		</label>
		{#if session.data?.user?.twoFactor}
			<label
				><span>{t('auth.code')}</span>
				<input
					class="field mono"
					type="text"
					bind:value={code}
					inputmode="numeric"
					autocomplete="one-time-code"
					spellcheck="false"
					required
				/>
			</label>
		{/if}
		{#if error}<p class="notice-warn" role="alert">{error}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={onclose} disabled={working}
				>{t('common.cancel')}</button
			>
			<button type="submit" class="btn btn-primary" disabled={working}>{t('confirm.submit')}</button
			>
		</div>
	</form>
</Dialog>

<style>
	form {
		display: flex;
		flex-direction: column;
		gap: 14px;
	}
	p {
		margin: 0;
		font-size: var(--text-md);
	}
	label {
		display: flex;
		flex-direction: column;
		gap: 6px;
		font-size: var(--text-md);
	}
</style>
