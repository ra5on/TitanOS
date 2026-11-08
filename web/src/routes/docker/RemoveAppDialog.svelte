<script lang="ts">
	// Confirms removing an app: its containers and compose file, and with the
	// box ticked also its data. Used by the Docker overview and the app page;
	// the caller sends the request.
	import Dialog from '#lib/components/Dialog.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';

	type Props = {
		app: string;
		volume: string;
		busy: boolean;
		error: string;
		onclose: () => void;
		onremove: (purge: boolean) => void;
	};
	let { app, volume, busy, error, onclose, onremove }: Props = $props();

	let purge = $state(false);
	const path = $derived(`/mnt/${volume}/appdata/${app}`);
</script>

<Dialog title={t('docker.removeTitle', { name: app })} locked={busy} {onclose}>
	<p class="text">
		{t(purge ? 'docker.removeAppPurge' : 'docker.removeApp', { app, path })}
	</p>
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	<label class="check"
		><input type="checkbox" bind:checked={purge} disabled={busy} /><span
			>{t('docker.removeData', { path })}</span
		></label
	>
	{#if purge}
		<p class="notice-error">{t('docker.removeDataWarning')}</p>
	{/if}
	<div class="dialog-actions">
		<button type="button" class="btn" disabled={busy} onclick={onclose}>{t('common.cancel')}</button
		>
		<button
			type="button"
			disabled={busy}
			class="btn {purge ? 'btn-danger' : 'btn-warn'}"
			onclick={() => onremove(purge)}
			>{t(purge ? 'docker.removeDataSubmit' : 'docker.removeSubmit')}</button
		>
	</div>
</Dialog>

<style>
	.check {
		display: flex;
		align-items: center;
		gap: var(--space-2-5);
	}
	.check input {
		width: 18px;
		height: 18px;
		margin: 0;
	}
	.text {
		font-size: var(--text-md);
	}
</style>
