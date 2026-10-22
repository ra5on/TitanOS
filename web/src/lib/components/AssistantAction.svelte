<script lang="ts">
	// A change the assistant proposes. Nothing happens until the person
	// confirms it here; then the browser calls the same route as the button
	// in the UI would. The dangerous button is on the left, as in dialogs;
	// "Reject" comes first in the markup and so gets the focus.
	import { assistant, rejectAction, runAction, type ChatAction } from '#lib/assistant.svelte.ts';
	import { t, tOr, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import Icon from './Icon.svelte';

	let { action, index }: { action: ChatAction; index: number } = $props();

	let ticked = $state(false);
	let typed = $state('');
	const id = $props.id();

	const title = $derived.by(() => {
		const key = `assistant.action.${action.key}`;
		return tOr(key, '') ? t(key as MessageKey, action.params ?? {}) : action.key;
	});
	const ready = $derived(action.level === 'normal' || (action.level === 'danger' && ticked) || (action.level === 'destroy' && typed === action.confirm));
	const pending = $derived(action.state === 'pending');
	const after = $derived((action.after ?? []).map((name) => (name === 'attachment' ? t('assistant.afterAttachment') : name)).join(', '));
</script>

<div class="card" class:danger={action.level !== 'normal'} class:done={!pending} role="group" aria-labelledby="{id}-title">
	<div class="head">
		<Icon name={action.level === 'normal' ? 'play' : 'power'} size={16} />
		<strong id="{id}-title">{title}</strong>
	</div>
	{#if action.reason}<p class="reason"><span class="muted">{t('assistant.reason')}</span> {action.reason}</p>{/if}
	{#if after && pending}<p class="notice-warn small" role="note">{t('assistant.after', { tools: after })}</p>{/if}

	{#if pending}
		{#if action.level === 'danger'}
			<label class="tick">
				<input type="checkbox" bind:checked={ticked} />
				<span>{t('assistant.confirmDanger')}</span>
			</label>
		{:else if action.level === 'destroy'}
			<label class="typed">
				<span>{t('assistant.confirmDestroy', { name: action.confirm ?? '' })}</span>
				<input class="field mono" bind:value={typed} autocomplete="off" spellcheck="false" autocapitalize="none" />
			</label>
		{/if}
		<div class="buttons">
			<button type="button" class="btn" onclick={() => rejectAction(index)} disabled={assistant.busy}>{t('assistant.reject')}</button>
			<button type="button" class="btn {action.level === 'normal' ? 'btn-primary' : action.level === 'danger' ? 'btn-warn' : 'btn-danger'} go" onclick={() => runAction(index, typed)} disabled={!ready || assistant.busy}>{t('assistant.run')}</button>
		</div>
	{:else}
		<p class="state state-{action.state}" role="status">
			{t(`assistant.state.${action.state}` as MessageKey)}{#if action.state === 'failed' && action.result}: <span class="mono">{action.result}</span>{/if}
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
	.typed {
		display: grid;
		gap: 6px;
		font-size: 0.8125rem;
	}
	.buttons {
		display: flex;
		gap: 8px;
	}
	/* The action stands left of "Reject". */
	.go {
		order: -1;
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
