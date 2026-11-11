<script lang="ts">
	// The KI-Agent (PLAN 44; look of PLAN 68, the design language of every
	// window): its side bar on the left, the chat on the right with title,
	// project and model at the top. Colors come from the TitanOS theme. On
	// the phone the agent is an app of its own whose side bar slides in from
	// the left, with the way back to TitanOS. Installed from here it is an
	// app of its own too (/agent.webmanifest, start /agent?app=1).
	import { fade, fly } from 'svelte/transition';
	import Dropdown from '#lib/components/Dropdown.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import NasPicker from '#lib/components/agent/NasPicker.svelte';
	import AgentMenu from '#lib/components/agent/AgentMenu.svelte';
	import AgentStart from '#lib/components/agent/AgentStart.svelte';
	import ModelPicker from '#lib/components/agent/ModelPicker.svelte';
	import ChatView from '#lib/components/agent/ChatView.svelte';
	import Composer from '#lib/components/agent/Composer.svelte';
	import NotesPanel from '#lib/components/agent/NotesPanel.svelte';
	import KnowledgeDialog from '#lib/components/agent/KnowledgeDialog.svelte';
	import AgentSettings from '#lib/components/agent/AgentSettings.svelte';
	import InboxPanel from '#lib/components/agent/InboxPanel.svelte';
	import ProjectDialog from '#lib/components/agent/ProjectDialog.svelte';
	import ProjectSheet from '#lib/components/agent/ProjectSheet.svelte';
	import ChatSheets from './ChatSheets.svelte';
	import { isChatSheet } from './chat-sheets.ts';
	import { agentFull, setAgentFull } from '#lib/agent-full.svelte.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import FeatureView from '#lib/components/agent/features/FeatureView.svelte';
	import FeatureHost from '#lib/components/agent/features/FeatureHost.svelte';
	import { inWindow } from '#lib/desktop/frame.ts';
	import { tokens, type ChatInfo } from '#lib/agent.ts';
	import { AgentPage } from './agent-page.svelte.ts';

	// State, requests and effects live in agent-chats.svelte.ts and agent-page.svelte.ts.
	const ag = new AgentPage();
	const currentInfo = $derived(ag.currentInfo);

	// PLAN 67-1: the menu bar of the desktop – the same actions as the agent's own menu.
	import { registerMenuCommands } from '#lib/menu-commands.svelte.ts';
	const agentOn = () => !!ag.status?.enabled && !!ag.status.allowed;
	registerMenuCommands({
		'agent.newChat': { run: () => ag.newChat(''), enabled: agentOn },
		'agent.project': {
			run: () => (ag.dialog = { kind: 'project', project: null }),
			enabled: agentOn,
		},
		'agent.notes': { run: () => (ag.dialog = { kind: 'notes' }), enabled: agentOn },
		'agent.knowledge': {
			run: () => (ag.dialog = { kind: 'knowledge', tab: 'memory' }),
			enabled: agentOn,
		},
		'agent.inbox': { run: () => (ag.dialog = { kind: 'inbox' }), enabled: agentOn },
		'agent.projects': {
			items: () =>
				ag.projects.map((p) => ({ id: p.id, text: p.name, checked: ag.project === p.id })),
			run: (id) => {
				if (id && ag.projects.some((p) => p.id === id)) ag.newChat(id);
			},
		},
		'agent.model': {
			items: () =>
				(ag.status?.models ?? []).map((m) => ({
					id: m.name,
					text: m.name,
					checked: ag.model === m.name,
				})),
			run: (id) => {
				if (id && (ag.status?.models ?? []).some((m) => m.name === id)) ag.model = id;
			},
		},
		'agent.settings': { run: () => (ag.dialog = { kind: 'settings' }), enabled: agentOn },
	});
</script>

<svelte:window ontouchstart={ag.touchStart} ontouchmove={ag.touchMove} />

{#snippet chatMenu(info: ChatInfo | null)}
	<span class="more">
		<Dropdown label={t('ui.more')} icon="more" align="right">
			{#snippet children(close)}
				{#if info}
					{#if ag.phone}
						<button
							type="button"
							role="menuitem"
							onclick={() => {
								close();
								void ag.patch(info, { pinned: !info.pinned });
							}}
							><Icon name="pin" size={16} /><span
								>{info.pinned ? t('agent.unpin') : t('agent.pin')}</span
							></button
						>
					{/if}
					<button
						type="button"
						role="menuitem"
						onclick={() => {
							close();
							ag.dialog = { kind: 'rename', chat: info, title: info.title };
						}}><Icon name="edit" size={16} /><span>{t('agent.rename')}</span></button
					>
					<button
						type="button"
						role="menuitem"
						onclick={() => {
							close();
							ag.dialog = { kind: 'move', chat: info };
						}}><Icon name="folder" size={16} /><span>{t('agent.moveToProject')}</span></button
					>
					<button
						type="button"
						role="menuitem"
						onclick={() => {
							close();
							void ag.patch(info, { archived: !info.archived });
						}}
						><Icon name="archive" size={16} /><span
							>{info.archived ? t('agent.archive.restore') : t('agent.archive.do')}</span
						></button
					>
					<hr />
					<a role="menuitem" href={`/api/v1/agent/chats/${info.id}/export`} download onclick={close}
						><Icon name="download" size={16} /><span>{t('agent.exportMd')}</span></a
					>
					<a
						role="menuitem"
						href={`/api/v1/agent/chats/${info.id}/export?format=pdf`}
						download
						onclick={close}><Icon name="fileText" size={16} /><span>{t('agent.exportPdf')}</span></a
					>
					<hr />
				{/if}
				<button
					type="button"
					role="menuitem"
					onclick={() => {
						close();
						ag.dialog = { kind: 'settings' };
					}}><Icon name="gear" size={16} /><span>{t('agent.set.title')}</span></button
				>
				{#if info}
					<hr />
					<button
						type="button"
						role="menuitem"
						class="danger"
						disabled={ag.busy}
						onclick={() => {
							close();
							ag.dialog = { kind: 'delete', chat: info };
						}}><Icon name="trash" size={16} /><span>{t('agent.delete')}</span></button
					>
				{/if}
			{/snippet}
		</Dropdown>
	</span>
{/snippet}

<div
	class="agent"
	class:app={ag.app}
	class:phone={ag.phone}
	class:keyboard={ag.keyboard}
	class:calm={ag.calm}
	class:framed={inWindow}
	data-size={ag.look.fontSize}
>
	{#if ag.error}<div class="notice-error" role="alert">{ag.error}</div>{/if}

	{#if !ag.status}
		<p class="muted pad">{t('agent.loading')}</p>
	{:else if !ag.status.enabled}
		<div class="tile ui-card empty-state">
			<Icon name="sparkle" size={32} />
			<h1>{t('agent.offTitle')}</h1>
			<p class="muted">{ag.status.admin ? t('agent.offAdmin') : t('agent.offUser')}</p>
			{#if ag.status.admin}<a class="btn btn-primary" href="/settings/agent"
					>{t('agent.toSettings')}</a
				>{/if}
			{#if ag.phone || ag.installed}<button type="button" class="btn" onclick={ag.toTitanOS}
					>{t('agent.toTitanOS')}</button
				>{/if}
		</div>
	{:else if !ag.status.allowed}
		<div class="tile ui-card empty-state">
			<h1>{t('agent.notAllowedTitle')}</h1>
			<p class="muted">{t('agent.notAllowed')}</p>
			{#if ag.phone || ag.installed}<button type="button" class="btn" onclick={ag.toTitanOS}
					>{t('agent.toTitanOS')}</button
				>{/if}
		</div>
	{:else if !ag.status.volume}
		<div class="tile ui-card empty-state">
			<h1>{t('agent.noVolumeTitle')}</h1>
			<p class="muted">{t('agent.noVolume')}</p>
			{#if ag.phone || ag.installed}<button type="button" class="btn" onclick={ag.toTitanOS}
					>{t('agent.toTitanOS')}</button
				>{/if}
		</div>
	{:else}
		<div class="layout" class:folded={ag.folded && !ag.phone}>
			{#if !ag.phone && !ag.folded}
				<aside class="side"><AgentMenu {...ag.menuProps} /></aside>
			{/if}
			<section class="main" aria-label={ag.current?.title ?? t('agent.newChat')}>
				{#if ag.dialog?.kind === 'feature' && !ag.phone}
					<!-- PLAN 72: a function's view as a page here, the side bar stays. -->
					<FeatureHost id={ag.dialog.id} calm={ag.calm} onclose={() => (ag.dialog = null)} />
				{/if}
				{#if ag.phone}
					<header class="top">
						<button
							type="button"
							class="icon-btn"
							aria-label={t('agent.menuOpen')}
							aria-haspopup="dialog"
							aria-expanded={ag.menuOpen}
							onclick={() => (ag.menuOpen = true)}
							><Icon
								name="menu"
								size={20}
							/>{#if ag.chats.some((c) => c.unread || c.state === 'waiting')}<span
									class="badge"
									aria-hidden="true"
								></span>{/if}</button
						>
						<strong class="app-name">{t('nav.agent')}</strong>
						<span class="fill"></span>
						<button
							type="button"
							class="icon-btn"
							aria-label={t('agent.newChat')}
							title={t('agent.newChat')}
							onclick={() => ag.newChat('')}><Icon name="plus" size={20} /></button
						>
						{@render chatMenu(currentInfo ?? null)}
					</header>
					<div class="chat-head">
						{#if ag.current}<h1>{ag.current.title}</h1>{/if}
						{#if ag.current && ag.currentProject}<span class="chip proj"
								>{ag.currentProject.icon ?? ''} {ag.currentProject.name}</span
							>{/if}
						<ModelPicker
							models={ag.status.models}
							bind:model={ag.model}
							bind:think={ag.think}
							small
							calm={ag.calm}
						/>
					</div>
				{:else}
					<header class="top">
						{#if ag.folded}
							<button
								type="button"
								class="tool-btn"
								aria-label={t('agent.sideUnfold')}
								title={t('agent.sideUnfold')}
								onclick={() => ag.setFolded(false)}><Icon name="sidebar" size={17} /></button
							>
						{/if}
						<h1>{ag.current?.title || t('agent.newChat')}</h1>
						{#if ag.current && ag.currentProject}<span class="chip proj"
								>{ag.currentProject.icon ?? ''} {ag.currentProject.name}</span
							>{/if}
						<span class="fill"></span>
						<ModelPicker
							models={ag.status.models}
							bind:model={ag.model}
							bind:think={ag.think}
							small
							calm={ag.calm}
						/>
						{#if currentInfo}
							<button
								type="button"
								class="tool-btn"
								class:on={currentInfo.pinned}
								aria-label={currentInfo.pinned ? t('agent.unpin') : t('agent.pin')}
								aria-pressed={currentInfo.pinned}
								title={currentInfo.pinned ? t('agent.unpin') : t('agent.pin')}
								onclick={() => ag.patch(currentInfo, { pinned: !currentInfo.pinned })}
								><Icon name="pin" size={16} /></button
							>
						{/if}
						{#if !ag.installed && !inWindow}
							<button
								type="button"
								class="tool-btn"
								aria-label={agentFull.on ? t('agent.withMenu') : t('agent.fullScreen')}
								title={agentFull.on ? t('agent.withMenu') : t('agent.fullScreen')}
								onclick={() => setAgentFull(!agentFull.on)}
								><Icon name={agentFull.on ? 'sidebar' : 'maximize'} size={16} /></button
							>
						{/if}
						{@render chatMenu(currentInfo ?? null)}
					</header>
				{/if}
				{#if ag.status.reachable === false}
					<p class="notice-warn">{t('agent.ollamaDownLong', { error: ag.status.error ?? '' })}</p>
				{:else if ag.usable.length === 0}
					<p class="notice-warn">{t('agent.noToolModel')}</p>
				{:else if ag.contextNow && ag.contextNow < 16384}
					<p class="notice-warn">{t('agent.contextSmall', { tokens: tokens(ag.contextNow) })}</p>
				{/if}
				{#if ag.current}
					<ChatView
						showSteps={ag.look.steps}
						readonly={!!ag.readOnlyBy}
						onplanned={() => (ag.dialog = { kind: 'knowledge', tab: 'schedules' })}
						chat={ag.current}
						live={ag.look.typing ? ag.live : { ...ag.live, content: '' }}
						busy={ag.busy}
						deciding={ag.deciding}
						ondecide={ag.decide}
						onchoice={(c) => ag.send(c)}
						onretry={() => ag.send(t('agent.continueMessage'))}
						onundo={ag.undo}
					/>
				{:else}
					<AgentStart
						onshuffle={() => (ag.shuffle += 1)}
						show={ag.look.suggestions}
						user={ag.user}
						chats={ag.listedChats}
						projects={ag.projects}
						project={ag.project}
						suggestions={ag.suggestions}
						onopen={(id) => ag.open(id)}
						onproject={(p) => (ag.dialog = { kind: 'sheet', project: p })}
						onnewproject={() => (ag.dialog = { kind: 'project', project: null })}
						onstop={(c) => ag.stop(c.id)}
						onfill={(text) => ag.composer?.fill(text)}
					/>
				{/if}
				<div class="dock">
					<Composer
						micHold={ag.look.mic === 'hold'}
						bind:this={ag.composer}
						busy={ag.busy}
						bind:attachments={ag.attachments}
						disabled={!!ag.current?.pending || !!ag.readOnlyBy}
						voice={!!ag.status.voice}
						placeholder={ag.readOnlyBy
							? t('agent.readOnlyChat', { name: ag.readOnlyBy })
							: ag.current?.pending
								? t('agent.inputPending')
								: t('agent.inputShort')}
						onsend={ag.send}
						onstop={() => ag.stop()}
						onpicknas={() => (ag.dialog = { kind: 'nas' })}
						onerror={(m) => (ag.error = m)}
					/>
					<p class="where">{ag.whereText}</p>
				</div>
			</section>
		</div>
		{#if ag.phone && ag.menuOpen}
			<button
				type="button"
				class="scrim"
				aria-label={t('common.close')}
				transition:fade={{ duration: ag.calm ? 0 : 200 }}
				onclick={() => (ag.menuOpen = false)}
			></button>
			<aside
				class="drawer"
				aria-label={t('agent.menu')}
				transition:fly={{ x: -340, duration: ag.calm ? 0 : 260, opacity: 1 }}
			>
				<AgentMenu {...ag.menuProps} />
			</aside>
		{/if}
	{/if}
</div>

{#if ag.settingsShown && ag.status}
	<!-- The settings stay open beneath a window they open (PLAN 59), and
	     are there again when it closes. -->
	<AgentSettings
		bind:look={ag.look}
		status={ag.status}
		user={ag.user}
		calm={ag.calm}
		start={ag.settingsShown.section ?? 'list'}
		onknow={ag.getToKnow}
		onclose={() => ((ag.dialog = null), (ag.under = null))}
		onopen={(part) => ag.openOver({ kind: 'knowledge', tab: part, only: true })}
		chats={ag.chats.filter((c) => !c.author).length}
		ondeleteall={() => {
			ag.keepPinned = true;
			ag.openOver({ kind: 'deleteAll' });
		}}
	/>
{/if}
{#if ag.dialog?.kind === 'feature'}
	{#if ag.phone}<FeatureView
			id={ag.dialog.id}
			calm={ag.calm}
			onclose={() => (ag.dialog = null)}
		/>{/if}
{:else if ag.dialog?.kind === 'project'}
	<ProjectDialog
		project={ag.dialog.project}
		locations={ag.locations}
		onclose={() => (ag.dialog = null)}
		onsaved={async (p) => {
			ag.dialog = null;
			await ag.loadLists();
			if (p) ag.project = p.id;
			else ag.project = '';
		}}
	/>
{:else if ag.dialog?.kind === 'knowledge'}
	<KnowledgeDialog
		tab={ag.dialog.tab}
		only={ag.dialog.only}
		admin={ag.status?.admin ?? false}
		projects={ag.projects}
		onclose={() => (ag.dialog = null)}
		onopenchat={(id) => {
			ag.under = null;
			ag.dialog = null;
			void ag.open(id);
		}}
	/>
{:else if isChatSheet(ag.dialog)}
	<ChatSheets
		sheet={ag.dialog}
		chats={ag.chats}
		projects={ag.projects}
		bind:keepPinned={ag.keepPinned}
		set={(next) => (ag.dialog = next)}
		onopen={(id) => ag.open(id)}
		onpatch={ag.patch}
		onremove={ag.remove}
		onremoveall={ag.removeAll}
	/>
{:else if ag.dialog?.kind === 'inbox'}
	<InboxPanel
		calm={ag.calm}
		onclose={() => (ag.dialog = null)}
		oncount={(n) => (ag.inboxCount = n)}
		onask={(text) => {
			ag.dialog = null;
			ag.newChat('');
			void ag.send(text);
		}}
	/>
{:else if ag.dialog?.kind === 'notes'}
	<NotesPanel calm={ag.calm} onclose={() => (ag.dialog = null)} />
{:else if ag.dialog?.kind === 'nas'}
	<NasPicker
		onclose={() => (ag.dialog = null)}
		onpick={(refs) => {
			ag.dialog = null;
			const add = refs
				.filter((ref) => !ag.attachments.some((a) => a.ref === ref))
				.map((ref) => ({ name: ref.slice(ref.indexOf(':') + 1), ref }));
			ag.attachments = [...ag.attachments, ...add];
		}}
	/>
{:else if ag.dialog?.kind === 'sheet'}
	{@const p = ag.dialog.project}
	<ProjectSheet
		project={p}
		chats={ag.listedChats}
		onclose={() => (ag.dialog = null)}
		onopen={(id) => {
			ag.dialog = null;
			void ag.open(id);
		}}
		onedit={() => (ag.dialog = { kind: 'project', project: p })}
		onnew={() => {
			ag.dialog = null;
			ag.newChat(p.id);
			ag.project = p.id;
		}}
	/>
{/if}

<style>
	/* PLAN 68: the agent in the design language of every window: no
	   background of its own, colors from the theme, the side bar like the
	   side bar of a window, the chat at most 760 px wide in the middle. */
	.agent {
		/* Edge to edge in the window or the browser tab: the page around it
		   gives up its padding (below); TitanOS's banners stay above it. */
		position: relative;
		flex: 1;
		min-height: 0;
		display: flex;
		flex-direction: column;
		background: var(--bg);
		color: var(--text);
	}
	:global(main:has(> .agent:not(.phone))) {
		display: flex;
		flex-direction: column;
		padding: 0;
		gap: 0;
		height: 100dvh;
		min-height: 0;
		overflow: hidden;
	}
	:global(html:has(.agent.app.phone)),
	:global(body:has(.agent.app.phone)) {
		height: 100%;
		overflow: hidden;
		overscroll-behavior: none;
	}
	.agent.app.phone {
		position: fixed;
		top: var(--ag-vvtop, 0px);
		left: 0;
		right: 0;
		height: var(--ag-vvh, 100dvh);
		min-height: 0;
	}
	.layout {
		flex: 1;
		min-height: 0;
		display: grid;
		grid-template-columns: 220px minmax(0, 1fr);
	}
	.layout.folded,
	.agent.phone .layout {
		grid-template-columns: minmax(0, 1fr);
	}
	.side {
		min-height: 0;
		overflow: hidden;
		border-right: 1px solid var(--border);
		background: color-mix(in srgb, var(--surface) 70%, transparent);
	}
	/* Text size of the app (PLAN 53) through the root font size. */
	:global(html:has(.agent[data-size='s'])) {
		font-size: calc(16px * var(--font-scale) * 0.93);
	}
	:global(html:has(.agent[data-size='l'])) {
		font-size: calc(16px * var(--font-scale) * 1.1);
	}
	.main {
		position: relative;
		display: flex;
		flex-direction: column;
		min-height: 0;
		min-width: 0;
		overflow: hidden;
	}
	/* The head of the chat: title, project, model, pin and more. */
	.top {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		min-height: 56px;
		padding: var(--space-2-5) 20px;
		border-bottom: 1px solid var(--border);
		min-width: 0;
	}
	.top h1 {
		margin: 0;
		min-width: 0;
		font-size: var(--text-md);
		font-weight: 600;
		overflow-wrap: break-word;
		hyphens: auto;
	}
	.fill {
		flex: 1;
	}
	.proj {
		flex: none;
		max-width: 40%;
		overflow-wrap: break-word;
		hyphens: auto;
	}
	.chip {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1-5);
		padding: var(--space-1) var(--space-2-5);
		border-radius: 999px;
		background: var(--chip);
		color: var(--text);
		font-size: var(--text-sm);
	}
	.tool-btn,
	.top .more :global(.btn) {
		position: relative;
		width: 36px;
		height: 36px;
		min-height: 36px;
		padding: 0;
		border-radius: var(--radius-ctl);
		display: grid;
		place-items: center;
		border: 1px solid var(--border);
		background: none;
		color: var(--text);
		cursor: pointer;
		flex: none;
	}
	.tool-btn:hover,
	.top .more :global(.btn:hover) {
		background: color-mix(in srgb, var(--text) 7%, transparent);
	}
	.tool-btn.on {
		color: var(--accent);
		border-color: color-mix(in srgb, var(--accent) 50%, var(--border));
	}
	.icon-btn {
		position: relative;
		width: 40px;
		height: 40px;
		padding: 0;
		border-radius: var(--radius-ctl);
		display: grid;
		place-items: center;
		border: 0;
		background: none;
		color: var(--text);
		cursor: pointer;
		flex: none;
	}
	.icon-btn:hover {
		background: color-mix(in srgb, var(--text) 8%, transparent);
	}
	.agent.phone .top {
		min-height: 48px;
		padding: calc(env(safe-area-inset-top, 0px) + var(--space-1)) var(--space-1-5) var(--space-1);
		gap: 2px;
	}
	.agent.phone .top .more :global(.btn) {
		border: 0;
		width: 40px;
		height: 40px;
	}
	.app-name {
		margin-left: var(--space-1);
		font-size: var(--text-md);
		font-weight: 600;
	}
	.chat-head {
		display: flex;
		align-items: center;
		flex-wrap: wrap;
		gap: var(--space-2);
		padding: var(--space-2-5) 14px;
		border-bottom: 1px solid var(--border);
		min-width: 0;
	}
	.chat-head h1 {
		margin: 0;
		min-width: 0;
		max-width: 100%;
		font-size: var(--text-md);
		font-weight: 600;
		overflow-wrap: anywhere;
	}
	.chat-head .proj {
		max-width: 100%;
	}
	.badge {
		position: absolute;
		top: 8px;
		right: 8px;
		width: 8px;
		height: 8px;
		border-radius: 50%;
		background: var(--accent);
	}
	.notice-warn {
		margin: var(--space-2) var(--space-4) 0;
	}
	.dock {
		padding: var(--space-3) var(--space-5) 14px;
	}
	.dock :global(.composer),
	.where {
		max-width: 760px;
		margin-left: auto;
		margin-right: auto;
	}
	.where {
		margin-top: var(--space-1-5);
		margin-bottom: 0;
		text-align: center;
		font-size: var(--text-xs);
		color: var(--muted);
	}
	.agent.phone .dock {
		padding: var(--space-2) var(--space-3) calc(env(safe-area-inset-bottom, 0px) + var(--space-2));
	}
	.agent.phone.keyboard .dock {
		padding-bottom: var(--space-1-5);
	}
	.agent.phone.keyboard .where {
		display: none;
	}
	.scrim {
		position: fixed;
		inset: 0;
		z-index: 50;
		border: 0;
		padding: 0;
		background: rgb(0 0 0 / 0.5);
		cursor: default;
	}
	.drawer {
		position: fixed;
		z-index: 51;
		top: 0;
		bottom: 0;
		left: 0;
		width: min(84%, 320px);
		padding-top: env(safe-area-inset-top, 0px);
		background: var(--bg);
		border-right: 1px solid var(--border);
		box-shadow: var(--shadow-menu, 0 18px 44px rgb(0 0 0 / 0.5));
	}
	.empty-state {
		display: flex;
		flex-direction: column;
		align-items: center;
		text-align: center;
		gap: var(--space-2);
		padding: var(--space-5);
		margin: auto;
		max-width: 520px;
	}
	.empty-state :global(svg) {
		color: var(--accent);
	}
	.empty-state h1 {
		margin: 0;
		font-size: var(--text-xl);
	}
	.pad {
		padding: var(--space-4);
	}

	/* Windows of the agent (Geplant, Gedächtnis, Projekt …) slide in from
	   the right like the settings (PLAN 56); "Animationen: Aus" and "Weniger
	   Bewegung" leave them still. Only on /agent: TitanOS keeps its look. */
	:global(body:has(.agent:not(.calm)) dialog.tile[open]) {
		animation: ag-slide-in 0.3s cubic-bezier(0.2, 0.8, 0.2, 1);
	}
	:global(body:has(.agent.phone:not(.calm)) dialog.tile[open]) {
		animation-name: ag-slide-in-far;
	}
	@keyframes -global-ag-slide-in {
		from {
			transform: translateX(48px);
			opacity: 0;
		}
	}
	@keyframes -global-ag-slide-in-far {
		from {
			transform: translateX(100vw);
		}
	}
	@media (prefers-reduced-motion: reduce) {
		:global(dialog.tile[open]) {
			animation: none;
		}
	}
</style>
