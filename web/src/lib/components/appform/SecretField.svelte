<script lang="ts">
	// A secret of an app (password, key): hidden, with show, copy and,
	// where TitanOS made it up, a new random value.
	import Icon from '#lib/components/Icon.svelte';
	import { generateSecret } from '#lib/compose/template.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	type Props = {
		value: string;
		id?: string;
		placeholder?: string;
		/** Offer a new random value. */
		renew?: boolean;
		invalid?: boolean;
		oninput?: () => void;
	};
	let {
		value = $bindable(),
		id,
		placeholder = '',
		renew = false,
		invalid = false,
		oninput,
	}: Props = $props();
	let shown = $state(false);
	let copied = $state(false);
	async function copy() {
		try {
			await navigator.clipboard.writeText(value);
			copied = true;
			setTimeout(() => (copied = false), 1500);
		} catch {
			// Without a secure context there is no clipboard; showing still works.
		}
	}
</script>

<div class="line">
	<input
		{id}
		class="field mono"
		type={shown ? 'text' : 'password'}
		bind:value
		{placeholder}
		autocomplete="off"
		spellcheck="false"
		aria-invalid={invalid}
		{oninput}
	/>
	<button
		type="button"
		class="btn square"
		aria-label={t(shown ? 'apps.hide' : 'apps.show')}
		title={t(shown ? 'apps.hide' : 'apps.show')}
		onclick={() => (shown = !shown)}><Icon name="eye" size={15} /></button
	>
	<button
		type="button"
		class="btn square"
		aria-label={t(copied ? 'appform.copied' : 'appform.copy')}
		title={t(copied ? 'appform.copied' : 'appform.copy')}
		disabled={!value}
		onclick={copy}><Icon name={copied ? 'check' : 'copy'} size={15} /></button
	>
	{#if renew}<button
			type="button"
			class="btn"
			onclick={() => {
				value = generateSecret();
				oninput?.();
			}}><Icon name="refresh" size={15} />{t('appform.newSecret')}</button
		>{/if}
</div>

<style>
	.square {
		width: var(--control-height);
		padding: 0;
	}
</style>
