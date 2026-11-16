<script lang="ts">
	// A step that waits for the person (PLAN 68): a ring with "Wartet auf
	// deine Freigabe", then one banner card – the title in plain words, the
	// level written out (not only a color), when it was asked, "Abbrechen"
	// and "Ausführen", and "Details" with the tool, its arguments and the
	// content. Nothing here takes the focus or reacts to Enter.
	import { formatDateTime } from '#lib/format.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import type { Pending } from '#lib/agent.ts';
	import { argLines, askTitle, levelText, preview } from './chat-text.svelte.ts';

	let {
		pending: p,
		deciding = false,
		ondecide,
	}: {
		pending: Pending;
		/** An answer is on its way: the buttons wait. */
		deciding?: boolean;
		ondecide: (pending: Pending, approve: boolean) => void;
	} = $props();
	const lines = $derived(argLines(p.args));
	const shown = $derived(preview(p.args));
</script>

<!-- Not working, waiting: a ring instead of the running dots. -->
<p class="live-line wait">
	<span class="ring" aria-hidden="true"></span>{t('agent.waitsForYou')}
</p>
<div
	class="approval"
	class:danger={p.level !== 'normal'}
	role="group"
	aria-labelledby="pending-{p.id}"
>
	<div class="ask">
		<span class="ask-ic" aria-hidden="true">{p.level === 'normal' ? '✎' : '▲'}</span>
		<div class="ask-text">
			<strong id="pending-{p.id}">{askTitle(p)}</strong>
			<span
				>{t('agent.ask.level', {
					level: t(levelText[p.level] ?? 'agent.levelChange'),
				})} · {t('agent.ask.at', { time: formatDateTime(new Date(p.at)) })}</span
			>
		</div>
		<div class="ask-acts">
			<button type="button" class="btn" disabled={deciding} onclick={() => ondecide(p, false)}
				>{t('agent.decline')}</button
			>
			<button
				type="button"
				class="btn {p.level === 'destroy' ? 'btn-danger' : 'btn-primary'}"
				disabled={deciding}
				onclick={() => ondecide(p, true)}>{t('agent.approve')}</button
			>
		</div>
	</div>
	<details class="ask-details" open={!!shown}>
		<summary>{t('agent.ask.details')}</summary>
		<dl>
			<dt>{t('agent.ask.tool')}</dt>
			<dd>{p.tool}{p.summary && p.tool !== 'code_run' ? ' — ' + p.summary : ''}</dd>
			{#each lines as [k, v] (k)}<dt>{k}</dt>
				<dd>{v}</dd>{/each}
		</dl>
		{#if shown}<pre>{shown}</pre>{/if}
	</details>
</div>

<style>
	.live-line {
		display: flex;
		align-items: center;
		gap: var(--space-2-5);
		margin: 0;
		min-height: 32px;
		font-size: 0.95em;
		color: var(--muted);
	}
	.ring {
		flex: none;
		width: 14px;
		height: 14px;
		box-sizing: border-box;
		border-radius: 50%;
		border: 2px solid var(--status-warn, var(--ag-warm));
		background: radial-gradient(circle, var(--status-warn, var(--ag-warm)) 38%, transparent 44%);
	}
	.live-line,
	.approval {
		animation: come 0.32s cubic-bezier(0.22, 1, 0.36, 1) both;
	}
	@keyframes come {
		from {
			opacity: 0;
			transform: translateY(6px);
		}
	}
	:global(.agent.calm) :is(.live-line, .approval) {
		animation: none;
	}
	@media (prefers-reduced-motion: reduce) {
		.live-line,
		.approval {
			animation: none;
		}
	}
	/* The step that waits for approval: one banner card. */
	.approval {
		border: 1px solid var(--warn-border);
		border-radius: 20px;
		background: var(--surface);
		overflow: hidden;
	}
	.ask {
		display: flex;
		align-items: center;
		flex-wrap: wrap;
		gap: var(--space-2-5) var(--space-3);
		padding: var(--space-3) var(--space-4);
	}
	.ask-ic {
		flex: none;
		font-size: 16px;
		color: var(--warn-text);
	}
	.ask-text {
		flex: 1 1 16rem;
		min-width: 0;
		display: flex;
		flex-direction: column;
		gap: 2px;
	}
	.ask-text strong {
		font-size: 1.05em;
		font-weight: 600;
		overflow-wrap: anywhere;
	}
	.ask-acts .btn {
		border-radius: 999px;
		padding-left: var(--space-4);
		padding-right: var(--space-4);
	}
	.ask-text span {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.ask-acts {
		display: flex;
		gap: var(--space-2);
		margin-left: auto;
	}
	.ask-details {
		border-top: 1px solid var(--border);
		font-size: var(--text-sm);
	}
	.ask-details summary {
		cursor: pointer;
		padding: var(--space-1-5) var(--space-4);
		color: var(--muted);
	}
	.ask-details dl {
		display: grid;
		grid-template-columns: max-content 1fr;
		gap: 2px var(--space-3);
		margin: 0;
		padding: 0 var(--space-4) var(--space-2-5);
	}
	.ask-details dt {
		color: var(--muted);
	}
	.ask-details dd {
		margin: 0;
		overflow-wrap: anywhere;
	}
	.ask-details pre {
		margin: 0 var(--space-4) var(--space-3);
		max-height: 320px;
		overflow: auto;
		background: var(--bg);
		padding: var(--space-2);
		border-radius: var(--radius-small);
		white-space: pre-wrap;
	}
	@media (max-width: 760px) {
		.ask-acts {
			width: 100%;
			justify-content: flex-end;
		}
	}
</style>
