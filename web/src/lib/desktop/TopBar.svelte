<script lang="ts">
	// PLAN 67: the bar across the top of the desktop. PLAN 79-10: it starts
	// with the button "Menü" and the small search field of the menu, then the
	// menus of the program in front and the node switcher; on the right tasks,
	// messages, CPU and RAM in one frame (can be switched off) and the date.
	// The logo menu, the quick settings and the account's avatar are gone:
	// what they held is in the menu and in the Systemsteuerung.
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
		query = $bindable(),
		role,
		deskOp,
		onsearchkey,
	}: {
		menu: DeskMenu | null;
		launcher: boolean;
		launcherAt: 'top' | 'bottom' | 'left' | 'right';
		/** What is typed into the search field: the menu shows the hits. */
		query: string;
		role: string;
		deskOp: (op: DeskOp) => void;
		/** A key in the search field the menu may use (Enter, arrow down); true when it did. */
		onsearchkey: (event: KeyboardEvent) => boolean;
	} = $props();

	// PLAN 67-1: the menu bar of the top bar; Alt alone or F10 puts the keyboard there.
	let menuBar = $state<MenuBar>();
	export function focusBar() {
		menuBar?.focusBar();
	}
	let field = $state<HTMLInputElement>();
	/** The keyboard into the search field (the menu opened: typing finds programs). */
	export function focusSearch() {
		field?.focus();
	}
	function openMenu() {
		if (launcher) return;
		launcherAt = 'top';
		launcher = true;
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
<header
	class="topbar glass-panel"
	oncontextmenu={(e) => {
		const el = e.target as HTMLElement;
		if (el.closest('.menu, .floating-menu, [role="menu"]')) return;
		// The search field keeps the browser's own menu (paste).
		if (el.closest('.bar-search')) return;
		e.preventDefault();
		menu = { ...menuAt(e), kind: 'bar' };
	}}
>
	<button
		type="button"
		class="menu-btn"
		class:on={launcher}
		data-opens="launcher"
		data-menu-button
		aria-haspopup="dialog"
		aria-expanded={launcher}
		title={t('desktop.menu.title') + ' (' + t('desktop.menu.shortcut') + ')'}
		onclick={() => {
			if (!launcher) launcherAt = 'top';
			launcher = !launcher;
		}}><Icon name="grid" size={barIcon} /><span>{t('desktop.menu.title')}</span></button
	>
	<label class="bar-search" data-opens="launcher">
		<Icon name="search" size={barIcon - 2} />
		<input
			bind:this={field}
			bind:value={query}
			type="text"
			role="searchbox"
			autocomplete="off"
			spellcheck="false"
			placeholder={t('desktop.menu.searchShort')}
			aria-label={t('desktop.menu.search')}
			title={t('desktop.menu.search')}
			onfocus={openMenu}
			onclick={openMenu}
			oninput={openMenu}
			onkeydown={(e) => {
				if (onsearchkey(e)) e.preventDefault();
			}}
		/>
	</label>
	<span class="rule" aria-hidden="true"></span>
	<MenuBar bind:this={menuBar} ondesk={deskOp} onshowdesktop={showDesktop} />
	{#if clusterNodes.nodes.length > 1}<NodeMenu variant="desk" />{/if}
	<span class="space"></span>
	<div class="status">
		<TasksPanel badge />
		{#if role !== 'user'}<Notifications compact />{/if}
		{#if info && desktop.prefs.barLoad}
			<span class="load" title={t('desktop.widget.cpu') + ' · ' + t('desktop.widget.ram')}
				>{t('menubar.load', {
					cpu: formatPercent(info.cpu.usagePercent),
					ram: formatPercent(memPct),
				})}</span
			>
		{/if}
		<time class="clock" datetime={now.toISOString()}
			><span class="date">{dateText}</span><span class="time">{timeText}</span></time
		>
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
	/* PLAN 79-10: the button "Menü" (the dock's menu symbol and the word). */
	.menu-btn {
		flex: none;
		height: var(--pb);
		display: flex;
		align-items: center;
		gap: var(--space-1-5);
		padding: 0 var(--space-2-5);
		border: 1px solid color-mix(in srgb, var(--text) 18%, transparent);
		border-radius: 8px;
		background: color-mix(in srgb, var(--text) 7%, transparent);
		color: var(--text);
		font: inherit;
		font-size: var(--mb-font);
		white-space: nowrap;
		cursor: pointer;
	}
	.menu-btn:hover {
		background: color-mix(in srgb, var(--text) 13%, transparent);
	}
	.menu-btn.on {
		background: color-mix(in srgb, var(--accent) 28%, transparent);
		border-color: color-mix(in srgb, var(--accent) 60%, transparent);
	}
	.menu-btn:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	/* The small search field next to it, and the frame of CPU and RAM in its look. */
	.bar-search,
	.load {
		flex: none;
		height: var(--pb);
		display: flex;
		align-items: center;
		border: 1px solid color-mix(in srgb, var(--text) 18%, transparent);
		border-radius: 8px;
		background: color-mix(in srgb, var(--bg) 45%, transparent);
	}
	.bar-search {
		gap: var(--space-1-5);
		padding: 0 var(--space-2);
		color: var(--muted);
		cursor: text;
	}
	.bar-search:focus-within {
		border-color: var(--accent);
		box-shadow: 0 0 0 1px var(--accent);
	}
	.bar-search input {
		width: 7.5em;
		min-width: 0;
		padding: 0;
		border: 0;
		background: none;
		color: var(--text);
		font: inherit;
		font-size: var(--mb-font);
		outline: none;
	}
	.bar-search input::placeholder {
		color: var(--muted);
		opacity: 1;
	}
	.rule {
		flex: none;
		width: 1px;
		height: calc(var(--pb) * 0.6);
		margin: 0 var(--space-1);
		background: color-mix(in srgb, var(--text) 18%, transparent);
	}
	.load {
		margin: 0 var(--space-1);
		padding: 0 var(--space-2-5);
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
	/* PLAN 79-10: a narrow screen (windows open maximized there) keeps every
	   menu of the program whole: the button shows its symbol alone (the name
	   stays for screen readers and as the tooltip) and the search field is
	   as wide as its word. */
	@media (max-width: 1179px) {
		.menu-btn {
			position: relative;
			padding: 0 var(--space-2);
		}
		.menu-btn span {
			position: absolute;
			width: 1px;
			height: 1px;
			overflow: hidden;
			clip-path: inset(50%);
			white-space: nowrap;
		}
		.bar-search input {
			width: 4.4em;
		}
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
	.space {
		flex: 1;
	}
</style>
