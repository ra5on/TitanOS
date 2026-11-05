<script lang="ts">
	// The KI-Agent (PLAN 44, look of PLAN 48): its own menu on the left, the
	// chat on the right, with model and thinking level at the top and a
	// start page like the big chat apps. On the desktop the TitanOS menu
	// stays beside it (or full screen); on the phone the agent is an app of
	// its own whose menu slides in from the left, with the way back to
	// TitanOS. Installed from here it is an app of its own too
	// (/agent.webmanifest, start /agent?app=1, no TitanOS frame).
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { fade, fly } from 'svelte/transition';
	import Dialog from '#lib/components/Dialog.svelte';
	import Dropdown from '#lib/components/Dropdown.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import NasPicker from '#lib/components/agent/NasPicker.svelte';
	import AgentMenu from '#lib/components/agent/AgentMenu.svelte';
	import AgentStart from '#lib/components/agent/AgentStart.svelte';
	import ModelPicker from '#lib/components/agent/ModelPicker.svelte';
	import ChatView from '#lib/components/agent/ChatView.svelte';
	import Composer from '#lib/components/agent/Composer.svelte';
	import KnowledgeDialog from '#lib/components/agent/KnowledgeDialog.svelte';
	import LookDialog from '#lib/components/agent/LookDialog.svelte';
	import ProjectDialog from '#lib/components/agent/ProjectDialog.svelte';
	import ProjectSheet from '#lib/components/agent/ProjectSheet.svelte';
	import { api, apiFetch, responseError } from '#lib/api.ts';
	import { agentAppMode } from '#lib/agent-app.ts';
	import { agentFull, setAgentFull } from '#lib/agent-full.svelte.ts';
	import { clearAgentNotifications } from '#lib/web-push.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import { pageTitle } from '#lib/page-title.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import {
		defaultLook,
		follow,
		lookStyle,
		sanitizeLook,
		tokens,
		type AgentEvent,
		type Chat,
		type ChatInfo,
		type Entry,
		type Location,
		type Look,
		type Pending,
		type Project,
		type Status,
		type Suggestion,
	} from '#lib/agent.ts';

	const LOOK_KEY = 'titanos.agent.look';

	let status = $state<Status | null>(null);
	let chats = $state<ChatInfo[]>([]);
	let projects = $state<Project[]>([]);
	let locations = $state<Location[]>([]);
	let current = $state<Chat | null>(null);
	let project = $state('');
	let error = $state('');
	let busy = $state(false);
	let deciding = $state(false);
	let live = $state<{ thinking: string; content: string; step: Entry | null }>({
		thinking: '',
		content: '',
		step: null,
	});
	let model = $state('');
	let think = $state('');
	let look = $state<Look>(readLook());
	let dialog = $state<
		| { kind: 'project'; project: Project | null }
		| { kind: 'knowledge'; tab: 'memory' | 'skills' | 'schedules' | 'map' | 'me' | 'log' }
		| { kind: 'look' }
		| { kind: 'move'; chat: ChatInfo }
		| { kind: 'rename'; chat: ChatInfo; title: string }
		| { kind: 'delete'; chat: ChatInfo }
		| { kind: 'sheet'; project: Project }
		| { kind: 'nas' }
		| null
	>(null);
	let composer = $state<{ fill: (text: string) => void }>();
	let attachments = $state<{ name: string; file?: File; ref?: string }[]>([]);
	let phone = $state(matchMedia('(max-width: 760px)').matches);
	let stream: AbortController | null = null;

	let menuOpen = $state(false);

	// Installed as an app and on the phone it is always full screen; on the
	// desktop inside TitanOS full screen is a choice per device (PLAN 47/48).
	const installed = $derived(agentAppMode(page.url));
	const app = $derived(installed || phone || agentFull.on);
	/** Back to TitanOS: a full load, so the TitanOS manifest is there again. */
	const toTitanOS = () => location.assign('/');
	const user = $derived(session.data?.user?.name ?? '');
	const usable = $derived((status?.models ?? []).filter((m) => m.capabilities.includes('tools')));
	const contextNow = $derived(
		status?.context || status?.running?.find((r) => r.name === model)?.contextLength || 0,
	);

	$effect(() => {
		const query = matchMedia('(max-width: 760px)');
		const change = () => {
			phone = query.matches;
			if (!phone) menuOpen = false;
		};
		query.addEventListener('change', change);
		return () => query.removeEventListener('change', change);
	});
	$effect(() => {
		pageTitle.value = current?.title ? `${current.title} · ${t('nav.agent')}` : t('nav.agent');
		return () => (pageTitle.value = '');
	});

	function readLook(): Look {
		try {
			return sanitizeLook(JSON.parse(localStorage.getItem(LOOK_KEY) ?? 'null'));
		} catch {
			return defaultLook();
		}
	}
	$effect(() => {
		const value = JSON.stringify(look);
		try {
			localStorage.setItem(LOOK_KEY, value);
		} catch {
			// Only remembered while the page is open.
		}
	});
	const calm = $derived(
		look.calm ||
			(typeof matchMedia !== 'undefined' && matchMedia('(prefers-reduced-motion: reduce)').matches),
	);
	function buzz(pattern: number | number[]) {
		if (look.vibrate && !calm && 'vibrate' in navigator) navigator.vibrate(pattern);
	}

	async function loadStatus() {
		try {
			status = await api.get<Status>('/agent/status');
			if (!model) model = status.model || usable[0]?.name || '';
			if (!think) think = status.think || '';
		} catch (e) {
			error = apiErrorText(e);
		}
	}
	async function loadLists() {
		try {
			const [c, p] = await Promise.all([
				api.get<{ chats: ChatInfo[] }>('/agent/chats'),
				api.get<{ projects: Project[] }>('/agent/projects'),
			]);
			chats = c.chats;
			projects = p.projects;
		} catch (e) {
			error = apiErrorText(e);
		}
	}
	async function loadLocations() {
		if (!status?.admin) return;
		try {
			const s = await api.get<{ locations?: Location[] }>('/agent/settings');
			locations = s.locations ?? [];
		} catch {
			locations = [];
		}
	}

	$effect(() => {
		void (async () => {
			await loadStatus();
			if (status?.allowed && status.enabled) {
				await loadLists();
				void loadLocations();
				const id = page.url.searchParams.get('chat');
				if (id) void open(id, false);
				// From the file manager, the dashboard or a notification.
				const attach = page.url.searchParams.get('attach');
				if (attach)
					attachments = attach
						.split('|')
						.filter(Boolean)
						.map((ref) => ({
							name:
								ref
									.slice(ref.indexOf(':') + 1)
									.split('/')
									.pop() || ref,
							ref,
						}));
				const ask = page.url.searchParams.get('ask');
				if (ask || attach) queueMicrotask(() => composer?.fill(ask ?? ''));
				// Ollama loads the model now, not with the first message.
				void api.post('/agent/preload', { model }).catch(() => {});
			}
		})();
		return () => stream?.abort();
	});

	async function open(id: string, push = true) {
		menuOpen = false;
		stream?.abort();
		busy = false;
		live = { thinking: '', content: '', step: null };
		error = '';
		try {
			current = await api.get<Chat>(`/agent/chats/${id}`);
			if (current.model) model = current.model;
			if (current.think) think = current.think;
			if (current.project) project = current.project;
			if (push) void goto(`/agent?chat=${id}`);
			const listed = chats.find((c) => c.id === id);
			if (current.state === 'running' || listed?.state === 'running') watch(id);
		} catch (e) {
			error = apiErrorText(e);
		}
	}

	function newChat(inProject = project) {
		menuOpen = false;
		stream?.abort();
		current = null;
		busy = false;
		project = inProject;
		live = { thinking: '', content: '', step: null };
		void goto('/agent');
	}

	function watch(id: string) {
		stream?.abort();
		const controller = new AbortController();
		stream = controller;
		busy = true;
		live = { thinking: '', content: '', step: null };
		void follow(id, (e) => onEvent(id, e), controller.signal)
			.catch(() => {})
			.finally(() => {
				if (stream === controller) finish(id);
			});
	}

	function onEvent(id: string, e: AgentEvent) {
		if (!current || current.id !== id) return;
		switch (e.type) {
			case 'thinking':
				live.thinking += e.text;
				break;
			case 'content':
				live.content += e.text;
				break;
			case 'step':
				live.step = e.step;
				break;
			case 'entry':
				if (!current.entries.some((x) => x.id === e.entry.id)) current.entries.push(e.entry);
				if (e.entry.role === 'assistant') live = { thinking: '', content: '', step: live.step };
				if (e.entry.role === 'tool') live.step = null;
				break;
			case 'approval':
				current.pending = e.pending;
				current.state = 'waiting';
				buzz([60, 40, 60]);
				break;
			case 'state':
				current.state = e.state;
				break;
		}
	}

	async function finish(id: string) {
		busy = false;
		stream = null;
		live = { thinking: '', content: '', step: null };
		try {
			const fresh = await api.get<Chat>(`/agent/chats/${id}`);
			if (current?.id === id) current = fresh;
			if (fresh.state === 'idle') buzz(40);
			if (current?.id === id) seen(id, true);
		} catch {
			// The list reload below shows the state.
		}
		void loadLists();
	}

	async function send(text: string) {
		error = '';
		const files = attachments;
		try {
			let target = current;
			const uploads = files.filter((a) => a.file);
			if (!target && uploads.length) {
				// An empty chat first, so the files are there before it runs.
				target = await api.post<Chat>('/agent/chats', { draft: true, project, model, think });
			}
			for (const a of uploads) {
				const res = await apiFetch(
					`/agent/chats/${target!.id}/attachments?name=${encodeURIComponent(a.name)}`,
					{ method: 'POST', body: a.file },
				);
				if (!res.ok) throw await responseError(res);
			}
			const refs = files.map((a) =>
				a.file ? `[Anhang: ${a.name}]` : `[Datei: ${(a.ref ?? '').replace(':', '/')}]`,
			);
			const message = refs.length ? `${refs.join('\n')}\n\n${text}` : text;
			attachments = [];
			if (target) {
				const chat = await api.post<Chat>(`/agent/chats/${target.id}/messages`, {
					message,
					model,
					think,
				});
				current = chat;
				if (!chats.some((c) => c.id === chat.id)) chats = [chat, ...chats];
				void goto(`/agent?chat=${chat.id}`);
				watch(chat.id);
			} else {
				const chat = await api.post<Chat>('/agent/chats', { message, project, model, think });
				current = chat;
				chats = [chat, ...chats];
				void goto(`/agent?chat=${chat.id}`);
				watch(chat.id);
			}
		} catch (e) {
			attachments = files;
			error = apiErrorText(e);
		}
	}

	async function undo(step: Entry) {
		if (!current) return;
		error = '';
		try {
			await api.post(`/agent/chats/${current.id}/undo`, { step: step.id });
			current = await api.get<Chat>(`/agent/chats/${current.id}`);
		} catch (e) {
			error = apiErrorText(e);
		}
	}

	// On the phone a swipe from the left edge opens the menu, a swipe to
	// the left closes it again.
	let swipeStart: { x: number; y: number } | null = null;
	function touchStart(e: TouchEvent) {
		const p = e.touches[0];
		swipeStart =
			phone && (menuOpen || p.clientX <= 24) && !dialog ? { x: p.clientX, y: p.clientY } : null;
	}
	function touchMove(e: TouchEvent) {
		if (!swipeStart) return;
		const p = e.touches[0];
		const dx = p.clientX - swipeStart.x;
		if (Math.abs(p.clientY - swipeStart.y) > Math.abs(dx)) swipeStart = null;
		else if (!menuOpen && dx > 50) {
			swipeStart = null;
			menuOpen = true;
		} else if (menuOpen && dx < -50) {
			swipeStart = null;
			menuOpen = false;
		}
	}

	// Read state (PLAN 48): while a chat is on screen the page says so every
	// 15 seconds. The NAS then counts it as read on all devices and sends no
	// push message for it; notifications of it on this device go away.
	function seen(id: string, visible: boolean) {
		void apiFetch(`/agent/chats/${id}/seen`, {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({ visible }),
			keepalive: !visible,
		}).catch(() => {});
		if (visible) {
			const listed = chats.find((c) => c.id === id);
			if (listed?.unread) listed.unread = false;
			void clearAgentNotifications(id);
		}
	}
	$effect(() => {
		const id = current?.id;
		if (!id) return;
		const visible = () => document.visibilityState === 'visible';
		const ping = () => seen(id, visible());
		ping();
		const timer = setInterval(() => visible() && seen(id, true), 15000);
		document.addEventListener('visibilitychange', ping);
		return () => {
			clearInterval(timer);
			document.removeEventListener('visibilitychange', ping);
			seen(id, false);
		};
	});

	async function decide(p: Pending, approve: boolean) {
		if (!current) return;
		deciding = true;
		error = '';
		try {
			await api.post(`/agent/chats/${current.id}/decide`, { id: p.id, approve });
			current.pending = null;
			watch(current.id);
		} catch (e) {
			error = apiErrorText(e);
		} finally {
			deciding = false;
		}
	}

	async function stop(id = current?.id) {
		if (!id) return;
		try {
			await api.post(`/agent/chats/${id}/stop`);
		} catch (e) {
			error = apiErrorText(e);
		}
	}

	async function patch(chat: ChatInfo, body: Record<string, unknown>) {
		try {
			await api.patch(`/agent/chats/${chat.id}`, body);
			await loadLists();
			if (current?.id === chat.id) current = await api.get<Chat>(`/agent/chats/${chat.id}`);
		} catch (e) {
			error = apiErrorText(e);
		}
	}
	async function remove(chat: ChatInfo) {
		try {
			await api.delete(`/agent/chats/${chat.id}`);
			if (current?.id === chat.id) newChat();
			await loadLists();
		} catch (e) {
			error = apiErrorText(e);
		}
		dialog = null;
	}

	const suggestions = $derived<Suggestion[]>([
		{ title: t('agent.suggHealth'), sub: t('agent.suggHealthSub'), text: t('agent.suggestHealth') },
		{ title: t('agent.suggFind'), sub: t('agent.suggFindSub'), text: t('agent.suggestFind') },
		{
			title: t('agent.suggDocument'),
			sub: t('agent.suggDocumentSub'),
			text: t('agent.suggestDocument'),
		},
		{ title: t('agent.suggBackup'), sub: t('agent.suggBackupSub'), text: t('agent.suggestBackup') },
	]);
	const currentInfo = $derived(current ? chats.find((c) => c.id === current!.id) : undefined);
	const currentProject = $derived(projects.find((p) => p.id === (current?.project ?? project)));
	const menuProps = $derived({
		chats,
		projects,
		current: current?.id,
		project,
		host: session.data?.hostname ?? '',
		admin: status?.admin ?? false,
		onopen: (id: string) => open(id),
		onnew: () => newChat(''),
		onproject: (id: string) => {
			project = id;
			// On the phone the menu stays open and shows the project's chats.
			if (!phone && (!id || current?.project !== id)) newChat(id);
		},
		onnewproject: () => (dialog = { kind: 'project', project: null }),
		oneditproject: (p: Project) => (dialog = { kind: 'project', project: p }),
		onknowledge: () => (dialog = { kind: 'knowledge', tab: 'memory' }),
		onlook: () => (dialog = { kind: 'look' }),
		ontitanos: phone || installed ? toTitanOS : undefined,
	});
</script>

<svelte:window ontouchstart={touchStart} ontouchmove={touchMove} />

{#snippet chatMenu(info: ChatInfo)}
	<span class="more">
		<Dropdown label={t('ui.more')} icon="more" align="right">
			{#snippet children(close)}
				{#if phone}
					<button
						type="button"
						role="menuitem"
						onclick={() => {
							close();
							void patch(info, { pinned: !info.pinned });
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
						dialog = { kind: 'rename', chat: info, title: info.title };
					}}><Icon name="edit" size={16} /><span>{t('agent.rename')}</span></button
				>
				<button
					type="button"
					role="menuitem"
					onclick={() => {
						close();
						dialog = { kind: 'move', chat: info };
					}}><Icon name="folder" size={16} /><span>{t('agent.moveToProject')}</span></button
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
				<button
					type="button"
					role="menuitem"
					class="danger"
					disabled={busy}
					onclick={() => {
						close();
						dialog = { kind: 'delete', chat: info };
					}}><Icon name="trash" size={16} /><span>{t('agent.delete')}</span></button
				>
			{/snippet}
		</Dropdown>
	</span>
{/snippet}

<div
	class="agent"
	class:app
	class:phone
	class:glass={look.glass}
	class:glow={look.glow && !calm}
	class:calm
	data-style={look.style}
	data-bg={look.background}
	style={lookStyle(look)}
>
	{#if error}<div class="notice-error" role="alert">{error}</div>{/if}

	{#if !status}
		<p class="muted pad">{t('agent.loading')}</p>
	{:else if !status.enabled}
		<div class="tile empty-state">
			<Icon name="sparkle" size={32} />
			<h1>{t('agent.offTitle')}</h1>
			<p class="muted">{status.admin ? t('agent.offAdmin') : t('agent.offUser')}</p>
			{#if status.admin}<a class="btn btn-primary" href="/settings/agent">{t('agent.toSettings')}</a
				>{/if}
			{#if phone || installed}<button type="button" class="btn" onclick={toTitanOS}
					>{t('agent.toTitanOS')}</button
				>{/if}
		</div>
	{:else if !status.allowed}
		<div class="tile empty-state">
			<h1>{t('agent.notAllowedTitle')}</h1>
			<p class="muted">{t('agent.notAllowed')}</p>
			{#if phone || installed}<button type="button" class="btn" onclick={toTitanOS}
					>{t('agent.toTitanOS')}</button
				>{/if}
		</div>
	{:else if !status.volume}
		<div class="tile empty-state">
			<h1>{t('agent.noVolumeTitle')}</h1>
			<p class="muted">{t('agent.noVolume')}</p>
			{#if phone || installed}<button type="button" class="btn" onclick={toTitanOS}
					>{t('agent.toTitanOS')}</button
				>{/if}
		</div>
	{:else}
		<div class="layout">
			{#if !phone}
				<aside class="side tile"><AgentMenu {...menuProps} /></aside>
			{/if}
			<section class="main tile" aria-label={current?.title ?? t('agent.newChat')}>
				<header class="top">
					<div class="left">
						{#if phone}
							<button
								type="button"
								class="icon-btn"
								aria-label={t('agent.menuOpen')}
								aria-haspopup="dialog"
								aria-expanded={menuOpen}
								onclick={() => (menuOpen = true)}
								><Icon
									name="menu"
									size={20}
								/>{#if chats.some((c) => c.unread || c.state === 'waiting')}<span
										class="badge"
										aria-hidden="true"
									></span>{/if}</button
							>
						{/if}
					</div>
					<div class="mid">
						{#if current}
							<h1>{current.title}</h1>
							<span class="sub">
								{#if currentProject}<span class="proj"
										>{currentProject.icon ?? ''} {currentProject.name} ·</span
									>{/if}
								<ModelPicker models={status.models} bind:model bind:think small {calm} />
							</span>
						{:else}
							<ModelPicker models={status.models} bind:model bind:think {calm} />
						{/if}
					</div>
					<div class="right">
						{#if currentInfo && !phone}
							<button
								type="button"
								class="icon-btn"
								class:on={currentInfo.pinned}
								aria-label={currentInfo.pinned ? t('agent.unpin') : t('agent.pin')}
								aria-pressed={currentInfo.pinned}
								title={currentInfo.pinned ? t('agent.unpin') : t('agent.pin')}
								onclick={() => patch(currentInfo, { pinned: !currentInfo.pinned })}
								><Icon name="pin" size={17} /></button
							>
						{/if}
						{#if !phone && !installed}
							<button
								type="button"
								class="icon-btn"
								aria-label={agentFull.on ? t('agent.withMenu') : t('agent.fullScreen')}
								title={agentFull.on ? t('agent.withMenu') : t('agent.fullScreen')}
								onclick={() => setAgentFull(!agentFull.on)}
								><Icon name={agentFull.on ? 'sidebar' : 'maximize'} size={17} /></button
							>
						{/if}
						{#if currentInfo}{@render chatMenu(currentInfo)}{/if}
						{#if phone || current}
							<button
								type="button"
								class="icon-btn"
								aria-label={t('agent.newChat')}
								title={t('agent.newChat')}
								onclick={() => newChat(phone ? '' : project)}><Icon name="edit" size={18} /></button
							>
						{/if}
					</div>
				</header>
				{#if status.reachable === false}
					<p class="notice-warn">{t('agent.ollamaDownLong', { error: status.error ?? '' })}</p>
				{:else if usable.length === 0}
					<p class="notice-warn">{t('agent.noToolModel')}</p>
				{:else if contextNow && contextNow < 16384}
					<p class="notice-warn">{t('agent.contextSmall', { tokens: tokens(contextNow) })}</p>
				{/if}
				{#if current}
					<ChatView
						chat={current}
						live={look.typing ? live : { ...live, content: '' }}
						{busy}
						{deciding}
						ondecide={decide}
						onchoice={(c) => send(c)}
						onretry={() => send(t('agent.continueMessage'))}
						onundo={undo}
					/>
				{:else}
					<AgentStart
						{user}
						{chats}
						{projects}
						{project}
						{suggestions}
						onopen={(id) => open(id)}
						onproject={(p) => (dialog = { kind: 'sheet', project: p })}
						onnewproject={() => (dialog = { kind: 'project', project: null })}
						onstop={(c) => stop(c.id)}
						onfill={(text) => composer?.fill(text)}
					/>
				{/if}
				<div class="dock">
					<Composer
						bind:this={composer}
						{busy}
						bind:attachments
						disabled={!!current?.pending}
						placeholder={current?.pending ? t('agent.inputPending') : t('agent.inputShort')}
						onsend={send}
						onstop={() => stop()}
						onpicknas={() => (dialog = { kind: 'nas' })}
						onerror={(m) => (error = m)}
					/>
				</div>
			</section>
		</div>
		{#if phone && menuOpen}
			<button
				type="button"
				class="scrim"
				aria-label={t('common.close')}
				transition:fade={{ duration: calm ? 0 : 200 }}
				onclick={() => (menuOpen = false)}
			></button>
			<aside
				class="drawer"
				aria-label={t('agent.menu')}
				transition:fly={{ x: -340, duration: calm ? 0 : 260, opacity: 1 }}
			>
				<AgentMenu {...menuProps} />
			</aside>
		{/if}
	{/if}
</div>

{#if dialog?.kind === 'project'}
	<ProjectDialog
		project={dialog.project}
		{locations}
		onclose={() => (dialog = null)}
		onsaved={async (p) => {
			dialog = null;
			await loadLists();
			if (p) project = p.id;
			else project = '';
		}}
	/>
{:else if dialog?.kind === 'knowledge'}
	<KnowledgeDialog
		tab={dialog.tab}
		admin={status?.admin ?? false}
		{projects}
		onclose={() => (dialog = null)}
		onopenchat={(id) => {
			dialog = null;
			void open(id);
		}}
	/>
{:else if dialog?.kind === 'look'}
	<LookDialog bind:look onclose={() => (dialog = null)} />
{:else if dialog?.kind === 'move'}
	{@const chat = dialog.chat}
	<Dialog title={t('agent.moveToProject')} onclose={() => (dialog = null)}>
		<div class="choose">
			<button
				type="button"
				class="btn"
				onclick={() => {
					dialog = null;
					void patch(chat, { project: '' });
				}}>{t('agent.noProject')}</button
			>
			{#each projects as p (p.id)}
				<button
					type="button"
					class="btn"
					class:btn-primary={chat.project === p.id}
					onclick={() => {
						dialog = null;
						void patch(chat, { project: p.id });
					}}>{p.icon ?? ''} {p.name}</button
				>
			{/each}
		</div>
	</Dialog>
{:else if dialog?.kind === 'rename'}
	{@const d = dialog}
	<Dialog title={t('agent.rename')} onclose={() => (dialog = null)}>
		<form
			class="choose"
			onsubmit={(e) => {
				e.preventDefault();
				dialog = null;
				void patch(d.chat, { title: d.title });
			}}
		>
			<input
				class="field"
				bind:value={d.title}
				maxlength="200"
				required
				aria-label={t('agent.rename')}
			/>
			<div class="row">
				<button type="button" class="btn" onclick={() => (dialog = null)}
					>{t('common.cancel')}</button
				>
				<button type="submit" class="btn btn-primary">{t('common.save')}</button>
			</div>
		</form>
	</Dialog>
{:else if dialog?.kind === 'delete'}
	{@const chat = dialog.chat}
	<Dialog title={t('agent.deleteTitle')} onclose={() => (dialog = null)}>
		<p>{t('agent.deleteText', { title: chat.title })}</p>
		<div class="dialog-actions">
			<button type="button" class="btn btn-danger" onclick={() => remove(chat)}
				>{t('common.delete')}</button
			>
			<button type="button" class="btn" onclick={() => (dialog = null)}>{t('common.cancel')}</button
			>
		</div>
	</Dialog>
{:else if dialog?.kind === 'nas'}
	<NasPicker
		onclose={() => (dialog = null)}
		onpick={(refs) => {
			dialog = null;
			const add = refs
				.filter((ref) => !attachments.some((a) => a.ref === ref))
				.map((ref) => ({ name: ref.slice(ref.indexOf(':') + 1), ref }));
			attachments = [...attachments, ...add];
		}}
	/>
{:else if dialog?.kind === 'sheet'}
	{@const p = dialog.project}
	<ProjectSheet
		project={p}
		{chats}
		onclose={() => (dialog = null)}
		onopen={(id) => {
			dialog = null;
			void open(id);
		}}
		onedit={() => (dialog = { kind: 'project', project: p })}
		onnew={() => {
			dialog = null;
			newChat(p.id);
			project = p.id;
		}}
	/>
{/if}

<style>
	.agent {
		--ag-orb: conic-gradient(
			from 210deg,
			var(--accent),
			var(--ag-accent-2, var(--accent)),
			color-mix(in srgb, var(--accent) 45%, #fff),
			var(--accent)
		);
		position: relative;
		isolation: isolate;
		display: flex;
		flex-direction: column;
		gap: var(--gap);
		height: calc(
			100dvh - var(--header-height, 60px) - var(--status-height, 34px) - 2 * var(--page-pad)
		);
		min-height: 420px;
	}
	.agent[data-bg]:not([data-bg='none'])::before {
		content: '';
		position: absolute;
		inset: 0;
		z-index: -1;
		pointer-events: none;
		border-radius: var(--radius-tile);
	}
	.agent[data-bg='aurora']::before {
		background:
			radial-gradient(
				60% 50% at 25% 15%,
				color-mix(in srgb, var(--accent) 30%, transparent),
				transparent 70%
			),
			radial-gradient(
				50% 40% at 85% 20%,
				color-mix(in srgb, var(--ag-accent-2, var(--accent)) 25%, transparent),
				transparent 70%
			);
	}
	.agent[data-bg='ocean']::before {
		background: linear-gradient(
			170deg,
			color-mix(in srgb, var(--accent) 18%, transparent),
			transparent 60%
		);
	}
	.agent[data-bg='forest']::before {
		background: radial-gradient(
			70% 60% at 50% 100%,
			color-mix(in srgb, var(--ag-accent-2, var(--accent)) 22%, transparent),
			transparent 70%
		);
	}
	.agent.app {
		height: 100dvh;
		gap: 0;
	}
	.layout {
		flex: 1;
		min-height: 0;
		display: grid;
		grid-template-columns: 280px minmax(0, 1fr);
		gap: var(--gap);
	}
	.agent.app .layout {
		padding: 12px;
	}
	.agent.phone .layout {
		grid-template-columns: minmax(0, 1fr);
		padding: 0;
	}
	.side {
		padding: 0;
		overflow: hidden;
		min-height: 0;
	}
	.agent.glass .side,
	.agent.glass .main {
		background: color-mix(in srgb, var(--surface) 55%, transparent);
		backdrop-filter: blur(26px) saturate(160%);
		border-color: var(--glass-border, var(--border));
	}
	.main {
		display: flex;
		flex-direction: column;
		min-height: 0;
		padding: 0;
		overflow: hidden;
	}
	.agent.phone .main {
		border: 0;
		border-radius: 0;
		background: transparent;
		backdrop-filter: none;
		box-shadow: none;
	}
	.top {
		display: grid;
		grid-template-columns: minmax(0, 1fr) auto minmax(0, 1fr);
		align-items: center;
		gap: 8px;
		min-height: 58px;
		padding: 8px 12px;
		border-bottom: 1px solid color-mix(in srgb, var(--border) 60%, transparent);
	}
	.agent.phone .top {
		padding: calc(env(safe-area-inset-top, 0px) + 6px) 8px 6px;
		border-bottom: 0;
		grid-template-columns: 44px minmax(0, 1fr) auto;
	}
	.left,
	.right {
		display: flex;
		align-items: center;
		gap: 4px;
	}
	.right {
		justify-content: flex-end;
	}
	.mid {
		min-width: 0;
		max-width: min(560px, 60vw);
		display: flex;
		flex-direction: column;
		align-items: center;
		text-align: center;
	}
	.agent.phone .mid {
		max-width: none;
	}
	.mid h1 {
		margin: 0;
		max-width: 100%;
		font-size: var(--text-md);
		font-weight: 600;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.sub {
		display: flex;
		align-items: center;
		justify-content: center;
		gap: 2px;
		max-width: 100%;
		font-size: var(--text-xs);
		color: var(--muted);
		white-space: nowrap;
	}
	.proj {
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.icon-btn,
	.more :global(.btn) {
		position: relative;
		width: 40px;
		height: 40px;
		min-height: 40px;
		padding: 0;
		border-radius: 50%;
		display: grid;
		place-items: center;
		border: 0;
		background: none;
		color: var(--text);
		cursor: pointer;
		flex: none;
		transition: background 0.15s ease;
	}
	.icon-btn:hover,
	.more :global(.btn:hover) {
		background: color-mix(in srgb, var(--text) 8%, transparent);
	}
	.icon-btn.on {
		color: var(--accent);
	}
	.badge {
		position: absolute;
		top: 8px;
		right: 8px;
		width: 8px;
		height: 8px;
		border-radius: 50%;
		background: var(--accent);
		box-shadow: 0 0 8px var(--accent);
	}
	.notice-warn {
		margin: var(--space-2) var(--space-4) 0;
	}
	.dock {
		padding: var(--space-3) var(--space-5) var(--space-4);
	}
	.dock :global(.composer) {
		max-width: 800px;
		margin: 0 auto;
	}
	.agent.phone .dock {
		padding: 6px 10px calc(env(safe-area-inset-bottom, 0px) + 10px);
	}
	.scrim {
		position: fixed;
		inset: 0;
		z-index: 50;
		border: 0;
		padding: 0;
		background: rgb(0 0 0 / 0.5);
		backdrop-filter: blur(3px);
		cursor: default;
	}
	.drawer {
		position: fixed;
		z-index: 51;
		top: 0;
		bottom: 0;
		left: 0;
		width: min(84%, 340px);
		padding-top: env(safe-area-inset-top, 0px);
		background: color-mix(in srgb, var(--surface) 78%, transparent);
		backdrop-filter: blur(28px) saturate(170%);
		border-right: 1px solid var(--glass-border, var(--border));
		box-shadow: 20px 0 50px -20px rgb(0 0 0 / 0.7);
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
	.choose {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}
	.choose .row {
		display: flex;
		gap: var(--space-2);
		justify-content: flex-end;
	}
	.dialog-actions {
		display: flex;
		gap: var(--space-2);
		flex-wrap: wrap;
		margin-top: var(--space-3);
	}
</style>
