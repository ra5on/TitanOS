<script lang="ts">
	// The navigation as a sidebar on the left (Appearance › Navigation ›
	// "Side"), desktop only: the same tabs in the account's order as the
	// header. Folded it shows only the symbols; whether it is folded is
	// remembered per browser.
	import { page } from '$app/state';
	import { onMount } from 'svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { navForRole, visibleNav } from '#lib/navigation.ts';
	import { appearance } from '#lib/theme/appearance.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import { clusterNodes } from '#lib/cluster.svelte.ts';
	import Icon from './Icon.svelte';
	import Logo from './Logo.svelte';
	import NodeMenu from './NodeMenu.svelte';

	const KEY = 'titanos.nav.folded';

	const role = $derived(session.data?.user?.role ?? 'user');
	const items = $derived(visibleNav(navForRole(role), appearance.current.nav));
	let folded = $state(false);

	onMount(() => {
		try {
			folded = localStorage.getItem(KEY) === '1';
		} catch {
			// Without storage the sidebar starts unfolded.
		}
	});

	function toggle() {
		folded = !folded;
		try {
			localStorage.setItem(KEY, folded ? '1' : '0');
		} catch {
			// Only this page view keeps the choice.
		}
	}

	function isCurrent(href: string): boolean {
		const path = page.url.pathname;
		return href === '/' ? path === '/' : path === href || path.startsWith(href + '/');
	}
</script>

<aside
	class="side-nav bar-surface"
	class:folded
	class:magnify={appearance.current.nav.magnify}
	aria-label={t('nav.label')}
>
	<a class="brand" href="/" aria-label="TitanOS"><Logo size={28} wordmark={!folded} /></a>
	{#if clusterNodes.nodes.length > 1}<NodeMenu variant={folded ? 'badge' : 'card'} />{/if}
	<nav>
		{#each items as item (item.href)}
			<a
				href={item.href}
				aria-current={isCurrent(item.href) ? 'page' : undefined}
				aria-label={folded ? t(item.label) : undefined}
				><Icon name={item.icon} size={19} /><span class="label">{t(item.label)}</span></a
			>
		{/each}
	</nav>
	<button
		type="button"
		class="fold"
		aria-expanded={!folded}
		aria-label={t(folded ? 'nav.unfold' : 'nav.fold')}
		title={t(folded ? 'nav.unfold' : 'nav.fold')}
		onclick={toggle}
		><Icon name={folded ? 'chevronRight' : 'chevronLeft'} size={19} /><span class="label"
			>{t('nav.fold')}</span
		></button
	>
</aside>

<style>
	.side-nav {
		position: sticky;
		top: 0;
		z-index: 21;
		flex: none;
		width: 236px;
		height: 100vh;
		height: 100dvh;
		display: flex;
		flex-direction: column;
		gap: 8px;
		padding: 14px 12px;
		border-right: 1px solid var(--border);
	}
	.brand {
		display: flex;
		align-items: center;
		height: 34px;
		padding: 0 6px;
		margin-bottom: 4px;
	}
	nav {
		flex: 1;
		min-height: 0;
		display: flex;
		flex-direction: column;
		gap: 2px;
		overflow-y: auto;
		scrollbar-width: thin;
	}
	nav a,
	.fold {
		position: relative;
		flex: none;
		display: flex;
		align-items: center;
		gap: 12px;
		height: 38px;
		padding: 0 10px;
		border: 0;
		border-radius: var(--radius-ctl);
		background: transparent;
		color: var(--muted);
		font: inherit;
		font-size: 0.875rem;
		text-decoration: none;
		white-space: nowrap;
		cursor: pointer;
	}
	nav a:hover,
	.fold:hover {
		background: var(--chip);
		color: var(--text);
	}
	nav a[aria-current='page'] {
		background: color-mix(in srgb, var(--accent) 14%, transparent);
		box-shadow: inset 3px 0 0 var(--accent);
		color: var(--text);
		font-weight: 500;
	}
	nav a[aria-current='page'] :global(svg) {
		color: var(--accent);
	}
	nav a:focus-visible,
	.fold:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
	.fold {
		margin-top: 4px;
		border-top: 1px solid var(--border);
		border-radius: 0;
		padding-top: 4px;
		height: 46px;
	}

	.folded {
		width: 68px;
		align-items: center;
		padding: 14px 10px;
	}
	.folded .brand {
		padding: 0;
		justify-content: center;
	}
	.folded nav {
		width: 100%;
		align-items: center;
		overflow: visible;
	}
	.folded nav a,
	.folded .fold {
		width: 46px;
		justify-content: center;
		padding: 0;
	}
	.folded .fold {
		width: 100%;
	}
	/* Folded, the name appears beside the symbol on hover and focus. */
	.folded .label {
		position: absolute;
		left: calc(100% + 12px);
		top: 50%;
		transform: translateY(-50%);
		padding: 6px 10px;
		border: 1px solid var(--border);
		border-radius: var(--radius-small);
		background: var(--surface);
		box-shadow: var(--shadow-menu);
		color: var(--text);
		font-weight: 400;
		pointer-events: none;
		opacity: 0;
		visibility: hidden;
	}
	.folded nav a:hover .label,
	.folded nav a:focus-visible .label {
		opacity: 1;
		visibility: visible;
	}
	.folded .fold .label {
		display: none;
	}
	/* Like a dock: the symbol under the pointer grows, its neighbours a
	   little; the rows grow with them, so nothing overlaps. */
	.folded.magnify nav a {
		transition: height 220ms cubic-bezier(0.2, 0.8, 0.3, 1.2);
	}
	.folded.magnify nav a :global(svg) {
		transition: transform 220ms cubic-bezier(0.2, 0.8, 0.3, 1.2);
	}
	.folded.magnify nav a:hover {
		height: 52px;
	}
	.folded.magnify nav a:hover :global(svg) {
		transform: scale(1.45);
	}
	.folded.magnify nav a:has(+ a:hover),
	.folded.magnify nav a:hover + a {
		height: 44px;
	}
	.folded.magnify nav a:has(+ a:hover) :global(svg),
	.folded.magnify nav a:hover + a :global(svg) {
		transform: scale(1.2);
	}
	@media (prefers-reduced-motion: reduce) {
		.folded.magnify nav a,
		.folded.magnify nav a :global(svg) {
			transition: none;
		}
	}
	@media (max-width: 760px) {
		.side-nav {
			display: none;
		}
	}
	@media print {
		.side-nav {
			display: none;
		}
	}
</style>
