<script lang="ts">
	import { page } from '$app/state';
	import { dashboardControl } from '#lib/dashboard/control.svelte.ts';
	import { afterNavigate } from '$app/navigation';
	import { onMount } from 'svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { navForRole, visibleNav } from '#lib/navigation.ts';
	import { appearance } from '#lib/theme/appearance.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import { horizontalNav } from '#lib/horizontal-nav.ts';
	import { terminal, toggleTerminal } from '#lib/terminal.svelte.ts';
	import { assistant, toggleAssistant } from '#lib/assistant.svelte.ts';
	import Icon from './Icon.svelte';
	import { togglePalette } from '#lib/palette.svelte.ts';
	import AccountMenu from './AccountMenu.svelte';
	import Logo from './Logo.svelte';
	import NodeMenu from './NodeMenu.svelte';
	import Notifications from './Notifications.svelte';
	import { clusterNodes } from '#lib/cluster.svelte.ts';

	/** The tabs are in the sidebar (desktop); the header keeps the tools and names the area. */
	let { side = false }: { side?: boolean } = $props();

	const role = $derived(session.data?.user?.role ?? 'user');
	const allItems = $derived(navForRole(role));
	const items = $derived(visibleNav(allItems, appearance.current.nav));

	let mobileMenu = $state<HTMLElement>();
	let mobileOpen = $state(false);
	const currentLabel = $derived(
		allItems.find((item) => isCurrent(item.href))?.label ?? 'nav.label',
	);
	// The app on the home screen has no back button of the browser: show
	// one once there is a page to go back to (CSS shows it only there).
	let visited = $state(false);
	afterNavigate((navigation) => {
		mobileMenu?.hidePopover();
		if (navigation.type !== 'enter') visited = true;
	});
	onMount(() => {
		const desktop = matchMedia('(min-width: 761px)');
		const close = () => {
			if (desktop.matches) mobileMenu?.hidePopover();
		};
		desktop.addEventListener('change', close);
		return () => desktop.removeEventListener('change', close);
	});
	let nav = $state<HTMLElement>();
	let overflow = $state(false);
	let canScrollLeft = $state(false);
	let canScrollRight = $state(false);

	function scrollNav(direction: number) {
		nav?.scrollBy({
			left: direction * nav.clientWidth * 0.8,
			behavior: matchMedia('(prefers-reduced-motion: reduce)').matches ? 'instant' : 'smooth',
		});
	}

	// The navigation scrolls sideways when it does not fit; keep the current tab in view.
	$effect(() => {
		page.url.pathname;
		nav?.querySelector('[aria-current]')?.scrollIntoView({ block: 'nearest', inline: 'nearest' });
	});

	function isCurrent(href: string): boolean {
		const path = page.url.pathname;
		return href === '/' ? path === '/' : path === href || path.startsWith(href + '/');
	}
</script>

<header class:side>
	{#if visited && page.url.pathname !== '/'}<button
			type="button"
			class="app-back"
			onclick={() => history.back()}
			aria-label={t('nav.back')}
			title={t('nav.back')}><Icon name="arrowLeft" size={20} /></button
		>{/if}
	{#if clusterNodes.nodes.length > 1}<div class="brand"><NodeMenu variant="logo" /></div>{:else}<a
			class="brand"
			href="/"
			aria-label="TitanOS"><Logo size={28} /></a
		>{/if}
	{#if side}<h1 class="area">{t(currentLabel)}</h1>{/if}
	<button
		class="mobile-nav-trigger"
		type="button"
		popovertarget="mobile-navigation"
		aria-expanded={mobileOpen}
		aria-controls="mobile-navigation"
		aria-label={t('nav.openMenu', { page: t(currentLabel) })}
	>
		<svg
			width="20"
			height="20"
			viewBox="0 0 24 24"
			fill="none"
			stroke="currentColor"
			stroke-width="2"
			aria-hidden="true"><path d="M4 6h16M4 12h16M4 18h16"></path></svg
		>
		<span>{t(currentLabel)}</span>
	</button>
	<div
		id="mobile-navigation"
		class="mobile-navigation floating-menu"
		popover="auto"
		bind:this={mobileMenu}
		ontoggle={() => (mobileOpen = mobileMenu?.matches(':popover-open') ?? false)}
	>
		<div class="mobile-nav-heading">
			<strong>{t('nav.label')}</strong><button
				class="mobile-nav-close"
				type="button"
				popovertarget="mobile-navigation"
				popovertargetaction="hide"
				aria-label={t('nav.closeMenu')}>✕</button
			>
		</div>
		<nav class="mobile-links" aria-label={t('nav.label')}>
			{#each items as item (item.href)}
				<a
					href={item.href}
					aria-current={isCurrent(item.href) ? 'page' : undefined}
					onclick={() => mobileMenu?.hidePopover()}>{t(item.label)}</a
				>
			{/each}
		</nav>
	</div>
	<div
		class="nav-shell"
		use:horizontalNav={(state) => {
			overflow = state.overflow;
			canScrollLeft = state.left;
			canScrollRight = state.right;
		}}
	>
		<button
			class="nav-arrow"
			type="button"
			hidden={!overflow}
			disabled={!canScrollLeft}
			aria-label={t('nav.scrollLeft')}
			onclick={() => scrollNav(-1)}
		>
			<svg
				width="16"
				height="16"
				viewBox="0 0 24 24"
				fill="none"
				stroke="currentColor"
				stroke-width="2"
				aria-hidden="true"><path d="m15 6-6 6 6 6"></path></svg
			>
		</button>
		<nav bind:this={nav} aria-label={t('nav.label')}>
			<div class="nav-items">
				{#each items as item (item.href)}
					<a
						href={item.href}
						draggable="false"
						aria-current={isCurrent(item.href) ? 'page' : undefined}>{t(item.label)}</a
					>
				{/each}
			</div>
		</nav>
		<button
			class="nav-arrow"
			type="button"
			hidden={!overflow}
			disabled={!canScrollRight}
			aria-label={t('nav.scrollRight')}
			onclick={() => scrollNav(1)}
		>
			<svg
				width="16"
				height="16"
				viewBox="0 0 24 24"
				fill="none"
				stroke="currentColor"
				stroke-width="2"
				aria-hidden="true"><path d="m9 6 6 6-6 6"></path></svg
			>
		</button>
	</div>
	<div class="quick-access" role="group" aria-label={t('nav.quickActions')}>
		<button
			class="terminal"
			type="button"
			onclick={togglePalette}
			aria-label={t('palette.title')}
			title={t('palette.shortcut')}><Icon name="search" size={18} /></button
		>
		{#if role !== 'user'}
			<button
				type="button"
				class="terminal"
				aria-pressed={terminal.open}
				aria-label={t('terminal.title')}
				title={t('terminal.shortcut')}
				onclick={toggleTerminal}
			>
				<svg
					width="18"
					height="18"
					viewBox="0 0 24 24"
					fill="none"
					stroke="currentColor"
					stroke-width="2"
					stroke-linecap="round"
					stroke-linejoin="round"
					aria-hidden="true"><path d="m5 7 5 5-5 5M12 17h7"></path></svg
				>
			</button>
		{/if}
		{#if assistant.status?.available}
			<button
				type="button"
				class="terminal"
				aria-pressed={page.url.pathname === '/assistant'}
				aria-label={t('assistant.title')}
				title={t('assistant.shortcut')}
				onclick={toggleAssistant}
			>
				<Icon name="chat" size={18} />
			</button>
		{/if}
		{#if role !== 'user'}<Notifications compact />{/if}
		{#if page.url.pathname === '/' && dashboardControl.edit}<button
				class="terminal"
				type="button"
				aria-label={t('dashboard.editTitle')}
				aria-pressed={dashboardControl.editing}
				disabled={dashboardControl.editing}
				onclick={() => dashboardControl.edit?.()}><Icon name="edit" size={18} /></button
			>{/if}
	</div>
	<AccountMenu />
</header>

<style>
	header {
		position: sticky;
		top: 0;
		z-index: 20;
		height: 60px;
		display: flex;
		flex-wrap: nowrap;
		align-items: center;
		gap: 16px;
		padding: 8px 24px;
		background: var(--header);
		border-bottom: 1px solid var(--border);
	}
	header > :global(*) {
		flex: none;
	}
	.brand {
		display: flex;
	}
	.area {
		display: none;
	}
	@media (min-width: 761px) {
		header.side .brand,
		header.side > .nav-shell {
			display: none;
		}
		header.side .area {
			display: block;
			flex: 1 1 auto;
			min-width: 0;
			margin: 0;
			overflow: hidden;
			text-overflow: ellipsis;
			white-space: nowrap;
			font-size: 1.125rem;
			font-weight: 600;
		}
	}
	.app-back {
		display: none;
	}
	@media (display-mode: standalone) {
		.app-back {
			display: inline-flex;
			align-items: center;
			justify-content: center;
			min-width: 44px;
			min-height: 44px;
			margin-left: -8px;
			border: 0;
			background: none;
			color: var(--text);
			cursor: pointer;
		}
	}
	.quick-access {
		display: flex;
		align-items: center;
		gap: 0;
		height: 44px;
		padding: 3px 4px;
		box-sizing: border-box;
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		background: var(--chip);
		--quick-button-size: 36px;
	}
	.quick-access .terminal {
		width: var(--quick-button-size);
		height: 36px;
		border: 0;
		border-radius: var(--radius-small);
	}
	.quick-access .terminal:hover {
		background: color-mix(in srgb, var(--text) 7%, transparent);
	}
	.quick-access .terminal:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
	.terminal {
		width: 44px;
		height: 44px;
		justify-content: center;
		display: flex;
		align-items: center;
		gap: 8px;
		padding: 0;
		background: transparent;
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		color: var(--text);
		font: inherit;
		font-size: 0.875rem;
		cursor: pointer;
	}
	.terminal[aria-pressed='true'] {
		background: color-mix(in srgb, var(--accent) 16%, transparent);
		border-color: color-mix(in srgb, var(--accent) 45%, transparent);
		font-weight: 500;
	}

	header > .nav-shell {
		flex: 1 1 auto;
		min-width: 0;
		display: flex;
		align-items: center;
		gap: 4px;
	}
	.nav-arrow {
		flex: none;
		width: 28px;
		height: 44px;
		display: flex;
		align-items: center;
		justify-content: center;
		padding: 0;
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		background: transparent;
		color: var(--text);
		cursor: pointer;
	}
	.nav-arrow[hidden] {
		display: none;
	}
	.nav-arrow:disabled {
		opacity: 0.35;
		cursor: default;
	}
	.nav-arrow:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 1px;
	}
	.nav-items {
		display: flex;
		width: max-content;
		gap: 4px;
	}
	.nav-shell nav {
		flex: 1 1 auto;
		min-width: 0;
		overflow-x: auto;
		scrollbar-width: thin;
		scrollbar-color: var(--muted) var(--header);
		overscroll-behavior-x: contain;
		user-select: none;
	}
	nav:global(.dragging),
	nav:global(.dragging) a {
		cursor: grabbing;
	}
	.nav-shell nav a {
		flex: none;
		padding: 10px 14px;
		border-radius: var(--radius-ctl);
		font-size: 0.875rem;
		text-decoration: none;
		white-space: nowrap;
		color: var(--muted);
	}
	nav a:hover {
		color: var(--text);
	}
	nav a[aria-current='page'] {
		background: var(--chip);
		color: var(--text);
		font-weight: 500;
	}

	.mobile-nav-trigger {
		display: none;
	}
	.mobile-navigation {
		position: fixed;
		inset: 68px 8px auto;
		margin: 0;
		width: auto;
		max-height: calc(100dvh - 84px - env(safe-area-inset-bottom));
		overflow-y: auto;
		padding: 12px;
		/* Surface from .floating-menu (app.css), like every other menu. */
		border-radius: var(--radius-tile);
		color: var(--text);
	}
	.mobile-nav-heading {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 8px;
		margin-bottom: 8px;
	}
	.mobile-nav-close {
		width: 44px;
		height: 44px;
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		background: var(--chip);
		color: var(--text);
		font: inherit;
		cursor: pointer;
	}
	.mobile-links {
		display: grid;
		grid-template-columns: repeat(2, minmax(0, 1fr));
		gap: 8px;
	}
	.mobile-links a {
		display: flex;
		align-items: center;
		min-height: 44px;
		padding: 10px;
		overflow-wrap: anywhere;
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		color: var(--text);
		text-decoration: none;
	}
	.mobile-links a[aria-current='page'] {
		border-color: var(--accent);
	}
	.mobile-links a:focus-visible,
	.mobile-nav-trigger:focus-visible,
	.mobile-nav-close:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	@media (max-width: 760px) {
		header > .nav-shell {
			display: none;
		}
		header > .mobile-nav-trigger {
			display: flex;
			flex: 1 1 auto;
			min-width: 0;
			height: 44px;
			align-items: center;
			gap: 8px;
			padding: 0 8px;
			border: 1px solid var(--border);
			border-radius: var(--radius-ctl);
			background: var(--chip);
			color: var(--text);
			font: inherit;
			cursor: pointer;
		}
		.mobile-nav-trigger svg {
			flex: none;
		}
		.mobile-nav-trigger span {
			overflow: hidden;
			text-overflow: ellipsis;
			white-space: nowrap;
		}

		header {
			gap: 10px;
			padding: 8px 12px;
		}
		.brand :global(.logo > svg:last-child) {
			display: none;
		}
	}
	@media (max-width: 480px) {
		header {
			gap: 4px;
			padding: 8px;
		}
		.quick-access {
			--quick-button-size: 32px;
		}
		.nav-arrow {
			width: 28px;
		}
		.nav-shell,
		.nav-items {
			gap: 2px;
		}
		.nav-shell nav a {
			padding-inline: 10px;
		}
	}
	@media (max-width: 360px) {
		header {
			gap: 2px;
			padding-inline: 6px;
		}
		.quick-access {
			--quick-button-size: 28px;
		}
	}
</style>
