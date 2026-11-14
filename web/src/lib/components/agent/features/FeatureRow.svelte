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
		ontoggle?: (on: boolean) => void | Promise<void>;
		onopen?: () => void;
	};
	// PLAN 68: colors from the theme only; `color` is kept for the callers.
	let { icon, title, sub, on = false, toggle = true, ontoggle, onopen }: Props = $props();
</script>

<!-- PLAN 78-5: a flat row like every setting (styles: app.css, .srow). -->
<div class="srow">
	<div class="srow-head frow">
		<button type="button" class="open" onclick={onopen} disabled={!onopen}>
			<span class="srow-ico ic" aria-hidden="true">{icon}</span>
			<span class="srow-text"
				><strong class="srow-title">{title}</strong>{#if sub}<small class="srow-desc">{sub}</small
					>{/if}</span
			>
		</button>
		{#if toggle}
			<input
				type="checkbox"
				class="switch"
				role="switch"
				checked={on}
				aria-label={title}
				onchange={async (e) => {
					// The switch shows what is saved: a refused change goes back.
					const el = e.currentTarget;
					try {
						await ontoggle?.(el.checked);
					} finally {
						el.checked = on;
					}
				}}
			/>
		{/if}
	</div>
</div>

<style>
	/* The name opens the function's page, the switch stays beside it (also
	   on a phone: the row does not stack). */
	.frow {
		flex-wrap: nowrap;
	}
	.open {
		flex: 1;
		min-width: 0;
		display: flex;
		align-items: center;
		gap: var(--space-4);
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
		font-size: 17px;
	}
	.switch {
		flex: none;
	}
	.switch:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
</style>
