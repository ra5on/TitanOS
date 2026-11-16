<script lang="ts">
	// The actions of a message of the person on the phone (a long press):
	// change and ask again, copy, "Antwort neu erzeugen" for the last one,
	// search in the chat, remember it.
	import Icon from '#lib/components/Icon.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import type { Entry } from '#lib/agent.ts';
	import BottomSheet from '../BottomSheet.svelte';

	let {
		entry,
		calm = false,
		canChange = false,
		last = false,
		onclose,
		onedit,
		oncopy,
		onregenerate,
		onfind,
		onremember,
	}: {
		entry: Entry;
		calm?: boolean;
		/** It can be changed and asked again (nothing runs, not read-only). */
		canChange?: boolean;
		/** The person's last message: its answer can be made again. */
		last?: boolean;
		onclose: () => void;
		onedit: () => void;
		oncopy: () => void;
		onregenerate: () => void;
		onfind: () => void;
		onremember?: () => void;
	} = $props();
</script>

<BottomSheet label={t('agent.messageActions')} {calm} {onclose}>
	<p class="sh-quote">„{entry.content}“</p>
	{#if canChange}
		<button type="button" class="sh-item" onclick={onedit}
			><Icon name="edit" size={18} /><span class="sh-text">{t('agent.editResend')}</span></button
		>
	{/if}
	<button type="button" class="sh-item" onclick={oncopy}
		><Icon name="copy" size={18} /><span class="sh-text">{t('agent.copy')}</span></button
	>
	{#if canChange && last}
		<button type="button" class="sh-item" onclick={onregenerate}
			><Icon name="refresh" size={18} /><span class="sh-text">{t('agent.regenerateAnswer')}</span
			></button
		>
	{/if}
	<button type="button" class="sh-item" onclick={onfind}
		><Icon name="search" size={18} /><span class="sh-text">{t('agent.findInChat')}</span></button
	>
	{#if onremember}
		<button type="button" class="sh-item" onclick={onremember}
			><Icon name="bulb" size={18} /><span class="sh-text">{t('agent.rememberThis')}</span></button
		>
	{/if}
</BottomSheet>
