<script lang="ts">
	// The KI-Agent (PLAN 44; look of PLAN 68, the design language of every
	// window; PLAN 90-4 like the control panel): its side bar on the left; on
	// the right the start page, a chat with title, project and model at the
	// top, or one of the pages Geplant, Gedächtnis, Wissen and Einstellungen
	// (/agent?page=…). On the phone the head is one line: a tap on the title
	// opens project, model and thinking level as a sheet. Colors come from the TitanOS theme. On
	// the phone the agent is an app of its own whose side bar slides in from
	// the left, with the way back to TitanOS. Installed from here it is an
	// app of its own too (/agent.webmanifest, start /agent?app=1).
	import { fade, fly } from 'svelte/transition';
	import { dim, slideIn } from '#lib/agent-motion.ts';
	import Dropdown from '#lib/components/Dropdown.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import NasPicker from '#lib/components/agent/NasPicker.svelte';
	import AgentMenu from '#lib/components/agent/AgentMenu.svelte';
	import HomePage from '#lib/components/agent/pages/HomePage.svelte';
	import MemoryPage from '#lib/components/agent/pages/MemoryPage.svelte';
	import KnowledgePage from '#lib/components/agent/pages/KnowledgePage.svelte';
	import PlannedPage from '#lib/components/agent/pages/PlannedPage.svelte';
	import SettingsPage from '#lib/components/agent/pages/SettingsPage.svelte';
	import HeadSheet from '#lib/components/agent/HeadSheet.svelte';
	import ModelPicker from '#lib/components/agent/ModelPicker.svelte';
	import ChatView from '#lib/components/agent/ChatView.svelte';
	import Composer, { type Command } from '#lib/components/agent/Composer.svelte';
	import NotesPanel from '#lib/components/agent/NotesPanel.svelte';
	import ProjectDialog from '#lib/components/agent/ProjectDialog.svelte';
	import ProjectSheet from '#lib/components/agent/ProjectSheet.svelte';
	import ProjectList from '#lib/components/agent/ProjectList.svelte';
	import ChatSheets from './ChatSheets.svelte';
	import ChatSearch from './ChatSearch.svelte';
	import PromptTemplates from './PromptTemplates.svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { isChatSheet } from './chat-sheets.ts';
	import { agentFull, setAgentFull } from '#lib/agent-full.svelte.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import FeatureView from '#lib/components/agent/features/FeatureView.svelte';
	import FeatureHost from '#lib/components/agent/features/FeatureHost.svelte';
	import { inWindow } from '#lib/desktop/frame.ts';
	import { agentProfilePath, api } from '#lib/api.ts';
	import { apiErrorText, i18n } from '#lib/i18n/index.svelte.ts';
	import { DOT_SIZES, levelLabel, modelShown, tokens, type ChatInfo } from '#lib/agent.ts';
	import { AgentPage } from './agent-page.svelte.ts';
	import { nasSpeech } from '#lib/agent-speech.svelte.ts';
	import VoiceMode from '#lib/components/agent/VoiceMode.svelte';
	import SideResize, { readSideWidth } from '#lib/components/agent/SideResize.svelte';
	import PushHint from '#lib/components/agent/PushHint.svelte';
	import { trapFocus } from '#lib/focus-trap.ts';

	// State, requests and effects live in agent-chats.svelte.ts and agent-page.svelte.ts.
	const ag = new AgentPage();
	const currentInfo = $derived(ag.currentInfo);
	async function openWaiting(id: string) {
		await ag.open(id, false);
		if (ag.current?.id !== id) return;
		const url = new URL('/agent', location.origin);
		url.searchParams.set('chat', id);
		if (ag.profile) url.searchParams.set('profile', ag.profile);
		if (ag.current.pending) url.hash = 'pending-' + ag.current.pending.id;
		await goto(url.pathname + url.search + url.hash);
	}

	// PLAN 67-1: the menu bar of the desktop – the same actions as the agent's own menu.
	import { registerMenuCommands } from '#lib/menu-commands.svelte.ts';
	const agentOn = () => !!ag.status?.enabled && !!ag.status.allowed;
	registerMenuCommands({
		'agent.newChat': { run: () => ag.freshChat(''), enabled: agentOn },
		'agent.project': {
			run: () => (ag.dialog = { kind: 'project', project: null }),
			enabled: agentOn,
		},
		'agent.notes': { run: () => (ag.dialog = { kind: 'notes' }), enabled: agentOn },
		'agent.knowledge': { run: () => ag.show('memory'), enabled: agentOn },
		'agent.planned': { run: () => ag.show('planned'), enabled: agentOn },
		'agent.know': { run: () => ag.show('knowledge'), enabled: agentOn },
		'agent.inbox': { run: () => ag.newChat(''), enabled: agentOn },
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
					text: m.label || m.name,
					checked: ag.shownModel === m.name,
				})),
			run: (id) => {
				if (id && (ag.status?.models ?? []).some((m) => m.name === id)) ag.model = id;
			},
		},
		'agent.settings': { run: () => ag.show('settings'), enabled: agentOn },
	});

	// 1.07: the width of the side bar on the desktop, per device.
	let sideWidth = $state(readSideWidth());
	const unfold = $derived(ag.folded && !ag.phone ? () => ag.setFolded(false) : undefined);
	const pageNames = $derived({
		chat: t('nav.agent'),
		planned: t('agent.planned'),
		memory: t('agent.memory'),
		knowledge: t('agent.knowledge'),
		settings: t('agent.set.title'),
	});

	// The phone's bar at the bottom (15 Nov 2026): Chat, Projekte, Aufträge
	// and Notizen. The projects are a list in the place of the chat; opening
	// a chat or a page leaves it.
	let projectsTab = $state(false);
	// Set by the tab while the page still changes back to the chat.
	let wantProjects = false;
	$effect(() => {
		void ag.view;
		void ag.current?.id;
		void ag.fresh;
		projectsTab = wantProjects && ag.view === 'chat';
		if (ag.view === 'chat') wantProjects = false;
	});
	const tabsShown = $derived(
		ag.phone &&
			(ag.current || projectsTab || ag.onPage) &&
			!ag.keyboard &&
			(ag.view === 'chat' || ag.view === 'planned'),
	);
	const floating = $derived(ag.phone && !ag.onPage);
	// Height of the floating head on the phone; a title on two or three
	// lines makes it taller, and the chat keeps its first line below it.
	let headH = $state(0);

	// PLAN 90-4: the "/" commands of the input. The names follow the language.
	const commands: (Command & { id: string })[] = $derived(
		(
			[
				['new', 'newChat', false],
				['project', 'folder', false],
				['model', 'chip', false],
				['remember', 'bulb', true],
				['plan', 'clock', true],
				['find', 'search', true],
				['status', 'activity', false],
			] as const
		).map(([id, icon, arg]) => ({
			id,
			icon,
			arg,
			name: t(`agent.cmd.${id}.name`),
			label: t(`agent.cmd.${id}`),
		})),
	);
	/** A text for the memory ("Das merken" of a message), written on its page. */
	let memoryDraft = $state('');
	$effect(() => {
		// Leaving the page drops the draft; it is not offered again later.
		if (ag.view !== 'memory') memoryDraft = '';
	});
	async function remember(text: string) {
		try {
			await api.put('/agent/memory/new', { text });
			ag.say(t('agent.cmd.remembered'));
		} catch (e) {
			ag.error = apiErrorText(e);
		}
	}
	function command(name: string, arg: string) {
		const id = commands.find((c) => c.name === name)?.id;
		if (id === 'new') ag.freshChat();
		else if (id === 'project') {
			const wanted = arg && ag.projects.find((p) => p.name.toLowerCase() === arg.toLowerCase());
			if (wanted) setProject(wanted.id);
			else ag.dialog = { kind: 'head' };
		} else if (id === 'model') ag.dialog = { kind: 'head' };
		else if (id === 'remember') {
			if (arg) void remember(arg);
			else ag.show('memory');
		} else if (id === 'plan') {
			if (arg) void ag.send(t('agent.cmd.planAsk', { text: arg }));
			else ag.show('planned');
		} else if (id === 'find') {
			if (ag.current) ag.find = arg;
			else ag.say(t('agent.cmd.findNoChat'));
		} else if (id === 'status') {
			if (ag.busy) ag.say(t('agent.busyWait'));
			else void ag.send(t('agent.suggestHealth'));
		}
	}
	/** The project of the open chat, or the one a new chat goes to. */
	function setProject(id: string) {
		if (currentInfo) void ag.patch(currentInfo, { project: id });
		ag.project = id;
	}
</script>

<svelte:window ontouchstart={ag.touchStart} ontouchmove={ag.touchMove} />

<!-- The actions of the open chat. The settings are in the menu, not here
     a second time; without a chat there is nothing to show. -->
{#snippet chatMenu(info: ChatInfo | null)}
	{#if info}<span class="more">
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
								ag.find = '';
							}}><Icon name="search" size={16} /><span>{t('agent.findInChat')}</span></button
						>
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
						<a
							role="menuitem"
							href={agentProfilePath(`/api/v1/agent/chats/${info.id}/export?lang=${i18n.locale}`)}
							download
							onclick={close}
							><Icon name="download" size={16} /><span>{t('agent.exportMd')}</span></a
						>
						<a
							role="menuitem"
							href={agentProfilePath(
								`/api/v1/agent/chats/${info.id}/export?format=pdf&lang=${i18n.locale}`,
							)}
							download
							onclick={close}
							><Icon name="fileText" size={16} /><span>{t('agent.exportPdf')}</span></a
						>
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
		</span>{/if}
{/snippet}

<!-- Model and thinking level in the input: a chip that opens the sheet on
     the phone and the panel on the desktop. -->
{#snippet modelChip()}
	{#if !ag.status}
		<!-- nothing to choose yet -->
	{:else if ag.phone}
		<button
			type="button"
			class="m-chip"
			aria-haspopup="dialog"
			aria-label={t('agent.modelPick')}
			onclick={() => (ag.dialog = { kind: 'head' })}
			><strong>{modelShown(ag.status.models, ag.shownModel) || '–'}</strong
			>{#if ag.status.cloud}<span class="cloud-tag">{t('agent.cloud')}</span
				>{/if}{#if ag.shownThink}<span>{levelLabel(ag.shownThink, i18n.locale)}</span>{/if}</button
		>
	{:else}
		<ModelPicker
			models={ag.status.models}
			bind:model={() => ag.shownModel, (v) => (ag.model = v)}
			bind:think={() => ag.shownThink, (v) => (ag.think = v)}
			small
			calm={ag.calm}
		/>
		{#if ag.status.cloud}<span class="cloud-tag" title={t('agent.cloudHint')}
				>{t('agent.cloud')}</span
			>{/if}
	{/if}
{/snippet}

<div
	class="agent"
	class:app={ag.app}
	class:phone={ag.phone}
	class:keyboard={ag.keyboard}
	class:calm={ag.calm}
	class:framed={inWindow}
	data-size={ag.look.fontSize}
	style:--agent-head={ag.phone && headH ? `${headH}px` : null}
	style:--ag-dots={ag.look.dotColor || null}
	style:--ag-busy-text={ag.look.textColor || null}
	style:--ag-dot-size="{DOT_SIZES[ag.look.dotSize]}px"
	data-width={ag.look.chatWidth}
	data-trail={ag.look.trail ? 'on' : 'off'}
	data-shine={ag.look.textShine ? 'on' : 'off'}
	style:--ag-orbit={ag.look.trailSeconds + 's'}
>
	{#if ag.error}<div class="notice-error" role="alert">{ag.error}</div>{/if}
	<!-- 1.07: the installed agent app asks once for its own notifications. -->
	{#if ag.installed && ag.phone && !ag.profile}<PushHint active />{/if}

	{#if !ag.status && ag.statusFailed}
		<p class="pad">
			<button
				type="button"
				class="btn"
				onclick={() => {
					ag.error = '';
					ag.statusFailed = false;
					void ag.loadStatus();
				}}>{t('common.retry')}</button
			>
		</p>
	{:else if !ag.status}
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
		<div class="layout" class:folded={ag.folded && !ag.phone} style:--ag-side-w="{sideWidth}px">
			{#if !ag.phone && !ag.folded}
				<aside class="side"><AgentMenu {...ag.menuProps} /></aside>
				<SideResize bind:width={sideWidth} />
			{/if}
			<section class="main" class:floating aria-label={ag.current?.title ?? t('agent.newChat')}>
				{#if ag.dialog?.kind === 'feature' && !ag.phone}
					<!-- PLAN 72: a function's view as a page here, the side bar stays. -->
					<FeatureHost id={ag.dialog.id} calm={ag.calm} onclose={() => (ag.dialog = null)} />
				{/if}
				{#if ag.phone}
					<!-- PLAN 90-4: one line – menu or back, the title (a tap opens
					     project, model and thinking level), new chat, more. -->
					<header class="top" class:float={floating} bind:offsetHeight={headH}>
						{#if ag.onPage}
							<button
								type="button"
								class="icon-btn back-menu"
								aria-label={ag.part ? t('agent.set.back') : t('agent.menuOpen')}
								onclick={() => {
									if (ag.part) ag.show(ag.view);
									else ag.menuOpen = true;
								}}
								><Icon name="chevronLeft" size={19} /><span
									>{ag.part ? t('agent.set.back') : t('agent.night.menu')}</span
								></button
							>
							<h1 class="plain">{pageNames[ag.view]}</h1>
						{:else if !ag.current && !projectsTab}
							<div class="identity"><span aria-hidden="true">T</span><strong>TitanOS</strong></div>
							<button
								class="icon-btn back-menu"
								type="button"
								aria-label={t('agent.menuOpen')}
								aria-haspopup="dialog"
								aria-expanded={ag.menuOpen}
								onclick={() => (ag.menuOpen = true)}
								><Icon name="menu" size={19} />{t('agent.night.menu')}</button
							>
						{:else}
							<button
								type="button"
								class="icon-btn"
								aria-label={t('agent.menuOpen')}
								aria-haspopup="dialog"
								aria-expanded={ag.menuOpen}
								onclick={() => (ag.menuOpen = true)}
								><Icon
									name="menu"
									size={22}
								/>{#if ag.chats.some((c) => c.unread || c.state === 'waiting')}<span
										class="badge"
										aria-hidden="true"
									></span>{/if}</button
							>
							{#if projectsTab}<span class="fill"></span>{:else}<button
									type="button"
									class="p-title"
									aria-haspopup="dialog"
									aria-label={t('agent.head.open', {
										title: ag.current?.title || t('nav.agent'),
									})}
									onclick={() => (ag.dialog = { kind: 'head' })}
								>
									<span class="p-text">
										<strong class="p-name" role="heading" aria-level="1"
											>{ag.current?.title || t('nav.agent')}</strong
										>
										<small
											>{ag.currentProject
												? `${ag.currentProject.icon ?? ''} ${ag.currentProject.name} · `
												: ''}<span>{modelShown(ag.status?.models, ag.shownModel) || '–'}</span
											></small
										>
									</span>
									<Icon name="chevronDown" size={16} />
								</button>{/if}
							<button
								type="button"
								class="icon-btn"
								aria-label={t('agent.newChat')}
								title={t('agent.newChat')}
								onclick={() => ag.freshChat('')}><Icon name="newChat" size={22} /></button
							>
							{@render chatMenu(currentInfo ?? null)}
						{/if}
					</header>
				{:else if !ag.onPage && ag.current}
					<header class="top">
						{#if unfold}
							<button
								type="button"
								class="tool-btn"
								aria-label={t('agent.sideUnfold')}
								title={t('agent.sideUnfold')}
								onclick={unfold}><Icon name="sidebar" size={17} /></button
							>
						{/if}
						<h1>{ag.current.title || t('agent.newChat')}</h1>
						{#if ag.currentProject}<span class="chip proj"
								>{ag.currentProject.icon ?? ''} {ag.currentProject.name}</span
							>{/if}
						<span class="fill"></span>
						<button
							type="button"
							class="tool-btn"
							class:on={ag.find !== null}
							aria-pressed={ag.find !== null}
							aria-label={t('agent.findInChat')}
							title={t('agent.findInChat')}
							onclick={() => (ag.find = ag.find === null ? '' : null)}
							><Icon name="search" size={16} /></button
						>
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
				{#if !ag.onPage}
					{#if ag.status.reachable === false}
						<p class="notice-warn">
							{t('agent.ollamaDownLong', { error: ag.status.error ?? '' })}
						</p>
					{:else if ag.usable.length === 0}
						<p class="notice-warn">{t('agent.noToolModel')}</p>
					{:else if ag.contextNow && ag.contextNow < 16384}
						<p class="notice-warn">
							{t('agent.contextSmall', { tokens: tokens(ag.contextNow) })}
						</p>
					{/if}
				{/if}
				{#if projectsTab && ag.phone}
					<ProjectList
						projects={ag.projects}
						chats={ag.listedChats}
						onopen={(p) => (ag.dialog = { kind: 'sheet', project: p })}
						onnew={() => (ag.dialog = { kind: 'project', project: null })}
					/>
				{:else if ag.view === 'planned'}
					<PlannedPage
						projects={ag.projects}
						chats={ag.listedChats}
						phone={ag.phone}
						onunfold={unfold}
						onopenchat={(id) => ag.open(id)}
						oncount={(n) => (ag.plannedCount = n)}
					/>
				{:else if ag.view === 'memory'}
					{#key ag.part}
						<MemoryPage
							projects={ag.projects}
							chats={ag.chats}
							query={ag.part}
							draft={memoryDraft}
							phone={ag.phone}
							gestures={ag.look.gestures}
							onunfold={unfold}
							onopenchat={(id) => ag.open(id)}
						/>
					{/key}
				{:else if ag.view === 'knowledge'}
					<KnowledgePage
						admin={ag.status.admin}
						projects={ag.projects}
						phone={ag.phone}
						onunfold={unfold}
						oneditproject={(p) => (ag.dialog = { kind: 'project', project: p })}
						onopenproject={(p) => (ag.dialog = { kind: 'sheet', project: p })}
					/>
				{:else if ag.view === 'settings'}
					<SettingsPage
						bind:look={ag.look}
						status={ag.status}
						part={ag.part}
						calm={ag.calm}
						phone={ag.phone}
						onunfold={unfold}
						onknow={ag.getToKnow}
						chats={ag.chats.filter((c) => !c.author).length}
						ondeleteall={() => {
							ag.keepPinned = true;
							ag.dialog = { kind: 'deleteAll' };
						}}
					/>
				{:else if ag.current}
					<ChatView
						showSteps={ag.look.steps}
						mode={ag.look.dotMode}
						readonly={!!ag.readOnlyBy}
						onplanned={() => ag.show('planned')}
						chat={ag.current}
						jump={page.url.searchParams.get('chat') === ag.current.id
							? (page.url.searchParams.get('entry') ?? '')
							: ''}
						ontemplate={ag.readOnlyBy
							? undefined
							: (text) => (ag.dialog = { kind: 'templates', text })}
						live={ag.look.typing ? ag.live : { ...ag.live, content: '' }}
						busy={ag.busy}
						deciding={ag.deciding}
						phone={ag.phone}
						calm={ag.calm}
						bind:find={ag.find}
						ondecide={ag.decide}
						onedit={ag.edit}
						onchoice={(c) => ag.send(c)}
						onretry={() => ag.send(t('agent.continueMessage'))}
						onundo={ag.undo}
						onresend={ag.resend}
						onremember={(text) => {
							memoryDraft = text;
							ag.show('memory');
						}}
					/>
				{:else}
					<HomePage
						fresh={ag.fresh}
						preferDetails={ag.look.start === 'overview' && !ag.fresh}
						user={ag.user}
						chats={ag.listedChats}
						suggestions={ag.suggestions}
						hints={ag.hints}
						show={ag.look.suggestions}
						phone={ag.phone}
						onunfold={unfold}
						onapproval={openWaiting}
						calm={ag.calm}
						onopen={(id) => ag.open(id)}
						onstop={(c) => ag.stop(c.id)}
						onfill={(text) => ag.composer?.fill(text)}
						onask={(text) => {
							ag.newChat('');
							void ag.send(text);
						}}
						onshow={(view) => ag.show(view)}
						onshuffle={() => (ag.shuffle += 1)}
						oncount={(n) => (ag.inboxCount = n)}
					/>
				{/if}
				{#if !ag.onPage && !projectsTab}
					<div class="dock">
						{#if ag.notice}<p class="said" role="status">{ag.notice}</p>{/if}
						<Composer
							micHold={ag.look.mic === 'hold'}
							bind:this={ag.composer}
							busy={ag.busy}
							bind:attachments={ag.attachments}
							bind:text={ag.draft}
							disabled={!!ag.current?.pending || !!ag.readOnlyBy}
							voice={!!ag.status.voice || nasSpeech.hear}
							voiceNas={nasSpeech.hear}
							placeholder={ag.readOnlyBy
								? t('agent.readOnlyChat', { name: ag.readOnlyBy })
								: ag.current?.pending
									? t('agent.inputPending')
									: ag.phone
										? t('agent.inputShort')
										: t('agent.inputCommands')}
							{commands}
							phone={ag.phone}
							calm={ag.calm}
							chip={modelChip}
							onhead={() => (ag.dialog = { kind: 'head' })}
							projectName={ag.currentProject?.name ?? ''}
							autonomy={ag.autonomy}
							onautonomy={ag.readOnlyBy ? undefined : ag.setAutonomy}
							oncommand={ag.readOnlyBy ? undefined : command}
							onsend={ag.send}
							onstop={() => ag.stop()}
							onpicknas={() => (ag.dialog = { kind: 'nas' })}
							ontalk={ag.readOnlyBy ? undefined : () => (ag.dialog = { kind: 'talk' })}
							onerror={(m) => (ag.error = m)}
						/>
						<p class="where">{ag.whereText}</p>
					</div>
				{/if}
				{#if tabsShown}
					<nav class="tabs" aria-label={t('agent.tabs.label')}>
						<button
							type="button"
							class="tab"
							class:on={ag.view === 'chat' && !projectsTab}
							aria-current={ag.view === 'chat' && !projectsTab ? 'page' : undefined}
							onclick={() => {
								projectsTab = false;
								if (ag.view !== 'chat') ag.show('chat');
							}}><Icon name="chat" size={21} /><span>{t('agent.tabs.chat')}</span></button
						>
						<button
							type="button"
							class="tab"
							class:on={projectsTab}
							aria-current={projectsTab ? 'page' : undefined}
							onclick={() => {
								if (ag.view === 'chat') projectsTab = true;
								else {
									wantProjects = true;
									ag.show('chat');
								}
							}}><Icon name="folder" size={21} /><span>{t('agent.projects')}</span></button
						>
						<button
							type="button"
							class="tab"
							class:on={ag.view === 'planned'}
							aria-current={ag.view === 'planned' ? 'page' : undefined}
							onclick={() => ag.show('planned')}
							><Icon name="clock" size={21} /><span>{t('agent.tabs.planned')}</span
							>{#if ag.plannedCount}<span class="sr-only">{ag.plannedCount}</span>{/if}</button
						>
						<button type="button" class="tab" onclick={() => (ag.dialog = { kind: 'notes' })}
							><Icon name="edit" size={21} /><span>{t('agent.notes.title')}</span></button
						>
					</nav>
				{/if}
			</section>
		</div>
		{#if ag.phone && ag.menuOpen}
			<button
				type="button"
				class="scrim"
				aria-label={t('common.close')}
				transition:fade={dim(ag.calm)}
				onclick={() => (ag.menuOpen = false)}
			></button>
			<!-- 1.01: the menu over the chat holds the focus; Escape closes it.
			     It stays an <aside> (the menu of the agent) that acts as a modal. -->
			<!-- svelte-ignore a11y_no_noninteractive_element_to_interactive_role -->
			<aside
				class="drawer"
				role="dialog"
				aria-modal="true"
				aria-label={t('agent.menu')}
				tabindex="-1"
				transition:fly={slideIn(ag.calm, -340)}
				use:trapFocus={{ onescape: () => (ag.menuOpen = false) }}
			>
				<AgentMenu {...ag.menuProps} />
			</aside>
		{/if}
	{/if}
</div>

{#if ag.dialog?.kind === 'feature'}
	<!-- Also without the agent's page (agent off, account not allowed, no
	     volume): the alarm of the guard and the emergency access open from
	     their notification all the same (PLAN 90-6). -->
	{#if ag.phone || !ag.ready}<FeatureView
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
{:else if ag.dialog?.kind === 'head' && ag.status}
	<HeadSheet
		projects={ag.projects}
		project={ag.current ? (ag.current.project ?? '') : ag.project}
		models={ag.status.models}
		bind:model={() => ag.shownModel, (v) => (ag.model = v)}
		bind:think={() => ag.shownThink, (v) => (ag.think = v)}
		calm={ag.calm}
		onproject={ag.readOnlyBy ? undefined : setProject}
		onclose={() => (ag.dialog = null)}
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
{:else if ag.dialog?.kind === 'notes'}
	<NotesPanel calm={ag.calm} onclose={() => (ag.dialog = null)} />
{:else if ag.dialog?.kind === 'talk'}
	<VoiceMode
		chat={ag.current}
		busy={ag.busy}
		voiceNas={nasSpeech.hear}
		calm={ag.calm}
		onsend={ag.send}
		onclose={() => (ag.dialog = null)}
	/>
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
{#if ag.dialog?.kind === 'textSearch'}
	<ChatSearch
		projects={ag.projects}
		initial={ag.dialog.query}
		onclose={() => (ag.dialog = null)}
		onopen={(h) => {
			ag.dialog = null;
			const q = new URLSearchParams({ chat: h.chat, entry: h.entry });
			if (h.author) q.set('author', h.author);
			void ag.go('/agent?' + q);
		}}
	/>
{:else if ag.dialog?.kind === 'templates'}
	<PromptTemplates
		text={ag.dialog.text}
		occupied={!!ag.draft.trim() || ag.attachments.length > 0}
		onclose={() => (ag.dialog = null)}
		onuse={(text) => {
			ag.dialog = null;
			ag.freshChat(ag.project);
			ag.attachments = [];
			ag.draft = text;
			ag.composer?.fill(text);
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
		/* Owner 15 Nov 2026: a warm orange for what the agent does (send, the
		   line while it works) and a calmer blue for its links. Mixed with the
		   text color, so both stay readable on a light and a dark theme. */
		--ag-warm: var(--accent);
		--ag-edge: color-mix(in srgb, var(--accent) 76%, var(--text));
		--ag-glass: color-mix(in srgb, var(--surface) 88%, var(--accent));
		--ag-link: var(--ag-edge);
	}
	/* 0.96: the model is asked at an outside provider. A word, not only a
	   colour. */
	.cloud-tag {
		flex: none;
		padding: 1px 8px;
		border-radius: 999px;
		border: 1px solid color-mix(in srgb, var(--ag-warm) 60%, transparent);
		color: var(--text);
		font-size: 0.78em;
		font-weight: 600;
		white-space: nowrap;
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
		position: relative;
		flex: 1;
		min-height: 0;
		display: grid;
		/* 1.07: the side bar's width can be dragged (SideResize, per device). */
		grid-template-columns: var(--ag-side-w, 264px) minmax(0, 1fr);
	}
	.layout.folded,
	.agent.phone .layout {
		grid-template-columns: minmax(0, 1fr);
	}
	.side {
		min-height: 0;
		overflow: hidden;
		border-right: 1px solid var(--border);
		/* Like the column of the control panel (WindowSide). */
		background:
			linear-gradient(
				180deg,
				color-mix(in srgb, var(--text) 3%, transparent),
				color-mix(in srgb, var(--accent) 3%, transparent)
			),
			color-mix(in srgb, var(--surface) 88%, var(--bg));
	}
	/* Owner 15 Nov 2026: no blur in the agent on the phone. Menus, sheets
	   and cards are solid there – the glass let the chat shine through the
	   menus and made the page restless. */
	/* A web app on the iPhone's home screen reaches under the status bar,
	   and iOS blurs what lies there a little further down than the safe
	   area says. The heads start 40 % lower, so their titles stay sharp;
	   without a status bar above the page the inset is 0 and nothing moves. */
	:global(:root) {
		--agent-top: calc(env(safe-area-inset-top, 0px) * 1.4);
	}
	:global(:root:has(.agent.phone)) {
		--panel-bg: var(--surface);
		--panel-border: var(--border);
		--panel-backdrop: none;
		--panel-shadow: 0 0 0 transparent;
		--menu-bg: var(--surface);
		--menu-border: var(--border);
		--menu-backdrop: none;
		--menu-shadow: var(--shadow-menu);
		--dialog-bg: var(--surface);
		--dialog-border: var(--border);
		--dialog-backdrop: none;
		--dialog-shadow: var(--shadow-menu);
	}
	/* The pages of the agent in the shape of its chat (owner 15 Nov 2026:
	   Geplant, Gedächtnis, Wissen, Einstellungen, the overview, the dialogs
	   and sheets follow the new design): rounder rows and cards, buttons as
	   capsules, softer fields, the headings of the groups quiet and grey.
	   Only where the agent is; TitanOS keeps its shapes. */
	:global(body:has(.agent) :is(.agent, dialog, .sheet)) {
		--srow-radius: 16px;
		--radius-tile: 20px;
		--radius-ctl: 12px;
	}
	:global(body:has(.agent) :is(.agent, dialog, .sheet) .btn) {
		border-radius: 999px;
	}
	:global(body:has(.agent) :is(.agent, dialog, .sheet) :is(input, select).field) {
		border-radius: 12px;
	}
	:global(body:has(.agent) :is(.agent, dialog, .sheet) textarea.field) {
		border-radius: 14px;
	}
	.agent :global(.sgroup-title) {
		color: var(--muted);
		font-size: var(--text-md);
		font-weight: 500;
	}
	.agent :global(.srow-title) {
		font-weight: 500;
	}
	.agent :global(.scroll > .page > .head h1) {
		font-weight: 500;
		letter-spacing: -0.01em;
	}
	.agent :global(.srow-ico) {
		border-radius: 12px;
	}
	.agent :global(.chip) {
		border-radius: 999px;
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
		border-radius: 50%;
		display: grid;
		place-items: center;
		border: 0;
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
		background: color-mix(in srgb, var(--accent) 14%, transparent);
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
	/* The phone's head (15 Nov 2026, like the chat apps): no bar – round
	   buttons and the title in a capsule float over the chat, which scrolls
	   below them. They start under the status bar (--agent-top). On a page
	   (Geplant, Einstellungen …) the head is a plain row with the way back. */
	.agent.phone .top {
		min-height: 48px;
		padding: calc(var(--agent-top) + 4px) var(--space-3) 0;
		gap: var(--space-2);
	}
	.agent.phone .top.float {
		position: absolute;
		z-index: 6;
		top: 0;
		left: 0;
		right: 0;
		pointer-events: none;
	}
	.agent.phone .top.float > :global(*) {
		pointer-events: auto;
	}
	/* The chat fades out behind the buttons instead of running through them. */
	.agent.phone .top.float::before {
		content: '';
		position: absolute;
		z-index: -1;
		inset: 0 0 -18px;
		background: linear-gradient(
			to bottom,
			var(--bg) 0%,
			color-mix(in srgb, var(--bg) 82%, transparent) 62%,
			transparent 100%
		);
		pointer-events: none;
	}
	.agent.phone .icon-btn,
	.agent.phone .top .more :global(.btn) {
		width: 44px;
		height: 44px;
		min-height: 44px;
		border-radius: 50%;
		border: 1px solid var(--border);
		background: color-mix(in srgb, var(--text) 7%, var(--surface));
		box-shadow: 0 2px 10px rgb(0 0 0 / 0.18);
	}
	.p-title {
		flex: 1;
		min-width: 0;
		min-height: 44px;
		display: flex;
		align-items: center;
		justify-content: center;
		gap: var(--space-1-5);
		padding: 2px var(--space-3) 2px var(--space-4);
		border: 1px solid var(--border);
		border-radius: 999px;
		background: color-mix(in srgb, var(--text) 7%, var(--surface));
		box-shadow: 0 2px 10px rgb(0 0 0 / 0.18);
		color: var(--text);
		font: inherit;
		text-align: center;
		cursor: pointer;
	}
	.p-title :global(svg) {
		color: var(--muted);
		flex: none;
	}
	.p-text {
		min-width: 0;
		display: flex;
		flex-direction: column;
		line-height: 1.15;
	}
	/* Owner 16 Nov 2026 and WEB-UI rule: no cut-off text in the head – a
	   long title wraps instead of ending in "…", with no line limit, and the
	   pill and the space above the chat grow with it (--agent-head). A title
	   made from the first question has at most 60 characters, three lines
	   next to the buttons on an iPhone. The line below wraps between project
	   and model, never inside a name. */
	.p-name {
		overflow-wrap: break-word;
	}
	.p-text small {
		overflow-wrap: break-word;
	}
	.p-text small span {
		white-space: nowrap;
	}
	/* Owner 15 Nov 2026: the title was blurred and hard to read on the
	   iPhone while the line below it was sharp. It was a heading with
	   automatic hyphenation inside the button; it is now plain text like
	   that line, only bolder. */
	.p-name {
		font-size: var(--text-md);
		font-weight: 600;
		color: var(--text);
		hyphens: manual;
	}
	/* 1.07: the chat and the input use the whole main area with an even
	   margin (owner 17 Nov 2026), or the reading width of before. The
	   pages (start page, memory …) follow: full, or their 980 px. */
	.agent {
		--ag-chat-width: none;
		--ag-page-width: none;
	}
	.agent[data-width='read'] {
		--ag-chat-width: 760px;
		--ag-page-width: 980px;
	}
	.p-text small {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.agent.phone .top h1 {
		hyphens: manual;
	}
	.top h1.plain {
		flex: 1;
		padding-left: var(--space-1);
		font-size: var(--text-lg);
	}
	.said {
		max-width: var(--ag-chat-width);
		margin: 0 auto var(--space-1-5);
		font-size: var(--text-sm);
		color: var(--muted);
		text-align: center;
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
	.dock :global(.cmds),
	.where {
		max-width: var(--ag-chat-width);
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
		padding: var(--space-1-5) var(--space-3) var(--space-1-5);
	}
	.agent.phone .where {
		display: none;
	}
	/* Without the bar below it (keyboard open, a page) the input keeps the
	   space of the home indicator itself. */
	.agent.phone .dock:last-child {
		padding-bottom: calc(env(safe-area-inset-bottom, 0px) + var(--space-2));
	}
	.main.floating > :global(.scroll.phone > .page) {
		padding-top: max(calc(var(--agent-top) + 64px), calc(var(--agent-head, 0px) + 8px));
	}
	.main.floating > .notice-warn {
		margin-top: max(calc(var(--agent-top) + 60px), calc(var(--agent-head, 0px) + 8px));
	}
	/* The chip of the model in the input on the phone. */
	.m-chip {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		max-width: 100%;
		min-height: 44px;
		padding: 0 var(--space-3-5, 14px);
		border: 0;
		border-radius: 999px;
		background: color-mix(in srgb, var(--text) 8%, transparent);
		color: var(--text);
		font: inherit;
		font-size: var(--text-md);
		cursor: pointer;
		overflow: hidden;
		white-space: nowrap;
	}
	/* A long model name or alias wraps inside the chip instead of ending
	   in "…" (owner 16 Nov 2026: no cut-off text). */
	.m-chip strong {
		font-weight: 500;
		white-space: normal;
		overflow-wrap: anywhere;
		text-align: left;
		line-height: 1.2;
	}
	.m-chip span {
		color: var(--muted);
		flex: none;
	}
	/* The bar at the bottom of the phone: four places in one capsule. */
	.tabs {
		flex: none;
		display: flex;
		gap: 2px;
		margin: 0 var(--space-3) max(var(--space-2), calc(env(safe-area-inset-bottom, 0px) - 12px));
		padding: var(--space-1);
		border: 1px solid var(--border);
		border-radius: 999px;
		background: color-mix(in srgb, var(--text) 6%, var(--surface));
	}
	.tab {
		flex: 1;
		min-width: 0;
		min-height: 50px;
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: 2px;
		border: 0;
		border-radius: 999px;
		background: none;
		color: var(--muted);
		font: inherit;
		font-size: var(--text-xs);
		cursor: pointer;
	}
	.tab.on {
		background: var(--bg);
		color: var(--text);
	}
	.tab:focus-visible,
	.m-chip:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 1px;
	}
	.agent.phone.keyboard .dock {
		padding-bottom: var(--space-1-5);
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
		outline: 0;
		position: fixed;
		z-index: 51;
		top: 0;
		bottom: 0;
		left: 0;
		width: min(84%, 320px);
		padding-top: var(--agent-top);
		background: var(--bg);
		will-change: transform;
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
		animation: ag-slide-in 0.4s cubic-bezier(0.22, 1, 0.36, 1);
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
		:global(body:has(.agent:not(.calm)) dialog.tile[open]) {
			animation: none;
		}
	}

	:global([data-mode='dark']) .agent {
		--ag-glass: color-mix(in srgb, #252a24 80%, var(--surface));
		background:
			radial-gradient(
				ellipse at 0% 8%,
				color-mix(in srgb, #343d32 72%, var(--bg)),
				transparent 72%
			),
			color-mix(in srgb, #191d1a 82%, var(--bg));
	}
	.agent .side,
	.agent .drawer {
		background: color-mix(in srgb, var(--ag-glass) 90%, var(--bg));
	}
	.agent .main {
		background: transparent;
	}
	.agent .dock :global(.composer) {
		background: var(--ag-glass);
		border: 1px solid color-mix(in srgb, var(--text) 19%, var(--surface));
		border-radius: 22px;
	}
	.agent .dock :global(.composer:focus-within) {
		border-color: var(--ag-edge);
	}
	.agent .main :global(.grp),
	.agent .main :global(.srow) {
		--srow-bg: var(--ag-glass);
	}
	:global([data-nord-desktop]) .agent:not(.phone) {
		background: var(--bg);
		--ag-glass: var(--surface);
	}
	:global([data-nord-desktop]) .agent:not(.phone) .top {
		background: var(--surface);
		border-bottom: 1px solid var(--nord-line, var(--border));
	}
	:global([data-nord-desktop]) .agent:not(.phone) .dock :global(.composer) {
		border-color: var(--nord-line, var(--border));
		border-radius: var(--nord-radius, 16px);
	}
	:global([data-nord-desktop]) .agent:not(.phone) .dock :global(.composer:focus-within) {
		border-color: var(--ag-edge);
	}
	@media (prefers-reduced-motion: no-preference) {
		:global(html[data-nord-desktop] body:has(.agent:not(.phone):not(.calm)) dialog.tile[open]) {
			animation-duration: 175ms;
		}
	}
	.agent.phone .icon-btn.back-menu {
		width: auto;
		display: inline-flex;
		gap: 5px;
		padding: 0 12px;
		align-items: center;
		justify-content: center;
		font: inherit;
	}
	.identity {
		display: flex;
		align-items: center;
		gap: 8px;
		flex: 1;
		font-weight: 600;
	}
	.identity span {
		display: grid;
		place-items: center;
		width: 28px;
		height: 28px;
		border-radius: 8px;
		background: var(--accent-fill, var(--accent));
		color: var(--on-accent);
	}
</style>
