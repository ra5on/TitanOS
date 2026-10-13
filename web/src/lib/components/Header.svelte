<script lang="ts">
	import { page } from '$app/state';
	import type { Role } from '#lib/api.ts';
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import { system } from '#lib/system.svelte.ts';
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

	const host = $derived.by(() => {
		const name = system.info?.hostname ?? session.data?.hostname ?? '';
		const ip = system.info?.network?.ipv4.split('/')[0];
		return ip ? `${name} · ${ip}` : name;
	});

	let nav = $state<HTMLElement>();

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
	{#if host}<span class="host mono">{host}</span>{/if}
	<nav bind:this={nav} aria-label={t('nav.label')}>
		{#each items as item (item.href)}
			<a href={item.href} aria-current={isCurrent(item.href) ? 'page' : undefined}>{t(item.label)}</a>
		{/each}
	</nav>
	{#if role !== 'user'}
		<button type="button" class="terminal" aria-pressed={terminal.open} title={t('terminal.shortcut')} onclick={toggleTerminal}>
			<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m5 7 5 5-5 5M12 17h7"></path></svg>
			<span>{t('terminal.title')}</span>
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
		height: 2.75rem;
		display: flex;
		align-items: center;
		gap: 8px;
		padding: 0 14px;
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
	@media (max-width: 1100px) {
		.terminal span {
			display: none;
		}
	}
	.host {
		padding: 3px 8px;
		border: 1px solid var(--border);
		border-radius: 6px;
		font-size: 0.75rem;
		color: var(--muted);
		white-space: nowrap;
	}
	nav {
		flex: 1 1 auto;
		min-width: 0;
		display: flex;
		flex-wrap: nowrap;
		gap: 4px;
		overflow-x: auto;
		scrollbar-width: none;
	}
	nav a {
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
	@media (max-width: 1100px) {
		.host {
			display: none;
		}
	}
	@media (max-width: 760px) {
		header {
			gap: 10px;
			padding: 8px 12px;
		}
		.brand :global(svg:last-child) {
			display: none;
		}
	}
</style>
