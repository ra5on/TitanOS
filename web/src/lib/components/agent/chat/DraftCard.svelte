<script lang="ts">
	// 1.13: a draft of the agent ("Nur Vorschläge"): what it would have
	// written, nothing saved or sent. "Öffnen und bearbeiten" opens the
	// program's editor with it; the person finishes it there.
	import Icon from '#lib/components/Icon.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import type { AgentDraft } from '#lib/agent.ts';
	import { draftHref, draftList } from '#lib/agent/drafts.ts';
	import { openInProgram } from '#lib/agent/open.ts';

	let {
		draft,
		chatId,
		entryId,
		readonly = false,
	}: { draft: AgentDraft; chatId: string; entryId: string; readonly?: boolean } = $props();

	const f = $derived(draft.fields ?? {});
	const text = (v: unknown) => (typeof v === 'string' ? v : '');
	const title = $derived.by(() => {
		switch (draft.app) {
			case 'mail':
				return draft.kind === 'move' ? t('agentpim.draft.move') : t('agentpim.draft.mail');
			case 'calendar':
				return f.id ? t('agentpim.draft.eventChange') : t('agentpim.draft.event');
			case 'contacts':
				return f.id ? t('agentpim.draft.contactChange') : t('agentpim.draft.contact');
			case 'tasks':
				return draft.kind === 'complete' ? t('agentpim.draft.taskDone') : t('agentpim.draft.task');
		}
		return t('agentpim.draft.other');
	});
	// The lines of the card: what the editor will hold (whole, wrapped).
	const lines = $derived.by(() => {
		const out: [string, string][] = [];
		const add = (label: string, v: string) => {
			if (v.trim()) out.push([label, v]);
		};
		if (draft.app === 'mail' && draft.kind !== 'move') {
			add(t('agentpim.field.to'), draftList(f.to).join(', '));
			add(t('agentpim.field.cc'), draftList(f.cc).join(', '));
			add(t('agentpim.field.bcc'), draftList(f.bcc).join(', '));
			add(t('agentpim.field.subject'), text(f.subject));
		} else if (draft.app === 'mail') {
			add(t('agentpim.field.folder'), text(f.to));
		} else if (draft.app === 'calendar') {
			add(t('agentpim.field.title'), text(f.title));
			add(t('agentpim.field.start'), text(f.start));
			add(t('agentpim.field.location'), text(f.location));
		} else if (draft.app === 'contacts') {
			add(t('agentpim.field.name'), text(f.name));
			add(t('agentpim.field.org'), text(f.org));
		} else {
			add(t('agentpim.field.title'), text(f.title));
			add(t('agentpim.field.due'), text(f.due));
		}
		return out;
	});
	const body = $derived(draft.app === 'mail' ? text(f.text) : text(f.notes));
</script>

<div class="draft" role="group" aria-labelledby="draft-{entryId}">
	<div class="head">
		<span class="ic" aria-hidden="true"><Icon name="edit" size={16} /></span>
		<div class="title">
			<strong id="draft-{entryId}">{title}</strong>
			<span class="muted">{t('agentpim.draft.note')}</span>
		</div>
		{#if !readonly}
			<button
				type="button"
				class="btn btn-primary"
				onclick={() => openInProgram(draftHref(draft, chatId, entryId))}
				>{t('agentpim.draft.open')}</button
			>
		{/if}
	</div>
	{#if lines.length}
		<dl>
			{#each lines as [k, v] (k)}<dt>{k}</dt>
				<dd>{v}</dd>{/each}
		</dl>
	{/if}
	{#if body}<p class="body">{body}</p>{/if}
</div>

<style>
	.draft {
		border: 1px solid var(--border);
		border-radius: 18px;
		background: var(--surface);
		padding: var(--space-3) var(--space-4);
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}
	.head {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2) var(--space-3);
	}
	.ic {
		display: inline-flex;
		color: var(--accent);
	}
	.title {
		flex: 1 1 12rem;
		min-width: 0;
		display: flex;
		flex-direction: column;
		overflow-wrap: anywhere;
	}
	.muted {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	dl {
		display: grid;
		grid-template-columns: auto minmax(0, 1fr);
		gap: 2px var(--space-3);
		margin: 0;
	}
	dt {
		color: var(--muted);
	}
	dd {
		margin: 0;
		overflow-wrap: anywhere;
	}
	.body {
		margin: 0;
		white-space: pre-wrap;
		overflow-wrap: anywhere;
		max-height: 14rem;
		overflow-y: auto;
		padding: var(--space-2);
		border-radius: 10px;
		background: color-mix(in srgb, var(--text) 4%, transparent);
	}
</style>
