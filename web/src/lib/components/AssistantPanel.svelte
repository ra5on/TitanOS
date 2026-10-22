<script lang="ts">
	// The chat with the assistant, at the right edge of every page. It reads
	// the state of the NAS with the same tools as agents and changes nothing.
	import { ask, assistant, clearChat, setModel } from '#lib/assistant.svelte.ts';
	import { chatMarkdown } from '#lib/chat-markdown.ts';
	import { errorText, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import Icon from './Icon.svelte';

	let input = $state('');
	let field = $state<HTMLTextAreaElement>();
	let log = $state<HTMLElement>();

	const suggestions: MessageKey[] = ['assistant.suggest.disks', 'assistant.suggest.containers', 'assistant.suggest.backups', 'assistant.suggest.errors', 'assistant.suggest.temperatures'];

	// The model of the settings, unless one was picked in this tab.
	const chosen = $derived(assistant.model || assistant.settings?.model || '');
	const models = $derived(chosen && !assistant.models.includes(chosen) ? [chosen, ...assistant.models] : assistant.models);

	function send(text: string) {
		input = '';
		if (field) field.style.height = '';
		ask(text);
	}

	function keydown(event: KeyboardEvent) {
		if (event.key === 'Enter' && !event.shiftKey && !event.isComposing) {
			event.preventDefault();
			send(input);
		}
	}

	function grow() {
		if (!field) return;
		field.style.height = '';
		field.style.height = `${Math.min(field.scrollHeight, 180)}px`;
	}

	// New words at the bottom stay in view.
	$effect(() => {
		assistant.messages.length;
		assistant.step;
		assistant.messages.at(-1)?.content;
		if (log) log.scrollTop = log.scrollHeight;
	});
	$effect(() => {
		if (assistant.open) field?.focus();
	});
</script>

<svelte:window onkeydown={(event) => { if (event.key === 'Escape' && assistant.open && !event.defaultPrevented) assistant.open = false; }} />

{#if assistant.open}
	<aside class="panel" aria-label={t('assistant.title')}>
		<div class="head">
			<Icon name="chat" size={18} />
			<h2>{t('assistant.title')}</h2>
			<span class="grow"></span>
			<button type="button" class="icon" aria-label={t('assistant.new')} title={t('assistant.new')} onclick={clearChat} disabled={assistant.busy || assistant.messages.length === 0}><Icon name="plus" size={16} /></button>
			<a class="icon" href="/settings/assistant" aria-label={t('assistant.settings')} title={t('assistant.settings')}><Icon name="tool" size={16} /></a>
			<button type="button" class="icon" aria-label={t('assistant.close')} title={t('assistant.close')} onclick={() => (assistant.open = false)}><Icon name="close" size={16} /></button>
		</div>
		<label class="model">
			<span class="muted">{t('assistant.model')}</span>
			<select class="field" value={chosen} onchange={(event) => setModel(event.currentTarget.value)} disabled={assistant.busy}>
				{#each models as model (model)}<option value={model}>{model}</option>{:else}<option value="">{t('assistant.noModels')}</option>{/each}
			</select>
		</label>

		<div class="log" bind:this={log} aria-live="polite">
			{#each assistant.messages as message, index (index)}
				{#if message.role === 'user'}
					<div class="msg user">{message.content}</div>
				{:else}
					<div class="msg answer" class:problem={!!message.problem}>
						{#if message.tools?.length}
							<div class="tools">{#each message.tools as tool, i (i)}<span class="tool mono" class:failed={!tool.ok}>{tool.name}{tool.ok ? '' : ' ✗'}</span>{/each}</div>
						{/if}
						{#if message.problem}
							<p>{errorText(message.problem.code)}</p>
							{#if message.problem.detail}<p class="detail mono">{message.problem.detail}</p>{/if}
						{:else if message.content}
							<!-- eslint-disable-next-line svelte/no-at-html-tags -- escaped in chatMarkdown -->
							<div class="text">{@html chatMarkdown(message.content)}</div>
						{:else}
							<p class="muted" role="status">{assistant.step ? t('assistant.reading', { tool: assistant.step }) : t('assistant.thinking')}</p>
						{/if}
					</div>
				{/if}
			{:else}
				<div class="empty">
					<p>{t('assistant.intro')}</p>
					<div class="suggestions">
						{#each suggestions as key (key)}<button type="button" class="btn" onclick={() => send(t(key))}>{t(key)}</button>{/each}
					</div>
				</div>
			{/each}
		</div>

		<form onsubmit={(event) => { event.preventDefault(); send(input); }}>
			<textarea bind:this={field} bind:value={input} class="field" rows="1" placeholder={t('assistant.placeholder')} aria-label={t('assistant.message')} onkeydown={keydown} oninput={grow}></textarea>
			<button type="submit" class="btn btn-primary send" aria-label={t('assistant.send')} title={t('assistant.send')} disabled={assistant.busy || !input.trim()}><Icon name="send" size={16} /></button>
		</form>
		<p class="note muted">{t('assistant.note')}</p>
	</aside>
{/if}

<style>
	.panel {
		position: fixed;
		top: 60px;
		right: 0;
		bottom: 0;
		z-index: 30;
		width: min(440px, 100vw);
		display: flex;
		flex-direction: column;
		background: var(--surface);
		border-left: 1px solid var(--border);
		box-shadow: var(--shadow-menu);
		font-size: 0.875rem;
	}
	.head {
		display: flex;
		align-items: center;
		gap: 8px;
		padding: 12px 14px;
		border-bottom: 1px solid var(--border);
	}
	h2 {
		margin: 0;
		font-size: 0.9375rem;
		font-weight: 600;
	}
	.grow {
		flex: 1;
	}
	.icon {
		display: flex;
		align-items: center;
		justify-content: center;
		width: 32px;
		height: 32px;
		padding: 0;
		background: transparent;
		border: 1px solid transparent;
		border-radius: var(--radius-ctl);
		color: var(--text);
		cursor: pointer;
	}
	.icon:hover:not(:disabled) {
		border-color: var(--border);
	}
	.icon:disabled {
		opacity: 0.45;
		cursor: default;
	}
	.model {
		display: flex;
		align-items: center;
		gap: 10px;
		padding: 10px 14px;
		border-bottom: 1px solid var(--border);
		font-size: 0.8125rem;
	}
	.model select {
		flex: 1;
		min-width: 0;
	}
	.log {
		flex: 1;
		overflow-y: auto;
		display: flex;
		flex-direction: column;
		gap: 10px;
		padding: 14px;
	}
	.msg {
		padding: 9px 12px;
		border-radius: var(--radius-ctl);
		overflow-wrap: anywhere;
	}
	.msg.user {
		align-self: flex-end;
		max-width: 88%;
		background: var(--accent);
		color: var(--on-accent);
		white-space: pre-wrap;
	}
	.msg.answer {
		background: var(--chip);
	}
	.msg.problem {
		border: 1px solid var(--status-error);
	}
	.msg p {
		margin: 0;
	}
	.text :global(p) {
		margin: 0 0 8px;
	}
	.text :global(p:last-child) {
		margin-bottom: 0;
	}
	.text :global(ul) {
		margin: 0 0 8px;
		padding-left: 18px;
	}
	.text :global(code) {
		font-family: var(--font-mono);
		font-size: 0.8125rem;
	}
	.text :global(pre) {
		margin: 0 0 8px;
		padding: 8px;
		overflow-x: auto;
		background: var(--surface);
		border-radius: var(--radius-small);
	}
	.detail {
		margin-top: 6px !important;
		font-size: 0.75rem;
	}
	.tools {
		display: flex;
		flex-wrap: wrap;
		gap: 4px;
		margin-bottom: 6px;
	}
	.tool {
		padding: 1px 6px;
		border: 1px solid var(--border);
		border-radius: var(--radius-small);
		color: var(--muted);
		font-size: 0.6875rem;
	}
	.tool.failed {
		border-color: var(--status-warn);
	}
	.empty {
		display: flex;
		flex-direction: column;
		gap: 12px;
		color: var(--muted);
	}
	.empty p {
		margin: 0;
	}
	.suggestions {
		display: flex;
		flex-direction: column;
		align-items: flex-start;
		gap: 6px;
	}
	.suggestions .btn {
		text-align: left;
		white-space: normal;
		height: auto;
		padding: 6px 10px;
	}
	form {
		display: flex;
		align-items: flex-end;
		gap: 8px;
		padding: 12px 14px 6px;
		border-top: 1px solid var(--border);
	}
	textarea {
		flex: 1;
		min-height: 40px;
		max-height: 180px;
		height: auto;
		padding: 9px 12px;
		resize: none;
	}
	.send {
		width: 40px;
		height: 40px;
		padding: 0;
		justify-content: center;
	}
	.note {
		margin: 0;
		padding: 0 14px 10px;
		font-size: 0.6875rem;
	}
</style>
