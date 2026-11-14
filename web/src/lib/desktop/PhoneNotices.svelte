<script lang="ts">
	// PLAN 87: the notifications on the phone – pulled down from the top (the
	// home screen's bar, the bar of a program) or from the bell. A few quick
	// switches above, the list below: a swipe to the left deletes a message
	// (like its bin), to the right marks it read; pulling the list down loads
	// it anew. "Alle löschen" asks first.
	import { goto } from '$app/navigation';
	import Dialog from '../components/Dialog.svelte';
	import Icon from '../components/Icon.svelte';
	import SwipeRow from '../components/SwipeRow.svelte';
	import { formatDateTime } from '../format.ts';
	import { apiErrorText, t } from '../i18n/index.svelte.ts';
	import { noticeLink } from '../notice-link.ts';
	import {
		dismissNotifications,
		isUnread,
		loadNotifications,
		markRead,
		markSeen,
		noticeOwnLink,
		noticeText,
		visibleNotices,
	} from '../notifications.svelte.ts';
	import { system } from '../system.svelte.ts';
	import { appearance, saveAppearance } from '../theme/appearance.svelte.ts';
	import { resolvePalette, type ThemeId } from '../theme/themes.ts';
	import { desktop, savePrefs } from './wm.svelte.ts';
	import { PULL_DISTANCE } from './PhoneGestures.svelte';

	let open = $state(false);
	let busy = $state(false);
	let error = $state('');
	let confirmAll = $state<number[] | null>(null);

	// The list follows the NAS like the bell of the desktop.
	const latest = $derived(system.info?.notificationsLatest ?? 0);
	$effect(() => {
		latest;
		void loadNotifications();
	});
	$effect(() => {
		const show = () => (open = true);
		window.addEventListener('titanos-phone-notices', show);
		return () => window.removeEventListener('titanos-phone-notices', show);
	});

	const shown = $derived(visibleNotices());
	const SEVERITY = {
		info: 'notify.severity.info',
		warning: 'notify.severity.warning',
		error: 'notify.severity.error',
	} as const;
	const CHIP = { info: 'info', warning: 'warn', error: 'error' } as const;

	async function dismiss(ids: number[]) {
		busy = true;
		error = '';
		try {
			await dismissNotifications(ids);
			confirmAll = null;
		} catch (err) {
			error = apiErrorText(err);
		} finally {
			busy = false;
		}
	}
	function follow(href: string) {
		open = false;
		void goto(href);
	}

	// Quick switches: light or dark, the moving wallpaper, the phone's dock.
	const dark = $derived(resolvePalette(appearance.current, appearance.systemLight).dark);
	const DARK_KEY = 'titanos.phone.darkTheme';
	async function toggleDark() {
		let theme: ThemeId;
		if (dark) {
			try {
				localStorage.setItem(DARK_KEY, appearance.current.theme);
			} catch {
				// Not remembered: back to Graphit next time.
			}
			theme = 'hell';
		} else {
			let kept: string | null = null;
			try {
				kept = localStorage.getItem(DARK_KEY);
			} catch {
				kept = null;
			}
			theme =
				(['graphit', 'schwarz', 'mitternacht', 'eigen'] as const).find((x) => x === kept) ??
				'graphit';
		}
		try {
			await saveAppearance({ ...appearance.current, theme });
		} catch (err) {
			error = apiErrorText(err);
		}
	}
	const live = $derived(desktop.prefs.wallpaper.live);
	const dock = $derived(desktop.prefs.phone.dock.show);

	// Pulling the list down at its top loads it anew.
	let list = $state<HTMLElement>();
	let pull = $state(0);
	let pullFrom: number | null = null;
	let reloading = $state(false);
	function pullStart(e: TouchEvent) {
		pullFrom = list && list.scrollTop <= 0 && e.touches.length === 1 ? e.touches[0].clientY : null;
	}
	function pullMove(e: TouchEvent) {
		if (pullFrom === null) return;
		pull = Math.max(0, e.touches[0].clientY - pullFrom);
	}
	async function pullEnd() {
		if (pullFrom === null) return;
		pullFrom = null;
		const far = pull > PULL_DISTANCE;
		pull = 0;
		if (!far) return;
		reloading = true;
		await loadNotifications();
		reloading = false;
	}
</script>

{#if open}
	<Dialog
		title={t('notify.title')}
		onclose={() => {
			open = false;
			confirmAll = null;
		}}
	>
		<div class="quick" role="group" aria-label={t('phone.notices.quick')} data-no-sheet-swipe>
			<button
				type="button"
				class="switch-tile"
				aria-pressed={dark}
				data-quick="dark"
				onclick={toggleDark}
				><Icon name="eye" size={18} /><span>{t('phone.notices.dark')}</span><small
					>{dark ? t('look.on') : t('look.off')}</small
				></button
			>
			<button
				type="button"
				class="switch-tile"
				aria-pressed={live}
				data-quick="live"
				onclick={() =>
					savePrefs((p) => (p.wallpaper = { ...p.wallpaper, live: !p.wallpaper.live }))}
				><Icon name="photo" size={18} /><span>{t('phone.notices.live')}</span><small
					>{live ? t('look.on') : t('look.off')}</small
				></button
			>
			<button
				type="button"
				class="switch-tile"
				aria-pressed={dock}
				data-quick="dock"
				onclick={() =>
					savePrefs(
						(p) => (p.phone = { ...p.phone, dock: { ...p.phone.dock, show: !p.phone.dock.show } }),
					)}
				><Icon name="gridSmall" size={18} /><span>{t('phone.settings.dock')}</span><small
					>{dock ? t('look.on') : t('look.off')}</small
				></button
			>
		</div>
		<div class="head">
			<button type="button" class="btn" disabled={!shown.length} onclick={markSeen}
				>{t('phone.notices.allRead')}</button
			>
			<button
				type="button"
				class="btn"
				disabled={!shown.length || busy}
				onclick={() => (confirmAll = shown.map((n) => n.id))}>{t('notify.deleteAll')}</button
			>
			<a class="btn" href="/settings/notifications" onclick={() => (open = false)}
				>{t('notify.channels')}</a
			>
		</div>
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		{#if pull > 0 || reloading}
			<p class="reload muted" role="status">
				{t(
					reloading
						? 'phone.refresh.running'
						: pull > PULL_DISTANCE
							? 'phone.refresh.release'
							: 'phone.refresh.pull',
				)}
			</p>
		{/if}
		<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
		<ul
			class="list"
			bind:this={list}
			data-notices
			data-no-sheet-swipe
			ontouchstart={pullStart}
			ontouchmove={pullMove}
			ontouchend={pullEnd}
			ontouchcancel={pullEnd}
		>
			{#each shown as notice (notice.id)}
				{@const text = noticeText(notice)}
				{@const href = noticeOwnLink(notice) || noticeLink(notice.kind, notice.subject)}
				{@const fresh = isUnread(notice)}
				<li>
					<SwipeRow
						fullSwipe
						leading={fresh
							? {
									id: 'read',
									label: t('phone.notices.read'),
									icon: 'check',
									run: () => markRead(notice.id),
								}
							: undefined}
						actions={[
							{
								id: 'delete',
								label: t('common.delete'),
								icon: 'trash',
								danger: true,
								run: () => dismiss([notice.id]),
							},
						]}
					>
						<div class="notice" class:fresh data-notice={notice.id}>
							<div class="line">
								<span class="chip {CHIP[notice.severity]}">{t(SEVERITY[notice.severity])}</span>
								{#if fresh}<span class="new">{t('phone.notices.new')}</span>{/if}
								<time class="muted" datetime={notice.time}
									>{formatDateTime(new Date(notice.time))}</time
								>
							</div>
							<b>{text.title}</b>
							<span class="muted">{text.text}</span>
							<!-- The same without swiping: open, read, delete. -->
							<span class="row-actions">
								{#if href}<button type="button" class="as-link" onclick={() => follow(href)}
										>{t('ui.open')}</button
									>{/if}
								<span class="grow"></span>
								{#if fresh}<button
										type="button"
										class="icon-btn"
										data-notice-read
										aria-label={t('phone.notices.readOne', { title: text.title })}
										title={t('phone.notices.read')}
										onclick={() => markRead(notice.id)}><Icon name="check" size={18} /></button
									>{/if}
								<button
									type="button"
									class="icon-btn"
									data-notice-delete
									disabled={busy}
									aria-label={t('notify.deleteOne', { title: text.title })}
									title={t('common.delete')}
									onclick={() => dismiss([notice.id])}><Icon name="trash" size={18} /></button
								>
							</span>
						</div>
					</SwipeRow>
				</li>
			{:else}
				<li class="empty muted">{t('notify.empty')}</li>
			{/each}
		</ul>
		{#if shown.length}<p class="muted small">{t('phone.notices.swipeHint')}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (open = false)}>{t('common.close')}</button>
		</div>
	</Dialog>
{/if}
{#if confirmAll}
	<Dialog title={t('notify.deleteAllTitle')} locked={busy} onclose={() => (confirmAll = null)}>
		<p>{t('notify.deleteAllText', { count: confirmAll.length })}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={busy} onclick={() => (confirmAll = null)}
				>{t('common.cancel')}</button
			>
			<button
				type="button"
				class="btn btn-danger"
				disabled={busy}
				onclick={() => confirmAll && dismiss(confirmAll)}>{t('notify.deleteAllConfirm')}</button
			>
		</div>
	</Dialog>
{/if}

<style>
	.quick {
		display: grid;
		grid-template-columns: repeat(3, minmax(0, 1fr));
		gap: var(--space-2);
	}
	.switch-tile {
		min-height: 64px;
		display: flex;
		flex-direction: column;
		align-items: flex-start;
		justify-content: center;
		gap: 2px;
		padding: var(--space-2) var(--space-2-5);
		border: 1px solid var(--border);
		border-radius: 14px;
		background: var(--chip);
		color: var(--text);
		font: inherit;
		font-size: var(--text-sm);
		text-align: left;
		overflow-wrap: break-word;
		hyphens: auto;
		cursor: pointer;
	}
	.switch-tile[aria-pressed='true'] {
		background: color-mix(in srgb, var(--accent) 26%, var(--surface));
		border-color: var(--accent);
	}
	.switch-tile small {
		color: var(--muted);
	}
	.head {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2);
	}
	.head .btn {
		flex: 1 1 auto;
		min-height: 44px;
	}
	.list {
		margin: 0;
		padding: 0;
		list-style: none;
		max-height: 55dvh;
		overflow-y: auto;
		overscroll-behavior: contain;
		border-radius: 12px;
		border: 1px solid var(--border);
		--swipe-bg: var(--dialog-bg, var(--surface));
	}
	.list > li + li {
		border-top: 1px solid var(--border);
	}
	.notice {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
		padding: var(--space-3);
		border-left: 3px solid transparent;
		overflow-wrap: break-word;
	}
	.notice.fresh {
		border-left-color: var(--accent);
	}
	.line {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2);
	}
	.new {
		font-size: var(--text-xs);
		font-weight: 700;
		color: var(--accent);
	}
	time {
		margin-left: auto;
		font-size: var(--text-sm);
	}
	.row-actions {
		display: flex;
		align-items: center;
		gap: var(--space-2);
	}
	.grow {
		flex: 1;
	}
	.icon-btn {
		width: 44px;
		height: 44px;
		display: grid;
		place-items: center;
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		background: var(--chip);
		color: var(--text);
		cursor: pointer;
	}
	.as-link {
		align-self: flex-start;
		min-height: 44px;
		padding: 0;
		border: 0;
		background: none;
		color: var(--accent);
		font: inherit;
		text-decoration: underline;
		cursor: pointer;
	}
	.empty {
		padding: var(--space-4);
	}
	.reload {
		margin: 0;
		text-align: center;
	}
	.small {
		font-size: var(--text-sm);
		margin: 0;
	}
</style>
