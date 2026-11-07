<script lang="ts">
	import type { FeatureViewId } from '#lib/agent-features.ts';
	import { swipe } from '#lib/agent-swipe.ts';
	// The side bar of the KI-Agent (PLAN 48, look of PLAN 68 like the side
	// bar of every window): new chat, search, suggestions, planned, notes,
	// the projects with their chats, then the chats by day. Running chats
	// pulse, waiting ones say so, unread ones carry a dot. At the bottom the
	// settings, folding the side bar and, on the phone or in the installed
	// app, the way back to TitanOS. On the phone it slides in from the left.
	import Icon, { type IconName } from '#lib/components/Icon.svelte';
	import DesktopIcon from '#lib/desktop/DesktopIcon.svelte';
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { groupChats, type ChatInfo, type Project, type Swipe } from '#lib/agent.ts';

	let {
		chats,
		projects,
		current = '',
		project = '',
		admin = false,
		onopen,
		onnew,
		onproject,
		onnewproject,
		oneditproject,
		onknowledge,
		onplanned,
		onnotes,
		features = [],
		onfeature,
		gestures = false,
		pull = false,
		compact = false,
		onrefresh,
		swipeLeft = 'none',
		swipeRight = 'none',
		onaction,
		onsettings,
		inboxCount = 0,
		oninbox,
		plannedCount = 0,
		settingsOn = false,
		onfold,
		ontitanos,
	}: {
		chats: ChatInfo[];
		projects: Project[];
		current?: string;
		/** The open project (its chats show below it). */
		project?: string;
		admin?: boolean;
		/** author: another account's chat in a shared project (read-only). */
		onopen: (id: string, author?: string) => void;
		onnew: () => void;
		onproject: (id: string) => void;
		onnewproject: () => void;
		oneditproject: (p: Project) => void;
		onknowledge: () => void;
		/** Opens the planned tasks, reminders and the morning report. */
		onplanned: () => void;
		/** Opens the notes (PLAN 57). */
		onnotes?: () => void;
		/** Views of the functions that are on for this account (PLAN 54). */
		features?: { id: FeatureViewId; label: string; icon: IconName; count?: number }[];
		onfeature?: (id: FeatureViewId) => void;
		/** PLAN 53: swiping a chat aside and its menu. */
		gestures?: boolean;
		pull?: boolean;
		compact?: boolean;
		onrefresh?: () => void;
		swipeLeft?: Swipe;
		swipeRight?: Swipe;
		onaction?: (chat: ChatInfo, action: Swipe) => void;
		/** Opens the settings of the agent (PLAN 53). */
		onsettings: () => void;
		/** PLAN 53: open suggestions of the agent. */
		inboxCount?: number;
		oninbox?: () => void;
		/** Planned tasks and reminders of the account. */
		plannedCount?: number;
		/** The settings are open (their row is marked). */
		settingsOn?: boolean;
		/** Folds the side bar away (desktop). */
		onfold?: () => void;
		/** Back to TitanOS (phone, installed app). */
		ontitanos?: () => void;
	} = $props();

	let query = $state('');
	let searching = $state(false);
	function searchInput(node: HTMLInputElement) {
		node.focus();
	}
	const found = $derived(
		query.trim()
			? chats.filter((c) =>
					(c.title + ' ' + (c.preview ?? '')).toLowerCase().includes(query.trim().toLowerCase()),
				)
			: null,
	);
	const loose = $derived(
		chats.filter((c) => !c.project || !projects.some((p) => p.id === c.project)),
	);
	const groupLabel: Record<string, MessageKey> = {
		pinned: 'agent.groupPinned',
		today: 'agent.groupToday',
		yesterday: 'agent.groupYesterday',
		week: 'agent.groupWeek',
		older: 'agent.groupOlder',
	};
	const count = (id: string) => chats.filter((c) => c.project === id).length;
	const waiting = (id: string) =>
		chats.filter((c) => c.project === id && c.state === 'waiting').length;
	/** What a closed project shows of its chats: running, waiting, unread. */
	function stateIn(id: string): 'running' | 'waiting' | 'unread' | '' {
		const list = chats.filter((c) => c.project === id);
		if (list.some((c) => c.state === 'waiting')) return 'waiting';
		if (list.some((c) => c.state === 'running')) return 'running';
		return list.some((c) => c.unread) ? 'unread' : '';
	}
	const label = (a: Swipe, chat: ChatInfo) =>
		a === 'pin'
			? chat.pinned
				? t('agent.unpin')
				: t('agent.pin')
			: a === 'delete'
				? t('agent.delete')
				: a === 'menu'
					? t('agent.set.swipe.menu')
					: '';
	let pullFrom: number | null = null;
	let pulled = $state(0);
</script>

{#snippet row(chat: ChatInfo, sub = false)}
	<div
		class="swipe"
		use:swipe={{
			enabled: gestures && !chat.author,
			right: chat.author ? '' : label(swipeRight, chat),
			left: chat.author ? '' : label(swipeLeft, chat),
			onswipe: (side) => onaction?.(chat, side === 'right' ? swipeRight : swipeLeft),
			onmenu: () => !chat.author && onaction?.(chat, 'menu'),
		}}
	>
		<button
			type="button"
			class="item"
			class:sub
			class:on={chat.id === current}
			class:unread={chat.unread}
			aria-current={chat.id === current ? 'page' : undefined}
			title={chat.author ? t('agent.chatBy', { name: chat.author }) : undefined}
			onclick={() => onopen(chat.id, chat.author)}
		>
			{#if !sub}<span class="em" aria-hidden="true"
					>{#if chat.pinned}<Icon name="pin" size={14} />{:else if chat.origin === 'schedule'}<Icon
							name="clock"
							size={14}
						/>{:else if chat.origin === 'event'}<Icon
							name="bell"
							size={14}
						/>{:else if chat.origin === 'reminder'}<span class="emoji">⏰</span>{:else}<Icon
							name="chat"
							size={14}
						/>{/if}</span
				>{/if}
			<span class="name">{chat.title || t('agent.newChat')}</span>
			{#if chat.author}<span class="by">{chat.author}</span>{/if}
			{#if chat.state === 'running'}<span class="live" title={t('agent.running')}
					><span class="sr-only">{t('agent.running')}</span></span
				>{:else if chat.state === 'waiting'}<span class="wait">{t('agent.approvalShort')}</span
				>{:else if chat.unread}<span class="dot" title={t('agent.unread')}
					><span class="sr-only">{t('agent.unread')}</span></span
				>{/if}
		</button>
	</div>
{/snippet}

<nav class="menu" class:compact aria-label={t('agent.menu')}>
	<div class="top">
		<DesktopIcon glyph="agent" gradient="violet" size={28} name={t('nav.agent')} />
		<strong class="brand">{t('nav.agent')}</strong>
	</div>
	<button type="button" class="btn new-chat" onclick={onnew}
		><Icon name="plus" size={16} /><span>{t('agent.newChat')}</span></button
	>
	<!-- Pull to refresh is an extra; the list can also be reloaded by reopening. -->
	<div
		class="scroll"
		role="presentation"
		ontouchstart={(e) =>
			(pullFrom = pull && e.currentTarget.scrollTop <= 0 ? e.touches[0].clientY : null)}
		ontouchmove={(e) => {
			if (pullFrom === null) return;
			pulled = Math.max(0, Math.min(90, e.touches[0].clientY - pullFrom));
		}}
		ontouchend={() => {
			if (pulled >= 70) onrefresh?.();
			pullFrom = null;
			pulled = 0;
		}}
		style:--pull="{pulled}px"
	>
		{#if pulled > 0}<p class="pull" aria-hidden="true">
				<Icon name="refresh" size={16} />{pulled >= 70
					? t('agent.pullRelease')
					: t('agent.pullDown')}
			</p>{/if}
		<button
			type="button"
			class="item"
			class:on={searching}
			aria-expanded={searching}
			onclick={() => {
				searching = !searching;
				if (!searching) query = '';
			}}
			><span class="em"><Icon name="search" size={16} /></span><span class="name"
				>{t('agent.sideSearch')}</span
			></button
		>
		{#if searching}
			<label class="search">
				<input
					type="search"
					bind:value={query}
					use:searchInput
					placeholder={t('agent.searchChats')}
					aria-label={t('agent.searchChats')}
					onkeydown={(e) => {
						if (e.key === 'Escape') {
							query = '';
							searching = false;
						}
					}}
				/>
			</label>
		{/if}
		{#if oninbox}<button type="button" class="item" onclick={oninbox}
				><span class="em"><Icon name="bulb" size={16} /></span><span class="name"
					>{t('agent.inbox.title')}</span
				>{#if inboxCount}<span class="n">{inboxCount}</span>{/if}</button
			>{/if}
		<button type="button" class="item" onclick={onplanned}
			><span class="em"><Icon name="clock" size={16} /></span><span class="name"
				>{t('agent.planned')}</span
			>{#if plannedCount}<span class="n">{plannedCount}</span>{/if}</button
		>
		{#if onnotes}<button type="button" class="item" onclick={onnotes}
				><span class="em"><Icon name="fileText" size={16} /></span><span class="name"
					>{t('agent.notes.title')}</span
				></button
			>{/if}
		{#each features as f (f.id)}<button type="button" class="item" onclick={() => onfeature?.(f.id)}
				><span class="em"><Icon name={f.icon} size={16} /></span><span class="name">{f.label}</span
				>{#if f.count}<span class="n">{f.count}</span>{/if}</button
			>{/each}
		{#if found}
			<div class="group">{t('agent.found', { count: found.length })}</div>
			{#each found as chat (chat.id)}{@render row(chat)}{:else}
				<p class="muted empty">{t('agent.searchNone')}</p>
			{/each}
		{:else}
			<div class="group">{t('agent.projects')}</div>
			{#each projects as p (p.id)}
				{@const st = stateIn(p.id)}
				<div class="project-row">
					<button
						type="button"
						class="item"
						class:on={p.id === project}
						aria-expanded={p.id === project}
						onclick={() => onproject(p.id === project ? '' : p.id)}
					>
						<span class="em" aria-hidden="true">{p.icon || '📁'}</span>
						<span class="name">{p.name}</span>
						{#if p.owner}<span class="by" title={t('agent.sharedBy', { name: p.owner })}
								>{p.owner}</span
							>{:else if p.members?.length}<span
								class="by"
								title={t('agent.sharedWith', { names: p.members.join(', ') })}
								>👥 {p.members.length}</span
							>{/if}
						{#if st === 'waiting'}<span
								class="wait-n"
								title={t('agent.sideWaiting', { count: waiting(p.id) })}
								><span aria-hidden="true">▲ {waiting(p.id)}</span><span class="sr-only"
									>{t('agent.sideWaiting', { count: waiting(p.id) })}</span
								></span
							>{:else}
							{#if st === 'running'}<span class="live" title={t('agent.running')}
									><span class="sr-only">{t('agent.running')}</span></span
								>{:else if st === 'unread' && p.id !== project}<span
									class="dot"
									title={t('agent.unread')}><span class="sr-only">{t('agent.unread')}</span></span
								>{/if}
							<span class="n">{count(p.id)}</span>
						{/if}
					</button>
					{#if !p.owner}
						<button
							type="button"
							class="edit"
							aria-label={t('agent.editProject', { name: p.name })}
							title={t('agent.editProject', { name: p.name })}
							onclick={() => oneditproject(p)}><Icon name="edit" size={13} /></button
						>
					{/if}
				</div>
				{#if p.id === project}
					{#each chats.filter((c) => c.project === p.id) as chat (chat.id)}{@render row(
							chat,
							true,
						)}{:else}
						<p class="muted empty">{t('agent.projectEmpty')}</p>
					{/each}
				{/if}
			{/each}
			<button type="button" class="item add" onclick={onnewproject}
				><span class="em"><Icon name="plus" size={15} /></span><span class="name"
					>{t('agent.newProject')}</span
				></button
			>
			{#each groupChats(loose) as group (group.key)}
				<div class="group">{t(groupLabel[group.key])}</div>
				{#each group.chats as chat (chat.id)}{@render row(chat)}{/each}
			{/each}
		{/if}
	</div>
	<div class="foot">
		<button
			type="button"
			class="item"
			class:on={settingsOn}
			aria-current={settingsOn ? 'page' : undefined}
			onclick={onsettings}
			><span class="em"><Icon name="gear" size={16} /></span><span class="name"
				>{t('agent.set.title')}</span
			></button
		>
		{#if onfold}
			<button type="button" class="item add" onclick={onfold}
				><span class="em"><Icon name="sidebar" size={16} /></span><span class="name"
					>{t('agent.sideFold')}</span
				></button
			>
		{/if}
		{#if ontitanos}
			<button type="button" class="titanos" onclick={ontitanos}
				><Icon name="chevronLeft" size={17} />{t('agent.toTitanOS')}</button
			>
		{/if}
	</div>
</nav>

<style>
	/* PLAN 68: like the side bar of every window (Window.svelte .side):
	   plain rows, the open one tinted with the accent. */
	.menu {
		display: flex;
		flex-direction: column;
		min-height: 0;
		height: 100%;
		padding: 12px 10px;
		color: var(--text);
	}
	.top {
		display: flex;
		align-items: center;
		gap: 10px;
		padding: 4px 6px 14px;
		min-width: 0;
	}
	.brand {
		font-size: var(--text-lg);
		font-weight: 600;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.new-chat {
		justify-content: center;
		gap: 6px;
		margin: 0 4px 10px;
		min-height: 36px;
	}
	.search {
		display: flex;
		margin: 2px 4px 4px;
	}
	.search input {
		flex: 1;
		min-width: 0;
		min-height: 34px;
		padding: 0 10px;
		border-radius: var(--radius-ctl);
		border: 1px solid var(--border);
		background: var(--surface);
		color: var(--text);
		font: inherit;
		font-size: var(--text-base);
	}
	.search input:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -1px;
	}
	.scroll {
		flex: 1;
		min-height: 0;
		overflow-y: auto;
		display: flex;
		flex-direction: column;
		gap: 2px;
		scrollbar-width: thin;
	}
	/* Rows keep their height: a row that hides its overflow (.swipe) may
	   otherwise shrink to nothing when there are many chats. */
	.scroll > :global(*) {
		flex-shrink: 0;
	}
	.group {
		margin: 12px 10px 4px;
		font-size: var(--text-xs);
		letter-spacing: 0.05em;
		text-transform: uppercase;
		color: var(--muted);
	}
	.item {
		display: flex;
		align-items: center;
		gap: 10px;
		width: 100%;
		min-height: 34px;
		padding: 6px 10px;
		border-radius: calc(9px * var(--radius-scale, 1));
		border: 0;
		background: none;
		color: var(--text);
		font: inherit;
		font-size: var(--text-base);
		text-align: left;
		cursor: pointer;
	}
	.item :global(svg) {
		color: var(--muted);
		flex: none;
	}
	.item:hover {
		background: color-mix(in srgb, var(--text) 7%, transparent);
	}
	.item.on {
		background: color-mix(in srgb, var(--accent) 18%, transparent);
		/* Toward the text color: 4.5:1 on the tint in every theme (as Window.svelte). */
		color: color-mix(in srgb, var(--accent) 65%, var(--text));
		font-weight: 600;
	}
	.item.on :global(svg) {
		color: var(--accent);
	}
	.item:focus-visible,
	.edit:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
	.item.unread .name {
		font-weight: 600;
	}
	.item.sub {
		padding-left: 38px;
	}
	.item.add {
		color: var(--muted);
	}
	.em {
		width: 18px;
		text-align: center;
		flex: none;
		display: grid;
		place-items: center;
		font-size: 14px;
	}
	.name {
		flex: 1;
		min-width: 0;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.by {
		flex: none;
		font-size: var(--text-xs);
		padding: 0 7px;
		border-radius: 999px;
		background: var(--chip);
		color: var(--muted);
	}
	.emoji {
		font-size: 13px;
	}
	.n {
		margin-left: auto;
		color: var(--muted);
		font-size: var(--text-xs);
		font-weight: 400;
		flex: none;
	}
	.wait-n {
		flex: none;
		font-size: var(--text-xs);
		font-weight: 600;
		color: var(--warn-text);
	}
	.live,
	.dot {
		width: 8px;
		height: 8px;
		border-radius: 50%;
		flex: none;
		background: var(--accent);
	}
	.live {
		animation: breathe 1.8s ease-in-out infinite;
	}
	:global(.agent.calm) .live {
		animation: none;
	}
	@media (prefers-reduced-motion: reduce) {
		.live {
			animation: none;
		}
	}
	@keyframes breathe {
		50% {
			opacity: 0.4;
		}
	}
	.wait {
		font-size: var(--text-xs);
		padding: 1px 8px;
		border-radius: 999px;
		background: var(--warn-bg);
		border: 1px solid var(--warn-border);
		color: var(--warn-text);
		flex: none;
	}
	.project-row {
		display: flex;
		align-items: center;
	}
	.project-row .item {
		flex: 1;
		min-width: 0;
	}
	.project-row .edit {
		border: 0;
		background: transparent;
		color: var(--muted);
		padding: 6px;
		border-radius: 8px;
		cursor: pointer;
		opacity: 0;
	}
	.project-row:hover .edit,
	.project-row .edit:focus-visible {
		opacity: 1;
	}
	.empty {
		margin: 2px 0 4px 38px;
		font-size: var(--text-sm);
	}
	.foot {
		display: flex;
		flex-direction: column;
		gap: 2px;
		padding-top: 8px;
		padding-bottom: env(safe-area-inset-bottom, 0px);
	}
	.compact .item {
		min-height: 30px;
		padding-top: 4px;
		padding-bottom: 4px;
		font-size: var(--text-sm);
	}
	.pull {
		display: flex;
		align-items: center;
		justify-content: center;
		gap: 6px;
		height: var(--pull);
		margin: 0;
		overflow: hidden;
		font-size: var(--text-xs);
		color: var(--muted);
	}
	/* Swiping a chat aside: the row follows the finger over a strip that
	   names the action (PLAN 53). */
	.swipe {
		position: relative;
		border-radius: calc(9px * var(--radius-scale, 1));
		overflow: hidden;
		touch-action: pan-y;
	}
	.swipe::before {
		content: attr(data-label);
		position: absolute;
		inset: 0;
		display: flex;
		align-items: center;
		padding: 0 16px;
		font-size: var(--text-sm);
		font-weight: 600;
		color: var(--on-accent);
		opacity: 0;
		background: var(--accent);
	}
	.swipe:global([data-side='left'])::before {
		justify-content: flex-end;
		color: var(--error-text);
		background: var(--error-bg);
	}
	.swipe:global([data-side='right'])::before,
	.swipe:global([data-side='left'])::before {
		opacity: 0.75;
	}
	.swipe:global([data-armed='true'])::before {
		opacity: 1;
	}
	.swipe > .item {
		position: relative;
		transform: translateX(var(--dx, 0px));
		transition: transform 0.25s cubic-bezier(0.2, 0.8, 0.2, 1);
	}
	.swipe:global([data-side='left']) > .item,
	.swipe:global([data-side='right']) > .item {
		background: var(--surface);
	}
	.swipe:global(.dragging) > .item {
		transition: none;
	}
	.titanos {
		display: flex;
		align-items: center;
		justify-content: center;
		gap: 8px;
		width: 100%;
		min-height: 44px;
		margin-top: 6px;
		border-radius: var(--radius-ctl);
		border: 1px solid var(--border);
		background: var(--chip);
		color: var(--text);
		font: inherit;
		font-weight: 600;
		cursor: pointer;
	}
	@media (hover: none) {
		.project-row .edit {
			opacity: 1;
		}
	}
	@media (max-width: 760px) {
		.search input {
			font-size: max(16px, 1rem);
		}
		.item {
			min-height: 42px;
			font-size: 15px;
		}
	}
</style>
