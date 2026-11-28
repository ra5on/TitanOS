<script lang="ts">
	// §121 (owner 28.11.2026, draft Entwuerfe/121-handy-start/E-start-kompakt):
	// the phone's start page is one compact page – the NAS with its state,
	// then the sections the account chose in its order: values, apps, quick
	// folders, storage and the latest notices. Long press or "Anpassen"
	// changes favorites, sections and the dock. The pages with widgets of
	// PLAN 87 stay at "/?phone=pages"; the list of all apps is "/?phone=apps".
	import { onMount, tick } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import Icon from '#lib/components/Icon.svelte';
	import NodeMenu from '#lib/components/NodeMenu.svelte';
	import Dialog from '#lib/components/Dialog.svelte';
	import LiveUsage from '#lib/components/LiveUsage.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import { system } from '#lib/system.svelte.ts';
	import { api, type FileRoot } from '#lib/api.ts';
	import {
		formatBytes,
		formatDuration,
		formatMegabytesPerSecond,
		formatPercent,
	} from '#lib/format.ts';
	import { live, watchLive } from '#lib/live-usage.svelte.ts';
	import { loadStorageRing, type StorageRing } from '#lib/storage-ring.ts';
	import {
		noticeText,
		noticeOwnLink,
		markRead,
		unreadCount,
		visibleNotices,
	} from '#lib/notifications.svelte.ts';
	import { appearance } from '#lib/theme/appearance.svelte.ts';
	import { resolvePalette } from '#lib/theme/themes.ts';
	import { isLivePicture } from '#lib/live/shaders.ts';
	import LiveBackground from '#lib/live/LiveBackground.svelte';
	import { motionOK } from '#lib/components/sheet.ts';
	import MigrationDialog from '#lib/webapps/MigrationDialog.svelte';
	import DesktopIcon from './DesktopIcon.svelte';
	import PhoneHeader from './PhoneHeader.svelte';
	import PhoneHome from './PhoneHome.svelte';
	import PhoneApps from './PhoneApps.svelte';
	import { dayTint, wallpaperCss } from './wallpapers.ts';
	import { PHONE_FAVORITES_DEFAULT, startSections } from './phone-home.ts';
	import { programs } from './programs.svelte.ts';
	import { watchSources, dockerUpdates } from './sources.svelte.ts';
	import { desktop, loadDesktop, savePrefs } from './wm.svelte.ts';
	import { phoneHold } from './phone-hold.ts';
	import { phoneChrome } from './immersive-phone.svelte.ts';
	import { openPhoneProgram } from './phone-open.ts';
	import { closeIntoSymbol } from './phone-motion.ts';

	let header = $state<PhoneHeader>();
	let now = $state(new Date());
	let systemUpdate = $state(false);
	let liveOpen = $state(false);
	let choosing = $state(false);
	let roots = $state<FileRoot[] | null>(null);
	let ring = $state<StorageRing | null>(null);
	const admin = $derived(['admin', 'owner'].includes(session.data?.user?.role ?? ''));
	const light = $derived(!resolvePalette(appearance.current, appearance.systemLight).dark);
	const wp = $derived(desktop.prefs.wallpaper);
	const wallpaper = $derived(
		wp.source === 'custom'
			? `url('/api/v1/background?v=${wp.version}')`
			: wallpaperCss(wp.id, light),
	);
	const livePicture = $derived(wp.source === 'builtin' && isLivePicture(wp.id) ? wp.id : null);
	const byKey = $derived(new Map(programs().map((p) => [p.key, p])));
	const favorites = $derived(
		(desktop.prefs.phone.favorites ?? PHONE_FAVORITES_DEFAULT).flatMap((key) => {
			const p = byKey.get(key);
			return p ? [p] : [];
		}),
	);
	const sections = $derived(startSections(desktop.prefs.phone).filter((s) => s.shown));
	const unread = $derived(unreadCount());
	const latest = $derived(visibleNotices().slice(0, 2));
	const updates = $derived(admin ? (systemUpdate ? 1 : 0) + dockerUpdates() : 0);
	const info = $derived(system.info);
	const address = $derived(info?.network?.ipv4?.split('/')[0] ?? '');
	const shares = $derived((roots ?? []).filter((r) => r.kind === 'share'));
	const quick = $derived.by(() => {
		const chosen = desktop.prefs.phone.start?.folders;
		if (!chosen) return shares.slice(0, 4);
		return chosen.flatMap((name) => shares.filter((s) => s.name === name));
	});
	const v = $derived(live.value);
	const share = (used: number, total: number) =>
		total > 0 ? Math.min(100, (used / total) * 100) : 0;
	const RING_COLORS = { apps: '#6cb6ff', files: '#c69df7', vms: '#f0a35a', system: '#9aa3ad' };

	const notices = () => window.dispatchEvent(new Event('titanos-phone-notices'));
	const edit = () => {
		phoneChrome.customizeDock = false;
		phoneChrome.customize = true;
	};
	onMount(() => {
		loadDesktop();
		const timer = setInterval(() => (now = new Date()), 60000);
		const stop = watchSources();
		const stopLive = watchLive(admin);
		void tick().then(() =>
			closeIntoSymbol((key) =>
				document.querySelector<HTMLElement>(`[data-home-key="${CSS.escape(key)}"] .symbol`),
			),
		);
		return () => {
			clearInterval(timer);
			stop();
			stopLive();
		};
	});
	$effect(() => {
		if (!sections.some((s) => s.id === 'folders')) return;
		let active = true;
		api
			.get<FileRoot[]>('/files/roots')
			.then((list) => active && (roots = Array.isArray(list) ? list : []))
			.catch(() => active && (roots = []));
		return () => {
			active = false;
		};
	});
	$effect(() => {
		if (!admin || !sections.some((s) => s.id === 'storage')) return;
		let active = true;
		const read = () =>
			loadStorageRing(session.services.docker !== false)
				.then((r) => active && (ring = r))
				.catch(() => {});
		void read();
		const timer = setInterval(read, 60000);
		return () => {
			active = false;
			clearInterval(timer);
		};
	});
	$effect(() => {
		if (!admin) return;
		let active = true;
		void api
			.get<{ available: unknown }>('/update')
			.then((result) => {
				if (active) systemUpdate = !!result.available;
			})
			.catch(() => {});
		return () => {
			active = false;
		};
	});
	function toggleFolder(name: string) {
		const current = desktop.prefs.phone.start?.folders ?? quick.map((s) => s.name);
		const next = current.includes(name)
			? current.filter((n) => n !== name)
			: [...current, name].slice(0, 8);
		savePrefs((p) => {
			p.phone = {
				...p.phone,
				start: {
					order: p.phone.start?.order ?? [],
					hidden: p.phone.start?.hidden ?? [],
					folders: next,
				},
			};
		});
	}
	function ringPath(start: number, part: number): string {
		// An arc on a circle of radius 40 around (50, 50), clockwise from the top.
		const r = 40;
		const a0 = start * 2 * Math.PI - Math.PI / 2;
		const a1 = (start + part) * 2 * Math.PI - Math.PI / 2;
		const large = part > 0.5 ? 1 : 0;
		return `M ${50 + r * Math.cos(a0)} ${50 + r * Math.sin(a0)} A ${r} ${r} 0 ${large} 1 ${50 + r * Math.cos(a1)} ${50 + r * Math.sin(a1)}`;
	}
	const arcs = $derived.by(() => {
		if (!ring || ring.total <= 0) return [];
		let at = 0;
		return ring.parts
			.filter((p) => p.bytes > 0)
			.map((p) => {
				const part = Math.min(0.9999, p.bytes / ring!.total);
				const arc = { id: p.id, d: ringPath(at, part) };
				at += part;
				return arc;
			});
	});
</script>

{#if page.url.searchParams.get('phone') === 'pages' || page.state.remote}
	<PhoneHome onhome={() => goto('/')} />
{:else}
	<div class="nord-home" style:--wallpaper={wallpaper} style:--tint="{dayTint(now)}deg">
		<div class="paper" aria-hidden="true">
			{#if livePicture}<LiveBackground picture={livePicture} moving={wp.live && motionOK()} />{/if}
		</div>
		<PhoneHeader bind:this={header} bare />
		{#if page.url.searchParams.get('phone') === 'apps'}
			<PhoneApps onsearch={() => header?.showMenu(true)} />
		{:else}
			<main class="body" use:phoneHold={edit} data-nord-home>
				<section class="hero">
					<div class="title">
						<h1><NodeMenu variant="start" /></h1>
						<p class="state">
							<span class="dot" class:off={!system.reachable} aria-hidden="true"></span>
							{system.reachable ? t('start.connected') : t('start.unreachable')}
						</p>
						{#if info}<p class="facts">
								{[address, t('start.uptime', { time: formatDuration(info.uptimeSeconds) })]
									.filter(Boolean)
									.join(' · ')}
							</p>{/if}
					</div>
					<div class="round-tools">
						<button
							type="button"
							class="round"
							aria-label={t('desktop.menu.search')}
							onclick={() => header?.showMenu(true)}><Icon name="search" size={19} /></button
						>
						<button
							type="button"
							class="round"
							data-phone-bell
							aria-label={unread ? t('phone.bellUnread', { count: unread }) : t('phone.bell')}
							onclick={notices}
							><Icon name="bell" size={19} />{#if unread}<span class="badge"
									>{unread > 9 ? '9+' : unread}</span
								>{/if}</button
						>
					</div>
				</section>

				{#each sections as section (section.id)}
					{#if section.id === 'values'}
						<button
							type="button"
							class="strip surface"
							aria-label={t('live.open')}
							onclick={() => (liveOpen = true)}
						>
							<span
								><small>{t('live.cpu')}</small><b>{v ? formatPercent(v.cpu) : '–'}</b><i class="bar"
									><i style:width="{v?.cpu ?? 0}%"></i></i
								></span
							>
							<span
								><small>{t('start.ram')}</small><b>{v ? formatBytes(v.memoryUsed) : '–'}</b><i
									class="bar"><i style:width="{v ? share(v.memoryUsed, v.memoryTotal) : 0}%"></i></i
								></span
							>
							<span
								><small>{t('live.storage')}</small><b
									>{v && v.storageTotal
										? formatPercent(share(v.storageUsed, v.storageTotal))
										: '–'}</b
								><i class="bar"
									><i style:width="{v ? share(v.storageUsed, v.storageTotal) : 0}%"></i></i
								></span
							>
							<span
								><small>{t('start.net')}</small><b
									>{v ? t('live.rate', { value: formatMegabytesPerSecond(v.rx + v.tx) }) : '–'}</b
								></span
							>
						</button>
					{:else if section.id === 'apps'}
						<div class="section-title">
							<a class="title-link" href="/?phone=apps"
								><h2>{t('phone.tabs.apps')}</h2>
								<Icon name="chevronRight" size={18} /></a
							>
							{#if updates}<a class="pill accent" href="/settings/updates"
									>{t('start.updates', { count: updates })}</a
								>{/if}
						</div>
						<div class="apps surface">
							{#each favorites as app (app.key)}
								<button
									type="button"
									data-home-key={app.key}
									aria-label={app.label}
									onclick={(e) => openPhoneProgram(app, e.currentTarget.querySelector('.symbol'))}
								>
									<span class="symbol"
										><DesktopIcon
											glyph={app.glyph}
											gradient={app.gradient}
											image={app.image}
											size={44}
											name=""
										/>{#if app.badge?.count}<span class="badge">{app.badge.count}</span>{/if}</span
									>
									<span class="app-name">{app.label}</span>
								</button>
							{:else}
								<p class="empty">{t('phone.nord.noFavorites')}</p>
							{/each}
						</div>
					{:else if section.id === 'folders' && roots && shares.length}
						<div class="section-title">
							<h2>{t('start.folders')}</h2>
							<a class="pill" href="/files">{t('start.allFiles')}</a>
						</div>
						<div class="folders" use:phoneHold={() => (choosing = true)}>
							{#each quick as root (root.id)}
								<a class="folder surface" href="/files?root={encodeURIComponent(root.id)}&path="
									><span class="folder-symbol"><Icon name="folder" size={20} /></span><span
										><b>{root.name}</b><small
											>{t('start.free', { value: formatBytes(root.freeBytes) })}</small
										></span
									></a
								>
							{/each}
							<button type="button" class="folder surface choose" onclick={() => (choosing = true)}
								><span class="folder-symbol"><Icon name="edit" size={18} /></span><span
									><b>{t('start.chooseFolders')}</b></span
								></button
							>
						</div>
					{:else if section.id === 'storage' && admin && ring}
						<div class="section-title">
							<h2>{t('live.storage')}</h2>
							<button type="button" class="pill" onclick={() => (liveOpen = true)}
								>{t('live.title')}</button
							>
						</div>
						<a class="storage surface" href="/storage">
							<svg viewBox="0 0 100 100" width="84" height="84" aria-hidden="true">
								<circle cx="50" cy="50" r="40" class="track" />
								{#each arcs as arc (arc.id)}<path
										d={arc.d}
										stroke={RING_COLORS[arc.id]}
										class="arc"
									/>{/each}
								<text x="50" y="49" class="pct">{formatPercent(share(ring.used, ring.total))}</text>
								<text x="50" y="64" class="pct-label">{t('start.used')}</text>
							</svg>
							<span class="legend">
								<b
									>{t('start.usedOf', {
										used: formatBytes(ring.used),
										total: formatBytes(ring.total),
									})}</b
								>
								{#each ring.parts as part (part.id)}
									<span class="legend-row"
										><span
											><i class="swatch" style:background={RING_COLORS[part.id]}></i>{t(
												`start.part.${part.id}`,
											)}</span
										><span class="muted"
											>{part.id === 'apps' && ring.measuring
												? t('start.measuring')
												: formatBytes(part.bytes)}</span
										></span
									>
								{/each}
							</span>
						</a>
					{:else if section.id === 'notices'}
						<div class="section-title">
							<h2>{t('phone.nord.glance')}</h2>
							<button type="button" class="pill" onclick={notices}>{t('start.allNotices')}</button>
						</div>
						<div class="list surface">
							{#each latest as notice (notice.id)}
								{@const label = noticeText(notice)}
								<button
									type="button"
									onclick={() => {
										const href = noticeOwnLink(notice);
										if (href) {
											markRead(notice.id);
											void goto(href);
										} else notices();
									}}
								>
									<span class="row-symbol"
										><Icon name={notice.kind.includes('mail') ? 'mail' : 'bell'} size={18} /></span
									>
									<span class="row-copy"><b>{label.title}</b><small>{label.text}</small></span><Icon
										name="chevronRight"
										size={16}
									/>
								</button>
							{/each}
							{#if !latest.length}<button type="button" onclick={notices}
									><span class="row-symbol"><Icon name="bell" size={18} /></span>
									<span class="row-copy">{t('phone.nord.noNotices')}</span><Icon
										name="chevronRight"
										size={16}
									/></button
								>{/if}
						</div>
					{/if}
				{/each}
				<button type="button" class="customize" onclick={edit}
					><Icon name="edit" size={15} />{t('start.customize')}</button
				>
			</main>
		{/if}
	</div>
	{#if liveOpen}
		<Dialog title={t('live.title')} onclose={() => (liveOpen = false)} dismiss>
			<LiveUsage onnavigate={() => (liveOpen = false)} />
		</Dialog>
	{/if}
	{#if choosing && roots}
		<Dialog title={t('start.chooseFolders')} onclose={() => (choosing = false)} dismiss>
			<p class="choose-hint">{t('start.chooseHint')}</p>
			<div class="choose-list">
				{#each shares as root (root.id)}
					<label
						><input
							type="checkbox"
							checked={quick.some((q) => q.name === root.name)}
							onchange={() => toggleFolder(root.name)}
						/><span>{root.name}</span></label
					>
				{/each}
			</div>
		</Dialog>
	{/if}
	<MigrationDialog />
{/if}

<style>
	.nord-home {
		position: fixed;
		inset: 0;
		background: #0b202a;
		isolation: isolate;
		display: flex;
		flex-direction: column;
		color: #f5f9fb;
		--glass: var(--nord-chrome);
	}
	.paper,
	.nord-home::before {
		position: absolute;
		inset: 0;
		pointer-events: none;
	}
	.paper {
		z-index: -2;
		background: var(--wallpaper) center / cover;
		filter: hue-rotate(var(--tint)) var(--wallpaper-filter, saturate(1));
	}
	.nord-home::before {
		content: '';
		z-index: -1;
		background: linear-gradient(
			180deg,
			rgb(2 13 20 / 0.42),
			rgb(4 20 27 / 0.18) 44%,
			rgb(4 18 24 / 0.44)
		);
	}
	.body {
		box-sizing: border-box;
		flex: 1;
		min-height: 0;
		width: 100%;
		max-width: 680px;
		margin: auto;
		overflow-y: auto;
		overscroll-behavior: contain;
		padding: calc(14px + env(safe-area-inset-top)) max(16px, env(safe-area-inset-right))
			calc(var(--phone-dock-height, 0px) + 34px + env(safe-area-inset-bottom))
			max(16px, env(safe-area-inset-left));
		text-shadow: 0 1px 5px #0b1b24;
		-webkit-user-select: none;
		user-select: none;
		-webkit-touch-callout: none;
	}
	h1,
	h2,
	p {
		margin: 0;
	}
	.hero {
		display: flex;
		align-items: flex-start;
		justify-content: space-between;
		gap: 10px;
	}
	.title {
		min-width: 0;
	}
	.state,
	.facts {
		font-size: 0.86rem;
		overflow-wrap: anywhere;
	}
	.state {
		display: flex;
		align-items: center;
		gap: 7px;
		font-weight: 600;
	}
	.facts {
		color: rgb(245 249 251 / 0.85);
		margin-top: 2px;
	}
	.dot {
		width: 9px;
		height: 9px;
		border-radius: 50%;
		background: #3fb950;
		box-shadow: 0 0 0 3px rgb(63 185 80 / 0.25);
	}
	.dot.off {
		background: #e3b341;
		box-shadow: 0 0 0 3px rgb(227 179 65 / 0.25);
	}
	.round-tools {
		display: flex;
		gap: 8px;
		flex: none;
	}
	.round,
	.pill,
	.customize,
	.strip,
	.apps button,
	.list button,
	.folder {
		font: inherit;
		color: inherit;
		cursor: pointer;
		-webkit-tap-highlight-color: transparent;
	}
	.round {
		position: relative;
		display: grid;
		place-items: center;
		width: 44px;
		height: 44px;
		border-radius: 50%;
		border: 1px solid rgb(224 244 255 / 0.23);
		background: var(--glass);
		-webkit-backdrop-filter: var(--nord-blur);
		backdrop-filter: var(--nord-blur);
	}
	.badge {
		position: absolute;
		top: -3px;
		right: -3px;
		min-width: 18px;
		height: 18px;
		padding: 0 5px;
		border-radius: 9px;
		background: #e5534b;
		color: #fff;
		font-size: 0.68rem;
		font-weight: 700;
		display: grid;
		place-items: center;
		text-shadow: none;
	}
	.surface {
		border: 1px solid #c7f0fb40;
		background: var(--glass);
		-webkit-backdrop-filter: var(--nord-blur);
		backdrop-filter: var(--nord-blur);
		text-shadow: none;
	}
	.strip {
		display: grid;
		grid-template-columns: repeat(4, minmax(0, 1fr));
		width: 100%;
		margin-top: 14px;
		padding: 0;
		border-radius: 16px;
		text-align: start;
	}
	.strip > span {
		display: flex;
		flex-direction: column;
		padding: 9px 6px 10px 10px;
		min-width: 0;
	}
	.strip > span + span {
		border-left: 1px solid #dff6ff2e;
	}
	.strip small {
		font-size: 0.72rem;
		opacity: 0.82;
	}
	.strip b {
		font-size: 0.98rem;
		overflow-wrap: anywhere;
	}
	.bar {
		display: block;
		height: 4px;
		margin-top: 6px;
		border-radius: 3px;
		background: rgb(255 255 255 / 0.14);
		overflow: hidden;
	}
	.bar > i {
		display: block;
		height: 100%;
		border-radius: 3px;
		background: var(--accent);
	}
	.section-title {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 8px;
		min-height: 44px;
		margin-top: 14px;
	}
	h2 {
		font-size: 1.08rem;
		letter-spacing: -0.02em;
		font-weight: 700;
	}
	.title-link {
		display: flex;
		align-items: center;
		gap: 2px;
		min-height: 44px;
		color: inherit;
		text-decoration: none;
	}
	.pill {
		display: inline-flex;
		align-items: center;
		min-height: 36px;
		padding: 0 13px;
		border-radius: 999px;
		border: 1px solid rgb(224 244 255 / 0.2);
		background: var(--glass);
		-webkit-backdrop-filter: var(--nord-blur);
		backdrop-filter: var(--nord-blur);
		font-size: 0.82rem;
		font-weight: 600;
		text-decoration: none;
		text-shadow: none;
		white-space: nowrap;
	}
	.pill.accent {
		background: var(--accent);
		border-color: var(--accent);
		color: var(--on-accent, #0b1622);
	}
	.apps {
		display: grid;
		grid-template-columns: repeat(4, minmax(0, 1fr));
		gap: 10px 2px;
		padding: 10px 6px;
		border-radius: 18px;
	}
	.apps button {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 4px;
		min-width: 0;
		padding: 2px;
		border: 0;
		background: none;
	}
	.symbol {
		position: relative;
		display: block;
	}
	.app-name {
		font-size: 0.72rem;
		font-weight: 500;
		line-height: 1.2;
		text-align: center;
		hyphens: manual;
		overflow-wrap: normal;
		word-break: keep-all;
		max-width: calc(100% + 4px);
	}
	.empty {
		grid-column: 1 / -1;
		padding: 6px;
	}
	.folders {
		display: grid;
		grid-template-columns: repeat(2, minmax(0, 1fr));
		gap: 8px;
	}
	.folder {
		display: flex;
		align-items: center;
		gap: 10px;
		min-height: 52px;
		padding: 8px 10px;
		border-radius: 14px;
		text-decoration: none;
		text-align: start;
	}
	.folder > span:last-child {
		display: flex;
		flex-direction: column;
		min-width: 0;
	}
	.folder b {
		font-size: 0.9rem;
		overflow-wrap: anywhere;
	}
	.folder small {
		font-size: 0.74rem;
		opacity: 0.8;
	}
	.folder-symbol {
		display: grid;
		place-items: center;
		width: 32px;
		height: 32px;
		flex: none;
		border-radius: 9px;
		color: var(--accent);
		background: color-mix(in srgb, var(--accent) 18%, transparent);
	}
	.choose {
		border-style: dashed;
	}
	.storage {
		display: flex;
		align-items: center;
		gap: 14px;
		padding: 12px;
		border-radius: 18px;
		color: inherit;
		text-decoration: none;
	}
	.track {
		fill: none;
		stroke: rgb(255 255 255 / 0.12);
		stroke-width: 11;
	}
	.arc {
		fill: none;
		stroke-width: 11;
	}
	.pct {
		fill: currentColor;
		font-size: 15px;
		font-weight: 700;
		text-anchor: middle;
	}
	.pct-label {
		fill: currentColor;
		font-size: 9px;
		text-anchor: middle;
	}
	.legend {
		display: flex;
		flex-direction: column;
		gap: 3px;
		flex: 1;
		min-width: 0;
		font-size: 0.82rem;
	}
	.legend b {
		font-size: 0.95rem;
	}
	.legend-row {
		display: flex;
		justify-content: space-between;
		gap: 8px;
	}
	.legend-row > span:first-child {
		display: flex;
		align-items: center;
		gap: 7px;
	}
	.swatch {
		width: 14px;
		height: 5px;
		border-radius: 3px;
	}
	.muted {
		opacity: 0.82;
	}
	.list {
		border-radius: 18px;
		overflow: hidden;
	}
	.list button {
		display: flex;
		width: 100%;
		align-items: center;
		gap: 10px;
		padding: 8px 12px;
		min-height: 52px;
		border: 0;
		background: none;
		text-align: start;
	}
	.list button + button {
		border-top: 1px solid #dff6ff2e;
	}
	.row-symbol {
		display: grid;
		place-items: center;
		border-radius: 9px;
		background: linear-gradient(145deg, #3b6974, #224558);
		width: 30px;
		height: 30px;
		flex: none;
	}
	.row-copy {
		min-width: 0;
		flex: 1;
	}
	.row-copy b,
	.row-copy small {
		display: block;
		overflow-wrap: anywhere;
	}
	.row-copy b {
		font-size: 0.9rem;
	}
	.row-copy small {
		font-size: 0.78rem;
		opacity: 0.84;
	}
	.customize {
		display: flex;
		align-items: center;
		gap: 6px;
		margin: 16px auto 0;
		min-height: 44px;
		border: 0;
		background: none;
		font-size: 0.86rem;
		font-weight: 600;
		text-shadow: inherit;
	}
	.choose-hint {
		margin-bottom: 10px;
		color: var(--muted);
	}
	.choose-list {
		display: flex;
		flex-direction: column;
	}
	.choose-list label {
		display: flex;
		align-items: center;
		gap: 12px;
		min-height: 48px;
		overflow-wrap: anywhere;
	}
	@media (prefers-reduced-transparency: reduce) {
		.surface,
		.round,
		.pill {
			background: #1a3038;
			backdrop-filter: none;
		}
	}
</style>
