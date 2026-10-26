<script lang="ts">
	// A change the assistant proposes. Nothing happens until the person
	// confirms it here; then the browser calls the same route as the button
	// in the UI would. The dangerous button is on the left, as in dialogs;
	// "Reject" comes first in the markup and so gets the focus.
	import { assistant, rejectAction, runAction, type ChatAction } from '#lib/assistant.svelte.ts';
	import { t, tOr, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import Dialog from './Dialog.svelte';
	import Icon from './Icon.svelte';

	let { action, index }: { action: ChatAction; index: number } = $props();

	let ticked = $state(false);
	// A destroying action asks once more in a plain dialog; the API still
	// wants the target name, which is sent after the click.
	let asking = $state(false);
	const id = $props.id();
	// The tick and the open dialog belong to one stored action; another
	// action in the same place starts unconfirmed.
	let shown = '';
	$effect.pre(() => {
		if (action.id !== shown) {
			shown = action.id;
			ticked = false;
			asking = false;
		}
	});

	const title = $derived.by(() => {
		const key = `assistant.action.${action.key}`;
		return tOr(key, '') ? t(key as MessageKey, action.params ?? {}) : action.key;
	});
	const task = $derived(action.key === 'task');
	const ready = $derived(action.level === 'normal' || (action.level === 'danger' && ticked) || action.level === 'destroy');
	const pending = $derived(action.state === 'pending');
	const after = $derived((action.after ?? []).map((name) => (name === 'attachment' ? t('assistant.afterAttachment') : name)).join(', '));
</script>

<div class="card" class:danger={action.level !== 'normal'} class:done={!pending} role="group" aria-labelledby="{id}-title">
	<div class="head">
		<Icon name={task ? 'list' : action.level === 'normal' ? 'play' : 'power'} size={16} />
		<strong id="{id}-title">{title}</strong>
	</div>
	{#if action.key === 'api.change'}<p class="mono small route">{action.params?.method} {action.params?.path}</p>{/if}
	{#if task && action.steps?.length}
		<ol class="steps">{#each action.steps as step, i (i)}<li>{step}</li>{/each}</ol>
	{/if}
	{#if action.params?.preview}
		<details class="preview" open={action.key === 'files.move'}><summary>{t('assistant.preview')}</summary><pre class="mono">{action.params.preview}</pre></details>
	{/if}
	{#if action.reason && !task}<p class="reason"><span class="muted">{t('assistant.reason')}</span> {action.reason}</p>{/if}
	{#if after && pending}<p class="notice-warn small" role="note">{t('assistant.after', { tools: after })}</p>{/if}

	{#if action.state === 'running'}
		<p class="state" role="status">{t('assistant.autoRunning')}</p>
	{:else if pending}
		{#if task}<p class="small muted">{t('assistant.taskNote')}</p>{/if}
		{#if action.level === 'danger'}
			<label class="tick">
				<input type="checkbox" bind:checked={ticked} />
				<span>{t('assistant.confirmDanger')}</span>
			</label>
		{/if}
		<div class="buttons">
			<button type="button" class="btn" onclick={() => rejectAction(index)} disabled={assistant.busy}>{t('assistant.reject')}</button>
			<button type="button" class="btn {action.level === 'normal' ? 'btn-primary' : action.level === 'danger' ? 'btn-warn' : 'btn-danger'} go" onclick={() => (action.level === 'destroy' ? (asking = true) : runAction(index))} disabled={!ready || assistant.busy}>{t(task ? 'assistant.approvePlan' : 'assistant.run')}</button>
		</div>
		{#if asking}
			<Dialog title={t('assistant.destroyTitle')} locked={assistant.busy} onclose={() => (asking = false)}>
				<p class="notice-error">{t('assistant.destroyText', { action: title, name: action.confirm ?? '' })}</p>
				<div class="dialog-actions">
					<button type="button" class="btn" onclick={() => (asking = false)} disabled={assistant.busy}>{t('common.cancel')}</button>
					<button type="button" class="btn btn-danger" disabled={assistant.busy} onclick={() => ((asking = false), runAction(index, action.confirm ?? ''))}>{t('assistant.run')}</button>
				</div>
			</Dialog>
		{/if}
	{:else}
		<p class="state state-{action.state}" role="status">
			{t(task && action.state === 'done' ? 'assistant.planApproved' : (`assistant.state.${action.state}` as MessageKey))}{#if action.auto && action.state === 'done'} · {t('assistant.inTask')}{/if}{#if action.state === 'failed' && action.result}: <span class="mono">{action.result}</span>{/if}
		</p>
	{/if}
</div>

<style>
	.card {
		display: grid;
		gap: 8px;
		padding: 10px 12px;
		border: 1px solid var(--accent);
		border-radius: var(--radius-ctl);
		background: var(--surface);
	}
	.card.danger {
		border-color: var(--status-warn);
	}
	.card.done {
		border-color: var(--border);
	}
	.head {
		display: flex;
		align-items: center;
		gap: 8px;
	}
	p {
		margin: 0;
	}
	.small {
		font-size: 0.75rem;
	}
	.tick {
		display: flex;
		align-items: flex-start;
		gap: 8px;
		font-size: 0.8125rem;
	}
	.tick input {
		margin-top: 2px;
	}
	.buttons {
		display: flex;
		gap: 8px;
	}
	/* The action stands left of "Reject". */
	.go {
		order: -1;
	}
	.route {
		color: var(--muted);
		overflow-wrap: anywhere;
	}
	.steps {
		margin: 0;
		padding-left: 20px;
		font-size: 0.8125rem;
	}
	.preview summary {
		cursor: pointer;
		font-size: 0.75rem;
		color: var(--muted);
	}
	.preview pre {
		margin: 6px 0 0;
		max-height: 220px;
		overflow: auto;
		padding: 8px;
		background: var(--chip);
		border-radius: var(--radius-small);
		font-size: 0.75rem;
		white-space: pre-wrap;
		overflow-wrap: anywhere;
	}
	.state {
		font-size: 0.8125rem;
		color: var(--muted);
		overflow-wrap: anywhere;
	}
	.state-done {
		color: var(--status-ok);
	}
	.state-failed {
		color: var(--status-error);
	}
</style>
