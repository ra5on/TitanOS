<script lang="ts">
	// The menu of the KI-Agent (PLAN 48), the same on the desktop (left of
	// the chat, beside the TitanOS menu) and on the phone (slides in from the
	// left): search, new chat, the projects with their chats, then the chats
	// by day. Running chats glow, waiting ones say so, unread ones carry a
	// dot. At the bottom knowledge, look and settings, and on the phone or in
	// the installed app the way back to TitanOS.
	import Icon from '#lib/components/Icon.svelte';
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { groupChats, type ChatInfo, type Project } from '#lib/agent.ts';

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
</script>

{#snippet row(chat: ChatInfo, sub = false)}
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
{/snippet}

<nav class="menu" aria-label={t('agent.menu')}>
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
	<div class="scroll">
		<button type="button" class="item new" onclick={onnew}
			><span class="em"><Icon name="newChat" size={17} /></span><span class="name"
				>{t('agent.newChat')}</span
			></button
		>
		<button type="button" class="item" onclick={onplanned}
			><span class="em"><Icon name="clock" size={17} /></span><span class="name"
				>{t('agent.planned')}</span
			></button
		>
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
			<button type="button" onclick={onknowledge}
				><Icon name="bulb" size={19} /><span>{t('agent.knowledgeShort')}</span></button
			>
			<button type="button" onclick={onlook}
				><Icon name="palette" size={19} /><span>{t('agent.look')}</span></button
			>
			{#if admin}
				<a href="/settings/agent"><Icon name="gear" size={19} /><span>{t('nav.settings')}</span></a>
			{/if}
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
