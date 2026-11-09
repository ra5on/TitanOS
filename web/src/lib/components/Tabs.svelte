<script lang="ts" generics="T extends string">
	// Tabs above the content of a settings page. The page keeps the chosen
	// tab (often in ?tab=) and renders the content itself.
	let {
		tabs,
		value,
		label,
		onchange,
	}: {
		tabs: { id: T; label: string; badge?: string | number }[];
		value: T;
		label: string;
		onchange: (id: T) => void;
	} = $props();
</script>

<div class="tabs" role="tablist" aria-label={label}>
	{#each tabs as tab (tab.id)}
		<button
			class="btn"
			type="button"
			role="tab"
			aria-selected={value === tab.id}
			data-tab={tab.id}
			onclick={() => onchange(tab.id)}
			>{tab.label}{#if tab.badge !== undefined && tab.badge !== ''}<span class="badge"
					>{tab.badge}</span
				>{/if}</button
		>
	{/each}
</div>

<style>
	.tabs {
		display: flex;
		flex-wrap: wrap;
		border-bottom: 1px solid var(--border);
	}
	.tabs button {
		border: 0;
		border-radius: 0;
		min-height: 44px;
		gap: var(--space-1-5);
	}
	.tabs [aria-selected='true'] {
		/* Mixed toward the text colour: readable (4.5:1) on every theme and background. */
		color: color-mix(in srgb, var(--accent) 70%, var(--text));
		font-weight: 600;
		border-bottom: 2px solid var(--accent);
	}
	.badge {
		font-size: var(--text-xs);
		padding: 1px var(--space-1-5);
		border-radius: 999px;
		background: var(--border);
		color: var(--text);
	}
	@media (max-width: 760px) {
		.tabs {
			flex-wrap: nowrap;
			overflow-x: auto;
		}
		.tabs button {
			white-space: nowrap;
			padding: 0 var(--space-2);
		}
	}
</style>
