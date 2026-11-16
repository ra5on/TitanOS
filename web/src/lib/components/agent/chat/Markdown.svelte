<script lang="ts">
	// The text of an answer in a chat of the KI-Agent: safe Markdown (it is
	// escaped first, chatMarkdown), tables in a card with "Tabelle kopieren",
	// code in a card with its language, "Kopieren" and "Vergrößern". Also
	// the answer that streams in (`streaming`, with `answer` for its look).
	import { chatMarkdown } from '#lib/chat-markdown.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { copyText } from './chat-text.svelte.ts';

	let {
		text,
		streaming = false,
		answer = false,
	}: {
		text: string;
		/** The text still streams in (an open code block is shown as such). */
		streaming?: boolean;
		/** Looks like an answer on its own (the text that streams in). */
		answer?: boolean;
	} = $props();

	let root = $state<HTMLDivElement>();

	// ----- tables and code: a button that copies them -----
	// A table stays a table on the phone too (owner 15 Nov 2026: like the
	// chat apps); one that is wider than the screen scrolls sideways.
	function tableText(table: HTMLTableElement): string {
		return [...table.rows]
			.map((r) => [...r.cells].map((c) => (c.textContent ?? '').trim()).join('\t'))
			.join('\n');
	}
	function copyButton(label: string, value: () => string): HTMLButtonElement {
		const button = document.createElement('button');
		button.type = 'button';
		button.textContent = label;
		button.addEventListener('click', () => {
			void copyText(value()).then((ok) => {
				button.textContent = ok ? t('agent.copiedShort') : t('agent.copyFailed');
				setTimeout(() => (button.textContent = label), 1800);
			});
		});
		return button;
	}
	$effect(() => {
		void text;
		if (!root) return;
		for (const wrap of root.querySelectorAll<HTMLElement>('.table:not([data-done])')) {
			const table = wrap.querySelector('table');
			if (!table) continue;
			wrap.dataset.done = 'true';
			const foot = document.createElement('div');
			foot.className = 'table-foot';
			foot.append(copyButton(t('agent.tableCopy'), () => tableText(table)));
			wrap.after(foot);
		}
		for (const head of root.querySelectorAll<HTMLElement>('.code-head:not([data-done])')) {
			const code = head.parentElement?.querySelector('code');
			if (!code) continue;
			head.dataset.done = 'true';
			// "Vergrößern" lays the block over the whole chat, "Verkleinern"
			// (or Escape) puts it back.
			const box = head.parentElement!;
			const grow = document.createElement('button');
			grow.type = 'button';
			grow.textContent = t('agent.codeGrow');
			grow.addEventListener('click', () => {
				const full = box.classList.toggle('full');
				grow.textContent = full ? t('agent.codeShrink') : t('agent.codeGrow');
			});
			const tools = document.createElement('span');
			tools.className = 'code-tools';
			tools.append(
				copyButton(t('agent.copy'), () => code.textContent ?? ''),
				grow,
			);
			head.append(tools);
		}
	});
</script>

<!-- eslint-disable-next-line svelte/no-at-html-tags -- chatMarkdown escapes everything first -->
<div class="md" class:answer bind:this={root}>{@html chatMarkdown(text, streaming)}</div>

<style>
	.md.answer {
		line-height: 1.6;
		overflow-wrap: anywhere;
	}
	/* Under a table: copy it. */
	.md :global(.table-foot) {
		display: flex;
		justify-content: flex-end;
		margin: calc(-1 * var(--space-2)) 0 var(--space-3);
	}
	.md :global(.table-foot button) {
		min-height: 28px;
		padding: 0 var(--space-1);
		border: 0;
		border-radius: var(--radius-small, 6px);
		background: none;
		color: var(--muted);
		font: inherit;
		font-size: var(--text-sm);
		cursor: pointer;
	}
	.md :global(.table-foot button:hover) {
		color: var(--text);
		background: color-mix(in srgb, var(--text) 7%, transparent);
	}
	.md :global(p) {
		margin: 0 0 0.75em;
	}
	.md :global(a) {
		color: var(--ag-link, var(--accent));
	}
	.md :global(li) {
		margin: 0.4em 0;
	}
	.md :global(li::marker) {
		color: var(--muted);
	}
	.md :global(.table) {
		margin: var(--space-1) 0 var(--space-3);
		border: 1px solid var(--border);
		border-radius: 14px;
		background: none;
		overflow-x: auto;
	}
	.md :global(.table table) {
		width: 100%;
		overflow-wrap: normal;
	}
	/* Words stay whole in a table (owner 15 Nov 2026: "Docker-Ap / ps",
	   "System-Ü / bersicht" in a narrow first column): the readable page
	   lets cells break anywhere, here a word only breaks at a hyphen or
	   when it alone is wider than the table. */
	.md :global(.table :is(th, td)) {
		padding: 9px 14px;
		font-size: var(--text-base);
		overflow-wrap: break-word;
		word-break: normal;
		hyphens: manual;
	}
	.md :global(.table th) {
		font-size: inherit;
		font-weight: 600;
		background: color-mix(in srgb, var(--text) 4%, transparent);
	}
	.md :global(.table tr:last-child td) {
		border-bottom: 0;
	}
	/* A block of code: its language and "Kopieren" in the head; a long
	   line scrolls sideways instead of breaking. */
	.md :global(.code) {
		margin: var(--space-1) 0 var(--space-3);
		border: 1px solid var(--border);
		border-radius: 14px;
		background: var(--surface);
		overflow: hidden;
	}
	.md :global(.code-head) {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-2);
		min-height: 38px;
		padding: 0 var(--space-1-5) 0 14px;
		border-bottom: 1px solid var(--border);
		color: var(--muted);
		font-family: var(--font-mono, ui-monospace, monospace);
		font-size: var(--text-sm);
	}
	.md :global(.code-head button) {
		min-height: 30px;
		padding: 0 var(--space-2);
		border: 0;
		border-radius: 8px;
		background: none;
		color: var(--muted);
		font: inherit;
		font-family: var(--font-sans, inherit);
		cursor: pointer;
	}
	.md :global(.code-head button:hover) {
		color: var(--text);
		background: color-mix(in srgb, var(--text) 7%, transparent);
	}
	.md :global(.code pre) {
		margin: 0;
		padding: var(--space-3) 14px;
		overflow-x: auto;
		white-space: pre;
		line-height: 1.5;
		font-size: var(--text-base);
	}
	.md :global(.code-tools) {
		display: inline-flex;
		gap: 2px;
	}
	/* Enlarged: over the whole chat, with larger text; lines still scroll. */
	.md :global(.code.full) {
		position: fixed;
		z-index: 70;
		inset: 0;
		margin: 0;
		border: 0;
		border-radius: 0;
		display: flex;
		flex-direction: column;
		padding-top: var(--agent-top, 0px);
		background: var(--bg);
	}
	.md :global(.code.full pre) {
		flex: 1;
		overflow: auto;
		font-size: var(--text-md);
		padding-bottom: calc(env(safe-area-inset-bottom, 0px) + var(--space-4));
	}
	/* The colors of the code (lib/code-highlight.ts), mixed with the text
	   color so they hold on a light and a dark theme. */
	.md :global(.t-c) {
		color: var(--muted);
		font-style: italic;
	}
	.md :global(.t-a) {
		color: color-mix(in srgb, #d9905f 82%, var(--text));
	}
	.md :global(.t-s) {
		color: color-mix(in srgb, #6fae6c 82%, var(--text));
	}
	.md :global(.t-k) {
		color: color-mix(in srgb, #6f9ff0 82%, var(--text));
	}
	.md :global(.t-n),
	.md :global(.t-v) {
		color: color-mix(in srgb, #b98ae6 80%, var(--text));
	}
	.md :global(.code pre code) {
		padding: 0;
		background: none;
		font-size: inherit;
		overflow-wrap: normal;
	}
	.md :global(code) {
		font-family: var(--font-mono, ui-monospace, monospace);
		font-size: 0.9em;
		background: var(--chip);
		padding: 1px 5px;
		border-radius: var(--radius-small);
		overflow-wrap: anywhere;
	}
	.md :global(ul),
	.md :global(ol) {
		margin: 0 0 0.6em;
		padding-left: 1.3em;
	}
	.md :global(h1),
	.md :global(h2),
	.md :global(h3) {
		font-size: var(--text-md);
		margin: 0.6em 0 0.3em;
	}
	/* What can be pressed changes colour softly, not at once (0.100). */
	.md :global(.code-head button),
	.md :global(.table-foot button) {
		transition:
			background-color 0.16s ease,
			color 0.16s ease,
			border-color 0.16s ease;
	}
	@media (max-width: 760px) {
		.md :global(.table :is(th, td)) {
			padding: var(--space-2-5) var(--space-2-5);
		}
	}
</style>
