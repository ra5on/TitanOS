<script lang="ts">
	// The bar at the bottom when TitanOS runs as an app from the home screen
	// on a phone: the main places within thumb reach. Elsewhere (browser,
	// wider screens) it is not shown; CSS decides, so nothing jumps.
	import { page } from '$app/state';
	import type { Role } from '#lib/api.ts';
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import Icon, { type IconName } from './Icon.svelte';

	const TABS: { href: string; label: MessageKey; icon: IconName; roles?: Role[] }[] = [
		{ href: '/', label: 'nav.dashboard', icon: 'home' },
		{ href: '/storage', label: 'nav.storage', icon: 'disk', roles: ['owner', 'admin'] },
		{ href: '/docker', label: 'nav.docker', icon: 'grid', roles: ['owner', 'admin'] },
		{ href: '/files', label: 'nav.files', icon: 'folder' },
		{ href: '/settings', label: 'nav.settings', icon: 'tool' },
	];
	const role = $derived(session.data?.user?.role ?? 'user');
	const tabs = $derived(TABS.filter((tab) => !tab.roles || tab.roles.includes(role)));
	function current(href: string): boolean {
		const path = page.url.pathname;
		return href === '/' ? path === '/' : path === href || path.startsWith(href + '/');
	}
</script>

<nav class="app-tabs" aria-label={t('nav.label')}>
	{#each tabs as tab (tab.href)}
		<a href={tab.href} aria-current={current(tab.href) ? 'page' : undefined}><Icon name={tab.icon} size={22} /><span>{t(tab.label)}</span></a>
	{/each}
</nav>

<style>
	.app-tabs {
		display: none;
	}
	@media (display-mode: standalone) and (max-width: 760px) {
		.app-tabs {
			position: fixed;
			left: 0;
			right: 0;
			bottom: 0;
			z-index: 25;
			display: flex;
			justify-content: space-around;
			padding: 6px 4px calc(6px + env(safe-area-inset-bottom));
			background: var(--header);
			border-top: 1px solid var(--border);
		}
		a {
			flex: 1;
			min-height: 48px;
			display: flex;
			flex-direction: column;
			align-items: center;
			justify-content: center;
			gap: 2px;
			color: var(--muted);
			text-decoration: none;
			font-size: 0.6875rem;
		}
		a[aria-current='page'] {
			color: var(--accent);
		}
	}
</style>
