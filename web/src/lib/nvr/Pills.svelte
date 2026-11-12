<script lang="ts" generics="T extends string">
	// A choice of several things as pills that switch on and off (drawing
	// 79-nvr: "✓ Personen  ✓ Fahrzeuge  Tiere"). The pills wrap; a pill that
	// cannot be chosen says why.
	import Icon from '../components/Icon.svelte';

	let {
		options,
		chosen,
		label,
		disabled = false,
		ontoggle,
	}: {
		options: { value: T; label: string; disabled?: boolean; why?: string }[];
		chosen: T[];
		/** Names the group for screen readers. */
		label: string;
		disabled?: boolean;
		ontoggle: (value: T, on: boolean) => void;
	} = $props();
</script>

<div class="pills" role="group" aria-label={label}>
	{#each options as option (option.value)}
		{@const on = chosen.includes(option.value)}
		<button
			type="button"
			class="pill"
			class:on
			aria-pressed={on}
			disabled={disabled || option.disabled}
			title={option.why}
			onclick={() => ontoggle(option.value, !on)}
			>{#if on}<Icon name="check" size={14} />{/if}<span>{option.label}</span></button
		>
	{/each}
</div>

<style>
	.pills {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2);
		justify-content: flex-end;
		min-width: 0;
	}
	.pill {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1);
		min-height: 32px;
		padding: 4px 12px;
		border: 1px solid var(--border);
		border-radius: 999px;
		background: var(--surface);
		color: var(--muted);
		font: inherit;
		font-size: var(--text-sm);
		cursor: pointer;
		max-width: 100%;
		text-align: left;
	}
	.pill span {
		overflow-wrap: anywhere;
	}
	.pill.on {
		border-color: var(--accent);
		background: color-mix(in srgb, var(--accent) 14%, var(--surface));
		color: var(--text);
	}
	.pill:disabled {
		opacity: 0.55;
		cursor: not-allowed;
	}
	.pill:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	@media (max-width: 640px) {
		.pills {
			justify-content: flex-start;
		}
	}
</style>
