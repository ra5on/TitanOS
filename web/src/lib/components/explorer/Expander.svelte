<script lang="ts">
	// PLAN 75-19, style E3 (owner 9.11.2026): one setting or group per row –
	// symbol, name, a summary below. A simple setting has its control at the
	// right of the row; a group opens downward (chevron) and shows its parts.
	import type { Snippet } from 'svelte';
	import Icon, { type IconName } from '#lib/components/Icon.svelte';

	type Props = {
		title: string;
		summary?: string;
		icon?: IconName;
		/** The control of a simple setting, at the right of the row. */
		control?: Snippet;
		/** The parts of a group: the row opens downward to show them. */
		children?: Snippet;
		open?: boolean;
		id?: string;
	};
	let {
		title,
		summary,
		icon,
		control,
		children,
		open = $bindable(false),
		id = 'exp-' + Math.random().toString(36).slice(2, 9),
	}: Props = $props();
</script>

<section class="expander" class:open>
	{#if children}
		<button
			type="button"
			class="row head"
			aria-expanded={open}
			aria-controls={id}
			onclick={() => (open = !open)}
		>
			{#if icon}<span class="ico" aria-hidden="true"><Icon name={icon} size={19} /></span>{/if}
			<span class="t"
				><b>{title}</b>{#if summary}<small>{summary}</small>{/if}</span
			>
			{#if control}<span class="ctl">{@render control()}</span>{/if}
			<span class="chev" aria-hidden="true"><Icon name="chevronDown" size={16} /></span>
		</button>
		{#if open}<div class="body" {id}>{@render children()}</div>{/if}
	{:else}
		<div class="row">
			{#if icon}<span class="ico" aria-hidden="true"><Icon name={icon} size={19} /></span>{/if}
			<span class="t"
				><b>{title}</b>{#if summary}<small>{summary}</small>{/if}</span
			>
			{#if control}<span class="ctl">{@render control()}</span>{/if}
		</div>
	{/if}
</section>

<style>
	.expander {
		margin-bottom: var(--space-1);
		border: 1px solid var(--border);
		border-radius: var(--radius-small);
		background: color-mix(in srgb, var(--surface) 70%, transparent);
	}
	.row {
		display: flex;
		align-items: center;
		gap: var(--space-3);
		width: 100%;
		min-height: 56px;
		padding: var(--space-2) var(--space-4);
		border: 0;
		background: none;
		color: inherit;
		font: inherit;
		text-align: left;
	}
	button.row {
		cursor: pointer;
	}
	button.row:hover {
		background: color-mix(in srgb, var(--text) 4%, transparent);
	}
	button.row:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
	.ico {
		display: grid;
		place-items: center;
		width: 30px;
		flex: none;
		color: color-mix(in srgb, var(--text) 85%, transparent);
	}
	.t {
		flex: 1;
		min-width: 0;
	}
	.t b {
		display: block;
		font-weight: 500;
	}
	.t small {
		display: block;
		color: var(--muted);
		font-size: var(--text-sm);
		overflow-wrap: anywhere;
	}
	.ctl {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		flex: none;
		max-width: 60%;
	}
	.chev {
		color: var(--muted);
		transition: transform 0.15s;
	}
	.open .chev {
		transform: rotate(180deg);
	}
	.body {
		border-top: 1px solid var(--border);
		background: color-mix(in srgb, var(--bg) 35%, transparent);
	}
	/* Parts of a group: rows indented under the group's name. */
	.body :global(.part) {
		display: flex;
		align-items: center;
		gap: var(--space-3);
		min-height: 46px;
		padding: var(--space-1-5) var(--space-4) var(--space-1-5)
			calc(var(--space-4) + 30px + var(--space-3));
	}
	.body :global(.part + .part) {
		border-top: 1px solid var(--border);
	}
	.body :global(.part > .t) {
		flex: 1;
		min-width: 0;
	}
	.body :global(.part small) {
		display: block;
		color: var(--muted);
		font-size: var(--text-sm);
	}
	/* PLAN 79-1: a field beside the name keeps its own width (a .field is
	   100 % wide by default and squeezed the name to one letter per line). */
	.ctl :global(:is(select.field, input.field)),
	.body :global(.part > :is(select.field, input.field)) {
		flex: none;
		width: auto;
		min-width: 9rem;
		max-width: min(17rem, 60%);
	}
	.body :global(.part > .t) {
		overflow-wrap: break-word;
	}
	@media (max-width: 560px) {
		.row {
			flex-wrap: wrap;
		}
		.ctl {
			max-width: none;
			width: 100%;
			padding-left: calc(30px + var(--space-3));
		}
		.body :global(.part) {
			flex-wrap: wrap;
			padding-left: var(--space-4);
		}
		/* The field stands below the name, as wide as the row. */
		.body :global(.part:has(> :is(select.field, input.field)) > .t) {
			flex-basis: 100%;
		}
		.ctl :global(:is(select.field, input.field)),
		.body :global(.part > :is(select.field, input.field)) {
			width: 100%;
			max-width: none;
		}
	}
</style>
