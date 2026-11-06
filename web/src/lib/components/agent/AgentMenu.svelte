<script lang="ts">
	import { swipe } from '#lib/agent-swipe.ts';
	// The menu of the KI-Agent (PLAN 48), the same on the desktop (left of
	// the chat, beside the TitanOS menu) and on the phone (slides in from the
	// left): search, new chat, the projects with their chats, then the chats
	// by day. Running chats glow, waiting ones say so, unread ones carry a
	// dot. At the bottom knowledge, look and settings, and on the phone or in
	// the installed app the way back to TitanOS.
	import Icon from '#lib/components/Icon.svelte';
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { groupChats, type ChatInfo, type Project, type Swipe } from '#lib/agent.ts';

	let {
		chats,
		projects,
		current = '',
		project = '',
		host = '',
		admin = false,
		onopen,
		onnew,
		onproject,
		onnewproject,
		oneditproject,
		onknowledge,
		onplanned,
		onnotes,
		onpaper,
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
		onlook,
		ontitanos,
	}: {
		chats: ChatInfo[];
		projects: Project[];
		current?: string;
		/** The open project (its chats show below it). */
		project?: string;
		host?: string;
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
		/** Opens "Papierkram" when the function is on (PLAN 54). */
		onpaper?: () => void;
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
		onlook: () => void;
		/** Back to TitanOS (phone, installed app). */
		ontitanos?: () => void;
	} = $props();

	let query = $state('');
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
		<span class="logo" aria-hidden="true"><Icon name="sparkle" size={18} /></span>
		<span class="brand"
			><strong>{t('agent.appName')}</strong>{#if host}<small>{host}</small>{/if}</span
		>
	</div>
	<label class="search">
		<Icon name="search" size={16} />
		<input
			type="search"
			bind:value={query}
			placeholder={t('agent.searchChats')}
			aria-label={t('agent.searchChats')}
		/>
	</label>
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
		<button type="button" class="item new" onclick={onnew}
			><span class="em"><Icon name="newChat" size={17} /></span><span class="name"
				>{t('agent.newChat')}</span
			></button
		>
		{#if oninbox}<button type="button" class="item" onclick={oninbox}
				><span class="em"><Icon name="bulb" size={17} /></span><span class="name"
					>{t('agent.inbox.title')}</span
				>{#if inboxCount}<span class="count">{inboxCount}</span>{/if}</button
			>{/if}
		<button type="button" class="item" onclick={onplanned}
			><span class="em"><Icon name="clock" size={17} /></span><span class="name"
				>{t('agent.planned')}</span
			></button
		>
		{#if onnotes}<button type="button" class="item" onclick={onnotes}
				><span class="em"><Icon name="fileText" size={17} /></span><span class="name"
					>{t('agent.notes.title')}</span
				></button
			>{/if}
		{#if onpaper}<button type="button" class="item" onclick={onpaper}
				><span class="em"><Icon name="list" size={17} /></span><span class="name"
					>{t('agent.f.paper')}</span
				></button
			>{/if}
		{#if found}
			<div class="group">{t('agent.found', { count: found.length })}</div>
			{#each found as chat (chat.id)}{@render row(chat)}{:else}
				<p class="muted empty">{t('agent.searchNone')}</p>
			{/each}
		{:else}
			<div class="group">{t('agent.projects')}</div>
			{#each projects as p (p.id)}
				<div class="project-row">
					<button
						type="button"
						class="item"
						class:on={p.id === project && !current}
						aria-expanded={p.id === project}
						onclick={() => onproject(p.id === project ? '' : p.id)}
					>
						<span class="em" aria-hidden="true" style:--tile={p.color || 'var(--accent)'}
							>{p.icon || '📁'}</span
						>
						<span class="name">{p.name}</span>
						{#if p.owner}<span class="by" title={t('agent.sharedBy', { name: p.owner })}
								>{p.owner}</span
							>{:else if p.members?.length}<span
								class="by"
								title={t('agent.sharedWith', { names: p.members.join(', ') })}
								>👥 {p.members.length}</span
							>{/if}
						{#if p.id !== project}
							{@const st = stateIn(p.id)}
							{#if st === 'waiting'}<span class="wait">{t('agent.approvalShort')}</span
								>{:else if st === 'running'}<span class="live" title={t('agent.running')}
									><span class="sr-only">{t('agent.running')}</span></span
								>{:else if st === 'unread'}<span class="dot" title={t('agent.unread')}
									><span class="sr-only">{t('agent.unread')}</span></span
								>{/if}
						{/if}
						<span class="n">{count(p.id)}</span>
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
		<div class="tools">
			<button type="button" class="wide" onclick={onsettings}
				><Icon name="gear" size={19} /><span>{t('agent.set.title')}</span></button
			>
		</div>
		{#if ontitanos}
			<button type="button" class="titanos" onclick={ontitanos}
				><Icon name="chevronLeft" size={17} />{t('agent.toTitanOS')}</button
			>
		{/if}
	</div>
</nav>

<style>
	.menu {
		display: flex;
		flex-direction: column;
		min-height: 0;
		height: 100%;
		color: var(--text);
	}
	.top {
		display: flex;
		align-items: center;
		gap: 10px;
		padding: 16px 14px 8px;
	}
	.logo {
		width: 34px;
		height: 34px;
		border-radius: 11px;
		flex: none;
		display: grid;
		place-items: center;
		color: #fff;
		background: var(--ag-orb, var(--accent));
		box-shadow: 0 0 20px -2px color-mix(in srgb, var(--accent) 70%, transparent);
	}
	.brand {
		display: flex;
		flex-direction: column;
		min-width: 0;
	}
	.brand strong {
		font-size: 16px;
	}
	.brand small {
		color: var(--muted);
		font-size: var(--text-xs);
	}
	.search {
		margin: 4px 12px 8px;
		display: flex;
		align-items: center;
		gap: 8px;
		padding: 0 12px;
		min-height: 40px;
		border-radius: 12px;
		background: color-mix(in srgb, var(--text) 7%, transparent);
		color: var(--muted);
	}
	.search input {
		flex: 1;
		min-width: 0;
		border: 0;
		outline: none;
		background: transparent;
		color: var(--text);
		font: inherit;
		font-size: var(--text-base);
	}
	.scroll {
		flex: 1;
		min-height: 0;
		overflow-y: auto;
		padding: 0 8px 10px;
		display: flex;
		flex-direction: column;
		gap: 1px;
		scrollbar-width: thin;
	}
	/* Rows keep their height: a row that hides its overflow (.swipe) may
	   otherwise shrink to nothing when there are many chats. */
	.scroll > :global(*) {
		flex-shrink: 0;
	}
	.group {
		padding: 14px 10px 4px;
		font-size: var(--text-xs);
		letter-spacing: 0.07em;
		text-transform: uppercase;
		color: var(--muted);
	}
	.item {
		display: flex;
		align-items: center;
		gap: 10px;
		width: 100%;
		min-height: 36px;
		padding: 7px 10px;
		border-radius: 10px;
		border: 0;
		background: none;
		color: var(--text);
		font: inherit;
		font-size: var(--text-base);
		text-align: left;
		cursor: pointer;
		transition: background 0.15s ease;
	}
	.item :global(svg) {
		color: var(--muted);
		flex: none;
	}
	.item:hover {
		background: color-mix(in srgb, var(--text) 6%, transparent);
	}
	.item.on {
		background: linear-gradient(
			90deg,
			color-mix(in srgb, var(--accent) 28%, transparent),
			color-mix(in srgb, var(--accent) 4%, transparent)
		);
		box-shadow: inset 3px 0 0 var(--accent);
	}
	.item.unread .name {
		font-weight: 600;
	}
	.item.new {
		font-weight: 600;
	}
	.item.new :global(svg) {
		color: var(--text);
	}
	.item.sub {
		padding-left: 44px;
	}
	.item.add {
		color: var(--muted);
	}
	.em {
		width: 22px;
		text-align: center;
		flex: none;
		display: grid;
		place-items: center;
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
		background: color-mix(in srgb, var(--text) 8%, transparent);
		color: var(--muted);
	}
	.emoji {
		font-size: 13px;
	}
	.n {
		color: var(--muted);
		font-size: var(--text-xs);
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
		box-shadow: 0 0 10px var(--accent);
		animation: breathe 1.8s ease-in-out infinite;
	}
	:global(.agent.calm) .live {
		animation: none;
	}
	@keyframes breathe {
		50% {
			opacity: 0.45;
			box-shadow: 0 0 2px var(--accent);
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
		margin: 2px 0 4px 44px;
		font-size: var(--text-sm);
	}
	.foot {
		border-top: 1px solid color-mix(in srgb, var(--border) 70%, transparent);
		padding: 6px 8px calc(8px + env(safe-area-inset-bottom, 0px));
	}
	.tools {
		display: flex;
		gap: 4px;
	}
	.tools > * {
		flex: 1;
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 4px;
		padding: 8px 4px;
		border-radius: 12px;
		border: 0;
		background: none;
		color: var(--text);
		font: inherit;
		font-size: var(--text-xs);
		text-decoration: none;
		cursor: pointer;
	}
	.count {
		margin-left: auto;
		min-width: 22px;
		padding: 1px 7px;
		border-radius: 999px;
		font-size: var(--text-xs);
		font-weight: 700;
		text-align: center;
		color: var(--on-accent, #fff);
		background: var(--ag-gradient, var(--accent));
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
	/* Swiping a chat aside: the row follows the finger over a colored
	   strip that names the action (PLAN 53). */
	.swipe {
		position: relative;
		border-radius: 12px;
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
		color: #fff;
		opacity: 0;
		background: linear-gradient(90deg, #3b82f6, #6366f1);
	}
	.swipe:global([data-side='left'])::before {
		justify-content: flex-end;
		background: linear-gradient(90deg, #e11d48, #fb7185);
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
	.tools > .wide {
		flex-direction: row;
		justify-content: center;
		gap: 8px;
		min-height: 44px;
		font-size: var(--text-sm);
		border: 1px solid color-mix(in srgb, var(--text) 10%, transparent);
	}
	.tools > *:hover {
		background: color-mix(in srgb, var(--text) 6%, transparent);
	}
	.titanos {
		display: flex;
		align-items: center;
		justify-content: center;
		gap: 8px;
		width: 100%;
		min-height: 44px;
		margin-top: 6px;
		border-radius: 12px;
		border: 1px solid var(--border);
		background: color-mix(in srgb, var(--text) 6%, transparent);
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
