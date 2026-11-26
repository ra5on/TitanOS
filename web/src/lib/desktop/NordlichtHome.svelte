<script lang="ts">
	import { onMount, tick } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import Icon from '#lib/components/Icon.svelte';
	import NodeMenu from '#lib/components/NodeMenu.svelte';
	import { i18n, t } from '#lib/i18n/index.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import { system } from '#lib/system.svelte.ts';
	import { storage } from '#lib/storage.svelte.ts';
	import { api } from '#lib/api.ts';
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
	import { dayTint, wallpaperCss } from './wallpapers.ts';
	import { PHONE_FAVORITES_DEFAULT } from './phone-home.ts';
	import { programs } from './programs.svelte.ts';
	import { watchSources, dockerUpdates } from './sources.svelte.ts';
	import { desktop, loadDesktop } from './wm.svelte.ts';
	import { phoneHold } from './phone-hold.ts';
	import { phoneChrome } from './immersive-phone.svelte.ts';
	import { openPhoneProgram } from './phone-open.ts';
	import { closeIntoSymbol } from './phone-motion.ts';

	let header = $state<PhoneHeader>();
	let now = $state(new Date());
	let systemUpdate = $state(false);
	const admin = $derived(['admin', 'owner'].includes(session.data?.user?.role ?? ''));
	const light = $derived(!resolvePalette(appearance.current, appearance.systemLight).dark);
	const wp = $derived(desktop.prefs.wallpaper);
	const wallpaper = $derived(
		wp.source === 'custom'
			? `url('/api/v1/background?v=${wp.version}')`
			: wallpaperCss(wp.id, light),
	);
	const live = $derived(wp.source === 'builtin' && isLivePicture(wp.id) ? wp.id : null);
	const byKey = $derived(new Map(programs().map((p) => [p.key, p])));
	const favorites = $derived(
		(desktop.prefs.phone.favorites ?? PHONE_FAVORITES_DEFAULT).flatMap((key) => {
			const p = byKey.get(key);
			return p ? [p] : [];
		}),
	);
	const unread = $derived(unreadCount());
	const latest = $derived(visibleNotices().slice(0, 2));
	const updates = $derived(admin ? (systemUpdate ? 1 : 0) + dockerUpdates() : 0);
	const volumes = $derived(storage.status?.volumes ?? []);
	const problems = $derived(volumes.filter((v) => v.state !== 'ok').length);
	const storageApp = $derived(byKey.get('app:storage'));
	const date = $derived(
		new Intl.DateTimeFormat(i18n.locale, { weekday: 'long', day: 'numeric', month: 'long' }).format(
			now,
		),
	);
	const notices = () => window.dispatchEvent(new Event('titanos-phone-notices'));
	const edit = () => {
		phoneChrome.customizeDock = false;
		phoneChrome.customize = true;
	};
	onMount(() => {
		loadDesktop();
		const timer = setInterval(() => (now = new Date()), 60000);
		const stop = watchSources();
		void tick().then(() =>
			closeIntoSymbol((key) =>
				document.querySelector<HTMLElement>(`[data-home-key="${CSS.escape(key)}"] .symbol`),
			),
		);
		return () => {
			clearInterval(timer);
			stop();
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
</script>

{#if page.url.searchParams.get('phone') === 'pages' || page.state.remote}
	<PhoneHome onhome={() => goto('/')} />
{:else}
	<div class="nord-home" style:--wallpaper={wallpaper} style:--tint="{dayTint(now)}deg">
		<div class="paper" aria-hidden="true">
			{#if live}<LiveBackground picture={live} moving={wp.live && motionOK()} />{/if}
		</div>
		<PhoneHeader bind:this={header} />
		<main class="body" use:phoneHold={edit} data-nord-home>
			<section class="hero">
				<p class="date">{date}</p>
				<h1><NodeMenu variant="hero" /></h1>
				{#if storageApp && storage.status && system.reachable}
					<button
						type="button"
						class="status"
						class:warning={problems}
						onclick={() => goto(storageApp!.href)}
					>
						<Icon name={problems ? 'info' : 'check'} size={16} />
						{problems
							? t('desktop.widget.volumesAttention', { count: problems })
							: volumes.length
								? t('phone.nord.healthy')
								: t('desktop.widget.noVolumes')}
						<Icon name="chevronRight" size={15} />
					</button>
				{/if}
			</section>
			<section class="favorites">
				<div class="section-title">
					<h2>{t('phone.nord.favorites')}</h2>
					<button type="button" onclick={() => header?.showMenu()}
						>{t('phone.nord.allApps')}<Icon name="chevronRight" size={16} /></button
					>
				</div>
				{#if favorites.length}
					<div class="fav-grid">
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
										size={58}
										name=""
									/></span
								>
								<span class="app-name">{app.label}</span>
							</button>
						{/each}
					</div>
				{:else}<p class="empty">{t('phone.nord.noFavorites')}</p>{/if}
			</section>
			<section class="overview">
				<div class="section-title">
					<h2>{t('phone.nord.glance')}</h2>
					<button
						class="bell"
						type="button"
						data-phone-bell
						aria-label={unread ? t('phone.bellUnread', { count: unread }) : t('phone.bell')}
						onclick={notices}
						><Icon name="bell" size={19} />{#if unread}<span class="badge"
								>{unread > 9 ? '9+' : unread}</span
							>{/if}</button
					>
				</div>
				<div class="list">
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
								><Icon name={notice.kind.includes('mail') ? 'mail' : 'bell'} size={19} /></span
							>
							<span class="row-copy"><b>{label.title}</b><small>{label.text}</small></span><Icon
								name="chevronRight"
								size={16}
							/>
						</button>
					{/each}
					{#if updates}<button type="button" onclick={() => goto('/settings/updates')}
							><span class="row-symbol"><Icon name="download" size={19} /></span>
							<span class="row-copy"
								><b>{t('desktop.side.updates')}</b><small
									>{t('desktop.badge.updates', { count: updates })}</small
								></span
							><Icon name="chevronRight" size={16} /></button
						>{/if}
					{#if !latest.length && !updates}<button type="button" onclick={notices}
							><span class="row-symbol"><Icon name="bell" size={19} /></span>
							<span class="row-copy">{t('phone.nord.noNotices')}</span><Icon
								name="chevronRight"
								size={16}
							/></button
						>{/if}
				</div>
			</section>
			<button type="button" class="pages-link" onclick={() => goto('/?phone=pages')}
				><Icon name="grid" size={15} />{t('phone.nord.pages')}<Icon
					name="chevronRight"
					size={14}
				/></button
			>
		</main>
	</div>
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
			rgb(2 13 20 / 0.39),
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
		padding: 10px max(16px, env(safe-area-inset-right)) calc(108px + env(safe-area-inset-bottom))
			max(16px, env(safe-area-inset-left));
		text-shadow: 0 1px 5px #0b1b24;
		-webkit-user-select: none;
		user-select: none;
		-webkit-touch-callout: none;
	}
	.hero {
		padding: 5px 0 12px;
	}
	.date {
		font-size: 0.92rem;
		font-weight: 600;
		margin: 0 0 5px;
	}
	h1,
	h2 {
		margin: 0;
	}
	.status,
	.section-title button,
	.fav-grid button,
	.list button,
	.pages-link {
		font: inherit;
		color: inherit;
		border: 0;
		cursor: pointer;
	}
	.status {
		display: flex;
		align-items: center;
		gap: 9px;
		padding: 0 13px;
		min-height: 44px;
		background: rgb(18 66 60 / 0.88);
		border: 1px solid #a5f2e33d;
		border-radius: 16px;
		margin-top: 7px;
		font-size: 0.92rem;
		font-weight: 600;
	}
	.status.warning {
		background: #65482a;
	}
	.section-title {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 8px;
		min-height: 44px;
	}
	h2 {
		font-size: 1.26rem;
		letter-spacing: -0.035em;
		font-weight: 700;
	}
	.section-title button,
	.pages-link {
		display: flex;
		align-items: center;
		justify-content: center;
		gap: 5px;
		min-height: 44px;
		background: transparent;
		font-size: 0.9rem;
		font-weight: 600;
		text-shadow: inherit;
	}
	.fav-grid {
		display: grid;
		grid-template-columns: repeat(3, minmax(0, 1fr));
		gap: 14px 4px;
		margin-top: 4px;
	}
	.fav-grid button {
		display: flex;
		align-items: center;
		flex-direction: column;
		gap: 6px;
		min-width: 0;
		background: none;
		padding: 4px;
		text-shadow: inherit;
	}
	.app-name {
		font-weight: 600;
		font-size: 0.86rem;
		text-wrap: balance;
		overflow-wrap: anywhere;
	}
	.overview {
		margin-top: 16px;
	}
	.bell {
		position: relative;
		width: 44px;
	}
	.badge {
		position: absolute;
		right: 1px;
		top: 0;
		min-width: 17px;
		padding: 1px 4px;
		border-radius: 12px;
		background: #8fd8c9;
		color: #102d32;
		text-shadow: none;
		font-size: 0.69rem;
	}
	.list {
		border: 1px solid #c7f0fb40;
		border-radius: 21px;
		background: var(--glass);
		-webkit-backdrop-filter: var(--nord-blur);
		backdrop-filter: var(--nord-blur);
		overflow: hidden;
	}
	.list button {
		display: flex;
		width: 100%;
		align-items: center;
		gap: 10px;
		padding: 10px 12px;
		min-height: 60px;
		background: none;
		text-align: start;
		text-shadow: none;
	}
	.list button + button {
		border-top: 1px solid #dff6ff2e;
	}
	.row-symbol {
		display: grid;
		place-items: center;
		border-radius: 10px;
		background: linear-gradient(145deg, #3b6974, #224558);
		width: 34px;
		height: 34px;
		flex: none;
	}
	.row-copy {
		min-width: 0;
		flex: 1;
	}
	.row-copy b,
	.row-copy small {
		display: block;
		overflow: hidden;
		white-space: nowrap;
		text-overflow: ellipsis;
	}
	.row-copy small {
		opacity: 0.84;
	}
	.pages-link {
		margin: 16px auto 0;
	}
	.empty {
		margin: 6px 0 12px;
	}
	@media (prefers-reduced-transparency: reduce) {
		.list {
			background: #1a3038;
			backdrop-filter: none;
		}
	}
</style>
