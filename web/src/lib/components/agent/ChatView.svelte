<script lang="ts">
	// One chat of the KI-Agent: the person's messages, the steps of each turn
	// folded into one box ("5 Schritte · 6 s"), the thoughts, the answers
	// (safe Markdown), questions with buttons, the card of a step that waits
	// for approval and, while it runs, what streams in.
	import Icon from '#lib/components/Icon.svelte';
	import { chatMarkdown } from '#lib/chat-markdown.ts';
	import { formatDateTime } from '#lib/format.ts';
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import {
		blocks,
		stepDetail,
		stepFailed,
		stepWord,
		type Chat,
		type Entry,
		type Pending,
	} from '#lib/agent.ts';

	let {
		chat,
		live,
		busy = false,
		deciding = false,
		ondecide,
		onchoice,
		onretry,
	}: {
		chat: Chat;
		live: { thinking: string; content: string; step: Entry | null };
		busy?: boolean;
		deciding?: boolean;
		ondecide: (pending: Pending, approve: boolean) => void;
		onchoice: (choice: string) => void;
		onretry?: () => void;
	} = $props();

	let scroller = $state<HTMLDivElement>();
	let pinned = $state(true);
	const list = $derived(blocks(chat.entries));
	// Only the last answer offers its buttons; older questions are answered.
	const lastAnswer = $derived.by(() => {
		for (let i = list.length - 1; i >= 0; i--) {
			const b = list[i];
			if (b.kind === 'user') return '';
			if (b.kind === 'answer') return b.entry.id;
		}
		return '';
	});

	function scrolled() {
		if (!scroller) return;
		pinned = scroller.scrollHeight - scroller.scrollTop - scroller.clientHeight < 80;
	}
	// Stay at the bottom while text streams in, unless the person scrolled up.
	$effect(() => {
		void chat.entries.length;
		void live.content;
		void live.thinking;
		void chat.pending;
		if (pinned && scroller)
			queueMicrotask(() => scroller!.scrollTo({ top: scroller!.scrollHeight }));
	});

	function stepLabel(e: Entry): string {
		if (e.role === 'note')
			return t(e.note === 'approved' ? 'agent.noteApproved' : 'agent.noteDeclined');
		const word = stepWord(e.tool);
		return word ? t(`agent.step.${word}` as MessageKey) : (e.tool ?? '').replaceAll('_', ' ');
	}
	function seconds(steps: Entry[]): string {
		const ms = steps.reduce((sum, s) => sum + (s.durationMs ?? 0), 0);
		return ms < 1000 ? '' : ` · ${Math.round(ms / 1000)} s`;
	}
	const noteText: Record<string, MessageKey> = {
		memory: 'agent.noteMemory',
		skill: 'agent.noteSkill',
		schedule: 'agent.noteSchedule',
		stopped: 'agent.noteStopped',
		interrupted: 'agent.noteInterrupted',
		error: 'agent.noteError',
		limit: 'agent.noteLimit',
	};
	const levelText: Record<string, MessageKey> = {
		normal: 'agent.levelChange',
		danger: 'agent.levelDanger',
		destroy: 'agent.levelDestroy',
	};
	function argLines(args: unknown): [string, string][] {
		if (!args || typeof args !== 'object') return [];
		return Object.entries(args as Record<string, unknown>)
			.filter(([k]) => k !== 'content' && k !== 'sheets')
			.map(([k, v]) => [k, typeof v === 'string' ? v : JSON.stringify(v)] as [string, string])
			.map(([k, v]) => [k, v.length > 160 ? v.slice(0, 159) + '…' : v]);
	}
	function preview(args: unknown): string {
		if (!args || typeof args !== 'object') return '';
		const c = (args as Record<string, unknown>).content;
		return typeof c === 'string' ? (c.length > 600 ? c.slice(0, 599) + '…' : c) : '';
	}
</script>

<div class="messages" bind:this={scroller} onscroll={scrolled}>
	<div class="column">
		{#each list as block, i (block.kind === 'steps' ? `s${i}` : block.entry.id)}
			{#if block.kind === 'user'}
				<div class="user">{block.entry.content}</div>
			{:else if block.kind === 'steps'}
				{#if block.thinking}
					<details class="thinking">
						<summary><Icon name="bulb" size={14} /> {t('agent.thoughts')}</summary>
						<p>{block.thinking}</p>
					</details>
				{/if}
				{#if block.steps.length}
					<details class="steps" open={i === list.length - 1 && busy}>
						<summary>
							<span class="ok">✓</span>
							{t('agent.steps', {
								count: block.steps.filter((s) => s.role === 'tool').length,
							})}{seconds(block.steps)}
						</summary>
						{#each block.steps as step (step.id)}
							<div class="step" class:failed={stepFailed(step)}>
								<span class="mark" aria-hidden="true">{stepFailed(step) ? '!' : '✓'}</span>
								<span class="what">{stepLabel(step)}</span>
								{#if stepDetail(step.args)}<code>{stepDetail(step.args)}</code>{/if}
								{#if stepFailed(step)}<span class="sr-only">{t('agent.stepFailed')}</span>{/if}
								{#if step.durationMs}<span class="time"
										>{(step.durationMs / 1000).toFixed(1).replace('.', ',')} s</span
									>{/if}
							</div>
						{/each}
					</details>
				{/if}
			{:else if block.kind === 'answer'}
				<div class="answer">
					{#if block.entry.content}
						<!-- eslint-disable-next-line svelte/no-at-html-tags -- chatMarkdown escapes everything first -->
						<div class="md">{@html chatMarkdown(block.entry.content)}</div>
					{/if}
					{#if block.entry.choices?.length && block.entry.id === lastAnswer && !busy}
						<div class="choices">
							{#each block.entry.choices as choice, n (choice)}
								<button
									type="button"
									class="btn"
									class:btn-primary={n === block.entry.choices.length - 1}
									onclick={() => onchoice(choice)}>{choice}</button
								>
							{/each}
						</div>
					{/if}
				</div>
			{:else if block.kind === 'note'}
				{#if block.entry.note === 'memory' || block.entry.note === 'skill' || block.entry.note === 'schedule'}
					<span class="learn">✦ {t(noteText[block.entry.note])}: {block.entry.content}</span>
				{:else}
					<div class="note" class:bad={block.entry.note === 'error'}>
						{t(noteText[block.entry.note ?? ''] ?? 'agent.noteError', {
							count: block.entry.content ?? '',
						})}
						{#if block.entry.note === 'error' && block.entry.content}<span class="muted">
								· {block.entry.content}</span
							>{/if}
						{#if (block.entry.note === 'error' || block.entry.note === 'interrupted' || block.entry.note === 'limit') && i === list.length - 1 && onretry}
							<button type="button" class="btn" onclick={onretry}>{t('agent.continue')}</button>
						{/if}
					</div>
				{/if}
			{/if}
		{/each}

		{#each chat.entries.filter((e) => e.role === 'tool' && e.files?.length) as step (step.id)}
			{#each step.files ?? [] as file (file.root + file.path)}
				<a
					class="file tile"
					href={`/files?root=${encodeURIComponent(file.root)}&path=${encodeURIComponent(file.path.split('/').slice(0, -1).join('/'))}`}
				>
					<span class="doc" aria-hidden="true">{file.path.split('.').pop()?.toUpperCase()}</span>
					<span class="meta">
						<strong>{file.path.split('/').pop()}</strong>
						<span class="muted">{file.root.replace(/^share\//, '')} › {file.path}</span>
					</span>
					<span class="btn">{t('agent.openInFiles')}</span>
				</a>
			{/each}
		{/each}

		{#if busy}
			{#if live.thinking && !live.content}
				<p class="shimmer">{t('agent.thinking')}</p>
			{/if}
			{#if live.step}
				<div class="live-step">
					<span class="spin" aria-hidden="true"></span>
					{stepLabel(live.step)}
					{#if stepDetail(live.step.args)}<code>{stepDetail(live.step.args)}</code>{/if}
				</div>
			{/if}
			{#if live.content}
				<!-- eslint-disable-next-line svelte/no-at-html-tags -- chatMarkdown escapes everything first -->
				<div class="answer md">{@html chatMarkdown(live.content)}</div>
			{:else if !live.thinking && !live.step}
				<p class="shimmer">{t('agent.working')}</p>
			{/if}
		{/if}

		{#if chat.pending && !busy}
			{@const p = chat.pending}
			<div class="approval" role="group" aria-labelledby="pending-{p.id}">
				<div class="row">
					<strong id="pending-{p.id}">{t('agent.approvalTitle')}</strong>
					<span class="chip {p.level === 'normal' ? '' : 'warn'}"
						>{t(levelText[p.level] ?? 'agent.levelChange')}</span
					>
				</div>
				<p class="what">{p.tool.replaceAll('_', ' ')}{p.summary ? ' — ' + p.summary : ''}</p>
				<dl>
					{#each argLines(p.args) as [k, v] (k)}<dt>{k}</dt>
						<dd>{v}</dd>{/each}
				</dl>
				{#if preview(p.args)}<pre>{preview(p.args)}</pre>{/if}
				<div class="row">
					<button
						type="button"
						class="btn btn-primary"
						disabled={deciding}
						onclick={() => ondecide(p, true)}>{t('agent.approve')}</button
					>
					<button type="button" class="btn" disabled={deciding} onclick={() => ondecide(p, false)}
						>{t('agent.decline')}</button
					>
					<span class="muted small">{formatDateTime(new Date(p.at))}</span>
				</div>
			</div>
		{/if}
	</div>
</div>

<style>
	.messages {
		flex: 1;
		overflow-y: auto;
		padding: var(--space-4) var(--space-5);
		overscroll-behavior: contain;
	}
	.column {
		max-width: 800px;
		margin: 0 auto;
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
	}
	.user {
		align-self: flex-end;
		max-width: 78%;
		padding: 9px 14px;
		border-radius: 18px 18px 5px 18px;
		background: var(--ag-gradient, var(--accent));
		color: var(--on-accent);
		white-space: pre-wrap;
		overflow-wrap: anywhere;
	}
	:global(.agent[data-style='clean']) .user {
		background: var(--chip);
		color: var(--text);
		border: 1px solid var(--border);
	}
	:global(.agent[data-style='compact']) .user {
		background: transparent;
		color: var(--text);
		border-left: 3px solid var(--accent);
		border-radius: 0;
		align-self: stretch;
		max-width: none;
		padding: 2px 10px;
	}
	:global(.agent[data-style='compact']) .column {
		gap: var(--space-2);
	}
	.answer {
		line-height: 1.55;
		overflow-wrap: anywhere;
	}
	.md :global(p) {
		margin: 0 0 0.5em;
	}
	.md :global(table) {
		border-collapse: collapse;
		width: 100%;
		font-size: var(--text-base);
		margin: 4px 0;
		display: block;
		overflow-x: auto;
	}
	.md :global(th),
	.md :global(td) {
		text-align: left;
		padding: 5px 8px;
		border-bottom: 1px solid var(--border);
	}
	.md :global(th) {
		color: var(--muted);
		font-weight: 500;
	}
	.md :global(code),
	code {
		font-family: var(--font-mono, ui-monospace, monospace);
		font-size: 0.9em;
		background: var(--chip);
		padding: 1px 5px;
		border-radius: var(--radius-small);
		overflow-wrap: anywhere;
	}
	.md :global(ul),
	.md :global(ol) {
		margin: 0 0 0.5em;
		padding-left: 1.3em;
	}
	.md :global(h1),
	.md :global(h2),
	.md :global(h3) {
		font-size: var(--text-md);
		margin: 0.6em 0 0.3em;
	}
	.thinking,
	.steps {
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		font-size: var(--text-base);
		overflow: hidden;
	}
	.thinking {
		border-style: dashed;
	}
	.thinking p {
		margin: 0;
		padding: 8px 12px;
		color: var(--muted);
		white-space: pre-wrap;
		max-height: 260px;
		overflow-y: auto;
	}
	summary {
		cursor: pointer;
		padding: 6px 12px;
		color: var(--muted);
		display: flex;
		align-items: center;
		gap: 8px;
		background: color-mix(in srgb, var(--text) 3%, transparent);
	}
	.step {
		display: flex;
		align-items: center;
		gap: 8px;
		padding: 6px 12px;
		border-top: 1px solid var(--border);
		flex-wrap: wrap;
	}
	.step .time {
		margin-left: auto;
		color: var(--muted);
		font-size: var(--text-xs);
	}
	.ok,
	.mark {
		color: var(--ok-text);
	}
	.failed .mark {
		color: var(--warn-text);
		font-weight: 700;
	}
	.failed .what {
		color: var(--warn-text);
	}
	.choices {
		display: flex;
		gap: var(--space-2);
		flex-wrap: wrap;
		margin-top: 6px;
	}
	.learn {
		align-self: flex-start;
		font-size: var(--text-xs);
		padding: 4px 10px;
		border-radius: 999px;
		border: 1px solid color-mix(in srgb, var(--accent) 40%, transparent);
		background: color-mix(in srgb, var(--accent) 12%, transparent);
	}
	.note {
		font-size: var(--text-base);
		color: var(--muted);
		display: flex;
		align-items: center;
		gap: var(--space-2);
		flex-wrap: wrap;
	}
	.note.bad {
		color: var(--warn-text);
	}
	.file {
		display: flex;
		align-items: center;
		gap: var(--space-3);
		padding: var(--space-3);
		text-decoration: none;
		color: var(--text);
	}
	.file .doc {
		width: 38px;
		height: 44px;
		border-radius: var(--radius-small);
		display: grid;
		place-items: center;
		background: var(--accent);
		color: var(--on-accent);
		font-weight: 700;
		font-size: var(--text-xs);
		flex: none;
	}
	.file .meta {
		display: flex;
		flex-direction: column;
		min-width: 0;
		flex: 1;
	}
	.file .meta .muted {
		font-size: var(--text-sm);
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.live-step {
		display: flex;
		align-items: center;
		gap: 8px;
		font-size: var(--text-base);
		color: var(--muted);
	}
	.spin {
		width: 13px;
		height: 13px;
		border-radius: 50%;
		border: 2px solid var(--border);
		border-top-color: var(--accent);
		animation: spin 0.9s linear infinite;
		flex: none;
	}
	@keyframes spin {
		to {
			transform: rotate(360deg);
		}
	}
	.shimmer {
		margin: 0;
		font-weight: 500;
		background: linear-gradient(
			90deg,
			var(--muted) 0%,
			var(--text) 40%,
			var(--accent) 50%,
			var(--text) 60%,
			var(--muted) 100%
		);
		background-size: 200% 100%;
		-webkit-background-clip: text;
		background-clip: text;
		color: transparent;
		animation: shimmer 2.2s linear infinite;
	}
	@keyframes shimmer {
		from {
			background-position: 100% 0;
		}
		to {
			background-position: -100% 0;
		}
	}
	:global(.agent.calm) .shimmer,
	:global(.agent.calm) .spin {
		animation: none;
	}
	@media (prefers-reduced-motion: reduce) {
		.shimmer,
		.spin {
			animation: none;
		}
	}
	.approval {
		border: 1px solid var(--warn-border);
		background: var(--warn-bg);
		border-radius: var(--radius-ctl);
		padding: var(--space-3);
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}
	.approval .row {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		flex-wrap: wrap;
	}
	.approval .what {
		margin: 0;
	}
	.approval dl {
		display: grid;
		grid-template-columns: max-content 1fr;
		gap: 2px 12px;
		margin: 0;
		font-size: var(--text-sm);
	}
	.approval dt {
		color: var(--muted);
	}
	.approval dd {
		margin: 0;
		overflow-wrap: anywhere;
	}
	.approval pre {
		margin: 0;
		max-height: 160px;
		overflow: auto;
		background: var(--bg);
		padding: 8px;
		border-radius: var(--radius-small);
		font-size: var(--text-sm);
		white-space: pre-wrap;
	}
	.small {
		font-size: var(--text-xs);
	}
	@media (max-width: 760px) {
		.messages {
			padding: var(--space-3);
		}
		.user {
			max-width: 86%;
		}
	}
</style>
