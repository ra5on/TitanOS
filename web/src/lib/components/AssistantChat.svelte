<script lang="ts">
	// The open chat: messages, what the model read, cards for actions, and
	// the field for the next question. Used in the side panel and on the
	// page /assistant.
	import {
		assistant,
		chosenModel,
		readAttachment,
		regenerate,
		send,
		setPrefs,
		stop,
		type ChatAttachment,
		type ChatEntry
	} from '#lib/assistant.svelte.ts';
	import { chatMarkdown } from '#lib/chat-markdown.ts';
	import { errorText, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import { page } from '$app/state';
	import AssistantAction from './AssistantAction.svelte';
	import NasFilePicker from './NasFilePicker.svelte';
	import Icon from './Icon.svelte';

	let input = $state('');
	let attachments = $state<ChatAttachment[]>([]);
	let attachError = $state('');
	let field = $state<HTMLTextAreaElement>();
	let picker = $state<HTMLInputElement>();
	let log = $state<HTMLElement>();
	let editing = $state<{ index: number; text: string } | null>(null);
	let copied = $state(-1);
	let picking = $state(false);

	const admin = $derived(['owner', 'admin'].includes(session.data?.user?.role ?? ''));
	const suggestions = $derived<MessageKey[]>(
		admin
			? ['assistant.suggest.disks', 'assistant.suggest.backups', 'assistant.suggest.errors', 'assistant.suggest.howto', 'assistant.suggest.sort']
			: ['assistant.suggest.shares', 'assistant.suggest.files', 'assistant.suggest.sort']
	);
	const chosen = $derived(chosenModel());
	const models = $derived.by(() => {
		const names = assistant.models.map((m) => m.name);
		return chosen && !names.includes(chosen) ? [chosen, ...names] : names;
	});
	const vision = $derived(assistant.models.find((m) => m.name === chosen)?.capabilities.includes('vision') ?? false);
	const lastAnswer = $derived(assistant.messages.findLastIndex((m) => m.role === 'assistant' && !!m.content));
	const lastQuestion = $derived(assistant.messages.findLastIndex((m) => m.role === 'user'));
	const reading = $derived.by(() => {
		const last = assistant.messages.at(-1);
		return assistant.busy && last?.role === 'tool' && !last.action ? (last.tool ?? '') : '';
	});
	const currentPage = $derived(assistant.fromPage);

	function submit() {
		if (assistant.busy || !input.trim()) return;
		const text = input;
		const files = attachments;
		input = '';
		attachments = [];
		if (field) field.style.height = '';
		send(text, files);
	}

	function keydown(event: KeyboardEvent) {
		if (event.key === 'Enter' && !event.shiftKey && !event.isComposing) {
			event.preventDefault();
			submit();
		}
	}

	function grow() {
		if (!field) return;
		field.style.height = '';
		field.style.height = `${Math.min(field.scrollHeight, 200)}px`;
	}

	async function attach(event: Event) {
		attachError = '';
		const files = Array.from((event.currentTarget as HTMLInputElement).files ?? []);
		for (const file of files) {
			if (attachments.length >= 4) break;
			try {
				attachments.push(await readAttachment(file));
			} catch (err) {
				attachError = errorText(err instanceof Error && 'code' in err ? String(err.code) : 'internal');
			}
		}
		if (picker) picker.value = '';
	}

	function saveEdit() {
		if (!editing || !editing.text.trim()) return;
		const { index, text } = editing;
		const files = assistant.messages[index]?.attachments ?? [];
		editing = null;
		send(text, files, index);
	}

	async function copy(text: string, index: number) {
		try {
			await navigator.clipboard.writeText(text);
			copied = index;
			setTimeout(() => (copied = -1), 1500);
		} catch {
			// No clipboard (plain HTTP): nothing to do.
		}
	}

	function details(entry: ChatEntry): string {
		const args = entry.args && JSON.stringify(entry.args) !== '{}' ? JSON.stringify(entry.args) + '\n\n' : '';
		const text = entry.content.length > 3000 ? entry.content.slice(0, 3000) + ' …' : entry.content;
		return args + text;
	}

	// New words at the bottom stay in view, unless the person scrolled up.
	let follow = true;
	function scrolled() {
		if (log) follow = log.scrollHeight - log.scrollTop - log.clientHeight < 80;
	}
	$effect(() => {
		assistant.messages.length;
		assistant.draft.content;
		assistant.draft.thinking;
		assistant.chatId;
		if (log && follow) log.scrollTop = log.scrollHeight;
	});
	$effect(() => {
		assistant.chatId;
		follow = true;
		editing = null;
	});
	$effect(() => {
		if (page.url.pathname === '/assistant') field?.focus();
	});
</script>

<div class="chat">
	<div class="log" bind:this={log} onscroll={scrolled} aria-live="polite">
		{#each assistant.messages as message, index (index)}
			{#if message.role === 'user'}
				{#if editing?.index === index}
					<div class="edit">
						<textarea class="field" bind:value={editing.text} rows="3" aria-label={t('assistant.editQuestion')}></textarea>
						<div class="row">
							<button type="button" class="btn" onclick={() => (editing = null)}>{t('common.cancel')}</button>
							<button type="button" class="btn btn-primary" onclick={saveEdit} disabled={!editing.text.trim()}>{t('assistant.askAgain')}</button>
						</div>
					</div>
				{:else}
					<div class="msg user">
						{message.content}
						{#if message.attachments?.length}
							<div class="files">{#each message.attachments as file (file.name)}<span class="file"><Icon name={file.kind === 'image' ? 'image' : 'fileText'} size={12} />{file.name}</span>{/each}</div>
						{/if}
					</div>
					{#if !assistant.busy}
						<div class="under right">
							<button type="button" class="mini" title={t('assistant.editQuestion')} aria-label={t('assistant.editQuestion')} onclick={() => (editing = { index, text: message.content })}><Icon name="edit" size={13} /></button>
						</div>
					{/if}
				{/if}
			{:else if message.role === 'tool'}
				{#if message.action}
					<AssistantAction action={message.action} {index} />
				{:else}
					<details class="step" class:failed={!message.ok}>
						<summary><span class="mono">{message.tool}</span>{message.ok ? '' : ` – ${t('assistant.toolFailed')}`}</summary>
						<pre class="mono">{details(message)}</pre>
					</details>
				{/if}
			{:else}
				{#if message.thinking}
					<details class="think"><summary>{t('assistant.thoughts')}</summary><p>{message.thinking}</p></details>
				{/if}
				{#if message.problem}
					<div class="msg answer problem" role="alert">
						<p>{errorText(message.problem.code)}</p>
						{#if message.problem.detail}<p class="detail mono">{message.problem.detail}</p>{/if}
					</div>
				{:else if message.content}
					<!-- eslint-disable-next-line svelte/no-at-html-tags -- escaped in chatMarkdown -->
					<div class="msg answer text">{@html chatMarkdown(message.content)}</div>
					{#if message.sources?.length}
						<div class="sources">
							<span class="muted">{t('assistant.sources')}</span>
							{#each message.sources as source (source.url)}
								<a href={source.url} target="_blank" rel="noopener noreferrer" title={source.url}>{source.title || new URL(source.url).hostname} <span class="muted">({new URL(source.url).hostname})</span></a>
							{/each}
						</div>
					{/if}
				{/if}
				{#if message.stopped}<p class="muted small">{t('assistant.stopped')}</p>{/if}
				{#if message.content && !assistant.busy}
					<div class="under">
						<button type="button" class="mini" title={t('assistant.copy')} aria-label={t('assistant.copy')} onclick={() => copy(message.content, index)}><Icon name={copied === index ? 'check' : 'copy'} size={13} /></button>
						{#if index === lastAnswer && lastAnswer > lastQuestion}
							<button type="button" class="mini" title={t('assistant.regenerate')} aria-label={t('assistant.regenerate')} onclick={regenerate}><Icon name="refresh" size={13} /></button>
						{/if}
					</div>
				{/if}
			{/if}
		{:else}
			<div class="empty">
				<p>{t(admin ? 'assistant.intro' : 'assistant.introUser')}</p>
				<div class="suggestions">
					{#each suggestions as key (key)}<button type="button" class="btn" onclick={() => send(t(key))}>{t(key)}</button>{/each}
				</div>
			</div>
		{/each}

		{#if assistant.busy}
			{#if assistant.draft.thinking}
				<details class="think" open><summary>{t('assistant.thoughts')}</summary><p>{assistant.draft.thinking}</p></details>
			{/if}
			{#if assistant.draft.content}
				<!-- eslint-disable-next-line svelte/no-at-html-tags -- escaped in chatMarkdown -->
				<div class="msg answer text">{@html chatMarkdown(assistant.draft.content)}</div>
			{:else}
				<p class="muted working" role="status">{reading ? t('assistant.reading', { tool: reading }) : t('assistant.thinking')}</p>
			{/if}
		{/if}
		{#if assistant.error}<p class="notice-error" role="alert">{errorText(assistant.error)}</p>{/if}
	</div>

	<form onsubmit={(event) => { event.preventDefault(); submit(); }}>
		{#if attachments.length}
			<div class="files pending">
				{#each attachments as file, i (i)}
					<span class="file">
						<Icon name={file.kind === 'image' ? 'image' : file.kind === 'nas' ? 'folder' : 'fileText'} size={12} />{file.name}
						<button type="button" class="mini" aria-label={t('assistant.removeAttachment', { name: file.name })} onclick={() => attachments.splice(i, 1)}><Icon name="close" size={11} /></button>
					</span>
				{/each}
			</div>
			{#if attachments.some((a) => a.kind === 'image' || (a.kind === 'nas' && /\.(png|jpe?g|webp|gif)$/i.test(a.name))) && !vision}<p class="muted small">{t('assistant.noVision')}</p>{/if}
		{/if}
		{#if attachError}<p class="notice-error small" role="alert">{attachError}</p>{/if}
		<div class="compose">
			<input bind:this={picker} type="file" multiple hidden accept="image/png,image/jpeg,image/webp,.txt,.md,.json,.yaml,.yml,.log,.csv,.conf,.cfg,.ini,.toml,.xml,.sh,.py,.js,.ts" onchange={attach} />
			<button type="button" class="icon" aria-label={t('assistant.attach')} title={t('assistant.attach')} onclick={() => picker?.click()} disabled={attachments.length >= 4}><Icon name="upload" size={16} /></button>
			<button type="button" class="icon" aria-label={t('assistant.attachNas')} title={t('assistant.attachNas')} onclick={() => (picking = true)} disabled={attachments.length >= 4}><Icon name="folder" size={16} /></button>
			<textarea bind:this={field} bind:value={input} class="field" rows="1" placeholder={t('assistant.placeholder')} aria-label={t('assistant.message')} onkeydown={keydown} oninput={grow}></textarea>
			{#if assistant.busy}
				<button type="button" class="btn send" aria-label={t('assistant.stop')} title={t('assistant.stop')} onclick={stop}><Icon name="stop" size={16} /></button>
			{:else}
				<button type="submit" class="btn btn-primary send" aria-label={t('assistant.send')} title={t('assistant.send')} disabled={!input.trim()}><Icon name="send" size={16} /></button>
			{/if}
		</div>
		<div class="options">
			<label class="model">
				<span class="muted">{t('assistant.model')}</span>
				<select class="field" value={chosen} onchange={(event) => setPrefs({ model: event.currentTarget.value })} disabled={assistant.busy}>
					{#each models as model (model)}<option value={model}>{model}</option>{:else}<option value="">{t('assistant.noModels')}</option>{/each}
				</select>
			</label>
			{#if currentPage}
				<label class="context" title={t('assistant.pageContextHint')}>
					<input type="checkbox" checked={assistant.prefs.pageContext} onchange={(event) => setPrefs({ pageContext: event.currentTarget.checked })} />
					<span class="muted">{t('assistant.pageContext')}</span>
				</label>
			{/if}
		</div>
		<p class="note muted">{t(assistant.status?.persistent ? 'assistant.note' : 'assistant.noteMemory')}</p>
	</form>
</div>

{#if picking}
	<NasFilePicker max={4 - attachments.length} onclose={() => (picking = false)} onpick={(files) => { attachments.push(...files); picking = false; }} />
{/if}

<style>
	.chat {
		flex: 1;
		min-height: 0;
		display: flex;
		flex-direction: column;
		font-size: 0.875rem;
	}
	.log {
		flex: 1;
		min-height: 0;
		overflow-y: auto;
		display: flex;
		flex-direction: column;
		gap: 8px;
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
	.msg p,
	.think p {
		margin: 0;
	}
	.text :global(p),
	.text :global(ul),
	.text :global(ol),
	.text :global(pre),
	.text :global(.table) {
		margin: 0 0 8px;
	}
	.text :global(:last-child) {
		margin-bottom: 0;
	}
	.text :global(h4) {
		margin: 4px 0 6px;
		font-size: 0.875rem;
	}
	.text :global(ul),
	.text :global(ol) {
		padding-left: 20px;
	}
	.text :global(code) {
		font-family: var(--font-mono);
		font-size: 0.8125rem;
	}
	.text :global(pre) {
		padding: 8px;
		overflow-x: auto;
		background: var(--surface);
		border-radius: var(--radius-small);
	}
	.text :global(.table) {
		overflow-x: auto;
	}
	.text :global(table) {
		border-collapse: collapse;
		font-size: 0.8125rem;
	}
	.text :global(th),
	.text :global(td) {
		padding: 4px 8px;
		border-bottom: 1px solid var(--border);
		text-align: left;
	}
	.text :global(a) {
		color: var(--accent);
	}
	.detail {
		margin-top: 6px !important;
		font-size: 0.75rem;
	}
	.small {
		margin: 0;
		font-size: 0.75rem;
	}
	.step,
	.think {
		font-size: 0.75rem;
		color: var(--muted);
	}
	.step summary,
	.think summary {
		cursor: pointer;
		width: fit-content;
		padding: 2px 8px;
		border: 1px solid var(--border);
		border-radius: var(--radius-small);
	}
	.step.failed summary {
		border-color: var(--status-warn);
	}
	.step pre {
		margin: 6px 0 0;
		max-height: 240px;
		overflow: auto;
		padding: 8px;
		background: var(--chip);
		border-radius: var(--radius-small);
		white-space: pre-wrap;
		overflow-wrap: anywhere;
	}
	.think p {
		margin-top: 6px;
		padding-left: 10px;
		border-left: 2px solid var(--border);
		white-space: pre-wrap;
	}
	.sources {
		display: flex;
		flex-wrap: wrap;
		gap: 4px 10px;
		font-size: 0.75rem;
	}
	.sources a {
		color: var(--accent);
		overflow-wrap: anywhere;
	}
	.under {
		display: flex;
		gap: 2px;
		margin-top: -4px;
	}
	.under.right {
		justify-content: flex-end;
	}
	.mini {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		width: 24px;
		height: 24px;
		padding: 0;
		background: transparent;
		border: 1px solid transparent;
		border-radius: var(--radius-small);
		color: var(--muted);
		cursor: pointer;
	}
	.mini:hover {
		border-color: var(--border);
		color: var(--text);
	}
	.edit {
		display: grid;
		gap: 6px;
	}
	.row {
		display: flex;
		justify-content: flex-end;
		gap: 6px;
	}
	.files {
		display: flex;
		flex-wrap: wrap;
		gap: 4px;
		margin-top: 6px;
	}
	.file {
		display: inline-flex;
		align-items: center;
		gap: 4px;
		padding: 1px 6px;
		border-radius: var(--radius-small);
		background: var(--chip);
		color: var(--text);
		font-size: 0.75rem;
	}
	.files.pending {
		margin: 0 0 8px;
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
	.working {
		margin: 0;
	}
	form {
		padding: 10px 14px 6px;
		border-top: 1px solid var(--border);
	}
	.compose {
		display: flex;
		align-items: flex-end;
		gap: 8px;
	}
	textarea {
		flex: 1;
		min-height: 40px;
		max-height: 200px;
		height: auto;
		padding: 9px 12px;
		resize: none;
	}
	.edit textarea {
		resize: vertical;
	}
	.icon,
	.send {
		flex: none;
		width: 40px;
		height: 40px;
		padding: 0;
		justify-content: center;
	}
	.icon {
		display: flex;
		align-items: center;
		background: transparent;
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		color: var(--text);
		cursor: pointer;
	}
	.icon:disabled {
		opacity: 0.45;
		cursor: default;
	}
	.options {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 6px 14px;
		margin-top: 8px;
		font-size: 0.75rem;
	}
	.model {
		display: flex;
		align-items: center;
		gap: 8px;
		flex: 1;
		min-width: 180px;
	}
	.model select {
		flex: 1;
		min-width: 0;
		height: 30px;
		font-size: 0.75rem;
	}
	.context {
		display: flex;
		align-items: center;
		gap: 6px;
	}
	.note {
		margin: 6px 0 0;
		font-size: 0.6875rem;
	}
</style>
