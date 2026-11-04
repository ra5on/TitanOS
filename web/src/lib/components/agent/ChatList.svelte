<script lang="ts">
	// The list on the left of the KI-Agent (desktop): new chat, search, the
	// projects with their chats, then the chats without a project by day.
	import Icon from '#lib/components/Icon.svelte';
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { groupChats, type ChatInfo, type Project } from '#lib/agent.ts';

	let {
		chats,
		projects,
		current = '',
		project = '',
		onopen,
		onnew,
		onproject,
		onnewproject,
		oneditproject,
	}: {
		chats: ChatInfo[];
		projects: Project[];
		current?: string;
		project?: string;
		onopen: (id: string) => void;
		onnew: () => void;
		onproject: (id: string) => void;
		onnewproject: () => void;
		oneditproject: (p: Project) => void;
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
</script>

<nav class="tile list" aria-label={t('agent.chats')}>
	<button type="button" class="btn btn-primary new" onclick={onnew}
		><Icon name="plus" size={16} /> {t('agent.newChat')}</button
	>
	<input
		class="field"
		type="search"
		bind:value={query}
		placeholder={t('agent.searchChats')}
		aria-label={t('agent.searchChats')}
	/>
	<div class="scroll">
		{#if found}
			<div class="group">{t('agent.found', { count: found.length })}</div>
			{#each found as chat (chat.id)}
				<button
					type="button"
					class="item"
					class:on={chat.id === current}
					onclick={() => onopen(chat.id)}
					><Icon name="chat" size={15} /><span class="name">{chat.title}</span></button
				>
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
						<span class="dot-color" style:background={p.color || 'var(--accent)'}></span>
						<span class="name">{p.icon ? p.icon + ' ' : ''}{p.name}</span>
						<span class="n">{count(p.id)}</span>
					</button>
					<button
						type="button"
						class="edit"
						aria-label={t('agent.editProject', { name: p.name })}
						title={t('agent.editProject', { name: p.name })}
						onclick={() => oneditproject(p)}><Icon name="edit" size={13} /></button
					>
				</div>
				{#if p.id === project}
					{#each chats.filter((c) => c.project === p.id) as chat (chat.id)}
						<button
							type="button"
							class="item sub"
							class:on={chat.id === current}
							onclick={() => onopen(chat.id)}
						>
							<span class="name">{chat.title}</span>
							{#if chat.state === 'running'}<span class="run" title={t('agent.running')}
									><span class="sr-only">{t('agent.running')}</span></span
								>{:else if chat.state === 'waiting'}<span class="wait" title={t('agent.waiting')}
									>!</span
								>{/if}
						</button>
					{:else}
						<p class="muted empty">{t('agent.projectEmpty')}</p>
					{/each}
				{/if}
			{/each}
			<button type="button" class="item add" onclick={onnewproject}
				><Icon name="plus" size={15} /> {t('agent.newProject')}</button
			>
			{#each groupChats(loose) as group (group.key)}
				<div class="group">{t(groupLabel[group.key])}</div>
				{#each group.chats as chat (chat.id)}
					<button
						type="button"
						class="item"
						class:on={chat.id === current}
						onclick={() => onopen(chat.id)}
					>
						<Icon
							name={chat.pinned
								? 'pin'
								: chat.origin === 'schedule'
									? 'clock'
									: chat.origin === 'event'
										? 'bell'
										: 'chat'}
							size={15}
						/>
						<span class="name">{chat.title}</span>
						{#if chat.state === 'running'}<span class="run" title={t('agent.running')}
								><span class="sr-only">{t('agent.running')}</span></span
							>{:else if chat.state === 'waiting'}<span class="wait" title={t('agent.waiting')}
								>!</span
							>{/if}
					</button>
				{/each}
			{/each}
		{/if}
	</div>
</nav>

<style>
	.list {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		padding: var(--space-3);
		min-height: 0;
		overflow: hidden;
	}
	.new {
		justify-content: flex-start;
		gap: 8px;
	}
	.scroll {
		overflow-y: auto;
		min-height: 0;
		flex: 1;
		display: flex;
		flex-direction: column;
		gap: 1px;
		margin: 0 calc(-1 * var(--space-1));
		padding: 0 var(--space-1);
	}
	.group {
		font-size: var(--text-xs);
		text-transform: uppercase;
		letter-spacing: 0.06em;
		color: var(--muted);
		margin: var(--space-3) 8px 4px;
	}
	.item {
		display: flex;
		align-items: center;
		gap: 8px;
		width: 100%;
		padding: 6px 8px;
		border: 0;
		border-radius: var(--radius-small);
		background: transparent;
		color: var(--text);
		font: inherit;
		font-size: var(--text-base);
		text-align: left;
		cursor: pointer;
		min-height: 32px;
	}
	.item:hover {
		background: color-mix(in srgb, var(--text) 5%, transparent);
	}
	.item.on {
		background: color-mix(in srgb, var(--accent) 16%, transparent);
		color: var(--accent);
	}
	.item.sub {
		padding-left: 26px;
	}
	.item.add {
		color: var(--muted);
	}
	.name {
		flex: 1;
		min-width: 0;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.n {
		color: var(--muted);
		font-size: var(--text-xs);
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
		border-radius: var(--radius-small);
		cursor: pointer;
		opacity: 0;
	}
	.project-row:hover .edit,
	.project-row .edit:focus-visible {
		opacity: 1;
	}
	.dot-color {
		width: 9px;
		height: 9px;
		border-radius: 50%;
		flex: none;
	}
	.run {
		width: 8px;
		height: 8px;
		border-radius: 50%;
		background: var(--accent);
		box-shadow: 0 0 0 3px color-mix(in srgb, var(--accent) 25%, transparent);
		flex: none;
	}
	.wait {
		font-weight: 700;
		color: var(--warn-text);
	}
	.empty {
		margin: 2px 0 4px 26px;
		font-size: var(--text-sm);
	}
	@media (hover: none) {
		.project-row .edit {
			opacity: 1;
		}
	}
</style>
