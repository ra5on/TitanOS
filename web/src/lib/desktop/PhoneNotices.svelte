<script lang="ts">
	import { page } from '$app/state';
	import PageHead from '../components/PageHead.svelte';
	import { goto } from '$app/navigation';
	import Dialog from '../components/Dialog.svelte';
	import Icon from '../components/Icon.svelte';
	import SwipeRow from '../components/SwipeRow.svelte';
	import { formatDateTime } from '../format.ts';
	import { apiErrorText, i18n, t } from '../i18n/index.svelte.ts';
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
		type Notice,
	} from '../notifications.svelte.ts';
	import { onMount } from 'svelte';
	import { phoneView } from './mode.svelte.ts';
	import { bellGroup } from '../notify-groups.ts';
	import DesktopIcon from './DesktopIcon.svelte';
	import { PULL_DISTANCE } from './PhoneGestures.svelte';

	let busy = $state(false);
	let error = $state('');
	let confirmAll = $state<number[] | null>(null);

	onMount(() => {
		if (!phoneView()) void loadNotifications();
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
			if (ids.includes(Number(page.url.searchParams.get('id'))))
				await goto('/notifications', { replaceState: true, state: page.state });
		} catch (err) {
			error = apiErrorText(err);
		} finally {
			busy = false;
		}
	}
	function follow(href: string) {
		void goto(href);
	}

	let onlyUnread = $state(false);
	const detail = $derived(shown.find((n) => String(n.id) === page.url.searchParams.get('id')));
	const unread = $derived(shown.filter(isUnread));
	const groups = $derived.by(() => {
		const result = new Map<string, Notice[]>();
		for (const notice of onlyUnread ? unread : shown) {
			const day = new Date(notice.time).toDateString();
			const list = result.get(day) ?? [];
			list.push(notice);
			result.set(day, list);
		}
		return Array.from(result);
	});
	function dayLabel(time: string) {
		const date = new Date(time);
		const today = new Date();
		if (date.toDateString() === today.toDateString()) return t('calendar.today');
		today.setDate(today.getDate() - 1);
		if (date.toDateString() === today.toDateString()) return t('nord.notices.yesterday');
		return date.toLocaleDateString(i18n.locale, { day: 'numeric', month: 'long', year: 'numeric' });
	}
	const glyphs = {
		storage: 'storage',
		disks: 'storage',
		backups: 'backup',
		updates: 'updates',
		ups: 'settings',
		usb: 'shares',
		cameras: 'nvr',
		system: 'settings',
	} as const;
	function showDetail(notice: Notice) {
		void goto('/notifications?id=' + notice.id, { state: page.state });
		markRead(notice.id);
	}
	// Pulling the list down at its top loads it anew.
	let list = $state<HTMLElement>();
	let pull = $state(0);
	let pullFrom: number | null = null;
	let reloading = $state(false);
	function pullStart(e: TouchEvent) {
		pullFrom =
			list && window.scrollY <= 0 && list.scrollTop <= 0 && e.touches.length === 1
				? e.touches[0].clientY
				: null;
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

<PageHead title={t('notify.title')} />
<div class="notice-center">
	{#if detail}
		{@const text = noticeText(detail)}
		{@const href = noticeOwnLink(detail) || noticeLink(detail.kind, detail.subject)}
		<article class="notice-detail">
			<div class="line">
				<span class="chip {CHIP[detail.severity]}">{t(SEVERITY[detail.severity])}</span><time
					datetime={detail.time}>{formatDateTime(new Date(detail.time))}</time
				>
			</div>
			<h3>{text.title}</h3>
			<p>{text.text}</p>
			<div class="line">
				{#if href}<button class="btn btn-primary" onclick={() => follow(href)}
						>{t('ui.open')}</button
					>{/if}
				<button
					class="btn"
					disabled={busy}
					data-notice-delete
					onclick={() => detail && dismiss([detail.id])}
					><Icon name="trash" size={16} />{t('common.delete')}</button
				>
			</div>
		</article>
	{:else}
		<div class="nord-segments" role="group" aria-label={t('notify.title')}>
			<button aria-pressed={!onlyUnread} onclick={() => (onlyUnread = false)}
				>{t('menubar.appcenter.all')}</button
			>
			<button aria-pressed={onlyUnread} onclick={() => (onlyUnread = true)}
				>{t('nord.notices.unread')} · {unread.length}</button
			>
		</div>
		{#if pull > PULL_DISTANCE || reloading}<p role="status">
				{t(reloading ? 'phone.refresh.running' : 'phone.refresh.release')}
			</p>{/if}
		<!-- svelte-ignore a11y_no_static_element_interactions -->
		<div
			class="notice-days"
			bind:this={list}
			data-notices
			data-no-sheet-swipe
			ontouchstart={pullStart}
			ontouchmove={pullMove}
			ontouchend={pullEnd}
			ontouchcancel={() => {
				pullFrom = null;
				pull = 0;
			}}
		>
			{#each groups as [day, notices], index (day)}
				<div class="day-head">
					<h3>{dayLabel(notices[0].time)}</h3>
					{#if index === 0 && unread.length}<button
							class="text-button"
							type="button"
							onclick={markSeen}>{t('phone.notices.allRead')}</button
						>{/if}
				</div>
				<ul>
					{#each notices as notice (notice.id)}
						{@const text = noticeText(notice)}
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
								<button
									class="notice-row"
									data-notice={notice.id}
									onclick={() => showDetail(notice)}
								>
									<DesktopIcon
										glyph={notice.kind.startsWith('mail') ? 'mail' : glyphs[bellGroup(notice.kind)]}
										size={38}
									/>
									<span class="copy"
										><small
											>{t(SEVERITY[notice.severity])}{#if fresh}
												· {t('phone.notices.new')}{/if}</small
										><strong>{text.title}</strong></span
									>
									<time datetime={notice.time}
										>{new Date(notice.time).toLocaleTimeString(i18n.locale, {
											hour: '2-digit',
											minute: '2-digit',
										})}</time
									><Icon name="chevronRight" size={15} />
								</button>
							</SwipeRow>
						</li>
					{/each}
				</ul>
			{:else}<p class="empty muted">
					{t(onlyUnread ? 'nord.notices.emptyUnread' : 'notify.empty')}
				</p>{/each}
		</div>
		<div class="foot">
			<a class="text-button" href="/settings/notifications"
				><Icon name="gear" size={17} />{t('notify.channels')}</a
			>
			{#if shown.length}<button
					class="text-button"
					disabled={busy}
					onclick={() => (confirmAll = shown.map((n) => n.id))}>{t('notify.deleteAll')}</button
				>{/if}
		</div>
	{/if}
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
</div>
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
	.notice-center {
		display: grid;
		gap: 12px;
		min-height: 0;
	}
	.notice-days {
		--swipe-bg: var(--nord-surface);
	}
	ul {
		background: var(--nord-surface, var(--surface));
		padding: 0;
		margin: 0;
		list-style: none;
		border: 1px solid var(--nord-line);
		border-radius: 16px;
		overflow: hidden;
	}
	li + li {
		border-top: 1px solid var(--nord-line);
	}
	.day-head,
	.line,
	.foot,
	.text-button {
		display: flex;
		align-items: center;
		gap: 8px;
	}
	.day-head,
	.foot,
	.line {
		justify-content: space-between;
		flex-wrap: wrap;
	}
	.day-head {
		min-height: 48px;
	}
	h3,
	p {
		margin: 0;
	}
	.text-button {
		min-height: 44px;
		border: 0;
		background: none;
		padding: 0 3px;
		color: var(--accent);
		font: inherit;
		cursor: pointer;
		text-decoration: none;
	}
	.notice-row {
		border: 0;
		background: transparent;
		color: var(--text);
		font: inherit;
		display: flex;
		align-items: center;
		text-align: start;
		width: 100%;
		min-height: 60px;
		gap: 10px;
		padding: 8px 12px;
		cursor: pointer;
	}
	.copy {
		flex: 1;
		min-width: 0;
		display: grid;
		overflow-wrap: anywhere;
	}
	.copy small,
	time {
		font-size: var(--text-sm);
		color: var(--muted);
	}
	.notice-detail,
	.empty {
		background: var(--nord-surface);
		border-radius: 16px;
		padding: 16px;
	}
	.notice-detail {
		display: grid;
		gap: 14px;
		overflow-wrap: anywhere;
	}
</style>
