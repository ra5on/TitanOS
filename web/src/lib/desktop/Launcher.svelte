<script lang="ts">
	// PLAN 61: the main menu – every program with filters and the account's
	// buttons. A program dragged out of it lands on the desktop.
	// PLAN 79-10/79-11: the one menu ("Menü"). It looks the same from the top
	// bar's button and from the dock's. Its search field sits in the top bar
	// (`query` comes from there); the programs are sorted A–Z unless the
	// account put them in its own order (drag, or the arrows while arranging,
	// or Ctrl + Shift + arrow on a program).
	import { tick } from 'svelte';
	import Icon from '../components/Icon.svelte';
	import { i18n, t } from '../i18n/index.svelte.ts';
	import { logout, session } from '../session.svelte.ts';
	import DesktopIcon from './DesktopIcon.svelte';
	import { desktop, savePrefs } from './wm.svelte.ts';
	import { programs, vmScreens, type Program } from './programs.svelte.ts';
	import { menuMove, menuSorted } from './prefs.ts';
	import { api } from '../api.ts';
	import Dialog from '../components/Dialog.svelte';
	import { apiErrorText } from '../i18n/index.svelte.ts';
	import { dockerUpdates, sources } from './sources.svelte.ts';
	import { matchActions, quickActions, type QuickAction } from './quick-actions.ts';

	let {
		onopen,
		onclose,
		onpower,
		onsearch,
		query = '',
		at = 'top',
		phone = false,
	}: {
		onopen: (key: string, href?: string) => void;
		onclose: () => void;
		/**
		 * Restart or shut down: the desktop shows the dialog, so that it stays
		 * while the NAS prepares, whatever becomes of the menu.
		 */
		onpower: (action: 'restart' | 'shutdown') => void;
		/** Puts the keyboard into the search field of the bar (arrow up from the first row). */
		onsearch?: () => void;
		/** What is typed into the search field of the bar. */
		query?: string;
		/** Where it opens: under the top bar, or next to the dock's menu button (PLAN 69). */
		at?: 'top' | 'bottom' | 'left' | 'right';
		/** On the phone: across the whole width under the bar of the home screen. */
		phone?: boolean;
	} = $props();

	type Filter = 'all' | 'system' | 'docker' | 'vm' | 'webapp' | 'recent';
	let filter = $state<Filter>('all');

	const mac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform);
	const user = $derived(session.data?.user);
	const admin = $derived(['owner', 'admin'].includes(user?.role ?? ''));
	const all = $derived([...programs(), ...vmScreens()]);
	// PLAN 79-11: A–Z, or the account's own order.
	const custom = $derived(desktop.prefs.menuOrder.length > 0);
	const ordered = $derived(menuSorted(all, desktop.prefs.menuOrder, i18n.locale));
	// Search also finds the pages inside programs ("Docker › Netzwerke").
	const pages = $derived.by(() => {
		const q = query.trim().toLowerCase();
		if (!q) return [] as { key: string; href: string; label: string; program: Program }[];
		const out: { key: string; href: string; label: string; program: Program }[] = [];
		for (const p of ordered)
			for (const item of p.side)
				if ('label' in item) {
					const label = t(item.label);
					if (label.toLowerCase().includes(q) && !p.label.toLowerCase().includes(q))
						out.push({ key: p.key, href: item.href, label: `${p.label} › ${label}`, program: p });
				}
		return out.slice(0, 12);
	});
	const shown = $derived.by(() => {
		const q = query.trim().toLowerCase();
		let list = ordered.filter(
			(p) => !q || p.label.toLowerCase().includes(q) || p.key.toLowerCase().includes(q),
		);
		if (filter === 'system') list = list.filter((p) => p.kind === 'system');
		if (filter === 'docker') list = list.filter((p) => p.kind === 'docker');
		if (filter === 'vm') list = list.filter((p) => p.kind === 'vm');
		if (filter === 'webapp') list = list.filter((p) => p.kind === 'webapp');
		if (filter === 'recent')
			list = desktop.prefs.recent.flatMap((k) => list.filter((p) => p.key === k));
		return list;
	});
	// PLAN 70: quick actions. The backup jobs are asked for only while the menu
	// is open and something is typed; containers and VMs the desktop has anyway.
	let backups = $state<string[] | null>(null);
	$effect(() => {
		if (!admin || backups !== null || !query.trim()) return;
		backups = [];
		api
			.get<{ jobs?: { job: { id: string; enabled: boolean } }[] }>('/backups')
			.then((r) => (backups = (r.jobs ?? []).filter((j) => j.job.enabled).map((j) => j.job.id)))
			.catch(() => (backups = []));
	});
	const actions = $derived(
		matchActions(
			quickActions(t as never, admin, {
				containers: sources.containers,
				vms: sources.vms,
				backups: backups ?? [],
			}),
			query,
		),
	);
	let asking = $state<QuickAction | null>(null);
	let acting = $state(false);
	let actionNote = $state<{ ok: boolean; text: string } | null>(null);
	function pick(action: QuickAction) {
		if (action.local === 'restart' || action.local === 'shutdown') {
			onpower(action.local);
			return;
		}
		if (action.confirm) asking = action;
		else void run(action);
	}
	async function run(action: QuickAction) {
		asking = null;
		if (action.local === 'logout') {
			void logout();
			return;
		}
		if (!action.post) return;
		acting = true;
		actionNote = null;
		try {
			await api.post(action.post);
			actionNote = { ok: true, text: t('quick.done', { name: action.label }) };
		} catch (e) {
			actionNote = { ok: false, text: apiErrorText(e) };
		} finally {
			acting = false;
		}
	}

	// The number on "Updates" (it was on the logo menu): a system update and Docker apps.
	let systemUpdate = $state(false);
	$effect(() => {
		if (!admin) return;
		api
			.get<{ available: unknown }>('/update')
			.then((u) => (systemUpdate = !!u?.available))
			.catch(() => {});
	});
	const updates = $derived(admin ? (systemUpdate ? 1 : 0) + dockerUpdates() : 0);

	const onDesk = $derived(
		new Set([...desktop.prefs.icons, ...desktop.prefs.folders.flatMap((f) => f.apps)]),
	);

	// ---- PLAN 79-11: the account's own order ----

	/** Reordering needs the whole list in its order: not while searching or in "Zuletzt". */
	const canOrder = $derived(filter !== 'recent' && !query.trim());
	let arranging = $state(false);
	let moved = $state('');
	/** Puts `key` in front of `before` (a program shown now), or at the end. */
	function place(key: string, before: string | null) {
		const keys = ordered.map((p) => p.key);
		const next = menuMove(keys, key, before);
		if (next.join('\n') === keys.join('\n')) return;
		savePrefs((p) => (p.menuOrder = next));
		const p = ordered.find((x) => x.key === key);
		const index = shown.findIndex((x) => x.key === key);
		if (p && index >= 0)
			moved = t('desktop.menu.moved', { name: p.label, place: index + 1, total: shown.length });
	}
	/** One place towards the start (-1) or the end (1), among the programs shown. */
	async function step(key: string, by: 1 | -1) {
		if (!canOrder) return;
		const keys = shown.map((p) => p.key);
		const index = keys.indexOf(key);
		const target = index + by;
		if (index < 0 || target < 0 || target >= keys.length) return;
		const control = (document.activeElement as HTMLElement | null)?.dataset.move;
		// Towards the end: in front of the program after the next one.
		place(key, by < 0 ? keys[target] : (keys[target + 1] ?? null));
		await tick();
		// The moved node may lose the keyboard: it stays on the same program.
		const cell = root?.querySelector<HTMLElement>(`[data-key="${CSS.escape(key)}"]`);
		const again = control
			? cell?.querySelector<HTMLElement>(`[data-move="${control}"]:not(:disabled)`)
			: null;
		(again ?? cell?.querySelector<HTMLElement>('.prog'))?.focus();
	}
	function resetOrder() {
		savePrefs((p) => (p.menuOrder = []));
		moved = t('desktop.menu.orderAz');
	}

	// PLAN 73 (owner 8.11.2026: "lässt sich nicht auf den Arbeitsplatz ziehen"):
	// closing the menu at the start of the drag removed the dragged button and
	// the browser cancelled the drag. PLAN 79-11: while the pointer is over the
	// menu the drag reorders the programs; once it leaves the menu, the menu
	// steps aside (invisible, lets the pointer through) so the desktop and the
	// dock take the program, and it closes when the drag ends.
	let dragging = $state(false);
	let dragKey = $state<string | null>(null);
	let dropAt = $state<{ key: string; after: boolean } | null>(null);
	function drag(event: DragEvent, key: string) {
		event.dataTransfer?.setData('text/x-titanos-program', key);
		if (event.dataTransfer) event.dataTransfer.effectAllowed = 'copyMove';
		dragKey = key;
	}
	function dragEnd() {
		const left = dragging;
		dragging = false;
		dragKey = null;
		dropAt = null;
		if (left) onclose();
	}
	function dragOverWindow(event: DragEvent) {
		if (!dragKey || dragging || !root) return;
		const r = root.getBoundingClientRect();
		const inside =
			event.clientX >= r.left &&
			event.clientX <= r.right &&
			event.clientY >= r.top &&
			event.clientY <= r.bottom;
		if (!inside) {
			dropAt = null;
			dragging = true;
		}
	}
	function dragOver(event: DragEvent) {
		if (!dragKey) return;
		// Over the menu the desktop shows no place for the program.
		event.stopPropagation();
		event.preventDefault();
		if (!canOrder) return;
		const cell = (event.target as HTMLElement).closest<HTMLElement>('.cell');
		const key = cell?.dataset.key;
		if (!cell || !key) return;
		if (key === dragKey) {
			dropAt = null;
			return;
		}
		const r = cell.getBoundingClientRect();
		const after = event.clientX > r.left + r.width / 2;
		if (dropAt?.key !== key || dropAt.after !== after) dropAt = { key, after };
	}
	function drop(event: DragEvent) {
		if (!dragKey) return;
		event.stopPropagation();
		event.preventDefault();
		const key = dragKey;
		const to = dropAt;
		dropAt = null;
		if (!to || !canOrder) return;
		const rest = shown.map((p) => p.key).filter((k) => k !== key);
		const index = rest.indexOf(to.key);
		place(key, to.after ? (rest[index + 1] ?? null) : to.key);
	}

	let root = $state<HTMLElement>();
	function first() {
		if (shown[0]) onopen(shown[0].key);
		else if (pages[0]) onopen(pages[0].key, pages[0].href);
		else if (actions[0]) pick(actions[0]);
	}
	function items() {
		return [...(root?.querySelectorAll<HTMLElement>('.prog, .page, .action') ?? [])];
	}
	/**
	 * A key pressed in the search field of the bar: Enter opens the first hit,
	 * arrow down goes to the programs. True when the key was used.
	 */
	export function searchKey(event: KeyboardEvent): boolean {
		if (event.key === 'Enter' && (shown[0] || pages[0] || actions[0])) {
			first();
			return true;
		}
		if (event.key === 'ArrowDown') {
			const list = items();
			if (!list.length) return false;
			list[0].focus();
			return true;
		}
		return false;
	}
	function keys(event: KeyboardEvent) {
		if (event.key === 'Escape') {
			event.preventDefault();
			onclose();
			return;
		}
		const target = event.target as HTMLElement;
		// PLAN 79-11: Ctrl + Shift + arrow moves the program under the keyboard.
		if (
			(event.ctrlKey || event.metaKey) &&
			event.shiftKey &&
			(event.key === 'ArrowLeft' || event.key === 'ArrowRight') &&
			target.classList.contains('prog')
		) {
			event.preventDefault();
			const key = target.closest<HTMLElement>('.cell')?.dataset.key;
			if (key) void step(key, event.key === 'ArrowLeft' ? -1 : 1);
			return;
		}
		// PLAN 65-5: the arrow keys move through the programs and pages like a grid.
		if (!['ArrowDown', 'ArrowUp', 'ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key))
			return;
		const list = items();
		if (!list.length) return;
		const at = list.indexOf(document.activeElement as HTMLElement);
		if (at < 0) return;
		event.preventDefault();
		const top = list[0].getBoundingClientRect().top;
		const columns = Math.max(
			1,
			list.filter((el) => Math.abs(el.getBoundingClientRect().top - top) < 2).length,
		);
		const isPage = (i: number) => !list[i].classList.contains('prog');
		const stepBy = isPage(at) ? 1 : columns;
		let next = at;
		if (event.key === 'Home') next = 0;
		else if (event.key === 'End') next = list.length - 1;
		else if (event.key === 'ArrowRight') next = Math.min(list.length - 1, at + 1);
		else if (event.key === 'ArrowLeft') next = Math.max(0, at - 1);
		else if (event.key === 'ArrowDown') next = Math.min(list.length - 1, at + stepBy);
		else if (event.key === 'ArrowUp') {
			if (at - stepBy < 0) {
				onsearch?.();
				return;
			}
			next = at - stepBy;
		}
		list[next].focus();
	}
	const FILTERS: [Filter, Parameters<typeof t>[0]][] = [
		['all', 'desktop.menu.all'],
		['system', 'desktop.menu.system'],
		['docker', 'desktop.menu.docker'],
		['vm', 'desktop.menu.vms'],
		['webapp', 'webapps.menu.filter'],
		['recent', 'desktop.menu.recent'],
	];
	// On touch devices there is neither dragging nor a keyboard: the hint
	// names the arrows only (PLAN 79-16).
	const touch = $derived(
		phone || (typeof matchMedia === 'function' && matchMedia('(pointer: coarse)').matches),
	);
</script>

<svelte:window ondragover={dragOverWindow} />

<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
<div
	class="launcher glass-panel at-{at}"
	class:phone
	class:dragging
	bind:this={root}
	role="dialog"
	aria-label={t('desktop.menu.title')}
	tabindex="-1"
	onkeydown={keys}
	ondragover={dragOver}
	ondrop={drop}
>
	<div class="head">
		<div class="filters" role="group" aria-label={t('desktop.menu.filter')}>
			{#each FILTERS.filter(([id]) => id !== 'webapp' || all.some((p) => p.kind === 'webapp')) as [id, label] (id)}
				<button
					type="button"
					class="chip"
					class:on={filter === id}
					aria-pressed={filter === id}
					onclick={() => (filter = id)}>{t(label)}</button
				>
			{/each}
		</div>
		<div class="order">
			<span class="order-now" data-order={custom ? 'own' : 'az'}
				>{custom ? t('desktop.menu.orderOwn') : t('desktop.menu.orderAz')}</span
			>
			{#if custom}<button type="button" class="link" data-menu-reset onclick={resetOrder}
					>{t('desktop.menu.resetOrder')}</button
				>{/if}
			<button
				type="button"
				class="link"
				data-menu-arrange
				aria-pressed={arranging}
				onclick={() => (arranging = !arranging)}
				>{arranging ? t('desktop.menu.arrangeDone') : t('desktop.menu.arrange')}</button
			>
		</div>
	</div>
	{#if arranging}<p class="hint">
			{canOrder
				? t(touch ? 'desktop.menu.arrangeHintTouch' : 'desktop.menu.arrangeHint')
				: t('desktop.menu.arrangeOff')}
		</p>{/if}
	<div class="grid" class:gone={!shown.length && (pages.length > 0 || actions.length > 0)}>
		{#each shown as p, i (p.key)}
			<div
				class="cell"
				class:drop-before={dropAt?.key === p.key && !dropAt.after}
				class:drop-after={dropAt?.key === p.key && dropAt.after}
				class:dragged={dragKey === p.key}
				data-key={p.key}
			>
				<button
					type="button"
					class="prog"
					draggable="true"
					ondragstart={(e) => drag(e, p.key)}
					ondragend={dragEnd}
					onclick={() => onopen(p.key)}
					title={onDesk.has(p.key) ? undefined : t('desktop.menu.dragHint')}
				>
					<DesktopIcon
						glyph={p.glyph}
						gradient={p.gradient}
						image={p.image}
						name={p.label}
						size={52}
					/>
					<span>{p.label}</span>
				</button>
				{#if arranging}
					<span class="moves">
						<button
							type="button"
							class="mv"
							data-move="back"
							disabled={!canOrder || i === 0}
							aria-label={t('desktop.menu.moveBack', { name: p.label })}
							title={t('desktop.menu.moveBack', { name: p.label })}
							onclick={() => step(p.key, -1)}><Icon name="chevronLeft" size={15} /></button
						>
						<button
							type="button"
							class="mv"
							data-move="on"
							disabled={!canOrder || i === shown.length - 1}
							aria-label={t('desktop.menu.moveOn', { name: p.label })}
							title={t('desktop.menu.moveOn', { name: p.label })}
							onclick={() => step(p.key, 1)}><Icon name="chevronRight" size={15} /></button
						>
					</span>
				{/if}
			</div>
		{:else}
			{#if !pages.length && !actions.length}<p class="muted none">
					{t('desktop.menu.nothing')}
				</p>{/if}
		{/each}
	</div>
	<p class="sr-only" role="status" aria-live="polite">{moved}</p>
	{#if pages.length}
		<div class="pages">
			{#each pages as hit (hit.key + hit.href)}
				<button type="button" class="page" onclick={() => onopen(hit.key, hit.href)}>
					<DesktopIcon
						glyph={hit.program.glyph}
						gradient={hit.program.gradient}
						image={hit.program.image}
						name={hit.program.label}
						size={22}
					/>
					<span>{hit.label}</span>
				</button>
			{/each}
		</div>
	{/if}
	{#if actions.length}
		<div class="pages actions" role="group" aria-label={t('quick.title')}>
			<span class="group-title">{t('quick.title')}</span>
			{#each actions as action (action.id)}
				<button type="button" class="page action" disabled={acting} onclick={() => pick(action)}
					><Icon name={action.icon as 'play'} size={17} /><span>{action.label}</span
					>{#if action.confirm || action.local === 'restart' || action.local === 'shutdown'}<span
							class="muted small">…</span
						>{/if}</button
				>
			{/each}
		</div>
	{/if}
	{#if actionNote}<p
			class="action-note"
			class:notice-error={!actionNote.ok}
			role={actionNote.ok ? 'status' : 'alert'}
		>
			{actionNote.text}
		</p>{/if}
	<!-- Two rows of buttons of one size: the account and the places of the
	     system; below, fixed: log out, restart, shut down ("Sperren" left the menu, owner 13.11.2026: it did what "Abmelden" does). -->
	<div class="foot">
		<div class="row" data-menu-row="1">
			<button
				type="button"
				class="fb who"
				data-menu-do="account"
				title={t('menu.account')}
				onclick={() => onopen('app:account')}
			>
				<b class="avatar" aria-hidden="true">{(user?.name ?? '?').slice(0, 1).toUpperCase()}</b>
				<span>{user?.name}</span><span class="sr-only"> · {t('menu.account')}</span>
			</button>
			<button
				type="button"
				class="fb"
				data-menu-do="settings"
				onclick={() => onopen('app:settings')}
				><Icon name="gear" size={16} /><span>{t('desktop.app.settings')}</span></button
			>
			{#if admin}
				<button
					type="button"
					class="fb"
					data-menu-do="updates"
					onclick={() => onopen('app:settings', '/settings/updates')}
					><Icon name="download" size={16} /><span>{t('desktop.side.updates')}</span
					>{#if updates}<span class="count"
							><span class="sr-only">{t('desktop.menu.updatesCount', { count: updates })}</span
							><span aria-hidden="true">{updates}</span></span
						>{/if}</button
				>
				<button
					type="button"
					class="fb"
					data-menu-do="appcenter"
					onclick={() => onopen('app:appcenter')}
					><Icon name="plus" size={16} /><span>{t('desktop.app.appcenter')}</span></button
				>
			{/if}
		</div>
		<div class="row" data-menu-row="2">
			<button type="button" class="fb" data-menu-do="logout" onclick={() => void logout()}
				><Icon name="logout" size={16} /><span>{t('desktop.menu.logout')}</span></button
			>
			{#if admin}
				<button
					type="button"
					class="fb"
					data-menu-do="restart"
					title={t('desktop.menu.restart')}
					onclick={() => onpower('restart')}
					><Icon name="restart" size={16} /><span>{t('desktop.menu.restartShort')}</span></button
				>
				<button
					type="button"
					class="fb"
					data-menu-do="shutdown"
					title={t('desktop.menu.shutdown')}
					onclick={() => onpower('shutdown')}
					><Icon name="power" size={16} /><span>{t('desktop.menu.shutdownShort')}</span></button
				>
			{/if}
		</div>
	</div>
</div>
{#if asking}
	{@const a = asking}
	<Dialog title={t('quick.confirmTitle')} onclose={() => (asking = null)}>
		<p>{t('quick.confirm', { name: a.label })}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (asking = null)}>{t('common.cancel')}</button
			>
			<button type="button" class="btn btn-danger" onclick={() => void run(a)}
				>{t('quick.confirmDo')}</button
			>
		</div>
	</Dialog>
{/if}

<style>
	.launcher.dragging {
		opacity: 0;
		pointer-events: none;
	}
	.actions .group-title {
		display: block;
		padding: 2px var(--space-1) var(--space-1);
		color: var(--muted);
		font-size: var(--text-xs);
		letter-spacing: 0.06em;
		text-transform: uppercase;
	}
	.grid.gone {
		display: none;
	}
	.action-note {
		margin: var(--space-2) 0 0;
		font-size: var(--text-sm);
	}
	/* PLAN 62: nearly solid, so icons and widgets behind never shine through the text. */
	.launcher.glass-panel {
		background: color-mix(in srgb, var(--surface) 97%, transparent);
	}
	.launcher {
		position: absolute;
		left: 10px;
		top: 4px;
		width: min(780px, calc(100% - 20px));
		max-height: calc(100% - 14px);
		display: flex;
		flex-direction: column;
		padding: 18px;
		border-radius: 20px;
		z-index: 9000;
		animation: drop 0.16s ease-out;
	}
	/* Opened from the dock: next to it, rising from its edge. */
	.launcher.at-bottom {
		top: auto;
		bottom: calc(var(--dock-room, 90px) + 4px);
		left: max(10px, calc(50% - 390px));
		animation-name: rise;
	}
	.launcher.at-left,
	.launcher.at-right {
		top: auto;
		bottom: 10px;
		animation-name: rise;
	}
	.launcher.at-left {
		left: calc(var(--dock-room, 90px) + 4px);
		width: min(780px, calc(100% - var(--dock-room, 90px) - 20px));
	}
	.launcher.at-right {
		left: auto;
		right: calc(var(--dock-room, 90px) + 4px);
		width: min(780px, calc(100% - var(--dock-room, 90px) - 20px));
	}
	@keyframes rise {
		from {
			opacity: 0;
			transform: translateY(8px) scale(0.98);
		}
	}
	@keyframes drop {
		from {
			opacity: 0;
			transform: translateY(-8px) scale(0.98);
		}
	}
	/* On the phone: across the width, under the bar of the home screen. */
	.launcher.phone {
		position: fixed;
		left: calc(8px + env(safe-area-inset-left));
		right: calc(8px + env(safe-area-inset-right));
		top: calc(64px + env(safe-area-inset-top));
		bottom: auto;
		width: auto;
		max-height: calc(100dvh - 72px - env(safe-area-inset-top) - env(safe-area-inset-bottom));
		padding: var(--space-3);
		overflow-y: auto;
		z-index: 40;
		/* Solid: the home screen's icons must not shine through the names. */
		background: var(--surface);
	}
	.head {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-2) var(--space-3);
		margin-bottom: var(--space-3);
	}
	.order {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-1) var(--space-3);
		font-size: var(--text-sm);
	}
	.order-now {
		color: var(--muted);
	}
	.link {
		min-height: 28px;
		padding: 0 var(--space-1);
		border: 0;
		border-radius: 6px;
		background: none;
		color: var(--text);
		font: inherit;
		text-decoration: underline;
		text-underline-offset: 3px;
		cursor: pointer;
	}
	.link[aria-pressed='true'] {
		font-weight: 600;
	}
	.link:focus-visible,
	.mv:focus-visible,
	.fb:focus-visible,
	.chip:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	.hint {
		margin: 0 0 var(--space-2);
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.filters {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-1-5);
	}
	.chip {
		height: 30px;
		padding: 0 14px;
		border: 1px solid var(--border);
		border-radius: 999px;
		background: var(--chip);
		color: var(--text);
		font: inherit;
		font-size: var(--text-sm);
		font-weight: 600;
		cursor: pointer;
	}
	.chip.on {
		background: var(--accent);
		border-color: var(--accent);
		color: var(--on-accent);
	}
	.grid {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(110px, 1fr));
		gap: var(--space-2-5) var(--space-1);
		overflow-y: auto;
		min-height: 120px;
		padding: var(--space-1);
	}
	.cell {
		display: flex;
		flex-direction: column;
		min-width: 0;
		border-radius: 12px;
	}
	/* Where the dragged program lands: a line at that side of the program. */
	.cell.drop-before {
		box-shadow: -3px 0 0 var(--accent);
	}
	.cell.drop-after {
		box-shadow: 3px 0 0 var(--accent);
	}
	.cell.dragged {
		opacity: 0.45;
	}
	.moves {
		display: flex;
		justify-content: center;
		gap: var(--space-1);
		padding-bottom: var(--space-1);
	}
	.mv {
		width: 30px;
		height: 26px;
		display: grid;
		place-items: center;
		border: 1px solid var(--border);
		border-radius: 7px;
		background: var(--chip);
		color: var(--text);
		cursor: pointer;
	}
	.mv:disabled {
		opacity: 0.4;
		cursor: default;
	}
	.prog {
		flex: 1;
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 7px;
		padding: var(--space-2) var(--space-1);
		border: 0;
		border-radius: 12px;
		background: none;
		color: var(--text);
		font: inherit;
		font-size: var(--text-sm);
		text-align: center;
		cursor: pointer;
		overflow-wrap: anywhere;
	}
	.prog:hover,
	.prog:focus-visible,
	.page:hover,
	.page:focus-visible {
		background: color-mix(in srgb, var(--accent) 16%, transparent);
	}
	/* The keyboard's place is visible, not only a tint like the pointer's. */
	.prog:focus-visible,
	.page:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
	.none {
		grid-column: 1 / -1;
		text-align: center;
	}
	.pages {
		display: flex;
		flex-direction: column;
		gap: 2px;
		margin-top: var(--space-2);
		border-top: 1px solid var(--border);
		padding-top: var(--space-2);
	}
	.page {
		display: flex;
		align-items: center;
		gap: var(--space-2-5);
		padding: var(--space-1-5) var(--space-2);
		border: 0;
		border-radius: 8px;
		background: none;
		color: var(--text);
		font: inherit;
		text-align: left;
		cursor: pointer;
	}
	.foot {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		border-top: 1px solid var(--border);
		padding-top: var(--space-3);
		margin-top: var(--space-3);
	}
	/* Four places of one size in each row; fewer buttons (a user) keep their size. */
	.row {
		display: grid;
		grid-template-columns: repeat(4, minmax(0, 1fr));
		gap: var(--space-2);
	}
	.fb {
		display: flex;
		align-items: center;
		justify-content: center;
		gap: var(--space-2);
		min-height: 40px;
		min-width: 0;
		padding: var(--space-1-5) var(--space-2-5);
		border: 1px solid var(--border);
		border-radius: 10px;
		background: var(--chip);
		color: var(--text);
		font: inherit;
		font-size: var(--text-sm);
		line-height: 1.25;
		text-align: left;
		cursor: pointer;
	}
	.fb:hover {
		background: color-mix(in srgb, var(--accent) 16%, var(--chip));
	}
	.fb span {
		min-width: 0;
		overflow-wrap: anywhere;
	}
	.fb :global(svg) {
		flex: none;
	}
	.avatar {
		width: 22px;
		height: 22px;
		flex: none;
		border-radius: 50%;
		display: grid;
		place-items: center;
		background: var(--accent);
		color: var(--on-accent);
		font-size: var(--text-xs);
	}
	.count {
		flex: none;
		padding: 0 7px;
		border-radius: 9px;
		background: var(--accent);
		color: var(--on-accent);
		font-size: var(--text-xs);
		font-weight: 600;
	}
	/* Narrow (phone): two places in a row, the same order. */
	@media (max-width: 620px) {
		.row {
			grid-template-columns: repeat(2, minmax(0, 1fr));
		}
		.fb {
			justify-content: flex-start;
		}
	}
	.muted {
		color: var(--muted);
	}
	.small {
		font-size: var(--text-sm);
	}
	@media (prefers-reduced-motion: reduce) {
		.launcher {
			animation: none;
		}
	}
</style>
