<script lang="ts">
	// PLAN 61: the main menu – every program with search, filters and the
	// account's buttons. A program dragged out of it lands on the desktop.
	import Icon from '../components/Icon.svelte';
	import PowerDialog from '../components/PowerDialog.svelte';
	import { t } from '../i18n/index.svelte.ts';
	import { logout, session } from '../session.svelte.ts';
	import DesktopIcon from './DesktopIcon.svelte';
	import { desktop } from './wm.svelte.ts';
	import { programs, vmScreens, type Program } from './programs.svelte.ts';
	import { SYSTEM_APPS } from './apps.ts';

	let {
		onopen,
		onclose,
		onlock,
	}: { onopen: (key: string, href?: string) => void; onclose: () => void; onlock?: () => void } =
		$props();

	type Filter = 'all' | 'system' | 'docker' | 'vm' | 'recent';
	let filter = $state<Filter>('all');
	let query = $state('');
	let power = $state<'restart' | 'shutdown' | null>(null);
	let powerMenu = $state(false);
	let field = $state<HTMLInputElement>();
	$effect(() => field?.focus());

	const admin = $derived(['owner', 'admin'].includes(session.data?.user?.role ?? ''));
	const all = $derived([...programs(), ...vmScreens()]);
	// Search also finds the pages inside programs ("Docker › Netzwerke").
	const pages = $derived.by(() => {
		const q = query.trim().toLowerCase();
		if (!q) return [] as { key: string; href: string; label: string; program: Program }[];
		const out: { key: string; href: string; label: string; program: Program }[] = [];
		for (const p of all)
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
		let list = all.filter(
			(p) => !q || p.label.toLowerCase().includes(q) || p.key.toLowerCase().includes(q),
		);
		if (filter === 'system') list = list.filter((p) => p.kind === 'system');
		if (filter === 'docker') list = list.filter((p) => p.kind === 'docker');
		if (filter === 'vm') list = list.filter((p) => p.kind === 'vm');
		if (filter === 'recent')
			list = desktop.prefs.recent.flatMap((k) => list.filter((p) => p.key === k));
		return list;
	});
	const onDesk = $derived(
		new Set([...desktop.prefs.icons, ...desktop.prefs.folders.flatMap((f) => f.apps)]),
	);
	void SYSTEM_APPS;

	function drag(event: DragEvent, key: string) {
		event.dataTransfer?.setData('text/x-titanos-program', key);
		if (event.dataTransfer) event.dataTransfer.effectAllowed = 'copyMove';
		// The menu steps aside so the desktop can take the icon.
		setTimeout(onclose, 0);
	}
	function keys(event: KeyboardEvent) {
		if (event.key === 'Escape') {
			event.preventDefault();
			onclose();
		} else if (event.key === 'Enter' && (shown[0] || pages[0])) {
			event.preventDefault();
			if (shown[0]) onopen(shown[0].key);
			else onopen(pages[0].key, pages[0].href);
		}
	}
	const FILTERS: [Filter, Parameters<typeof t>[0]][] = [
		['all', 'desktop.menu.all'],
		['system', 'desktop.menu.system'],
		['docker', 'desktop.menu.docker'],
		['vm', 'desktop.menu.vms'],
		['recent', 'desktop.menu.recent'],
	];
</script>

<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
<div
	class="launcher glass-panel"
	role="dialog"
	aria-label={t('desktop.menu.title')}
	tabindex="-1"
	onkeydown={keys}
>
	<label class="search">
		<Icon name="search" size={17} />
		<input
			bind:this={field}
			bind:value={query}
			placeholder={t('desktop.menu.search')}
			aria-label={t('desktop.menu.search')}
		/>
		<kbd>{t('desktop.menu.shortcut')}</kbd>
	</label>
	<div class="filters" role="group" aria-label={t('desktop.menu.filter')}>
		{#each FILTERS as [id, label] (id)}
			<button
				type="button"
				class="chip"
				class:on={filter === id}
				aria-pressed={filter === id}
				onclick={() => (filter = id)}>{t(label)}</button
			>
		{/each}
	</div>
	<div class="grid">
		{#each shown as p (p.key)}
			<button
				type="button"
				class="prog"
				draggable="true"
				ondragstart={(e) => drag(e, p.key)}
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
		{:else}
			{#if !pages.length}<p class="muted none">{t('desktop.menu.nothing')}</p>{/if}
		{/each}
	</div>
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
	<footer>
		<span class="who">
			<b class="avatar" aria-hidden="true"
				>{(session.data?.user?.name ?? '?').slice(0, 1).toUpperCase()}</b
			>
			<span
				><b>{session.data?.user?.name}</b><br /><span class="muted small"
					>{t(`role.${session.data?.user?.role ?? 'user'}` as Parameters<typeof t>[0])} · {session
						.data?.hostname}</span
				></span
			>
		</span>
		<span class="space"></span>
		<button type="button" class="btn" onclick={() => onopen('app:settings')}
			><Icon name="gear" size={15} />{t('desktop.app.settings')}</button
		>
		<button type="button" class="btn" onclick={() => onopen('app:account')}
			><Icon name="users" size={15} />{t('desktop.menu.account')}</button
		>
		{#if onlock}<button type="button" class="btn" onclick={onlock}
				><Icon name="lock" size={15} />{t('desktop.menu.lock')}</button
			>{/if}
		<span class="power">
			<button
				type="button"
				class="btn"
				aria-haspopup="menu"
				aria-expanded={powerMenu}
				onclick={() => (powerMenu = !powerMenu)}
				><Icon name="power" size={15} />{t('desktop.menu.power')}</button
			>
			{#if powerMenu}
				<div class="power-menu dropdown-panel" role="menu">
					<button type="button" role="menuitem" onclick={() => void logout()}
						><span>{t('desktop.menu.logout')}</span></button
					>
					{#if admin}
						<button
							type="button"
							role="menuitem"
							onclick={() => ((power = 'restart'), (powerMenu = false))}
							><span>{t('desktop.menu.restart')}</span></button
						>
						<button
							type="button"
							role="menuitem"
							onclick={() => ((power = 'shutdown'), (powerMenu = false))}
							><span>{t('desktop.menu.shutdown')}</span></button
						>
					{/if}
				</div>
			{/if}
		</span>
	</footer>
</div>
{#if power}<PowerDialog action={power} onclose={() => (power = null)} />{/if}

<style>
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
	@keyframes drop {
		from {
			opacity: 0;
			transform: translateY(-8px) scale(0.98);
		}
	}
	.search {
		display: flex;
		align-items: center;
		gap: 10px;
		height: 44px;
		padding: 0 14px;
		border: 1px solid var(--border);
		border-radius: 12px;
		background: color-mix(in srgb, var(--bg) 55%, transparent);
		color: var(--muted);
	}
	.search input {
		flex: 1;
		min-width: 0;
		border: 0;
		background: none;
		color: var(--text);
		font: inherit;
		font-size: var(--text-md);
		outline: none;
	}
	kbd {
		font: inherit;
		font-size: var(--text-xs);
		color: var(--muted);
		white-space: nowrap;
	}
	.filters {
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
		margin: 12px 0;
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
		gap: 10px 4px;
		overflow-y: auto;
		min-height: 120px;
		padding: 4px;
	}
	.prog {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 7px;
		padding: 8px 4px;
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
		outline: none;
	}
	.none {
		grid-column: 1 / -1;
		text-align: center;
	}
	.pages {
		display: flex;
		flex-direction: column;
		gap: 2px;
		margin-top: 8px;
		border-top: 1px solid var(--border);
		padding-top: 8px;
	}
	.page {
		display: flex;
		align-items: center;
		gap: 10px;
		padding: 6px 8px;
		border: 0;
		border-radius: 8px;
		background: none;
		color: var(--text);
		font: inherit;
		text-align: left;
		cursor: pointer;
	}
	footer {
		display: flex;
		align-items: center;
		flex-wrap: wrap;
		gap: 8px;
		border-top: 1px solid var(--border);
		padding-top: 12px;
		margin-top: 12px;
	}
	footer .btn {
		display: inline-flex;
		align-items: center;
		gap: 6px;
	}
	.who {
		display: flex;
		align-items: center;
		gap: 10px;
		line-height: 1.25;
	}
	.avatar {
		width: 36px;
		height: 36px;
		border-radius: 50%;
		display: grid;
		place-items: center;
		background: var(--accent);
		color: var(--on-accent);
	}
	.space {
		flex: 1;
	}
	.muted {
		color: var(--muted);
	}
	.small {
		font-size: var(--text-sm);
	}
	.power {
		position: relative;
	}
	.power-menu {
		position: absolute;
		right: 0;
		bottom: calc(100% + 6px);
		min-width: 13rem;
		z-index: 2;
	}
	@media (prefers-reduced-motion: reduce) {
		.launcher {
			animation: none;
		}
	}
</style>
