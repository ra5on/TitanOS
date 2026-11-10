<script lang="ts">
	// PLAN 67: the bar across the top of the desktop: the menus of the program
	// in front, the node switcher, and on the right load, tasks, messages,
	// quick settings, search, date and account.
	import AccountMenu from '../components/AccountMenu.svelte';
	import Icon from '../components/Icon.svelte';
	import NodeMenu from '../components/NodeMenu.svelte';
	import Notifications from '../components/Notifications.svelte';
	import TasksPanel from '../components/TasksPanel.svelte';
	import { clusterNodes } from '../cluster.svelte.ts';
	import { formatPercent } from '../format.ts';
	import { i18n, t } from '../i18n/index.svelte.ts';
	import { system } from '../system.svelte.ts';
	import { menuAt, showDesktop, type DeskMenu } from './desk-ops.ts';
	import MenuBar from './menubar/MenuBar.svelte';
	import type { DeskOp } from './menubar/menus.ts';
	import { desktop } from './wm.svelte.ts';

	let {
		menu = $bindable(),
		launcher = $bindable(),
		launcherAt = $bindable(),
		center = $bindable(),
		role,
		deskOp,
	}: {
		menu: DeskMenu | null;
		launcher: boolean;
		launcherAt: 'top' | 'bottom' | 'left' | 'right';
		center: boolean;
		role: string;
		deskOp: (op: DeskOp) => void;
	} = $props();

	// PLAN 67-1: the menu bar of the top bar; Alt alone or F10 puts the keyboard there.
	let menuBar = $state<MenuBar>();
	export function focusBar() {
		menuBar?.focusBar();
	}

	const barIcon = $derived({ s: 14, m: 17, l: 20 }[desktop.prefs.barSize]);

	// Date and time on the right of the top bar.
	let now = $state(new Date());
	$effect(() => {
		const timer = setInterval(() => (now = new Date()), 15_000);
		return () => clearInterval(timer);
	});
	const dateText = $derived(
		new Intl.DateTimeFormat(i18n.locale, {
			weekday: 'short',
			day: 'numeric',
			month: 'short',
		}).format(now),
	);
	const timeText = $derived(
		new Intl.DateTimeFormat(i18n.locale, { hour: '2-digit', minute: '2-digit' }).format(now),
	);

	const info = $derived(system.info);
	const memPct = $derived(
		info
			? (100 * (info.memory.totalBytes - info.memory.availableBytes)) / info.memory.totalBytes
			: 0,
	);
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<!-- PLAN 67: one bar across the top (variant B): the logo menu, the
	     program in front with its menus, the node switcher; on the right
	     load, tasks, messages, quick settings, search, date and account.
	     Open windows are in the dock. -->
<!-- svelte-ignore a11y_no_static_element_interactions -->
<header
	class="topbar glass-panel"
	oncontextmenu={(e) => {
		if ((e.target as HTMLElement).closest('.menu, .floating-menu, [role="menu"]')) return;
		e.preventDefault();
		menu = { ...menuAt(e), kind: 'bar' };
	}}
>
	<MenuBar bind:this={menuBar} ondesk={deskOp} onshowdesktop={showDesktop} />
	{#if clusterNodes.nodes.length > 1}<NodeMenu variant="desk" />{/if}
	<span class="space"></span>
	<div class="status">
		{#if info}
			<span class="load" title={t('desktop.widget.cpu') + ' · ' + t('desktop.widget.ram')}
				>{t('menubar.load', {
					cpu: formatPercent(info.cpu.usagePercent),
					ram: formatPercent(memPct),
				})}</span
			>
		{/if}
		<TasksPanel badge />
		{#if role !== 'user'}<Notifications compact />{/if}
		<button
			type="button"
			class="pb"
			class:on={center}
			data-opens="center"
			onclick={() => (center = !center)}
			title={t('menubar.quick')}
			aria-label={t('menubar.quick')}
			aria-expanded={center}><Icon name="columns" size={barIcon} /></button
		>
		<button
			type="button"
			class="pb"
			class:on={launcher}
			data-opens="launcher"
			onclick={() => {
				launcherAt = 'top';
				launcher = !launcher;
			}}
			title={t('desktop.menu.title') + ' (' + t('desktop.menu.shortcut') + ')'}
			aria-label={t('desktop.menu.title')}
			aria-expanded={launcher}><Icon name="search" size={barIcon} /></button
		>
		<time class="clock" datetime={now.toISOString()}
			><span class="date">{dateText}</span><span class="time">{timeText}</span></time
		>
		<AccountMenu compact />
	</div>
</header>

<style>
	/* PLAN 67: one bar across the whole width, glass like the rest of the
	   desktop (Settings › Appearance), a line below it. */
	.topbar.glass-panel {
		height: var(--bar-h);
		flex: none;
		display: flex;
		align-items: center;
		gap: var(--space-1-5);
		padding: 0 var(--space-2) 0 var(--space-1-5);
		position: relative;
		z-index: 9500;
		border: 0;
		border-bottom: 1px solid color-mix(in srgb, var(--text) 14%, transparent);
		border-radius: 0;
		box-shadow: 0 1px 10px rgb(0 0 0 / 0.18);
		--mb-font: max(0.8125rem, calc(var(--bar-h) * 0.34));
		font-size: var(--mb-font);
	}
	.status {
		flex: none;
		display: flex;
		align-items: center;
		gap: 2px;
		--quick-button-size: var(--pb);
	}
	.load {
		padding: 0 var(--space-2);
		color: var(--muted);
		white-space: nowrap;
		font-variant-numeric: tabular-nums;
	}
	.clock {
		display: flex;
		gap: 0.5em;
		padding: 0 var(--space-2);
		white-space: nowrap;
		font-variant-numeric: tabular-nums;
	}
	.clock .date::after {
		content: ' ·';
		color: var(--muted);
	}
	/* Narrower screens keep the menus: the load goes first, then the date. */
	@media (max-width: 1279px) {
		.load {
			display: none;
		}
	}
	@media (max-width: 1099px) {
		.clock .date {
			display: none;
		}
	}
	.pb {
		width: var(--pb);
		height: var(--pb);
		flex: none;
		display: grid;
		place-items: center;
		border: 0;
		border-radius: 8px;
		background: none;
		color: var(--text);
		cursor: pointer;
	}
	.pb:hover {
		background: color-mix(in srgb, var(--text) 9%, transparent);
	}
	.pb.on {
		background: color-mix(in srgb, var(--accent) 24%, transparent);
	}
	.pb:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	.space {
		flex: 1;
	}
</style>
