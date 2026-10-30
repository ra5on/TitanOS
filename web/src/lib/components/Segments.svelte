<script lang="ts" generics="T extends string">
	// A small choice of two to five modes as one row of buttons (radiogroup).
	let {
		options,
		value = $bindable(),
		label,
		disabled = false,
		onchange,
	}: {
		options: { value: T; label: string }[];
		value: T;
		label: string;
		disabled?: boolean;
		onchange?: (value: T) => void;
	} = $props();
</script>

<div class="segments" role="radiogroup" aria-label={label}>
	{#each options as option (option.value)}
		<button
			type="button"
			role="radio"
			aria-checked={value === option.value}
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
		gap: 4px;
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
		font-size: 0.8125rem;
		cursor: pointer;
	}
	.segments button[aria-checked='true'] {
		background: var(--accent);
		color: var(--on-accent);
	}
	.segments button:disabled {
		cursor: default;
		opacity: 0.6;
	}
</style>
