<script lang="ts">
	import MigrationDialog from '../webapps/MigrationDialog.svelte';
	// PLAN 61: TitanOS on the phone – programs as a grid like a home screen;
	// each opens full screen with a way back (PhoneBar).
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import Icon from '../components/Icon.svelte';
	import AccountMenu from '../components/AccountMenu.svelte';
	import NodeMenu from '../components/NodeMenu.svelte';
	import { clusterNodes } from '../cluster.svelte.ts';
	import { t } from '../i18n/index.svelte.ts';
	import { system } from '../system.svelte.ts';
	import { storage } from '../storage.svelte.ts';
	import { formatPercent } from '../format.ts';
	import DesktopIcon from './DesktopIcon.svelte';
	import { arrangeIcons } from './prefs.ts';
	import { openTab, programs, type Program } from './programs.svelte.ts';
	import { watchSources } from './sources.svelte.ts';
	import { desktop, loadDesktop } from './wm.svelte.ts';
	import { wallpaperCss } from './wallpapers.ts';
	import { appearance } from '../theme/appearance.svelte.ts';
	import { resolvePalette } from '../theme/themes.ts';

	onMount(() => {
		loadDesktop();
		// Coming from the login (keyboard open) or a long page, Safari keeps the
		// old scroll position: the home screen always starts at its top.
		window.scrollTo(0, 0);
		return watchSources();
	});
	let query = $state('');
	const all = $derived(programs());
	const byKey = $derived(new Map(all.map((p) => [p.key, p])));
	// The phone shows every program: folders open as groups below.
	const keys = $derived(
		arrangeIcons(
			desktop.prefs,
			all.map((p) => ({ key: p.key, label: p.label, kind: p.kind })),
		).flatMap((k) =>
			k.startsWith('folder:')
				? (desktop.prefs.folders.find((f) => 'folder:' + f.id === k)?.apps ?? [])
				: [k],
		),
	);
	const shown = $derived(
		keys
			.map((k) => byKey.get(k))
			.filter((p) => !!p)
			.filter((p) => !query || p.label.toLowerCase().includes(query.toLowerCase())),
	);
	const missing = $derived(
		all.filter(
			(p) =>
				!keys.includes(p.key) && (!query || p.label.toLowerCase().includes(query.toLowerCase())),
		),
	);
	const light = $derived(!resolvePalette(appearance.current, appearance.systemLight).dark);
	const info = $derived(system.info);
	const memPct = $derived(
		info
			? (100 * (info.memory.totalBytes - info.memory.availableBytes)) / info.memory.totalBytes
			: 0,
	);
	const bad = $derived((storage.status?.volumes ?? []).filter((v) => v.state !== 'ok').length);

	// PLAN 63: a Docker app opens full screen in its remote view (a history
	// entry of its own, so the phone's Back returns to the grid).
	const remote = $derived(page.state.remote ? (byKey.get(page.state.remote) ?? null) : null);
	function open(p: Program) {
		// PLAN 66: a web app opens full screen above the grid, like a Docker app.
		if (p.kind === 'webapp')
			return void goto(page.url.pathname, {
				state: { webapp: { id: p.key.slice(7) } },
				shallow: true,
			});
		if (p.kind !== 'docker') return void goto(p.href);
		void goto(page.url.pathname, { state: { remote: p.key }, shallow: true });
	}
</script>

{#if remote}
	<div class="remote-full">
		{#await import('./RemoteView.svelte') then { default: RemoteView }}
			<RemoteView
				app={remote.key.replace(/^docker:/, '')}
				title={remote.label}
				phone
				onback={() => history.back()}
				onopentab={() => remote && openTab(remote.key, remote.href)}
			/>
		{:catch}
			<p class="load-failed">{t('desktop.window.loadFailed')}</p>
		{/await}
	</div>
{/if}
<div
	class="home"
	class:light
	style:--wallpaper="{desktop.prefs.wallpaper.source === 'custom'
		? `url('/api/v1/background?v=${desktop.prefs.wallpaper.version}')`
		: wallpaperCss(
				desktop.prefs.wallpaper.source === 'builtin' ? desktop.prefs.wallpaper.id : 'wave',
				light,
			)} center / cover"
>
	<header class="top">
		<label class="search glass-panel">
			<Icon name="search" size={17} />
			<input
				bind:value={query}
				placeholder={t('desktop.menu.search')}
				aria-label={t('desktop.menu.search')}
			/>
		</label>
		{#if clusterNodes.nodes.length > 1}<span class="node"><NodeMenu variant="phone" /></span>{/if}
		<span class="account glass-panel"><AccountMenu /></span>
	</header>
	<div class="status glass-panel">
		<span class="dot" class:warn={bad > 0} aria-hidden="true">{bad ? '!' : '✓'}</span>
		<div>
			<b>{bad ? t('desktop.widget.attention') : t('desktop.widget.allGood')}</b>
			<div class="muted small">
				{info
					? `${t('desktop.widget.cpu')} ${formatPercent(info.cpu.usagePercent)} · ${t('desktop.widget.ram')} ${formatPercent(memPct)}`
					: ''}
			</div>
		</div>
	</div>
	<div class="grid">
		{#each [...shown, ...missing] as p (p.key)}
			<button type="button" class="app" onclick={() => open(p)}>
				<span class="wrap">
					<DesktopIcon
						glyph={p.glyph}
						gradient={p.gradient}
						image={p.image}
						name={p.label}
						size={58}
					/>
					{#if p.badge?.count}<span class="badge">{p.badge.count}</span
						>{:else if p.badge?.warn}<span class="badge warn">!</span>{/if}
				</span>
				<span class="label">{p.label}</span>
			</button>
		{/each}
	</div>
</div>

<!-- PLAN 66: once after the update to 0.63, for owners and administrators. -->
<MigrationDialog />

<style>
	.load-failed {
		margin: 0;
		padding: var(--space-5);
		color: var(--text);
	}
	.remote-full {
		position: fixed;
		inset: 0;
		z-index: 50;
		display: flex;
		background: var(--bg);
	}
	.home {
		min-height: 100dvh;
		overflow-x: clip;
		/* PLAN 64: safe areas on every side (iPhone across: the notch is at a side). */
		padding: 0 calc(var(--space-3) + env(safe-area-inset-right))
			calc(20px + env(safe-area-inset-bottom)) calc(12px + env(safe-area-inset-left));
		position: relative;
		isolation: isolate;
		width: 100%;
		max-width: 100vw;
	}
	.home::before {
		content: '';
		position: fixed;
		inset: 0;
		z-index: -1;
		background: var(--wallpaper);
		filter: var(--wallpaper-filter, none);
	}
	/* The search stays in sight while the grid scrolls, below the status bar. */
	.top {
		position: sticky;
		top: 0;
		z-index: 5;
		display: flex;
		gap: var(--space-2);
		align-items: center;
		margin: 0 -12px var(--space-3);
		padding: calc(var(--space-2-5) + env(safe-area-inset-top)) var(--space-3) var(--space-2-5);
		background: linear-gradient(to bottom, rgb(0 0 0 / 0.28), transparent);
	}
	.light .top {
		background: linear-gradient(to bottom, rgb(255 255 255 / 0.4), transparent);
	}
	.node {
		flex: none;
		display: flex;
	}
	.search {
		flex: 1;
		display: flex;
		align-items: center;
		gap: var(--space-2);
		height: 44px;
		padding: 0 14px;
		border-radius: calc(14px * min(var(--radius-scale, 1), 1.6));
		color: var(--muted);
		min-width: 0;
	}
	.search input {
		flex: 1;
		min-width: 0;
		border: 0;
		background: none;
		color: var(--text);
		font: inherit;
		font-size: 16px;
		outline: none;
	}
	.account {
		border-radius: 14px;
		padding: 2px;
	}
	.status {
		display: flex;
		align-items: center;
		gap: var(--space-3);
		padding: var(--space-3);
		border-radius: 16px;
		margin-bottom: var(--space-4);
	}
	.dot {
		width: 38px;
		height: 38px;
		flex: none;
		border-radius: 50%;
		display: grid;
		place-items: center;
		background: linear-gradient(160deg, #4fd67a, #1b9a4b);
		color: #fff;
		font-weight: 700;
	}
	.dot.warn {
		background: linear-gradient(160deg, #ffd84d, #f0a91a);
		color: #2b2100;
	}
	.grid {
		display: grid;
		grid-template-columns: repeat(4, minmax(0, 1fr));
		gap: var(--space-4) var(--space-1);
	}
	.app {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: var(--space-1-5);
		border: 0;
		background: none;
		color: #fff;
		font: inherit;
		font-size: 11.5px;
		font-weight: 500;
		text-align: center;
		text-shadow: 0 1px 3px rgb(0 0 0 / 0.65);
		cursor: pointer;
		min-width: 0;
	}
	.light .app {
		color: var(--text);
		text-shadow: 0 0 3px rgb(255 255 255 / 0.9);
	}
	.label {
		max-width: 84px;
		hyphens: auto;
		overflow-wrap: anywhere;
		line-height: 1.2;
	}
	.wrap {
		position: relative;
	}
	.badge {
		position: absolute;
		top: -6px;
		right: -6px;
		min-width: 20px;
		height: 20px;
		padding: 0 5px;
		border-radius: 10px;
		background: #d93a3f;
		color: #fff;
		font-size: 11px;
		font-weight: 700;
		display: grid;
		place-items: center;
		text-shadow: none;
	}
	.badge.warn {
		background: #f0a91a;
		color: #2b2100;
	}
	.muted {
		color: var(--muted);
	}
	.small {
		font-size: var(--text-sm);
	}
</style>
