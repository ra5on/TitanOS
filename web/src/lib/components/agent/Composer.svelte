<script lang="ts">
	// The input of the KI-Agent: the task, the model and the thinking level
	// as the chosen model reports them (Ollama thinking.values), Enter sends,
	// Shift+Enter a new line. While a run works, the round button stops it.
	import Dropdown from '#lib/components/Dropdown.svelte';
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
		attachments = $bindable([]),
		onsend,
		onstop,
		onpicknas,
		onerror,
	}: {
		models?: Model[];
		model?: string;
		think?: string;
		busy?: boolean;
		disabled?: boolean;
		placeholder?: string;
		/** Files to send with the message: uploads or files of the NAS. */
		attachments?: { name: string; file?: File; ref?: string }[];
		onsend: (text: string) => void;
		onstop?: () => void;
		/** Opens the choice of files on the NAS (shares of the account). */
		onpicknas?: () => void;
		/** A file could not be taken, e.g. too large. */
		onerror?: (message: string) => void;
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
	let picker = $state<HTMLInputElement>();
	function picked(event: Event) {
		const files = (event.currentTarget as HTMLInputElement).files;
		const big: string[] = [];
		for (const file of files ?? []) {
			if (file.size > 20 * 1024 * 1024) big.push(file.name);
			else if (!attachments.some((a) => a.name === file.name))
				attachments = [...attachments, { name: file.name, file }];
		}
		// Larger files are chosen from the NAS instead (nothing is copied).
		if (big.length) onerror?.(t('agent.attachTooBig', { names: big.join(', ') }));
		if (picker) picker.value = '';
	}
	function send() {
		const value = text.trim() || (attachments.length ? t('agent.attachDefault') : '');
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
	{#if attachments.length}
		<div class="files">
			{#each attachments as a (a.name)}
				<span class="file"
					><Icon name={a.file ? 'clip' : 'file'} size={13} />{a.name}<button
						type="button"
						aria-label={t('agent.attachRemove', { name: a.name })}
						onclick={() => (attachments = attachments.filter((x) => x !== a))}
						><Icon name="close" size={12} /></button
					></span
				>
			{/each}
		</div>
	{/if}
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
		<input bind:this={picker} type="file" multiple hidden onchange={picked} />
		{#if onpicknas}
			<span class="attach-menu">
				<Dropdown label={t('agent.attach')} icon="clip" disabled={busy || disabled}>
					{#snippet children(close)}
						<button
							type="button"
							role="menuitem"
							onclick={() => {
								close();
								picker?.click();
							}}><Icon name="upload" size={16} /><span>{t('agent.attachDevice')}</span></button
						>
						<button
							type="button"
							role="menuitem"
							onclick={() => {
								close();
								onpicknas?.();
							}}><Icon name="folder" size={16} /><span>{t('agent.attachNas')}</span></button
						>
					{/snippet}
				</Dropdown>
			</span>
		{:else}
			<button
				type="button"
				class="round attach"
				aria-label={t('agent.attach')}
				title={t('agent.attach')}
				disabled={busy || disabled}
				onclick={() => picker?.click()}><Icon name="clip" size={16} /></button
			>
		{/if}
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
				disabled={disabled || (!text.trim() && !attachments.length)}
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
	.attach {
		background: none;
		border: 1px solid var(--border);
		color: var(--muted);
		width: 32px;
		height: 32px;
	}
	/* The clip opens a menu: same round look as the plain button. */
	.attach-menu :global(.btn) {
		width: 32px;
		height: 32px;
		min-height: 32px;
		padding: 0;
		border-radius: 50%;
		background: none;
		border: 1px solid var(--border);
		color: var(--muted);
	}
	.files {
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
	}
	.file {
		display: inline-flex;
		align-items: center;
		gap: 5px;
		padding: 2px 4px 2px 8px;
		border-radius: 999px;
		background: var(--chip);
		font-size: var(--text-sm);
		max-width: 100%;
		overflow-wrap: anywhere;
	}
	.file button {
		border: 0;
		background: none;
		color: var(--muted);
		cursor: pointer;
		padding: 2px;
		display: grid;
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
	/* Under 16px iOS zooms into the field on a tap and stays zoomed. */
	@media (max-width: 760px), (pointer: coarse) {
		textarea,
		.pill select {
			font-size: max(16px, 1rem);
		}
	}
	@media (max-width: 760px) {
		.attach-menu :global(.btn) {
			width: 40px;
			height: 40px;
			min-height: 40px;
		}
		.round {
			width: 40px;
			height: 40px;
		}
		.pill .muted {
			display: none;
		}
	}
</style>
