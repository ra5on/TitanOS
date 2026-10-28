<script lang="ts">
	// Name servers as chips. Typing a space, comma or semicolon (or Enter,
	// or leaving the field) turns the text into chips; pasting a list works.
	import { t } from '#lib/i18n/index.svelte.ts';
	import { looksLikeIP, parseDns } from '#lib/network.ts';

	let {
		value = $bindable([]),
		id,
		max = 3,
	}: { value: string[]; id: string; max?: number } = $props();

	let text = $state('');
	function commit(all: boolean) {
		const parts = text.split(/[\s,;]+/);
		const rest = all ? '' : (parts.pop() ?? '');
		const added = parseDns(parts.join(' '));
		if (added.length) value = parseDns([...value, ...added].join(' '));
		text = rest;
	}
	const invalid = $derived(value.filter((v) => !looksLikeIP(v)));
</script>

<div class="dns">
	{#each value as server (server)}
		<span class="chip" class:warn={!looksLikeIP(server)}
			><span class="mono">{server}</span><button
				type="button"
				aria-label={t('netx.dnsRemove', { server })}
				onclick={() => (value = value.filter((v) => v !== server))}>×</button
			></span
		>
	{/each}
	<input
		{id}
		class="field mono"
		type="text"
		spellcheck="false"
		autocapitalize="none"
		placeholder={value.length ? '' : '1.1.1.1, 9.9.9.9'}
		bind:value={text}
		oninput={() => {
			if (/[\s,;]/.test(text)) commit(false);
		}}
		onkeydown={(e) => {
			if (e.key === 'Enter' && text.trim()) {
				e.preventDefault();
				commit(true);
			} else if (e.key === 'Backspace' && !text && value.length) {
				value = value.slice(0, -1);
			}
		}}
		onblur={() => commit(true)}
	/>
</div>
{#if invalid.length}<p class="notice-warn" role="alert">
		{t('netx.dnsInvalid', { value: invalid.join(', ') })}
	</p>{/if}
{#if value.length > max}<p class="notice-warn" role="alert">{t('netx.dnsTooMany', { max })}</p>{/if}

<style>
	.dns {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 6px;
		max-width: 32rem;
	}
	.chip {
		gap: 4px;
	}
	.chip button {
		padding: 0 2px;
		background: none;
		border: 0;
		color: inherit;
		font: inherit;
		cursor: pointer;
		line-height: 1;
	}
	input {
		flex: 1 1 10rem;
		min-width: 8rem;
	}
</style>
