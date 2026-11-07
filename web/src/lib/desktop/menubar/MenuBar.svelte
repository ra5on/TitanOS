<script lang="ts">
	// PLAN 67-1: the menu bar in the top bar (variant B, like macOS): the
	// TitanOS logo menu, the name of the program in front and its menus.
	// Menus open on a click and follow the pointer while one is open; Alt or
	// F10 brings the keyboard here (arrows move, Enter opens, Esc closes).
	import { tick } from 'svelte';
	import { api } from '../../api.ts';
	import Icon from '../../components/Icon.svelte';
	import PowerDialog from '../../components/PowerDialog.svelte';
	import ShortcutsDialog from '../../components/ShortcutsDialog.svelte';
	import { t } from '../../i18n/index.svelte.ts';
	import { lock } from '../../lock.svelte.ts';
	import {
		hasWindowCommands,
		runWindowCommand,
		windowCommandState,
	} from '../../menu-commands.svelte.ts';
	import { logout, session } from '../../session.svelte.ts';
	import { findProgram, openProgram, openTab } from '../programs.svelte.ts';
	import { dockerUpdates } from '../sources.svelte.ts';
	import {
		desktop,
		focus,
		frameOf,
		minimize,
		navigateWindow,
		requestClose,
		setSnap,
		toggleMax,
		wm,
		type Win,
	} from '../wm.svelte.ts';
	import {
		commandIds,
		desktopMenus,
		goTarget,
		helpUrl,
		logoMenu,
		menusFor,
		type DeskOp,
		type Menu,
		type MenuEntry,
		type MenuItem,
	} from './menus.ts';
	import { readCommandStates } from './protocol.ts';
	import { clearFrameCommands, frameMenus, setFrameCommands } from './state.svelte.ts';

	let {
		ondesk,
		onshowdesktop,
	}: {
		/** Operations on the desktop (folders, wallpaper, icons, widgets, dock). */
		ondesk: (op: DeskOp) => void;
		onshowdesktop: () => void;
	} = $props();

	const role = $derived(session.data?.user?.role ?? 'user');
	const admin = $derived(role === 'owner' || role === 'admin');
	const front = $derived<Win | undefined>(
		wm.windows.find((w) => w.id === wm.focused && !w.minimized),
	);
	const programName = $derived(
		front ? (findProgram(front.app)?.label ?? front.title) : t('menubar.desktop'),
	);
	const logo = $derived(logoMenu(role));
	const menus = $derived(front ? menusFor(front.app, role) : desktopMenus(role));
	const tops = $derived<Menu[]>([logo, ...menus]);

	let open = $state<string | null>(null);
	let sub = $state<string | null>(null);
	let at = $state({ x: 0, y: 0 });
	let subAt = $state({ x: 0, y: 0 });
	let bar = $state<HTMLElement>();
	let panel = $state<HTMLElement>();
	let subPanel = $state<HTMLElement>();
	let power = $state<'restart' | 'shutdown' | null>(null);
	let shortcuts = $state(false);
	let systemUpdate = $state(false);
	/** Where the keyboard came from (a window's page): it goes back there. */
	let returnTo: HTMLElement | null = null;
	let current = $state(0);

	const openMenu = $derived(tops.find((m) => m.id === open));
	const openSub = $derived.by(() => {
		const entry = openMenu?.items.find((i) => !('sep' in i) && i.id === sub) as
			MenuEntry | undefined;
		return entry && subOf(entry) ? entry : undefined;
	});
	/** The entries of a submenu: written in menus.ts, or supplied by the page (list). */
	function subOf(e: MenuEntry): MenuItem[] | undefined {
		if (e.sub) return e.sub;
		if (!e.list || !e.do || !('cmd' in e.do)) return undefined;
		const cmd = e.do.cmd;
		return (cmdState(cmd).items ?? []).map((item, i) => ({
			id: `${e.id}.${i}`,
			label: e.label,
			text: item.text,
			do: { cmd, arg: item.id },
			mark: item.checked === undefined ? undefined : ('radio' as const),
			picked: item.checked,
		}));
	}
	const updates = $derived((systemUpdate ? 1 : 0) + dockerUpdates());

	// A different program in front closes what was open.
	$effect(() => {
		void front?.app;
		open = null;
		sub = null;
	});

	function checkUpdates() {
		if (!admin) return;
		api
			.get<{ available: unknown }>('/update')
			.then((u) => (systemUpdate = !!u?.available))
			.catch(() => {});
	}
	$effect(() => {
		if (admin) checkUpdates();
	});

	// ---- What the pages in the windows announce ----

	function message(event: MessageEvent) {
		if (event.origin !== location.origin) return;
		const data = event.data as { type?: unknown } | null;
		if (data?.type !== 'titanos-menu') return;
		const win = wm.windows.find((w) => frameOf(w.id)?.contentWindow === event.source);
		if (!win) return;
		const states = readCommandStates(event.data, commandIds(win.app));
		if (states) setFrameCommands(win.id, states);
	}
	function askFrame(win: Win | undefined) {
		const target = win && frameOf(win.id)?.contentWindow;
		target?.postMessage({ type: 'titanos-menu-get' }, location.origin);
	}
	$effect(() => {
		// Windows that closed leave nothing behind.
		const ids = new Set(wm.windows.map((w) => w.id));
		for (const id of Object.keys(frameMenus.byWindow)) if (!ids.has(id)) clearFrameCommands(id);
	});

	// ---- State of an entry ----

	function cmdState(id: string): {
		enabled: boolean;
		checked?: boolean;
		items?: { id: string; text: string; checked?: boolean }[];
	} {
		if (!front) return { enabled: false };
		return (
			windowCommandState(front.id, id) ?? frameMenus.byWindow[front.id]?.[id] ?? { enabled: false }
		);
	}
	function deskChecked(op: DeskOp): boolean | undefined {
		const p = desktop.prefs;
		if (op.startsWith('sort:')) return p.sort === op.slice(5);
		if (op.startsWith('size:')) return p.iconSize === op.slice(5);
		if (op === 'widgets') return !p.widgetsHidden;
		if (op === 'dock') return p.dock.show;
		return undefined;
	}
	function entryState(e: MenuEntry): { enabled: boolean; checked?: boolean } {
		if (e.text !== undefined) return { enabled: true, checked: e.picked };
		if (e.sub) return { enabled: e.sub.some((i) => !('sep' in i) && entryState(i).enabled) };
		const action = e.do;
		if (!action) return { enabled: false };
		if ('cmd' in action) return cmdState(action.cmd);
		if ('go' in action)
			return {
				enabled: !!goTarget(action.go, front?.app ?? '', role, session.services),
			};
		if ('win' in action) {
			if (action.win === 'showDesktop') return { enabled: wm.windows.length > 0 };
			if (!front) return { enabled: false };
			if (action.win === 'maximize') return { enabled: true, checked: front.snap === 'max' };
			if (action.win === 'allOfProgram')
				return { enabled: wm.windows.filter((w) => w.app === front.app).length > 1 };
			return { enabled: true };
		}
		if ('desk' in action) return { enabled: true, checked: deskChecked(action.desk) };
		return { enabled: true };
	}

	// ---- Running an entry ----

	function run(e: MenuEntry) {
		const action = e.do;
		const win = front;
		close(false);
		if (!action) return;
		if ('cmd' in action) {
			if (!win) return;
			if (hasWindowCommands(win.id) && runWindowCommand(win.id, action.cmd, action.arg))
				return backToWindow();
			frameOf(win.id)?.contentWindow?.postMessage(
				{ type: 'titanos-menu-action', id: action.cmd, arg: action.arg },
				location.origin,
			);
			return backToWindow();
		}
		if ('go' in action) {
			const target = goTarget(action.go, win?.app ?? '', role, session.services);
			if (!target) return;
			if (target.kind === 'here' && win) {
				navigateWindow(win.id, target.href);
				focus(win.id);
			} else void openProgram(target.app, { href: target.href });
			return;
		}
		if ('desk' in action) return ondesk(action.desk);
		if ('win' in action) {
			if (action.win === 'showDesktop') return onshowdesktop();
			if (!win) return;
			switch (action.win) {
				case 'minimize':
					return minimize(win.id);
				case 'maximize':
					return toggleMax(win.id);
				case 'snapLeft':
					return setSnap(win.id, 'left');
				case 'snapRight':
					return setSnap(win.id, 'right');
				case 'allOfProgram':
					for (const w of wm.windows.filter((x) => x.app === win.app)) focus(w.id);
					return focus(win.id);
				case 'openTab':
					if (runWindowCommand(win.id, 'window.openTab')) return;
					return void openTab(win.app, win.href);
				case 'close':
					return requestClose(win.id);
			}
		}
		if ('sys' in action) {
			switch (action.sys) {
				case 'about':
					return void openProgram('app:settings', { href: '/settings/about' });
				case 'settings':
					return void openProgram('app:settings');
				case 'appcenter':
					return void openProgram('app:appcenter');
				case 'updates':
					return void openProgram('app:settings', { href: '/settings/updates' });
				case 'lock':
					return void lock();
				case 'logout':
					return void logout();
				case 'restart':
				case 'shutdown':
					power = action.sys;
					return;
				case 'help':
					window.open(helpUrl(win?.app ?? ''), '_blank', 'noopener');
					return;
				case 'shortcuts':
					shortcuts = true;
					return;
				case 'diagnose':
					return void openProgram('app:logs', { href: '/tools/bundle' });
			}
		}
	}

	/** After a page command the keyboard goes back into the window. */
	function backToWindow() {
		returnTo = null;
		const win = front;
		// A command that put the focus into the window's own parts (PLAN 71: the
		// search of the side bar) keeps it there.
		if (win)
			setTimeout(() => {
				const frame = frameOf(win.id);
				const own = document.activeElement;
				if (frame && own !== frame && frame.closest('section')?.contains(own)) return;
				frame?.focus();
			}, 0);
	}

	// ---- Opening and closing ----

	function place(id: string) {
		const button = bar?.querySelector<HTMLElement>(`[data-menu="${id}"]`);
		const r = button?.getBoundingClientRect();
		if (r) at = { x: Math.max(4, Math.min(r.left, window.innerWidth - 300)), y: r.bottom + 4 };
	}
	async function show(id: string, focusFirst: boolean) {
		place(id);
		open = id;
		sub = null;
		current = Math.max(
			0,
			tops.findIndex((m) => m.id === id),
		);
		if (id === 'logo') checkUpdates();
		else askFrame(front);
		if (focusFirst) {
			await tick();
			firstItem(panel)?.focus();
		}
	}
	function close(restore = true) {
		const wasOpen = open;
		open = null;
		sub = null;
		if (!restore) return;
		if (wasOpen) topButton(current)?.focus();
	}
	function leave() {
		close(false);
		const target = returnTo;
		returnTo = null;
		if (target && document.contains(target)) target.focus();
		else if (front) frameOf(front.id)?.focus();
		else (document.activeElement as HTMLElement | null)?.blur();
	}

	/** Alt or F10: the keyboard into the menu bar (the first menu of the program). */
	export function focusBar() {
		const active = document.activeElement as HTMLElement | null;
		if (!bar?.contains(active)) returnTo = active && active !== document.body ? active : null;
		current = tops.length > 1 ? 1 : 0;
		topButton(current)?.focus();
	}

	function topButton(i: number) {
		return bar?.querySelectorAll<HTMLElement>('[data-menu]')[i] ?? null;
	}
	const ITEMS = '[role^="menuitem"]:not(:disabled)';
	function firstItem(el: HTMLElement | undefined) {
		return el?.querySelector<HTMLElement>(ITEMS) ?? null;
	}
	function moveItem(el: HTMLElement | undefined, key: string) {
		if (!el) return;
		const items = [...el.querySelectorAll<HTMLElement>(ITEMS)];
		if (!items.length) return;
		const i = items.indexOf(document.activeElement as HTMLElement);
		const next =
			key === 'Home'
				? 0
				: key === 'End'
					? items.length - 1
					: (i + (key === 'ArrowDown' ? 1 : items.length - 1)) % items.length;
		items[next]?.focus();
	}

	function step(dir: 1 | -1, openIt: boolean) {
		current = (current + dir + tops.length) % tops.length;
		if (openIt) void show(tops[current].id, true);
		else topButton(current)?.focus();
	}

	function barKey(event: KeyboardEvent) {
		const target = event.target as HTMLElement;
		if (!target.matches('[data-menu]')) return;
		const id = target.dataset.menu ?? '';
		if (event.key === 'ArrowRight' || event.key === 'ArrowLeft') {
			event.preventDefault();
			step(event.key === 'ArrowRight' ? 1 : -1, !!open);
		} else if (
			event.key === 'ArrowDown' ||
			event.key === 'Enter' ||
			event.key === ' ' ||
			event.key === 'ArrowUp'
		) {
			event.preventDefault();
			void show(id, true);
		} else if (event.key === 'Escape') {
			event.preventDefault();
			event.stopPropagation();
			if (open) close();
			else leave();
		} else if (event.key === 'Tab') {
			close(false);
		}
	}

	async function openSubOf(e: MenuEntry, item: HTMLElement, focusFirst: boolean) {
		const r = item.getBoundingClientRect();
		const width = 260;
		subAt = {
			x: r.right + width > window.innerWidth ? Math.max(4, r.left - width) : r.right - 4,
			y: r.top - 5,
		};
		sub = e.id;
		if (focusFirst) {
			await tick();
			firstItem(subPanel)?.focus();
		}
	}

	function menuKey(event: KeyboardEvent, inSub: boolean) {
		const el = event.target as HTMLElement;
		if (['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) {
			event.preventDefault();
			moveItem(inSub ? subPanel : panel, event.key);
		} else if (event.key === 'ArrowRight') {
			event.preventDefault();
			const id = el.dataset.sub;
			const entry = id
				? (openMenu?.items.find((i) => !('sep' in i) && i.id === id) as MenuEntry | undefined)
				: undefined;
			if (!inSub && entry && subOf(entry)) void openSubOf(entry, el, true);
			else step(1, true);
		} else if (event.key === 'ArrowLeft') {
			event.preventDefault();
			if (inSub) {
				const parent = panel?.querySelector<HTMLElement>(`[data-sub="${sub}"]`);
				sub = null;
				parent?.focus();
			} else step(-1, true);
		} else if (event.key === 'Escape') {
			event.preventDefault();
			event.stopPropagation();
			if (inSub) {
				const parent = panel?.querySelector<HTMLElement>(`[data-sub="${sub}"]`);
				sub = null;
				parent?.focus();
			} else close();
		} else if (event.key === 'Tab') {
			event.preventDefault();
			leave();
		}
	}

	function outside(event: PointerEvent) {
		if (!open) return;
		const el = event.target as Element | null;
		if (el?.closest?.('.menubar, .mb-menu')) return;
		close(false);
	}
	function blurred() {
		// A click into a window's frame is a click beside the menu.
		setTimeout(() => {
			if (open && document.activeElement instanceof HTMLIFrameElement) close(false);
		}, 0);
	}

	/** The menus hang in the page itself: inside the glass bar their own glass would not blur. */
	function portal(node: HTMLElement) {
		document.body.appendChild(node);
		return { destroy: () => node.remove() };
	}

	// ---- Text ----

	const mac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform);
	function keysText(keys: string): string {
		return keys
			.split('+')
			.map((k) =>
				k === 'Ctrl'
					? mac
						? '⌘'
						: t('shortcuts.ctrl')
					: k === 'Del'
						? t('shortcuts.delete')
						: k === 'Alt' && mac
							? '⌥'
							: k,
			)
			.join(mac ? '' : '+');
	}
	function label(e: MenuEntry): string {
		return e.text ?? t(e.label, { name: programName });
	}
</script>

<svelte:window
	onmessage={message}
	onpointerdowncapture={outside}
	onblur={blurred}
	onresize={() => close(false)}
/>

{#snippet items(list: MenuItem[], inSub: boolean)}
	{#each list as item, i (i)}
		{#if 'sep' in item}
			<hr />
		{:else}
			{@const s = entryState(item)}
			{@const opens = !!item.sub || !!item.list}
			<button
				type="button"
				role={item.mark === 'check'
					? 'menuitemcheckbox'
					: item.mark === 'radio'
						? 'menuitemradio'
						: 'menuitem'}
				aria-checked={item.mark ? !!s.checked : undefined}
				aria-haspopup={opens ? 'menu' : undefined}
				aria-expanded={opens ? sub === item.id : undefined}
				data-sub={opens ? item.id : undefined}
				data-id={item.id}
				disabled={!s.enabled}
				tabindex="-1"
				class:open={opens && sub === item.id}
				onpointerenter={(e) => {
					if (inSub) return;
					if (opens && s.enabled) void openSubOf(item, e.currentTarget, false);
					else sub = null;
				}}
				onclick={(e) => {
					if (opens) void openSubOf(item, e.currentTarget, true);
					else run(item);
				}}
			>
				<span class="mark" aria-hidden="true"
					>{#if item.mark && s.checked}{item.mark === 'radio' ? '•' : '✓'}{/if}</span
				>
				<span class="text">{label(item)}</span>
				{#if item.id === 'sys.updates' && updates}<span class="count"
						>{t('menubar.updatesCount', { count: updates })}</span
					>{/if}
				{#if item.keys}<kbd>{keysText(item.keys)}</kbd>{/if}
				{#if opens}<span class="arrow" aria-hidden="true">›</span>{/if}
			</button>
		{/if}
	{/each}
{/snippet}

<!-- svelte-ignore a11y_interactive_supports_focus -->
<div
	class="menubar"
	role="menubar"
	aria-label={t('menubar.label')}
	tabindex="-1"
	bind:this={bar}
	onkeydown={barKey}
>
	{#each tops as m, i (m.id)}
		{#if i === 1}<b class="program" title={programName}>{programName}</b>{/if}
		<button
			type="button"
			role="menuitem"
			class="top"
			class:logo={m.id === 'logo'}
			class:on={open === m.id}
			data-menu={m.id}
			aria-haspopup="menu"
			aria-expanded={open === m.id}
			aria-label={m.id === 'logo' ? t('menubar.logo') : undefined}
			title={m.id === 'logo' ? t('menubar.logo') : undefined}
			tabindex={current === i ? 0 : -1}
			onclick={() => (open === m.id ? close(false) : show(m.id, false))}
			onpointerenter={() => {
				if (open && open !== m.id) void show(m.id, false);
			}}
			onfocus={() => (current = i)}
		>
			{#if m.id === 'logo'}
				<img src="/favicon.svg" alt="" width="20" height="20" />
				{#if updates}<span class="dot" aria-hidden="true"></span>{/if}
			{:else}{t(m.label)}{/if}
		</button>
		{#if i === 0 && tops.length === 1}<b class="program" title={programName}>{programName}</b>{/if}
	{/each}
</div>

{#if openMenu}
	<!-- svelte-ignore a11y_interactive_supports_focus -->
	<div
		class="mb-menu floating-menu"
		use:portal
		role="menu"
		aria-label={openMenu.id === 'logo' ? t('menubar.logo') : t(openMenu.label)}
		style:left="{at.x}px"
		style:top="{at.y}px"
		bind:this={panel}
		onkeydown={(e) => menuKey(e, false)}
	>
		{@render items(openMenu.items, false)}
	</div>
	{#if openSub}
		<!-- svelte-ignore a11y_interactive_supports_focus -->
		<div
			class="mb-menu floating-menu"
			use:portal
			role="menu"
			aria-label={label(openSub)}
			style:left="{subAt.x}px"
			style:top="{subAt.y}px"
			bind:this={subPanel}
			onkeydown={(e) => menuKey(e, true)}
		>
			{@render items(subOf(openSub) ?? [], true)}
		</div>
	{/if}
{/if}

{#if power}<PowerDialog action={power} onclose={() => (power = null)} />{/if}
{#if shortcuts}<ShortcutsDialog {admin} desktop onclose={() => (shortcuts = false)} />{/if}

<style>
	.menubar {
		display: flex;
		align-items: center;
		gap: 1px;
		min-width: 0;
		flex: 0 1 auto;
		outline: none;
	}
	.top {
		flex: none;
		height: var(--pb);
		display: flex;
		align-items: center;
		padding: 0 9px;
		border: 0;
		border-radius: 7px;
		background: none;
		color: var(--text);
		font: inherit;
		font-size: var(--mb-font, 0.875rem);
		white-space: nowrap;
		cursor: default;
	}
	.top:hover,
	.top.on {
		background: color-mix(in srgb, var(--text) 11%, transparent);
	}
	.top:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
	.top.logo {
		position: relative;
		padding: 0 6px;
	}
	.top.logo img {
		display: block;
		border-radius: 5px;
	}
	.top.logo .dot {
		position: absolute;
		top: 3px;
		right: 2px;
		width: 8px;
		height: 8px;
		border-radius: 50%;
		background: var(--accent);
		border: 1.5px solid var(--surface);
	}
	.program {
		flex: 0 1 auto;
		min-width: 2.5em;
		max-width: 220px;
		padding: 0 8px 0 4px;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
		font-size: var(--mb-font, 0.875rem);
		font-weight: 700;
	}
	.mb-menu {
		position: fixed;
		z-index: 9800;
		min-width: 230px;
		max-width: min(360px, calc(100vw - 8px));
		max-height: calc(100vh - 60px);
		overflow-y: auto;
		display: flex;
		flex-direction: column;
		padding: 5px;
		color: var(--text);
	}
	.mb-menu [role^='menuitem'] {
		display: flex;
		align-items: center;
		gap: 6px;
		min-height: 1.9rem;
		padding: 0 10px 0 4px;
		border: 0;
		border-radius: 6px;
		background: transparent;
		color: var(--text);
		font: inherit;
		font-size: var(--text-md);
		text-align: left;
		white-space: nowrap;
		cursor: default;
	}
	.mb-menu [role^='menuitem']:hover:not(:disabled),
	.mb-menu [role^='menuitem']:focus-visible,
	.mb-menu [role^='menuitem'].open {
		background: var(--accent);
		color: var(--on-accent);
		outline: none;
	}
	.mb-menu [role^='menuitem']:hover:not(:disabled) kbd,
	.mb-menu [role^='menuitem']:focus-visible kbd {
		color: inherit;
	}
	.mb-menu [role^='menuitem']:disabled {
		color: var(--muted);
		opacity: 0.75;
	}
	.mark {
		width: 1.1em;
		flex: none;
		text-align: center;
	}
	.text {
		flex: 1;
		min-width: 0;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	kbd {
		margin-left: 18px;
		color: var(--muted);
		font-family: inherit;
		font-size: var(--text-sm);
	}
	.arrow {
		margin-left: 14px;
	}
	.count {
		padding: 0 7px;
		border-radius: 9px;
		background: var(--accent);
		color: var(--on-accent);
		font-size: var(--text-xs);
		font-weight: 600;
	}
	.mb-menu hr {
		height: 1px;
		margin: 4px 6px;
		border: 0;
		background: var(--border);
	}
</style>
