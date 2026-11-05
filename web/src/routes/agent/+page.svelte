<script lang="ts">
	// The KI-Agent (PLAN 44): projects and chats on the left, the chat on the
	// right, on the phone a start page with the running task, project tiles
	// and the latest chats. Installed from here it is an app of its own
	// (/agent.webmanifest, start /agent?app=1, no TitanOS frame).
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import Dialog from '#lib/components/Dialog.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import AgentHome from '#lib/components/agent/AgentHome.svelte';
	import ChatList from '#lib/components/agent/ChatList.svelte';
	import ChatView from '#lib/components/agent/ChatView.svelte';
	import Composer from '#lib/components/agent/Composer.svelte';
	import KnowledgeDialog from '#lib/components/agent/KnowledgeDialog.svelte';
	import LookDialog from '#lib/components/agent/LookDialog.svelte';
	import ProjectDialog from '#lib/components/agent/ProjectDialog.svelte';
	import ProjectSheet from '#lib/components/agent/ProjectSheet.svelte';
	import { api, apiFetch, responseError } from '#lib/api.ts';
	import { agentAppMode } from '#lib/agent-app.ts';
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
		| null
	>(null);
	let composer = $state<{ fill: (text: string) => void }>();
	let attachments = $state<{ name: string; file?: File; ref?: string }[]>([]);
	let phone = $state(matchMedia('(max-width: 760px)').matches);
	let stream: AbortController | null = null;

	const app = $derived(agentAppMode(page.url));
	const user = $derived(session.data?.user?.name ?? '');
	const usable = $derived((status?.models ?? []).filter((m) => m.capabilities.includes('tools')));
	const contextNow = $derived(
		status?.context || status?.running?.find((r) => r.name === model)?.contextLength || 0,
	);

	$effect(() => {
		const query = matchMedia('(max-width: 760px)');
		const change = () => (phone = query.matches);
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

	// On the phone a swipe from the left edge goes back to the start.
	let swipeStart: { x: number; y: number } | null = null;
	function touchStart(e: TouchEvent) {
		const p = e.touches[0];
		swipeStart = view === 'chat' && p.clientX <= 24 ? { x: p.clientX, y: p.clientY } : null;
	}
	function touchMove(e: TouchEvent) {
		if (!swipeStart) return;
		const p = e.touches[0];
		if (Math.abs(p.clientY - swipeStart.y) > Math.abs(p.clientX - swipeStart.x)) swipeStart = null;
		else if (p.clientX - swipeStart.x > 60) {
			swipeStart = null;
			newChat(current?.project ?? '');
		}
	}

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

	const suggestions = $derived([
		t('agent.suggestHealth'),
		t('agent.suggestFind'),
		t('agent.suggestDocument'),
		t('agent.suggestBackup'),
	]);
	const currentInfo = $derived(current ? chats.find((c) => c.id === current!.id) : undefined);
	const currentProject = $derived(projects.find((p) => p.id === (current?.project ?? project)));
	const view = $derived(phone ? (current ? 'chat' : 'home') : 'desktop');
</script>

<svelte:window ontouchstart={touchStart} ontouchmove={touchMove} />

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
	{#if app && view !== 'chat'}
		<header class="appbar">
			<Icon name="sparkle" size={20} />
			<strong>{t('agent.appName')}</strong>
			<span class="muted host">{session.data?.hostname}</span>
			<span class="grow"></span>
			{#if status?.reachable === false}<span class="chip warn">{t('agent.ollamaDown')}</span>{/if}
			<button
				type="button"
				class="icon-btn"
				aria-label={t('agent.look')}
				onclick={() => (dialog = { kind: 'look' })}><Icon name="palette" size={18} /></button
			>
			<a class="icon-btn" href="/?" aria-label={t('agent.toTitanOS')} title={t('agent.toTitanOS')}
				><Icon name="external" size={18} /></a
			>
		</header>
	{/if}

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
		</div>
	{:else if !status.allowed}
		<div class="tile empty-state">
			<h1>{t('agent.notAllowedTitle')}</h1>
			<p class="muted">{t('agent.notAllowed')}</p>
		</div>
	{:else if !status.volume}
		<div class="tile empty-state">
			<h1>{t('agent.noVolumeTitle')}</h1>
			<p class="muted">{t('agent.noVolume')}</p>
		</div>
	{:else if view === 'home'}
		<div class="phone-home">
			{#if !app}
				<div class="phone-tools">
					<button
						type="button"
						class="icon-btn"
						aria-label={t('agent.knowledge')}
						onclick={() => (dialog = { kind: 'knowledge', tab: 'memory' })}
						><Icon name="bulb" size={18} /></button
					>
					<button
						type="button"
						class="icon-btn"
						aria-label={t('agent.look')}
						onclick={() => (dialog = { kind: 'look' })}><Icon name="palette" size={18} /></button
					>
				</div>
			{/if}
			<AgentHome
				{user}
				{chats}
				{projects}
				onopen={(id) => open(id)}
				onproject={(p) => (dialog = { kind: 'sheet', project: p })}
				onnewproject={() => (dialog = { kind: 'project', project: null })}
				onpin={(c) => patch(c, { pinned: !c.pinned })}
				onmove={(c) => (dialog = { kind: 'move', chat: c })}
				ondelete={(c) => (dialog = { kind: 'delete', chat: c })}
				onstop={(c) => stop(c.id)}
			/>
			{#if app}
				<button
					type="button"
					class="btn knowledge-link"
					onclick={() => (dialog = { kind: 'knowledge', tab: 'memory' })}
					><Icon name="bulb" size={16} /> {t('agent.knowledge')}</button
				>
			{/if}
		</div>
		<div class="dock">
			<div class="sugg">
				{#each suggestions as s (s)}<button type="button" onclick={() => composer?.fill(s)}
						>{s}</button
					>{/each}
			</div>
			<Composer
				bind:this={composer}
				models={status.models}
				bind:model
				bind:think
				bind:attachments
				onsend={send}
			/>
		</div>
	{:else}
		<div class="layout" class:single={view === 'chat'}>
			{#if view === 'desktop'}
				<ChatList
					{chats}
					{projects}
					current={current?.id}
					{project}
					onopen={(id) => open(id)}
					onnew={() => newChat()}
					onproject={(id) => {
						project = id;
						if (!id || current?.project !== id) newChat(id);
					}}
					onnewproject={() => (dialog = { kind: 'project', project: null })}
					oneditproject={(p) => (dialog = { kind: 'project', project: p })}
				/>
			{/if}
			<section class="tile chat" aria-label={current?.title ?? t('agent.newChat')}>
				{#if busy && view === 'chat'}
					<div class="pill" role="status">
						<span class="pill-dot" aria-hidden="true"></span>{t('agent.working')}
						<button type="button" onclick={() => stop()} aria-label={t('agent.stop')}
							><Icon name="stop" size={10} /></button
						>
					</div>
				{/if}
				<header class="chat-head">
					{#if view === 'chat'}
						<button
							type="button"
							class="icon-btn"
							aria-label={t('agent.back')}
							onclick={() => newChat(current?.project ?? '')}
							><Icon name="chevronLeft" size={20} /></button
						>
					{/if}
					<div class="title">
						<h1>
							{current?.title ??
								(currentProject
									? t('agent.newChatIn', { name: currentProject.name })
									: t('agent.newChat'))}
						</h1>
						<span class="muted sub">
							{#if currentProject}{currentProject.icon ?? ''} {currentProject.name} ·
							{/if}{model}{contextNow ? ' · ' + tokens(contextNow) : ''}
						</span>
					</div>
					{#if currentInfo}
						<button
							type="button"
							class="icon-btn wide-only"
							aria-label={currentInfo.pinned ? t('agent.unpin') : t('agent.pin')}
							title={currentInfo.pinned ? t('agent.unpin') : t('agent.pin')}
							onclick={() => patch(currentInfo, { pinned: !currentInfo.pinned })}
							><Icon name="pin" size={17} /></button
						>
						<button
							type="button"
							class="icon-btn wide-only"
							aria-label={t('agent.rename')}
							title={t('agent.rename')}
							onclick={() =>
								(dialog = { kind: 'rename', chat: currentInfo, title: currentInfo.title })}
							><Icon name="edit" size={17} /></button
						>
						<button
							type="button"
							class="icon-btn wide-only"
							aria-label={t('agent.moveToProject')}
							title={t('agent.moveToProject')}
							onclick={() => (dialog = { kind: 'move', chat: currentInfo })}
							><Icon name="folder" size={17} /></button
						>
						<a
							class="icon-btn wide-only"
							href={`/api/v1/agent/chats/${currentInfo.id}/export`}
							download
							aria-label={t('agent.exportMd')}
							title={t('agent.exportMd')}><Icon name="download" size={17} /></a
						>
						<a
							class="icon-btn wide-only"
							href={`/api/v1/agent/chats/${currentInfo.id}/export?format=pdf`}
							download
							aria-label={t('agent.exportPdf')}
							title={t('agent.exportPdf')}><Icon name="fileText" size={17} /></a
						>
						<button
							type="button"
							class="icon-btn"
							aria-label={t('agent.delete')}
							title={t('agent.delete')}
							disabled={busy}
							onclick={() => (dialog = { kind: 'delete', chat: currentInfo })}
							><Icon name="trash" size={17} /></button
						>
					{/if}
					{#if view === 'desktop'}
						<span class="sep"></span>
						<button
							type="button"
							class="icon-btn"
							aria-label={t('agent.knowledge')}
							title={t('agent.knowledge')}
							onclick={() => (dialog = { kind: 'knowledge', tab: 'memory' })}
							><Icon name="bulb" size={17} /></button
						>
						<button
							type="button"
							class="icon-btn"
							aria-label={t('agent.look')}
							title={t('agent.look')}
							onclick={() => (dialog = { kind: 'look' })}><Icon name="palette" size={17} /></button
						>
					{/if}
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
					<div class="welcome">
						<Icon name="sparkle" size={36} />
						<h2>{t('agent.welcome')}</h2>
						<p class="muted">{t('agent.welcomeText')}</p>
						<div class="sugg wrap">
							{#each suggestions as s (s)}<button type="button" onclick={() => composer?.fill(s)}
									>{s}</button
								>{/each}
						</div>
					</div>
				{/if}
				<div class="chat-dock">
					<Composer
						bind:this={composer}
						models={status.models}
						bind:model
						bind:think
						{busy}
						bind:attachments
						disabled={!!current?.pending}
						placeholder={current?.pending ? t('agent.inputPending') : ''}
						onsend={send}
						onstop={() => stop()}
					/>
				</div>
			</section>
		</div>
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
		display: flex;
		flex-direction: column;
		gap: var(--gap);
		height: calc(
			100dvh - var(--header-height, 60px) - var(--status-height, 34px) - 2 * var(--page-pad)
		);
		min-height: 420px;
	}
	.agent {
		position: relative;
		isolation: isolate;
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
	.agent.phone {
		height: calc(
			100dvh - var(--header-height, 60px) - var(--status-height, 34px) - 40px -
				var(--fab-clear, 0px)
		);
	}
	.agent.app.phone {
		height: 100dvh;
	}
	.appbar {
		display: flex;
		align-items: center;
		gap: 10px;
		padding: calc(env(safe-area-inset-top, 0px) + 10px) 16px 10px;
		border-bottom: 1px solid var(--border);
		background: var(--header);
		color: var(--text);
	}
	.appbar :global(svg) {
		color: var(--accent);
	}
	.appbar .host {
		font-size: var(--text-sm);
	}
	.grow {
		flex: 1;
	}
	.icon-btn {
		width: 34px;
		height: 34px;
		border-radius: 50%;
		display: grid;
		place-items: center;
		border: 0;
		background: color-mix(in srgb, var(--text) 6%, transparent);
		color: var(--text);
		cursor: pointer;
		flex: none;
	}
	.icon-btn:disabled {
		opacity: 0.4;
		cursor: default;
	}
	.layout {
		flex: 1;
		min-height: 0;
		display: grid;
		grid-template-columns: 270px minmax(0, 1fr);
		gap: var(--gap);
	}
	.layout.single {
		grid-template-columns: minmax(0, 1fr);
	}
	.agent.app .layout {
		padding: 12px;
	}
	.agent.app.phone .layout {
		padding: 0;
	}
	.chat {
		display: flex;
		flex-direction: column;
		min-height: 0;
		padding: 0;
		overflow: hidden;
	}
	.pill {
		align-self: center;
		margin-top: calc(env(safe-area-inset-top, 0px) + 6px);
		display: flex;
		align-items: center;
		gap: 8px;
		padding: 5px 6px 5px 12px;
		border-radius: 999px;
		background: var(--text);
		color: var(--bg);
		font-size: var(--text-sm);
	}
	.agent.glow .pill {
		box-shadow: 0 0 22px color-mix(in srgb, var(--accent) 55%, transparent);
	}
	.pill-dot {
		width: 8px;
		height: 8px;
		border-radius: 50%;
		background: var(--accent);
	}
	.pill button {
		width: 22px;
		height: 22px;
		border-radius: 50%;
		border: 0;
		display: grid;
		place-items: center;
		background: color-mix(in srgb, var(--bg) 20%, transparent);
		color: var(--bg);
		cursor: pointer;
	}
	.chat-head {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: 10px var(--space-4);
		border-bottom: 1px solid var(--border);
	}
	.agent.app.phone .chat-head {
		padding-top: calc(env(safe-area-inset-top, 0px) + 10px);
	}
	.title {
		flex: 1;
		min-width: 0;
		display: flex;
		flex-direction: column;
	}
	.title h1 {
		margin: 0;
		font-size: var(--text-lg);
		font-weight: 600;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.title .sub {
		font-size: var(--text-xs);
	}
	.sep {
		width: 1px;
		height: 22px;
		background: var(--border);
	}
	.notice-warn {
		margin: var(--space-2) var(--space-4) 0;
	}
	.welcome {
		flex: 1;
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		text-align: center;
		gap: var(--space-2);
		padding: var(--space-5);
	}
	.welcome :global(svg) {
		color: var(--accent);
	}
	.welcome h2 {
		margin: 0;
		font-size: var(--text-xl);
	}
	.welcome p {
		max-width: 520px;
	}
	.chat-dock {
		padding: var(--space-3) var(--space-5) var(--space-4);
	}
	.chat-dock :global(.composer) {
		max-width: 800px;
		margin: 0 auto;
	}
	.sugg {
		display: flex;
		gap: 6px;
		overflow-x: auto;
		scrollbar-width: none;
	}
	.sugg.wrap {
		flex-wrap: wrap;
		justify-content: center;
		margin-top: var(--space-2);
	}
	.sugg button {
		flex: none;
		border-radius: 999px;
		padding: 6px 12px;
		font: inherit;
		font-size: var(--text-sm);
		border: 1px solid var(--border);
		background: var(--surface);
		color: var(--text);
		cursor: pointer;
	}
	.agent.glass .sugg button {
		background: color-mix(in srgb, var(--surface) 75%, transparent);
		backdrop-filter: blur(14px);
		border-color: var(--glass-border);
	}
	.phone-home {
		flex: 1;
		min-height: 0;
		overflow-y: auto;
		position: relative;
	}
	.agent.app .phone-home {
		padding: 12px 16px;
	}
	.phone-tools {
		position: absolute;
		top: 4px;
		right: 0;
		display: flex;
		gap: 6px;
	}
	.knowledge-link {
		margin-top: var(--space-2);
	}
	.dock {
		display: flex;
		flex-direction: column;
		gap: 8px;
		padding-top: 6px;
	}
	.agent.app .dock {
		padding: 6px 10px calc(env(safe-area-inset-bottom, 0px) + 12px);
	}
	.agent.phone .chat-dock {
		padding: 8px 10px calc(env(safe-area-inset-bottom, 0px) + 10px);
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
	@media (max-width: 760px) {
		.chat-head {
			padding: 8px 10px;
		}
		.wide-only {
			display: none;
		}
	}
</style>
