<script lang="ts">
	// §121 (owner 28.11.2026, drafts Entwuerfe/121-handy-start): the bar at the
	// bottom of the phone – Start, Apps, Dateien, Konto with their names. It
	// takes the place of the dock (PLAN 87), which stays a choice under
	// Settings › Appearance › Dock › „Unten am Handy“.
	import { page } from '$app/state';
	import Icon, { type IconName } from '../components/Icon.svelte';
	import { t, type MessageKey } from '../i18n/index.svelte.ts';

	const tabs: { id: string; href: string; icon: IconName; label: MessageKey }[] = [
		{ id: 'start', href: '/', icon: 'home', label: 'phone.tabs.start' },
		{ id: 'apps', href: '/?phone=apps', icon: 'grid', label: 'phone.tabs.apps' },
		{ id: 'files', href: '/files', icon: 'folder', label: 'phone.tabs.files' },
		{ id: 'account', href: '/settings', icon: 'person', label: 'phone.tabs.account' },
	];
	const current = $derived.by(() => {
		const path = page.url.pathname;
		if (path === '/') return page.url.searchParams.get('phone') === 'apps' ? 'apps' : 'start';
		if (path === '/files' || path.startsWith('/files/')) return 'files';
		if (/^\/(settings|account)(\/|$)/.test(path)) return 'account';
		return '';
	});
	const shown = tabs;

	function reserve(node: HTMLElement) {
		const root = document.documentElement;
		const observer = new ResizeObserver(() =>
			root.style.setProperty('--phone-dock-height', node.offsetHeight + 'px'),
		);
		observer.observe(node);
		return {
			destroy() {
				observer.disconnect();
				root.style.removeProperty('--phone-dock-height');
			},
		};
	}
</script>

<nav
	class="tabbar"
	aria-label={t('phone.tabs.label')}
	data-phone-tabs
	data-no-edge-swipe
	use:reserve
	style:--count={shown.length}
>
	{#each shown as tab (tab.id)}
		<a
			href={tab.href}
			aria-current={current === tab.id ? 'page' : undefined}
			data-sveltekit-noscroll
			><Icon name={tab.icon} size={22} /><span>{t(tab.label)}</span></a
		>
	{/each}
</nav>

<style>
	:global(body:has(dialog[open])) .tabbar,
	:global(body:has(.phone-bar [aria-haspopup='menu'][aria-expanded='true'])) .tabbar {
		visibility: hidden;
	}
	.tabbar {
		position: fixed;
		z-index: 22;
		left: max(12px, env(safe-area-inset-left));
		right: max(12px, env(safe-area-inset-right));
		bottom: calc(10px + env(safe-area-inset-bottom));
		max-width: 480px;
		margin: 0 auto;
		display: grid;
		grid-template-columns: repeat(var(--count), minmax(0, 1fr));
		gap: 2px;
		padding: 5px;
		border: 1px solid rgb(220 247 255 / 0.28);
		border-radius: 34px;
		background: var(--nord-chrome, var(--surface));
		-webkit-backdrop-filter: var(--nord-blur);
		backdrop-filter: var(--nord-blur);
		box-shadow:
			inset 0 1px 1px rgb(255 255 255 / 0.2),
			0 7px 24px rgb(1 9 17 / 0.38);
		-webkit-user-select: none;
		user-select: none;
		-webkit-touch-callout: none;
	}
	a {
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: 2px;
		min-height: 54px;
		border-radius: 28px;
		color: rgb(236 244 248 / 0.78);
		text-decoration: none;
		font-size: 0.75rem;
		font-weight: 600;
		line-height: 1.15;
		text-align: center;
		-webkit-tap-highlight-color: transparent;
	}
	a span {
		overflow-wrap: normal;
		hyphens: manual;
	}
	a[aria-current='page'] {
		color: var(--accent);
		background: color-mix(in srgb, var(--accent) 17%, transparent);
	}
	a:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
	@media (prefers-reduced-transparency: reduce) {
		.tabbar {
			background: #1a3038;
			backdrop-filter: none;
		}
	}
</style>
