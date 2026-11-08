<script lang="ts">
	// PLAN 72, draft 03: before a Docker app opens a file of a share it does
	// not see yet, the share is mounted into the app. The app then sees the
	// whole share; its container is created anew. Owner and administrators.
	import Dialog from '#lib/components/Dialog.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';

	type Props = {
		app: string;
		share: string;
		file: string;
		busy: boolean;
		error: string;
		onconfirm: (readOnly: boolean) => void;
		onclose: () => void;
	};
	let { app, share, file, busy, error, onconfirm, onclose }: Props = $props();
	let readOnly = $state(false);
</script>

<Dialog title={t('files.openWith.mountTitle', { share, app })} locked={busy} {onclose}>
	<p>{t('files.openWith.mountText', { app, file, share, target: '/shares/' + share })}</p>
	<ul class="muted">
		<li>{t('files.openWith.mountSees', { app, share })}</li>
		<li>{t('files.openWith.mountRestart')}</li>
		<li>{t('files.openWith.mountUndo', { app })}</li>
	</ul>
	<label class="ro"
		><input type="checkbox" bind:checked={readOnly} disabled={busy} /><span
			>{t('files.openWith.mountReadOnly')}</span
		></label
	>
	{#if readOnly}<p class="muted small">{t('files.openWith.mountReadOnlyHint', { app })}</p>{/if}
	{#if busy}<p class="muted" role="status">{t('files.openWith.mounting', { app })}</p>{/if}
	{#if error}<p class="notice-warn" role="alert">{error}</p>{/if}
	<div class="dialog-actions">
		<button
			type="button"
			class="btn btn-primary"
			disabled={busy}
			onclick={() => onconfirm(readOnly)}>{t('files.openWith.mountConfirm')}</button
		>
		<!-- svelte-ignore a11y_autofocus -->
		<button type="button" class="btn" autofocus disabled={busy} onclick={onclose}
			>{t('common.cancel')}</button
		>
	</div>
</Dialog>

<style>
	p {
		margin: 0 0 10px;
	}
	ul {
		margin: 0 0 14px;
		padding-left: 18px;
		line-height: 1.5;
	}
	.ro {
		display: flex;
		align-items: center;
		gap: 8px;
		margin-bottom: 10px;
	}
	.small {
		font-size: var(--text-sm);
	}
</style>
