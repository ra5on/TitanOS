<script lang="ts">
	// The start of the KI-Agent on the phone (PLAN 44, Bild 7): a greeting,
	// the task that runs right now, the projects as tiles to swipe through and
	// all chats with a preview, searchable, pinned ones first and then by day
	// like on the desktop. A chat row swipes to the left for "pin", "rename",
	// "project" and "delete" (plain horizontal scrolling, so it works with
	// touch, mouse and keyboard alike).
	import Icon from '#lib/components/Icon.svelte';
	import { formatTime } from '#lib/format.ts';
	import { i18n, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { dayGroup, groupChats, type ChatInfo, type Project } from '#lib/agent.ts';

	let {
		user,
		chats,
		projects,
		onopen,
		onproject,
		onnewproject,
		onpin,
		onrename,
		onmove,
		ondelete,
		onstop,
		onmenu,
	}: {
		user: string;
		chats: ChatInfo[];
		projects: Project[];
		onopen: (id: string) => void;
		onproject: (p: Project) => void;
		onnewproject: () => void;
		onpin: (c: ChatInfo) => void;
		onrename: (c: ChatInfo) => void;
		onmove: (c: ChatInfo) => void;
		ondelete: (c: ChatInfo) => void;
		onstop: (c: ChatInfo) => void;
		/** Opens the TitanOS menu (phone, round menu layout). */
		onmenu?: () => void;
	} = $props();

	const now = new Date();
	const hello = $derived(
		now.getHours() < 11
			? t('agent.helloMorning', { name: user })
			: now.getHours() < 18
				? t('agent.helloDay', { name: user })
				: t('agent.helloEvening', { name: user }),
	);
	const today = $derived(
		now.toLocaleDateString(i18n.locale === 'de' ? 'de-DE' : 'en-GB', {
			weekday: 'long',
			day: 'numeric',
			month: 'long',
		}),
	);
	const active = $derived(chats.filter((c) => c.state === 'running' || c.state === 'waiting'));
	let query = $state('');
	const groups = $derived.by(() => {
		const q = query.trim().toLowerCase();
		const list = q
			? chats.filter((c) => (c.title + ' ' + (c.preview ?? '')).toLowerCase().includes(q))
			: chats;
		return groupChats(list);
	});
	const groupLabel: Record<string, MessageKey> = {
		pinned: 'agent.groupPinned',
		today: 'agent.groupToday',
		yesterday: 'agent.groupYesterday',
		week: 'agent.groupWeek',
		older: 'agent.groupOlder',
	};
	// After an action the row slides back.
	function act(event: Event, run: () => void) {
		(event.currentTarget as HTMLElement).closest('.swipe')?.scrollTo({ left: 0 });
		run();
	}
	function when(iso: string): string {
		const g = dayGroup(iso);
		if (g === 'today') return formatTime(new Date(iso));
		if (g === 'yesterday') return t('agent.groupYesterday');
		return new Date(iso).toLocaleDateString(i18n.locale === 'de' ? 'de-DE' : 'en-GB', {
			day: 'numeric',
			month: 'numeric',
		});
	}
	const project = (id?: string) => projects.find((p) => p.id === id);
</script>

<div class="home">
	<header>
		{#if onmenu}
			<button
				type="button"
				class="menu"
				aria-label={t('nav.openMenu', { page: t('nav.agent') })}
				aria-haspopup="dialog"
				onclick={onmenu}><Icon name="menu" size={20} /></button
			>
		{/if}
		<span>
			<small>{today}</small>
			<h1>{hello}</h1>
		</span>
	</header>

	{#each active as chat (chat.id)}
		<div class="live" class:waiting={chat.state === 'waiting'}>
			<button type="button" class="open" onclick={() => onopen(chat.id)}>
				<span class="orb" aria-hidden="true"><i></i></span>
				<span class="text">
					<strong>{chat.title}</strong>
					<span>{chat.state === 'waiting' ? t('agent.waitingForYou') : t('agent.working')}</span>
					{#if chat.state === 'running'}<span class="bar" aria-hidden="true"><i></i></span>{/if}
				</span>
			</button>
			{#if chat.state === 'running'}
				<button type="button" class="stop" aria-label={t('agent.stop')} onclick={() => onstop(chat)}
					><Icon name="stop" size={12} /></button
				>
			{/if}
		</div>
	{/each}

	<div class="h3">{t('agent.projects')}</div>
	<div class="tiles">
		{#each projects as p (p.id)}
			<button
				type="button"
				class="ptile"
				style:--tile={p.color || 'var(--accent)'}
				onclick={() => onproject(p)}
			>
				<span class="icon" aria-hidden="true">{p.icon || '📁'}</span>
				<span>
					<strong>{p.name}</strong>
					<small
						>{t('agent.chatCount', {
							count: chats.filter((c) => c.project === p.id).length,
						})}</small
					>
				</span>
			</button>
		{/each}
		<button type="button" class="ptile add" onclick={onnewproject}
			><Icon name="plus" size={20} /><span>{t('agent.newProject')}</span></button
		>
	</div>

	{#if chats.length === 0}
		<div class="h3">{t('agent.recent')}</div>
		<p class="muted">{t('agent.noChats')}</p>
	{:else}
		<label class="search">
			<Icon name="search" size={16} />
			<input
				type="search"
				bind:value={query}
				placeholder={t('agent.searchChats')}
				aria-label={t('agent.searchChats')}
			/>
		</label>
		{#each groups as group (group.key)}
			<div class="h3">{t(groupLabel[group.key])}</div>
			<ul class="list">
				{#each group.chats as chat (chat.id)}
					<li class="swipe">
						<button type="button" class="row" onclick={() => onopen(chat.id)}>
							<span class="pd" style:background={project(chat.project)?.color || 'var(--muted)'}
							></span>
							<span class="t">
								<strong
									>{#if chat.pinned}<span class="pinned"><Icon name="pin" size={14} /></span
										>{/if}{chat.title || t('agent.newChat')}</strong
								>
								<span>{chat.state === 'running' ? t('agent.working') : (chat.preview ?? '')}</span>
							</span>
							<span class="when">{when(chat.updated)}</span>
						</button>
						<span class="acts">
							<button type="button" class="pin" onclick={(e) => act(e, () => onpin(chat))}
								><Icon name="pin" size={16} />{chat.pinned
									? t('agent.unpin')
									: t('agent.pin')}</button
							>
							<button type="button" class="ren" onclick={(e) => act(e, () => onrename(chat))}
								><Icon name="edit" size={16} />{t('agent.rename')}</button
							>
							<button type="button" class="move" onclick={(e) => act(e, () => onmove(chat))}
								><Icon name="folder" size={16} />{t('agent.project')}</button
							>
							<button type="button" class="del" onclick={(e) => act(e, () => ondelete(chat))}
								><Icon name="trash" size={16} />{t('agent.delete')}</button
							>
						</span>
					</li>
				{/each}
			</ul>
		{:else}
			<p class="muted">{t('agent.searchNone')}</p>
		{/each}
	{/if}
</div>

<style>
	.home {
		display: flex;
		flex-direction: column;
		gap: 14px;
		padding: 4px 0 var(--space-4);
	}
	header {
		display: flex;
		align-items: center;
		gap: 12px;
	}
	header > span {
		display: flex;
		flex-direction: column;
		min-width: 0;
	}
	.menu {
		width: 40px;
		height: 40px;
		flex: none;
		border-radius: 50%;
		display: grid;
		place-items: center;
		border: 0;
		background: color-mix(in srgb, var(--text) 6%, transparent);
		color: var(--text);
		cursor: pointer;
	}
	.search {
		display: flex;
		align-items: center;
		gap: 8px;
		padding: 0 12px;
		min-height: 44px;
		border: 1px solid var(--border);
		border-radius: 14px;
		background: var(--surface);
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
		/* 16px: no zoom on iOS. */
		font-size: max(16px, 1rem);
	}
	.pinned {
		color: var(--accent);
		display: inline-grid;
		vertical-align: -2px;
		margin-right: 4px;
	}
	header small {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	h1 {
		margin: 0;
		font-size: 22px;
		letter-spacing: -0.01em;
	}
	.h3 {
		font-size: var(--text-sm);
		text-transform: uppercase;
		letter-spacing: 0.07em;
		color: var(--muted);
		margin-bottom: -6px;
	}
	.live {
		display: flex;
		align-items: center;
		gap: 10px;
		border-radius: 18px;
		padding: 12px 14px;
		background: linear-gradient(
			135deg,
			color-mix(in srgb, var(--accent) 22%, var(--surface)),
			var(--surface)
		);
		border: 1px solid color-mix(in srgb, var(--accent) 40%, transparent);
	}
	:global(.agent.glow) .live {
		box-shadow: 0 10px 30px -12px color-mix(in srgb, var(--accent) 70%, transparent);
	}
	.live.waiting {
		border-color: var(--warn-border);
		background: var(--warn-bg);
	}
	.live .open {
		flex: 1;
		display: flex;
		gap: 12px;
		align-items: center;
		background: none;
		border: 0;
		color: inherit;
		font: inherit;
		text-align: left;
		min-width: 0;
		cursor: pointer;
		padding: 0;
	}
	.orb {
		width: 34px;
		height: 34px;
		border-radius: 50%;
		flex: none;
		background: conic-gradient(
			from 200deg,
			var(--accent),
			var(--ag-accent-2, var(--accent)),
			var(--accent)
		);
		display: grid;
		place-items: center;
	}
	:global(.agent.glow) .orb {
		box-shadow: 0 0 18px color-mix(in srgb, var(--accent) 70%, transparent);
		animation: pulse 2.4s ease-in-out infinite;
	}
	:global(.agent.calm) .orb {
		animation: none;
	}
	@keyframes pulse {
		50% {
			box-shadow: 0 0 6px color-mix(in srgb, var(--accent) 40%, transparent);
		}
	}
	.orb i {
		width: 26px;
		height: 26px;
		border-radius: 50%;
		background: var(--surface);
	}
	.text {
		display: flex;
		flex-direction: column;
		min-width: 0;
		flex: 1;
	}
	.text strong {
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.text > span {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.bar {
		display: block;
		height: 4px;
		border-radius: 4px;
		margin-top: 7px;
		background: color-mix(in srgb, var(--text) 10%, transparent);
		overflow: hidden;
	}
	.bar i {
		display: block;
		height: 100%;
		width: 35%;
		border-radius: 4px;
		background: var(--ag-gradient, var(--accent));
		animation: slide 1.6s ease-in-out infinite;
	}
	@keyframes slide {
		from {
			transform: translateX(-100%);
		}
		to {
			transform: translateX(300%);
		}
	}
	:global(.agent.calm) .bar i {
		animation: none;
	}
	.stop {
		width: 32px;
		height: 32px;
		border-radius: 50%;
		border: 0;
		display: grid;
		place-items: center;
		background: color-mix(in srgb, var(--text) 12%, transparent);
		color: var(--text);
	}
	.tiles {
		display: flex;
		gap: 10px;
		overflow-x: auto;
		padding: 2px 0 4px;
		scrollbar-width: none;
	}
	.ptile {
		flex: none;
		width: 136px;
		height: 112px;
		border-radius: 18px;
		padding: 12px;
		display: flex;
		flex-direction: column;
		justify-content: space-between;
		align-items: flex-start;
		text-align: left;
		border: 0;
		font: inherit;
		cursor: pointer;
		color: var(--on-accent);
		background: linear-gradient(
			140deg,
			var(--tile),
			color-mix(in srgb, var(--tile) 55%, var(--surface))
		);
	}
	.ptile .icon {
		font-size: 22px;
	}
	.ptile strong {
		display: block;
		line-height: 1.2;
	}
	.ptile small {
		opacity: 0.85;
		font-size: var(--text-xs);
	}
	.ptile.add {
		background: transparent;
		border: 1.5px dashed var(--border);
		color: var(--muted);
		align-items: center;
		justify-content: center;
		gap: 6px;
	}
	.list {
		list-style: none;
		margin: 0;
		padding: 0;
		border-radius: 16px;
		overflow: hidden;
		border: 1px solid var(--border);
	}
	.swipe {
		display: flex;
		overflow-x: auto;
		scroll-snap-type: x mandatory;
		scrollbar-width: none;
		border-bottom: 1px solid var(--border);
	}
	.swipe:last-child {
		border-bottom: 0;
	}
	.row {
		flex: none;
		width: 100%;
		scroll-snap-align: start;
		display: flex;
		gap: 11px;
		align-items: center;
		padding: 10px 12px;
		background: var(--surface);
		border: 0;
		color: var(--text);
		font: inherit;
		text-align: left;
		cursor: pointer;
		min-height: 56px;
	}
	.pd {
		width: 8px;
		height: 8px;
		border-radius: 50%;
		flex: none;
	}
	.t {
		flex: 1;
		min-width: 0;
		display: flex;
		flex-direction: column;
	}
	.t strong {
		font-weight: 500;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.t span {
		color: var(--muted);
		font-size: var(--text-sm);
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.when {
		color: var(--muted);
		font-size: var(--text-xs);
		align-self: flex-start;
		margin-top: 2px;
	}
	.acts {
		display: flex;
		flex: none;
		scroll-snap-align: end;
	}
	.acts button {
		width: 64px;
		border: 0;
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: 3px;
		font: inherit;
		font-size: var(--text-xs);
		color: var(--on-accent);
		cursor: pointer;
	}
	.acts .pin {
		background: var(--accent);
	}
	.acts .ren {
		background: color-mix(in srgb, var(--accent) 30%, var(--surface));
		color: var(--text);
	}
	.acts .move {
		background: color-mix(in srgb, var(--accent) 55%, var(--surface));
		color: var(--text);
	}
	.acts .del {
		background: var(--status-error);
		color: var(--on-warn);
	}
</style>
