<script lang="ts">
	// The navigation beside every tools page, built like the settings sidebar
	// (decision of the owner of 4 Nov 2026, variant B): "Overview" on top,
	// then the groups with compact rows; the current page is highlighted.
	import { page } from '$app/state';
	import Icon from './Icon.svelte';
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { toolsCurrent, toolsGroups } from '#lib/tools-nav.ts';

	const path = $derived(page.url.pathname.replace(/\/$/, ''));
</script>

<nav class="tools-sidebar" aria-label={t('tools.navigation')}>
	<ul>
		<li>
			<a href="/tools" aria-current={path === '/tools' ? 'page' : undefined}
				><Icon name="gridSmall" size={16} /><span class="label">{t('tools.overview')}</span></a
			>
		</li>
	</ul>
	{#each toolsGroups as group (group.group)}
		<p class="group-title" id="tools-side-group-{group.group}">
			{t(`tools.group.${group.group}` as MessageKey)}
		</p>
		<ul aria-labelledby="tools-side-group-{group.group}">
			{#each group.entries as entry (entry.href)}
				<li>
					<a href={entry.href} aria-current={toolsCurrent(entry.href, path) ? 'page' : undefined}
						><Icon name={entry.icon} size={16} /><span class="label">{t(entry.label)}</span></a
					>
				</li>
			{/each}
		</ul>
	{/each}
</nav>

<style>
	.tools-sidebar {
		display: flex;
		flex-direction: column;
		min-width: 0;
		padding-top: var(--space-1);
	}
	.group-title {
		margin: var(--space-4) var(--space-2) var(--space-1);
		font-size: var(--text-xs);
		font-weight: 600;
		letter-spacing: 0.08em;
		text-transform: uppercase;
		color: var(--muted);
	}
	ul {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: 1px;
	}
	a {
		display: flex;
		align-items: center;
		gap: 10px;
		min-height: 2rem;
		padding: var(--space-1) var(--space-2);
		border-radius: var(--radius-ctl);
		color: var(--text);
		text-decoration: none;
		font-size: var(--text-md);
		line-height: 1.25;
	}
	a :global(svg) {
		flex: none;
		color: var(--muted);
	}
	a:hover {
		background: var(--chip);
	}
	a[aria-current='page'] {
		background: color-mix(in srgb, var(--accent) 14%, transparent);
		/* The accent alone is too faint on its own tint in light themes. */
		color: color-mix(in srgb, var(--accent) 60%, var(--text));
		font-weight: 500;
	}
	a[aria-current='page'] :global(svg) {
		color: var(--accent);
	}
	a:focus-visible {
		outline-offset: -2px;
	}
	.label {
		min-width: 0;
		overflow-wrap: anywhere;
	}
</style>
