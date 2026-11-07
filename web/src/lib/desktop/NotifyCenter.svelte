<script lang="ts">
	// PLAN 61: the notification centre – quick switches, running tasks and
	// the messages of the NAS in one panel at the right edge.
	import Icon from '../components/Icon.svelte';
	import { t } from '../i18n/index.svelte.ts';
	import {
		loadNotifications,
		markSeen,
		noticeText,
		visibleNotices,
		notifications,
	} from '../notifications.svelte.ts';
	import { session } from '../session.svelte.ts';
	import { appearance, saveAppearance } from '../theme/appearance.svelte.ts';
	import { THEMES } from '../theme/themes.ts';
	import { desktop, savePrefs } from './wm.svelte.ts';
	import { formatDateTime } from '../format.ts';

	let { onclose, onopen }: { onclose: () => void; onopen: (key: string, href?: string) => void } =
		$props();

	const admin = $derived(['owner', 'admin'].includes(session.data?.user?.role ?? ''));
	$effect(() => {
		if (admin) void loadNotifications();
	});
	const list = $derived(admin ? visibleNotices().slice(0, 30) : []);
	const dark = $derived(THEMES[appearance.current.theme as keyof typeof THEMES]?.dark !== false);
	function toggleTheme() {
		void saveAppearance({
			...appearance.current,
			theme: dark ? 'hell' : 'graphit',
		} as typeof appearance.current);
	}
	function toggleLive() {
		savePrefs((p) => (p.wallpaper = { ...p.wallpaper, live: !p.wallpaper.live }));
	}
	void notifications;
</script>

<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
<aside
	class="center glass-panel"
	aria-label={t('menubar.quick')}
	onkeydown={(e) => e.key === 'Escape' && onclose()}
>
	<div class="head">
		<b>{t('menubar.quick')}</b>
		<button type="button" class="x" onclick={onclose} aria-label={t('desktop.window.close')}
			><Icon name="close" size={15} /></button
		>
	</div>
	<div class="toggles">
		<button type="button" class="tg" class:on={!dark} aria-pressed={!dark} onclick={toggleTheme}
			><Icon name="palette" size={20} /><span>{t('desktop.center.light')}</span></button
		>
		<button
			type="button"
			class="tg"
			class:on={desktop.prefs.wallpaper.live}
			aria-pressed={desktop.prefs.wallpaper.live}
			onclick={toggleLive}
			><Icon name="image" size={20} /><span>{t('desktop.center.live')}</span></button
		>
		{#if admin}
			<button type="button" class="tg" onclick={() => onopen('app:monitor', '/tools/tasks')}
				><Icon name="list" size={20} /><span>{t('desktop.center.tasks')}</span></button
			>
			<button type="button" class="tg" onclick={() => onopen('app:monitor')}
				><Icon name="activity" size={20} /><span>{t('desktop.center.taskManager')}</span></button
			>
		{/if}
	</div>
	{#if admin}
		<div class="sub">
			<b>{t('desktop.center.messages')}</b>
			{#if list.length}<button type="button" class="link" onclick={markSeen}
					>{t('desktop.center.allRead')}</button
				>{/if}
		</div>
		<div class="list">
			{#each list as n (n.id)}
				{@const text = noticeText(n)}
				<div class="msg" class:warn={n.severity === 'warning'} class:err={n.severity === 'error'}>
					<span class="sev" aria-label={n.severity}
						>{n.severity === 'error' ? '!' : n.severity === 'warning' ? '!' : 'i'}</span
					>
					<div class="body">
						<b>{text.title}</b>
						<div class="muted small">{text.text}</div>
					</div>
					<span class="muted small time">{formatDateTime(new Date(n.time))}</span>
				</div>
			{:else}
				<p class="muted">{t('desktop.center.none')}</p>
			{/each}
		</div>
		<button
			type="button"
			class="btn"
			onclick={() => onopen('app:settings', '/settings/notifications')}
			>{t('desktop.center.settings')}</button
		>
	{/if}
</aside>

<style>
	/* PLAN 62: nearly solid, so icons and widgets behind never shine through the text. */
	.center.glass-panel {
		background: color-mix(in srgb, var(--surface) 97%, transparent);
	}
	.center {
		position: absolute;
		right: 10px;
		top: 0;
		bottom: 10px;
		width: min(400px, calc(100% - 20px));
		border-radius: 20px;
		padding: 16px;
		display: flex;
		flex-direction: column;
		gap: 12px;
		z-index: 9400;
		animation: slide 0.18s ease-out;
	}
	@keyframes slide {
		from {
			opacity: 0;
			transform: translateX(20px);
		}
	}
	.head,
	.sub {
		display: flex;
		align-items: center;
		justify-content: space-between;
	}
	.x {
		width: 30px;
		height: 30px;
		display: grid;
		place-items: center;
		border: 0;
		border-radius: 8px;
		background: none;
		color: var(--text);
		cursor: pointer;
	}
	.toggles {
		display: grid;
		grid-template-columns: repeat(4, 1fr);
		gap: 8px;
	}
	.tg {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 6px;
		padding: 10px 4px;
		border: 0;
		border-radius: 14px;
		background: color-mix(in srgb, var(--bg) 50%, transparent);
		color: var(--muted);
		font: inherit;
		font-size: var(--text-xs);
		text-align: center;
		cursor: pointer;
	}
	.tg.on {
		background: color-mix(in srgb, var(--accent) 28%, transparent);
		color: var(--text);
	}
	.list {
		flex: 1;
		overflow-y: auto;
		display: flex;
		flex-direction: column;
		gap: 8px;
	}
	.msg {
		display: flex;
		gap: 10px;
		align-items: flex-start;
		padding: 10px 12px;
		border-radius: 12px;
		background: color-mix(in srgb, var(--bg) 45%, transparent);
	}
	.sev {
		width: 26px;
		height: 26px;
		flex: none;
		border-radius: 50%;
		display: grid;
		place-items: center;
		font-weight: 700;
		background: var(--accent);
		color: var(--on-accent);
	}
	.warn .sev {
		background: #f0a91a;
		color: #2b2100;
	}
	.err .sev {
		background: #d93a3f;
		color: #fff;
	}
	.body {
		flex: 1;
		min-width: 0;
		overflow-wrap: anywhere;
	}
	.time {
		white-space: nowrap;
	}
	.muted {
		color: var(--muted);
	}
	.small {
		font-size: var(--text-sm);
	}
	.link {
		border: 0;
		background: none;
		color: var(--accent);
		font: inherit;
		font-size: var(--text-sm);
		cursor: pointer;
	}
	@media (prefers-reduced-motion: reduce) {
		.center {
			animation: none;
		}
	}
</style>
