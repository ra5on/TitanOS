<script lang="ts">
	import { page } from '$app/state';
	import { afterNavigate } from '$app/navigation';
	import { onMount } from 'svelte';
	import type { Role } from '#lib/api.ts';
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import { horizontalNav } from '#lib/horizontal-nav.ts';
	import { terminal, toggleTerminal } from '#lib/terminal.svelte.ts';
	import AccountMenu from './AccountMenu.svelte';
	import Logo from './Logo.svelte';
	import Notifications from './Notifications.svelte';

	// A plain user only works with their own shares and files.
	const NAV: { href: string; label: MessageKey; roles?: Role[] }[] = [
		{ href: '/', label: 'nav.dashboard' },
		{ href: '/storage', label: 'nav.storage', roles: ['owner', 'admin'] },
		{ href: '/shares', label: 'nav.shares', roles: ['owner', 'admin'] },
		{ href: '/files', label: 'nav.files' },
		{ href: '/users', label: 'nav.users', roles: ['owner', 'admin'] },
		{ href: '/docker', label: 'nav.docker', roles: ['owner', 'admin'] },
		{ href: '/vms', label: 'nav.vms', roles: ['owner', 'admin'] },
		{ href: '/apps', label: 'nav.apps', roles: ['owner', 'admin'] },
		{ href: '/plugins', label: 'nav.plugins', roles: ['owner', 'admin'] },
		{ href: '/settings', label: 'nav.settings' },
		{ href: '/tools', label: 'nav.tools', roles: ['owner', 'admin'] }
	];

	const role = $derived(session.data?.user?.role ?? 'user');
	const items = $derived(NAV.filter((item) => !item.roles || item.roles.includes(role)));

	let mobileMenu = $state<HTMLElement>();
	let mobileOpen = $state(false);
	const currentLabel = $derived(items.find((item) => isCurrent(item.href))?.label ?? 'nav.label');
	afterNavigate(() => mobileMenu?.hidePopover());
	onMount(() => {
		const desktop = matchMedia('(min-width: 761px)');
		const close = () => { if (desktop.matches) mobileMenu?.hidePopover(); };
		desktop.addEventListener('change', close);
		return () => desktop.removeEventListener('change', close);
	});
	let nav = $state<HTMLElement>();
	let overflow = $state(false);
	let canScrollLeft = $state(false);
	let canScrollRight = $state(false);

	function scrollNav(direction: number) {
		nav?.scrollBy({ left: direction * nav.clientWidth * 0.8, behavior: matchMedia('(prefers-reduced-motion: reduce)').matches ? 'instant' : 'smooth' });
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

<header>
	<a class="brand" href="/" aria-label="TitanOS"><Logo size={28} /></a>
	<button class="mobile-nav-trigger" type="button" popovertarget="mobile-navigation" aria-expanded={mobileOpen} aria-controls="mobile-navigation" aria-label={t('nav.openMenu', { page: t(currentLabel) })}>
		<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><path d="M4 6h16M4 12h16M4 18h16"></path></svg>
		<span>{t(currentLabel)}</span>
	</button>
	<div id="mobile-navigation" class="mobile-navigation" popover="auto" bind:this={mobileMenu} ontoggle={() => mobileOpen = mobileMenu?.matches(':popover-open') ?? false}>
		<div class="mobile-nav-heading"><strong>{t('nav.label')}</strong><button class="mobile-nav-close" type="button" popovertarget="mobile-navigation" popovertargetaction="hide" aria-label={t('nav.closeMenu')}>✕</button></div>
		<nav class="mobile-links" aria-label={t('nav.label')}>
			{#each items as item (item.href)}
				<a href={item.href} aria-current={isCurrent(item.href) ? 'page' : undefined} onclick={() => mobileMenu?.hidePopover()}>{t(item.label)}</a>
			{/each}
		</nav>
	</div>
	<div class="nav-shell" use:horizontalNav={(state) => { overflow = state.overflow; canScrollLeft = state.left; canScrollRight = state.right; }}>
		<button class="nav-arrow" type="button" hidden={!overflow} disabled={!canScrollLeft} aria-label={t('nav.scrollLeft')} onclick={() => scrollNav(-1)}>
			<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><path d="m15 6-6 6 6 6"></path></svg>
		</button>
		<nav bind:this={nav} aria-label={t('nav.label')}>
			<div class="nav-items">
				{#each items as item (item.href)}
					<a href={item.href} draggable="false" aria-current={isCurrent(item.href) ? 'page' : undefined}>{t(item.label)}</a>
				{/each}
			</div>
		</nav>
		<button class="nav-arrow" type="button" hidden={!overflow} disabled={!canScrollRight} aria-label={t('nav.scrollRight')} onclick={() => scrollNav(1)}>
			<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><path d="m9 6 6 6-6 6"></path></svg>
		</button>
	</div>
	{#if role !== 'user'}
		<button type="button" class="terminal" aria-pressed={terminal.open} aria-label={t('terminal.title')} title={t('terminal.shortcut')} onclick={toggleTerminal}>
			<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m5 7 5 5-5 5M12 17h7"></path></svg>
		</button>
		<Notifications />
	{/if}
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
	.nav-arrow[hidden] { display: none; }
	.nav-arrow:disabled { opacity: 0.35; cursor: default; }
	.nav-arrow:focus-visible { outline: 2px solid var(--accent); outline-offset: 1px; }
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
	nav:global(.dragging), nav:global(.dragging) a { cursor: grabbing; }
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

	.mobile-nav-trigger { display: none; }
	.mobile-navigation {
		position: fixed; inset: 68px 8px auto; margin: 0; width: auto;
		max-height: calc(100dvh - 84px - env(safe-area-inset-bottom)); overflow-y: auto;
		padding: 12px; border: 1px solid var(--border); border-radius: var(--radius-tile);
		background: var(--header); color: var(--text);
	}
	.mobile-nav-heading { display: flex; align-items: center; justify-content: space-between; gap: 8px; margin-bottom: 8px; }
	.mobile-nav-close { width: 44px; height: 44px; border: 1px solid var(--border); border-radius: var(--radius-ctl); background: var(--chip); color: var(--text); font: inherit; cursor: pointer; }
	.mobile-links { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 8px; }
	.mobile-links a { display: flex; align-items: center; min-height: 44px; padding: 10px; overflow-wrap: anywhere; border: 1px solid var(--border); border-radius: var(--radius-ctl); color: var(--text); text-decoration: none; }
	.mobile-links a[aria-current='page'] { border-color: var(--accent); }
	.mobile-links a:focus-visible, .mobile-nav-trigger:focus-visible, .mobile-nav-close:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
	@media (max-width: 760px) {
		header > .nav-shell { display: none; }
		header > .mobile-nav-trigger { display: flex; flex: 1 1 auto; min-width: 0; height: 44px; align-items: center; gap: 8px; padding: 0 8px; border: 1px solid var(--border); border-radius: var(--radius-ctl); background: var(--chip); color: var(--text); font: inherit; cursor: pointer; }
		.mobile-nav-trigger svg { flex: none; }
		.mobile-nav-trigger span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

		header {
			gap: 10px;
			padding: 8px 12px;
		}
		.brand :global(svg:last-child) {
			display: none;
		}
	}
	@media (max-width: 480px) {
		header { gap: 4px; padding: 8px; }
		.terminal { padding: 0 8px; }
		.nav-arrow { width: 28px; }
		.nav-shell, .nav-items { gap: 2px; }
		.nav-shell nav a { padding-inline: 10px; }
	}
</style>
