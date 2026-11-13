<script lang="ts">
	// PLAN 61: the bar above a program on the phone – back to the home
	// screen, the program's name and its pages.
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import Icon from '../components/Icon.svelte';
	import { t } from '../i18n/index.svelte.ts';
	import DesktopIcon from './DesktopIcon.svelte';
	import { sideFor, appForPath, sideActive } from './apps.ts';
	import { nvr } from '../nvr/nvr.svelte.ts';
	import { nvrPageAllowed, nvrSideFor, rightsOf } from '../nvr/side.svelte.ts';
	import { activeNode } from '../cluster-node.ts';
	import { clusterNodes } from '../cluster.svelte.ts';
	import { session } from '../session.svelte.ts';
	import { localCommandStates, runLocalCommand } from '../menu-commands.svelte.ts';
	import { entries, goTarget, menusFor, type MenuEntry } from './menubar/menus.ts';
	import { unreadCount } from '../notifications.svelte.ts';
	import { swipeDown } from '../components/sheet.ts';

	// PLAN 87: the bell, and pulling the bar down shows the notifications.
	const unread = $derived(unreadCount());
	const notices = () => window.dispatchEvent(new CustomEvent('titanos-phone-notices'));
	let pullFrom: { x: number; y: number } | null = null;
	let pulled = $state(0);
	function pullStart(e: TouchEvent) {
		pullFrom = e.touches.length === 1 ? { x: e.touches[0].clientX, y: e.touches[0].clientY } : null;
		pulled = 0;
	}
	function pullMove(e: TouchEvent) {
		if (!pullFrom) return;
		const dy = e.touches[0].clientY - pullFrom.y;
		const dx = e.touches[0].clientX - pullFrom.x;
		if (Math.abs(dx) > Math.abs(dy) || dy < -10) {
			pullFrom = null;
			return;
		}
		pulled = Math.max(0, dy);
		if (pulled > 10 && e.cancelable) e.preventDefault();
	}
	function pullEnd() {
		if (pullFrom && pulled > 60) notices();
		pullFrom = null;
		pulled = 0;
	}

	const app = $derived(appForPath(page.url.pathname));
	const role = $derived(session.data?.user?.role ?? 'user');
	// PLAN 79-7: entries only some roles see (the NVR's settings) are left out,
	// and an account without the admin role sees the NVR's pages it has a right for.
	const nvrRights = $derived(
		app?.id === 'nvr' && role === 'user' && nvr.loaded ? rightsOf(nvr.cameras) : null,
	);
	const items = $derived(
		nvrSideFor(sideFor(app?.side ?? [], role), nvrRights).filter((i) => 'href' in i),
	);
	let open = $state(false);
	// PLAN 67-1: the program's menus of the desktop's menu bar, as groups in the ⋯ menu.
	const pageHrefs = $derived(
		new Set(items.length > 1 ? items.map((i) => ('href' in i ? i.href : '')) : []),
	);
	const groups = $derived(
		app
			? menusFor('app:' + app.id, role)
					.filter((m) => m.id !== 'window' && m.id !== 'help')
					.map((m) => ({
						label: m.label,
						items: entries([m]).filter(
							(e) =>
								e.do &&
								('cmd' in e.do ||
									// A page already listed above is not repeated.
									('go' in e.do &&
										!pageHrefs.has(e.do.go) &&
										(!e.do.go.startsWith('/nvr') || nvrPageAllowed(e.do.go, nvrRights)))),
						),
					}))
					.filter((g) => g.items.length)
			: [],
	);
	const states = $derived(open ? localCommandStates() : {});
	function enabled(e: MenuEntry): boolean {
		if (!e.do) return false;
		if ('cmd' in e.do) return !!states[e.do.cmd]?.enabled;
		if ('go' in e.do) return !!goTarget(e.do.go, 'app:' + app?.id, role, session.services);
		return false;
	}
	function runEntry(e: MenuEntry) {
		open = false;
		if (!e.do) return;
		if ('cmd' in e.do) runLocalCommand(e.do.cmd);
		else if ('go' in e.do) {
			const target = goTarget(e.do.go, 'app:' + app?.id, role, session.services);
			if (target) void goto(target.href);
		}
	}
	// PLAN 64: while this tab manages another node of the cluster, its name stays in sight.
	const nodeId = activeNode();
	const node = $derived(nodeId ? clusterNodes.nodes.find((n) => n.id === nodeId) : undefined);
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<header
	class="phone-bar"
	ontouchstart={pullStart}
	ontouchmove={pullMove}
	ontouchend={pullEnd}
	ontouchcancel={pullEnd}
	style:--pulled="{Math.min(pulled, 80)}px"
>
	<button type="button" class="back" onclick={() => goto('/')} aria-label={t('desktop.phone.home')}
		><Icon name="chevronLeft" size={22} /></button
	>
	{#if app}<DesktopIcon
			glyph={app.glyph}
			gradient={app.gradient}
			size={26}
			name={t(app.label)}
		/>{/if}
	<b>{app ? t(app.label) : ''}</b>
	<span class="space"></span>
	{#if node}<span
			class="node mono"
			title={t('cluster.managing', { name: node.name })}
			aria-label={t('cluster.managing', { name: node.name })}>{node.name}</span
		>{/if}
	<button
		type="button"
		class="back bell"
		data-phone-bell
		aria-label={unread ? t('phone.bellUnread', { count: unread }) : t('phone.bell')}
		title={t('phone.bell')}
		onclick={notices}
		><Icon name="bell" size={20} />{#if unread}<span class="count"
				>{unread > 9 ? '9+' : unread}</span
			>{/if}</button
	>
	{#if items.length > 1 || groups.length}
		<button
			type="button"
			class="back"
			aria-haspopup="menu"
			aria-expanded={open}
			onclick={() => (open = !open)}
			aria-label={t('desktop.phone.pages')}><Icon name="more" size={20} /></button
		>
		{#if open}
			<!-- PLAN 87: a sheet from the bottom (app.css); a touch beside it or a swipe down closes it. -->
			<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
			<div class="sheet-back" onclick={() => (open = false)}></div>
			<div
				class="pages dropdown-panel floating-menu"
				role="menu"
				tabindex="-1"
				use:swipeDown={{ close: () => (open = false) }}
				onkeydown={(e) => e.key === 'Escape' && (open = false)}
			>
				{#if items.length > 1}<div class="menu-title">{t('menubar.phone.pages')}</div>{/if}
				{#each items.length > 1 ? items : [] as item, i (i)}
					{#if 'label' in item}
						<button
							type="button"
							role="menuitemradio"
							aria-checked={sideActive(item, page.url.pathname, app?.side ?? [])}
							onclick={() => ((open = false), goto(item.href))}><span>{t(item.label)}</span></button
						>
					{/if}
				{/each}
				{#each groups as g (g.label)}
					<hr />
					<div class="menu-title">{t(g.label)}</div>
					{#each g.items as e (e.id)}
						{@const st = e.do && 'cmd' in e.do ? states[e.do.cmd] : undefined}
						{#if e.list}
							<!-- A list the page supplies (shares, recent files …): its entries in place. -->
							{#if st?.items?.length}<div class="menu-sub">{t(e.label, { name: '' })}</div>{/if}
							{#each st?.items ?? [] as item, i (i)}
								<button
									type="button"
									role={item.checked === undefined ? 'menuitem' : 'menuitemradio'}
									aria-checked={item.checked}
									class="sub"
									onclick={() => {
										open = false;
										if (e.do && 'cmd' in e.do) runLocalCommand(e.do.cmd, item.id);
									}}><span>{item.text}</span></button
								>
							{/each}
						{:else}
							<button
								type="button"
								role={e.mark === 'check'
									? 'menuitemcheckbox'
									: e.mark === 'radio'
										? 'menuitemradio'
										: 'menuitem'}
								aria-checked={e.mark ? !!st?.checked : undefined}
								disabled={!enabled(e)}
								onclick={() => runEntry(e)}><span>{t(e.label, { name: '' })}</span></button
							>
						{/if}
					{/each}
				{/each}
			</div>
		{/if}
	{/if}
</header>

<style>
	.phone-bar {
		position: sticky;
		top: 0;
		z-index: 30;
		display: flex;
		align-items: center;
		gap: var(--space-2);
		height: calc(52px + env(safe-area-inset-top));
		padding: env(safe-area-inset-top) calc(var(--space-2) + env(safe-area-inset-right)) 0
			calc(8px + env(safe-area-inset-left));
		background: var(--header);
		border-bottom: 1px solid var(--border);
	}
	.node {
		max-width: 38%;
		padding: var(--space-1) var(--space-2-5);
		border-radius: 999px;
		border: 1px solid color-mix(in srgb, var(--accent) 60%, transparent);
		background: color-mix(in srgb, var(--accent) 14%, transparent);
		font-size: var(--text-sm);
		flex: none;
		overflow-wrap: break-word;
		hyphens: auto;
	}
	.bell {
		position: relative;
	}
	.count {
		position: absolute;
		top: 4px;
		right: 2px;
		min-width: 18px;
		height: 18px;
		padding: 0 4px;
		border-radius: 9px;
		background: var(--accent);
		color: var(--on-accent);
		font-size: 11px;
		font-weight: 700;
		display: grid;
		place-items: center;
	}
	.phone-bar {
		touch-action: pan-x;
	}
	.back {
		width: 44px;
		height: 44px;
		display: grid;
		place-items: center;
		border: 0;
		border-radius: 10px;
		background: none;
		color: var(--text);
		cursor: pointer;
	}
	b {
		min-width: 0;
		overflow-wrap: break-word;
		hyphens: auto;
	}
	.space {
		flex: 1;
	}
	/* The ⋯ button stays above the dimmed page: a second tap closes the sheet. */
	.back[aria-expanded='true'] {
		position: relative;
		z-index: 61;
	}
	.sheet-back {
		position: fixed;
		inset: 0;
		z-index: 60;
		background: var(--overlay);
	}
	.menu-sub {
		padding: var(--space-1-5) var(--space-2-5) 2px;
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.sub {
		padding-left: 22px !important;
	}
	.pages > * {
		flex-shrink: 0;
	}
	.pages {
		background: var(--surface);
		padding: var(--space-1-5);
		display: flex;
		flex-direction: column;
		max-height: calc(100dvh - 70px);
		overflow-y: auto;
		position: absolute;
		right: 8px;
		top: calc(100% + 4px);
		min-width: 14rem;
		max-width: calc(100vw - 16px);
	}
</style>
