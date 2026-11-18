<script lang="ts">
	// 1.11 (step 15): which events of a series a change or a deletion means
	// (draft 07): only this one, this and all following, the whole series.
	// `only` limits the choice (moving a series to another calendar is
	// always the whole series).
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import type { Scope } from '../calendar-api.ts';

	type Props = {
		value: Scope;
		/** delete or save: the hints differ. */
		mode: 'delete' | 'save';
		/** The number of events of the whole series, when the server knows it. */
		total?: number;
		only?: Scope[];
		label: string;
	};
	let { value = $bindable(), mode, total, only, label }: Props = $props();
	const id = $props.id();

	const rows = $derived(
		(
			[
				['this', 'calendar.scope.this', `calendar.scope.${mode}.thisHint`],
				['following', 'calendar.scope.following', `calendar.scope.${mode}.followingHint`],
				['all', 'calendar.scope.all', `calendar.scope.${mode}.allHint`],
			] as [Scope, MessageKey, string][]
		).filter(([v]) => !only || only.includes(v)),
	);
	function hint(v: Scope, key: string): string {
		if (v === 'all' && mode === 'delete' && total)
			return t('calendar.scope.delete.allHintN', { n: total });
		return t(key as MessageKey);
	}
</script>

<div class="choices" role="radiogroup" aria-label={label}>
	{#each rows as [v, title, key] (v)}
		<label class="choice" class:on={value === v}>
			<input type="radio" name="scope-{id}" value={v} bind:group={value} />
			<span><b>{t(title)}</b><small>{hint(v, key)}</small></span>
		</label>
	{/each}
</div>

<style>
	.choices {
		display: flex;
		flex-direction: column;
		gap: 3px;
		margin: var(--space-3) 0;
	}
	.choice {
		display: flex;
		align-items: flex-start;
		gap: var(--space-2);
		min-height: 44px;
		padding: var(--space-2) var(--space-3);
		border: 1px solid transparent;
		border-radius: var(--radius-ctl);
		background: var(--srow-bg);
		cursor: pointer;
	}
	.choice.on {
		border-color: var(--accent);
	}
	.choice:has(input:focus-visible) {
		outline: 2px solid var(--accent);
		outline-offset: 1px;
	}
	input {
		margin-top: 3px;
		accent-color: var(--accent);
		flex: none;
	}
	span {
		display: flex;
		flex-direction: column;
		min-width: 0;
		overflow-wrap: break-word;
	}
	small {
		color: var(--muted);
		font-size: var(--text-sm);
	}
</style>
