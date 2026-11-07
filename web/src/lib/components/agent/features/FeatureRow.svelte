<script lang="ts">
	// One function in the list (PLAN 54): icon, name, what it does, a switch
	// for owners and administrators, a tap opens its settings.
	type Props = {
		icon: string;
		color: string;
		title: string;
		sub?: string;
		on?: boolean;
		/** Without a switch (e.g. for accounts that cannot change it). */
		toggle?: boolean;
		ontoggle?: (on: boolean) => void;
		onopen?: () => void;
	};
	// PLAN 68: colors from the theme only; `color` is kept for the callers.
	let { icon, title, sub, on = false, toggle = true, ontoggle, onopen }: Props = $props();
</script>

<div class="frow">
	<button type="button" class="open" onclick={onopen} disabled={!onopen}>
		<span class="ic" aria-hidden="true">{icon}</span>
		<span class="t"
			><strong>{title}</strong>{#if sub}<small>{sub}</small>{/if}</span
		>
	</button>
	{#if toggle}
		<input
			type="checkbox"
			class="switch"
			role="switch"
			checked={on}
			aria-label={title}
			onchange={(e) => ontoggle?.(e.currentTarget.checked)}
		/>
	{/if}
</div>

<style>
	/* A row like every settings row: name and hint left, switch right. */
	.frow {
		display: flex;
		align-items: center;
		gap: 12px;
		min-height: 48px;
		padding: 10px 16px;
		border-top: 1px solid var(--panel-border, var(--border));
	}
	.frow:first-child {
		border-top: 0;
	}
	.open {
		flex: 1;
		min-width: 0;
		display: flex;
		align-items: center;
		gap: 12px;
		padding: 0;
		border: 0;
		background: none;
		color: inherit;
		text-align: left;
		cursor: pointer;
		font: inherit;
	}
	.open:disabled {
		cursor: default;
	}
	.open:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	.ic {
		width: 30px;
		height: 30px;
		flex-shrink: 0;
		display: grid;
		place-items: center;
		border-radius: var(--radius-ctl);
		font-size: 16px;
		background: var(--chip);
	}
	.t {
		display: flex;
		flex-direction: column;
		gap: 2px;
		min-width: 0;
	}
	.t strong {
		font-weight: 500;
	}
	.t small {
		color: color-mix(in srgb, var(--muted) 85%, var(--text));
		font-size: var(--text-sm);
		line-height: 1.35;
	}
	.switch:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
</style>
