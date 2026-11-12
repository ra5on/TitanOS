<script lang="ts">
	// A plain question before something that matters: "Abbrechen" has the
	// focus, nothing has to be typed (owner's rule: no typed confirmations).
	import Dialog from '../components/Dialog.svelte';
	import { t } from '../i18n/index.svelte.ts';

	let {
		title,
		text,
		confirm,
		danger = false,
		busy = false,
		error = '',
		onconfirm,
		onclose,
	}: {
		title: string;
		text: string | string[];
		/** The label of the button that does it. */
		confirm: string;
		danger?: boolean;
		busy?: boolean;
		error?: string;
		onconfirm: () => void;
		onclose: () => void;
	} = $props();
	const lines = $derived(Array.isArray(text) ? text : [text]);
</script>

<Dialog {title} locked={busy} {onclose}>
	{#each lines as line (line)}<p class="line">{line}</p>{/each}
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	<div class="dialog-actions">
		<!-- svelte-ignore a11y_autofocus -->
		<button type="button" class="btn" autofocus disabled={busy} onclick={onclose}
			>{t('common.cancel')}</button
		>
		<button
			type="button"
			class="btn {danger ? 'btn-danger' : 'btn-primary'}"
			disabled={busy}
			onclick={onconfirm}>{confirm}</button
		>
	</div>
</Dialog>

<style>
	.line {
		margin: 0;
		font-size: var(--text-md);
		overflow-wrap: anywhere;
	}
</style>
