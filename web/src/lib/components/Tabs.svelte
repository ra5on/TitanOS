<script lang="ts" generics="T extends string">
	// Tabs above the content of a settings page. The page keeps the chosen
	// tab (often in ?tab=) and renders the content itself. The arrow keys,
	// Home and End move between the tabs (WAI-ARIA tabs, 1.03).
	import { tabKeys } from '#lib/radio-group.ts';
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
	// One Tab stop for the row (1.03): the chosen tab, or the first when none is.
	const known = $derived(tabs.some((tab) => tab.id === value));
</script>

<!-- svelte-ignore a11y_interactive_supports_focus -->
<div class="tabs" role="tablist" aria-label={label} onkeydown={tabKeys}>
	{#each tabs as tab, i (tab.id)}
		<button
			class="btn"
			type="button"
			role="tab"
			aria-selected={value === tab.id}
			tabindex={value === tab.id || (!known && i === 0) ? 0 : -1}
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
