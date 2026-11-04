<script lang="ts">
	// The input of the KI-Agent: the task, the model and the thinking level
	// as the chosen model reports them (Ollama thinking.values), Enter sends,
	// Shift+Enter a new line. While a run works, the round button stops it.
	import Icon from '#lib/components/Icon.svelte';
	import { i18n, t } from '#lib/i18n/index.svelte.ts';
	import { levelLabel, type Model } from '#lib/agent.ts';

	let {
		models = [],
		model = $bindable(''),
		think = $bindable(''),
		busy = false,
		disabled = false,
		placeholder = '',
		onsend,
		onstop,
	}: {
		models?: Model[];
		model?: string;
		think?: string;
		busy?: boolean;
		disabled?: boolean;
		placeholder?: string;
		onsend: (text: string) => void;
		onstop?: () => void;
	} = $props();

	let text = $state('');
	let area = $state<HTMLTextAreaElement>();
	const usable = $derived(models.filter((m) => m.capabilities.includes('tools')));
	const chosen = $derived(usable.find((m) => m.name === model));
	const levels = $derived(chosen?.levels ?? []);
	// A level the model does not offer falls back to its default.
	$effect(() => {
		if (chosen && think && !levels.includes(think)) think = chosen.defaultLevel ?? '';
	});

	function grow() {
		if (!area) return;
		area.style.height = 'auto';
		area.style.height = Math.min(area.scrollHeight, 220) + 'px';
	}
	function send() {
		const value = text.trim();
		if (!value || busy || disabled) return;
		onsend(value);
		text = '';
		queueMicrotask(grow);
	}
	function key(event: KeyboardEvent) {
		if (event.key === 'Enter' && !event.shiftKey && !event.isComposing) {
			event.preventDefault();
			send();
		}
	}
	/** Puts a text into the field (suggestions). */
	export function fill(value: string) {
		text = value;
		queueMicrotask(() => {
			grow();
			area?.focus();
		});
	}
</script>

<form
	class="composer"
	onsubmit={(e) => {
		e.preventDefault();
		send();
	}}
>
	<textarea
		bind:this={area}
		bind:value={text}
		oninput={grow}
		onkeydown={key}
		rows="1"
		{disabled}
		aria-label={t('agent.input')}
		placeholder={placeholder || (busy ? t('agent.inputBusy') : t('agent.inputIdle'))}></textarea>
	<div class="tools">
		{#if usable.length > 1}
			<label class="pill">
				<span class="muted">{t('agent.model')}</span>
				<select bind:value={model} aria-label={t('agent.model')}>
					{#each usable as m (m.name)}<option value={m.name}>{m.name}</option>{/each}
				</select>
			</label>
		{:else if chosen}
			<span class="pill"><span class="muted">{t('agent.model')}</span> {chosen.name}</span>
		{/if}
		{#if levels.length > 0}
			<label class="pill">
				<span class="muted">{t('agent.think')}</span>
				<select bind:value={think} aria-label={t('agent.think')}>
					{#each levels as level (level)}<option value={level}
							>{levelLabel(level, i18n.locale)}</option
						>{/each}
				</select>
			</label>
		{/if}
		<span class="grow"></span>
		{#if busy}
			<button
				type="button"
				class="round stop"
				onclick={() => onstop?.()}
				aria-label={t('agent.stop')}
				title={t('agent.stop')}><Icon name="stop" size={14} /></button
			>
		{:else}
			<button
				type="submit"
				class="round send"
				disabled={disabled || !text.trim()}
				aria-label={t('agent.send')}
				title={t('agent.send')}><Icon name="arrowUp" size={18} /></button
			>
		{/if}
	</div>
</form>

<style>
	.composer {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		padding: 10px 10px 10px 14px;
		border: 1px solid var(--border);
		border-radius: 20px;
		background: var(--ag-input-bg, var(--bg));
	}
	:global(.agent.glass) .composer {
		background: color-mix(in srgb, var(--surface) 72%, transparent);
		backdrop-filter: blur(18px) saturate(140%);
		border-color: var(--glass-border);
		box-shadow: 0 12px 40px -16px rgb(0 0 0 / 0.55);
	}
	textarea {
		resize: none;
		border: 0;
		outline: none;
		background: transparent;
		color: var(--text);
		font: inherit;
		font-size: var(--text-md);
		line-height: 1.45;
		min-height: 24px;
		max-height: 220px;
		padding: 2px 0;
	}
	.tools {
		display: flex;
		align-items: center;
		gap: 6px;
		flex-wrap: wrap;
	}
	.grow {
		flex: 1;
	}
	.pill {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		border: 1px solid var(--border);
		border-radius: 999px;
		padding: 2px 10px;
		font-size: var(--text-sm);
		background: var(--surface);
		min-height: 30px;
	}
	.pill select {
		border: 0;
		background: transparent;
		color: var(--text);
		font: inherit;
		padding: 0;
		cursor: pointer;
	}
	.round {
		width: 36px;
		height: 36px;
		border-radius: 50%;
		display: grid;
		place-items: center;
		border: 0;
		cursor: pointer;
	}
	.send {
		background: var(--ag-gradient, var(--accent));
		color: var(--on-accent);
	}
	.send:disabled {
		opacity: 0.45;
		cursor: default;
	}
	.stop {
		background: var(--text);
		color: var(--bg);
	}
	@media (max-width: 760px) {
		.round {
			width: 40px;
			height: 40px;
		}
		.pill .muted {
			display: none;
		}
	}
</style>
