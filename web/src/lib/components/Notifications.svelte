<script lang="ts">
	import { page } from '$app/state';
	import { noticeLink } from '#lib/notice-link.ts';
	import { armSound } from '#lib/sound.svelte.ts';
	import { formatDateTime } from '#lib/format.ts';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';
	import { ApiError } from '#lib/api.ts';
	import {
		dismissNotifications,
		loadNotifications,
		markSeen,
		notifications,
		noticeText,
		unreadCount,
		visibleNotices,
	} from '#lib/notifications.svelte.ts';
	import { system } from '#lib/system.svelte.ts';
	import Icon from './Icon.svelte';
	import Dialog from './Dialog.svelte';
	import { askAbout, assistant } from '#lib/assistant.svelte.ts';

	let { compact = false }: { compact?: boolean } = $props();
	let open = $state(false);
	let busy = $state(false);
	let error = $state('');
	// The IDs "delete all" would remove, while its confirmation is open.
	let confirmAll = $state<number[] | null>(null);
	let trigger = $state<HTMLButtonElement>();
	async function dismiss(ids: number[]) {
		busy = true;
		error = '';
		try {
			await dismissNotifications(ids);
			confirmAll = null;
		} catch (err) {
			error = errorText(err instanceof ApiError ? err.code : 'internal');
		} finally {
			busy = false;
		}
	}
	let root = $state<HTMLElement>();
	// What was new when the list was opened stays marked while it is open.
	let newSince = $state(0);

	const latest = $derived(system.info?.notificationsLatest ?? 0);
	$effect(() => {
		latest;
		loadNotifications();
	});

	const unread = $derived(unreadCount());
	const shown = $derived(visibleNotices());
	const hidden = $derived(notifications.list.length - shown.length);
	const worst = $derived.by(() => {
		const fresh = shown.filter((notice) => Date.parse(notice.time) > notifications.seen);
		if (fresh.some((notice) => notice.severity === 'error')) return 'error';
		return fresh.some((notice) => notice.severity === 'warning') ? 'warning' : 'info';
	});

	function toggle() {
		open = !open;
		if (open) {
			newSince = notifications.seen;
			markSeen();
		}
	}

	function onWindowClick(event: MouseEvent) {
		if (open && !confirmAll && !root?.contains(event.target as Node)) open = false;
	}

	// Escape closes the list and hands the focus back to the bell; an open
	// confirmation handles its own Escape.
	function onKeydown(event: KeyboardEvent) {
		if (event.key !== 'Escape' || !open || confirmAll) return;
		const inside = root?.contains(document.activeElement);
		open = false;
		if (inside) trigger?.focus();
	}

	const SEVERITY = {
		info: 'notify.severity.info',
		warning: 'notify.severity.warning',
		error: 'notify.severity.error',
	} as const;
	const CHIP = { info: 'info', warning: 'warn', error: 'error' } as const;
</script>

<svelte:window
	onclick={onWindowClick}
	onpointerdown={() => void armSound()}
	onkeydown={(event) => {
		void armSound();
		onKeydown(event);
	}}
/>

<div class="notifications" class:compact bind:this={root}>
	<button
		type="button"
		class="trigger"
		bind:this={trigger}
		class:open
		aria-haspopup="dialog"
		aria-expanded={open}
		title={t('notify.title')}
		onclick={toggle}
	>
		<Icon name="bell" />
		<span class="sr-only">{t('notify.title')}</span>
		{#if unread > 0}
			<span class="count {worst}" aria-label={t('notify.unread', { count: unread })}
				>{unread > 9 ? '9+' : unread}</span
			>
		{/if}
	</button>

	{#if open}
		<div class="panel" role="dialog" aria-label={t('notify.title')}>
			<div class="head">
				<span class="name">{t('notify.title')}</span>
				<span class="links"
					><a href="/account#bell" onclick={() => (open = false)}>{t('notify.filterSound')}</a><a
						href="/settings/notifications"
						onclick={() => (open = false)}>{t('notify.channels')}</a
					></span
				>
			</div>
			{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
			{#if shown.length > 0}
				<button
					class="btn clear"
					type="button"
					disabled={busy}
					onclick={() => {
						error = '';
						confirmAll = shown.map((notice) => notice.id);
					}}>{t('notify.deleteAll')}</button
				>
			{/if}
			{#if hidden > 0}
				<a class="hidden-note muted" href="/account#bell" onclick={() => (open = false)}
					>{t('notify.hidden', { count: hidden })}</a
				>
			{/if}
			{#if shown.length === 0}
				<p class="empty muted">{t('notify.empty')}</p>
			{:else}
				<ul>
					{#each shown as notice (notice.id)}
						{@const text = noticeText(notice)}
						{@const href = noticeLink(notice.kind, notice.subject)}
						<li class:fresh={Date.parse(notice.time) > newSince}>
							<div class="line">
								<span class="chip {CHIP[notice.severity]}">{t(SEVERITY[notice.severity])}</span>
								<time class="muted" datetime={notice.time}
									>{formatDateTime(new Date(notice.time))}</time
								>
								<button
									class="btn dismiss"
									type="button"
									disabled={busy}
									aria-label={t('notify.deleteOne', { title: text.title })}
									title={t('common.delete')}
									onclick={() => dismiss([notice.id])}><Icon name="trash" size={14} /></button
								>
							</div>
							<span class="what">{text.title}</span>
							<span class="muted">{text.text}</span>
							<!-- Docker pages stay as in 0.5.9 at the owner's request (docs/design/README.md,
							     "Bedienung und Rückmeldungen (0.6.0)"; screenshots are compared against it),
							     so the links added in 0.6.0 are left out there. -->
							{#if !page.url.pathname.startsWith('/docker') && (href || notice.subject)}
								<span class="notice-links">
									{#if href}<a class="notice-link" {href} onclick={() => (open = false)}
											>{t('ui.open')}</a
										>{/if}
									{#if notice.subject}<a
											class="notice-link"
											href={'/tools/logs?' + new URLSearchParams({ search: notice.subject, range: '24h' })}
											onclick={() => (open = false)}>{t('notify.inLogs')}</a
										>{/if}
								</span>
							{/if}
							{#if assistant.status?.available}
								<button
									type="button"
									class="notice-link ask"
									onclick={() => {
										open = false;
										askAbout(
											t('assistant.askNotice', {
												title: text.title,
												text: text.text,
												time: formatDateTime(new Date(notice.time)),
											}),
										);
									}}>{t('assistant.ask')}</button
								>
							{/if}
						</li>
					{/each}
				</ul>
				<p class="foot muted">{t('notify.sinceStart')}</p>
			{/if}
		</div>
	{/if}
	{#if confirmAll}
	<Dialog title={t('notify.deleteAllTitle')} locked={busy} onclose={() => (confirmAll = null)}>
		<p>{t('notify.deleteAllText', { count: confirmAll.length })}</p>
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
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
</div>

<style>
	.hidden-note {
		display: block;
		font-size: 0.85em;
		margin-block: 0.25rem;
	}
	.ask {
		width: fit-content;
		padding: 0;
		background: none;
		border: 0;
		color: var(--accent);
		font: inherit;
		text-decoration: underline;
		cursor: pointer;
	}
	.clear {
		margin: 0 16px 8px;
	}
	.dismiss {
		margin-left: auto;
		padding: 5px;
		min-height: 28px;
	}
	.notifications {
		position: relative;
	}
	.trigger {
		position: relative;
		width: 2.75rem;
		height: 2.75rem;
		display: flex;
		align-items: center;
		justify-content: center;
		background: transparent;
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		color: var(--text);
		cursor: pointer;
	}
	.compact .trigger {
		width: var(--quick-button-size, 36px);
		height: 36px;
		border: 0;
		border-radius: var(--radius-small);
	}
	.compact .trigger:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
	.compact .count {
		top: 0;
		right: 0;
		min-width: 16px;
		height: 16px;
		padding: 0 3px;
		font-size: 10px;
	}
	.trigger.open,
	.trigger:hover {
		background: var(--chip);
	}
	.sr-only {
		position: absolute;
		width: 1px;
		height: 1px;
		overflow: hidden;
		clip-path: inset(50%);
	}
	.count {
		position: absolute;
		top: -6px;
		right: -6px;
		min-width: 1.25rem;
		height: 1.25rem;
		display: flex;
		align-items: center;
		justify-content: center;
		padding: 0 4px;
		border-radius: 999px;
		background: var(--accent);
		color: var(--on-accent);
		font-size: 0.6875rem;
		font-weight: 700;
	}
	.count.warning {
		background: var(--status-warn);
		color: var(--on-warn);
	}
	.count.error {
		background: var(--status-error);
		color: var(--on-warn);
	}
	.panel {
		position: absolute;
		right: 0;
		top: 52px;
		z-index: 30;
		width: 400px;
		max-width: calc(100vw - 24px);
		max-height: min(70vh, 560px);
		display: flex;
		flex-direction: column;
		background: var(--menu-bg);
		border: 1px solid var(--panel-border);
		border-radius: var(--radius-ctl);
		-webkit-backdrop-filter: var(--panel-backdrop);
		backdrop-filter: var(--panel-backdrop);
		box-shadow: var(--shadow-menu);
		font-size: 0.8125rem;
	}
	.head {
		display: flex;
		align-items: baseline;
		justify-content: space-between;
		gap: 12px;
		padding: 12px 14px;
		border-bottom: 1px solid var(--panel-border);
	}
	.links,
	.notice-links {
		display: flex;
		flex-wrap: wrap;
		gap: 4px 14px;
	}
	.name {
		font-size: 0.9375rem;
		font-weight: 600;
	}
	ul {
		margin: 0;
		padding: 0;
		list-style: none;
		overflow-y: auto;
	}
	li {
		display: flex;
		flex-direction: column;
		gap: 4px;
		padding: 12px 14px;
		border-bottom: 1px solid var(--panel-border);
		border-left: 3px solid transparent;
	}
	li.fresh {
		border-left-color: var(--accent);
	}
	.line {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 10px;
	}
	time {
		font-size: 0.75rem;
	}
	.what {
		font-size: 0.875rem;
		font-weight: 600;
	}
	.empty,
	.foot {
		margin: 0;
		padding: 12px 14px;
	}
	.foot {
		font-size: 0.75rem;
	}
	@media (max-width: 760px) {
		.panel {
			position: fixed;
			top: 60px;
			right: 12px;
			max-width: calc(100vw - 24px);
			max-height: calc(100dvh - 76px);
		}
	}
</style>
