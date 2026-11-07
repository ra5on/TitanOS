<script lang="ts">
	// PLAN 61: the bar above a program on the phone – back to the home
	// screen, the program's name and its pages.
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import Icon from '../components/Icon.svelte';
	import { t } from '../i18n/index.svelte.ts';
	import DesktopIcon from './DesktopIcon.svelte';
	import { appForPath, sideActive } from './apps.ts';
	import { activeNode } from '../cluster-node.ts';
	import { clusterNodes } from '../cluster.svelte.ts';
	import { session } from '../session.svelte.ts';
	import { localCommandStates, runLocalCommand } from '../menu-commands.svelte.ts';
	import { entries, goTarget, menusFor, type MenuEntry } from './menubar/menus.ts';

	const app = $derived(appForPath(page.url.pathname));
	const items = $derived((app?.side ?? []).filter((i) => 'href' in i));
	let open = $state(false);
	// PLAN 67-1: the program's menus of the desktop's menu bar, as groups in the ⋯ menu.
	const role = $derived(session.data?.user?.role ?? 'user');
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
									('go' in e.do && !pageHrefs.has(e.do.go))),
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

<header class="phone-bar">
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
			<div class="pages dropdown-panel floating-menu" role="menu">
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
		gap: 8px;
		height: calc(52px + env(safe-area-inset-top));
		padding: env(safe-area-inset-top) calc(8px + env(safe-area-inset-right)) 0
			calc(8px + env(safe-area-inset-left));
		background: var(--header);
		border-bottom: 1px solid var(--border);
	}
	.node {
		max-width: 38%;
		padding: 4px 10px;
		border-radius: 999px;
		border: 1px solid color-mix(in srgb, var(--accent) 60%, transparent);
		background: color-mix(in srgb, var(--accent) 14%, transparent);
		font-size: var(--text-sm);
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
		flex: none;
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
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.space {
		flex: 1;
	}
	.menu-sub {
		padding: 6px 10px 2px;
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
		padding: 6px;
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
