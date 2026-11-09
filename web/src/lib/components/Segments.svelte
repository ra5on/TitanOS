<script lang="ts" generics="T extends string">
	// A small choice of two to five modes as one row of buttons (radiogroup).
	let {
		options,
		value = $bindable(),
		label,
		disabled = false,
		compact = false,
		onchange,
	}: {
		/** `hint`: the full name when the label is short ("S"), for screen readers and the tooltip. */
		options: { value: T; label: string; hint?: string }[];
		value: T;
		label: string;
		disabled?: boolean;
		/** PLAN 68: the quiet form in rows (gray track, the choice raised and bold). */
		compact?: boolean;
		onchange?: (value: T) => void;
	} = $props();
</script>

<div class="segments" class:compact role="radiogroup" aria-label={label}>
	{#each options as option (option.value)}
		<button
			type="button"
			role="radio"
			aria-checked={value === option.value}
			aria-label={option.hint}
			title={option.hint}
			{disabled}
			onclick={() => {
				value = option.value;
				onchange?.(option.value);
			}}>{option.label}</button
		>
	{/each}
</div>

<style>
	.segments {
		display: inline-flex;
		flex-wrap: wrap;
		align-self: flex-start;
		gap: var(--space-1);
		padding: 3px;
		border: 1px solid var(--panel-border, var(--border));
		border-radius: var(--radius-ctl);
	}
	.segments button {
		min-height: calc(var(--control-height, 2.25rem) - 6px);
		padding: 0 14px;
		background: transparent;
		border: 0;
		border-radius: var(--radius-ctl);
		color: var(--text);
		font: inherit;
		font-size: var(--text-base);
		cursor: pointer;
	}
	.segments button[aria-checked='true'] {
		background: var(--accent);
		color: var(--on-accent);
	}
	.segments.compact {
		gap: 2px;
		border: 0;
		background: var(--chip);
	}
	.compact button {
		min-height: 26px;
		padding: 0 var(--space-2-5);
		border-radius: calc(var(--radius-ctl) - 2px);
		font-size: var(--text-sm);
		white-space: nowrap;
	}
	.compact button[aria-checked='true'] {
		background: var(--surface);
		color: var(--text);
		font-weight: 600;
		box-shadow: 0 1px 2px rgb(0 0 0 / 0.3);
	}
	.segments button:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 1px;
	}
	.segments button:disabled {
		cursor: default;
		opacity: 0.6;
	}
</style>
