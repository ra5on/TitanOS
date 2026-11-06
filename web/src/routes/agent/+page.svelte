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
	import AgentSettings from '#lib/components/agent/AgentSettings.svelte';
	import InboxPanel from '#lib/components/agent/InboxPanel.svelte';
	import ProjectDialog from '#lib/components/agent/ProjectDialog.svelte';
	import ProjectSheet from '#lib/components/agent/ProjectSheet.svelte';
	import { api, apiFetch, responseError } from '#lib/api.ts';
	import { agentAppMode } from '#lib/agent-app.ts';
	import { agentFull, setAgentFull } from '#lib/agent-full.svelte.ts';
	import { clearAgentNotifications } from '#lib/web-push.ts';
	import { canSpeak, speak, speechPrefs, stopSpeaking } from '#lib/agent-speech.svelte.ts';
	import { apiErrorText, i18n, t } from '#lib/i18n/index.svelte.ts';
	import { pageTitle } from '#lib/page-title.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import {
		accountLook,
		defaultLook,
		DEVICE_LOOK,
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
		type Swipe,
		type NasHint,
	} from '#lib/agent.ts';

	const LOOK_KEY = 'titanos.agent.look';

	let status = $state<Status | null>(null);
	let chats = $state<ChatInfo[]>([]);
	let projects = $state<Project[]>([]);
	let locations = $state<Location[]>([]);
	let current = $state<Chat | null>(null);
	/** The author when the open chat belongs to another account (read-only). */
	let readOnlyBy = $state('');
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
		| {
				kind: 'knowledge';
				tab: 'memory' | 'skills' | 'schedules' | 'map' | 'me' | 'log';
				only?: boolean;
		  }
		| { kind: 'look' }
		| { kind: 'settings'; section?: 'list' | 'speech' | 'look' | 'models' | 'initiative' | 'mind' }
		| { kind: 'actions'; chat: ChatInfo }
		| { kind: 'inbox' }
		| { kind: 'move'; chat: ChatInfo }
		| { kind: 'rename'; chat: ChatInfo; title: string }
		| { kind: 'delete'; chat: ChatInfo }
		| { kind: 'deleteAll' }
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
	// PLAN 53: the look and the app settings belong to the account and live on
	// the NAS; only device things (vibration, screen awake, text size) stay
	// in this browser. The local copy shows the page before the answer comes.
	let prefsLoaded = false;
	let prefsTimer: ReturnType<typeof setTimeout> | undefined;
	$effect(() => {
		api
			.get<Record<string, unknown>>('/agent/prefs')
			// Saved only after a successful read, so a hiccup never overwrites
			// the account's settings with this device's copy.
			.then((saved) => {
				if (saved && Object.keys(saved).length) {
					const device = Object.fromEntries(DEVICE_LOOK.map((k) => [k, look[k]]));
					look = sanitizeLook({ ...look, ...saved, ...device });
				}
				prefsLoaded = true;
			})
			.catch(() => {});
	});
	$effect(() => {
		const value = JSON.stringify(look);
		try {
			localStorage.setItem(LOOK_KEY, value);
		} catch {
			// Only remembered while the page is open.
		}
		if (!prefsLoaded) return;
		const body = accountLook(look);
		clearTimeout(prefsTimer);
		prefsTimer = setTimeout(() => void api.put('/agent/prefs', body).catch(() => {}), 600);
	});
	$effect(() => {
		speechPrefs.rate = look.rate;
	});
	// Keep the screen on while the agent works (device setting, PLAN 53).
	$effect(() => {
		const working = look.keepAwake && chats.some((c) => c.state === 'running');
		const nav = navigator as Navigator & {
			wakeLock?: { request: (type: 'screen') => Promise<{ release: () => Promise<void> }> };
		};
		if (!working || !nav.wakeLock) return;
		let lock: { release: () => Promise<void> } | null = null;
		let gone = false;
		nav.wakeLock
			.request('screen')
			.then((l) => (gone ? void l.release() : (lock = l)))
			.catch(() => {});
		return () => {
			gone = true;
			void lock?.release().catch(() => {});
		};
	});
	const calm = $derived(
		look.calm ||
			look.motion !== 'full' ||
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
				if (id) void open(id, false, page.url.searchParams.get('author') ?? '');
				else if (!page.url.searchParams.has('ask') && !page.url.searchParams.has('attach')) {
					// PLAN 53: what opens first is a setting of the account.
					const saved = await api
						.get<{ start?: string }>('/agent/prefs')
						.catch(() => ({}) as { start?: string });
					const start = saved.start ?? look.start;
					const last = [...chats]
						.filter((c) => !c.author)
						.sort((a, b) => b.updated.localeCompare(a.updated))[0];
					if (start === 'last' && last) void open(last.id, false);
					else if (start === 'overview' && phone) menuOpen = true;
				}
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

	async function open(id: string, push = true, author = '') {
		menuOpen = false;
		stopSpeaking();
		stream?.abort();
		busy = false;
		live = { thinking: '', content: '', step: null };
		error = '';
		try {
			// Another account's chat in a shared project is only read (PLAN 51).
			const fetched = await api.get<Chat & { author?: string; readOnly?: boolean }>(
				`/agent/chats/${id}` + (author ? `?author=${encodeURIComponent(author)}` : ''),
			);
			readOnlyBy = fetched.readOnly ? (fetched.author ?? author) : '';
			current = fetched;
			if (readOnlyBy) {
				if (fetched.project) project = fetched.project;
				if (push) void goto(`/agent?chat=${id}&author=${encodeURIComponent(readOnlyBy)}`);
				return;
			}
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
		readOnlyBy = '';
		stopSpeaking();
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
			case 'reset':
				// The round is asked again; what streamed in does not count.
				live = { thinking: '', content: '', step: live.step };
				break;
			case 'plan':
				current.plan = e.plan;
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
			// Read the new answer aloud if wished (Darstellung, PLAN 51).
			if (look.read && current?.id === id && document.visibilityState === 'visible' && canSpeak()) {
				const last = [...fresh.entries]
					.reverse()
					.find((e) => e.role === 'assistant' && e.content?.trim());
				if (last) speak(last.id, last.content ?? '', i18n.locale);
			}
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
	// Swipes from the edge (PLAN 53, Einstellungen › Darstellung und Gesten):
	// from the left the chats, from the right the settings; back again.
	function touchStart(e: TouchEvent) {
		const p = e.touches[0];
		const edges = look.gestures && look.edge;
		const left = p.clientX <= 24;
		const right = p.clientX >= innerWidth - 24;
		swipeStart =
			phone && !dialog && (menuOpen || (edges && (left || right)))
				? { x: p.clientX, y: p.clientY }
				: null;
	}
	function touchMove(e: TouchEvent) {
		if (!swipeStart) return;
		const p = e.touches[0];
		const dx = p.clientX - swipeStart.x;
		if (Math.abs(p.clientY - swipeStart.y) > Math.abs(dx)) swipeStart = null;
		else if (!menuOpen && dx > 50) {
			swipeStart = null;
			menuOpen = true;
			buzz(8);
		} else if (!menuOpen && dx < -50) {
			swipeStart = null;
			dialog = { kind: 'settings' };
			buzz(8);
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
		if (!id || readOnlyBy) return;
		const visible = () => document.visibilityState === 'visible';
		const ping = () => seen(id, visible());
		ping();
		const timer = setInterval(() => visible() && seen(id, true), 15000);
		document.addEventListener('visibilitychange', ping);
		// iOS may close the app without a visibility change (swiped away).
		const gone = () => seen(id, false);
		window.addEventListener('pagehide', gone);
		return () => {
			clearInterval(timer);
			document.removeEventListener('visibilitychange', ping);
			window.removeEventListener('pagehide', gone);
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

	// "Alle Chats löschen" (PLAN 56): the own chats, pinned ones stay if wished.
	let keepPinned = $state(true);
	async function removeAll() {
		try {
			await api.delete(`/agent/chats${keepPinned ? '?keepPinned=true' : ''}`);
			if (current && !(keepPinned && current.pinned)) newChat();
			await loadLists();
		} catch (e) {
			error = apiErrorText(e);
		}
		dialog = null;
	}

	// PLAN 53: suggestions that fit the moment – warnings of the NAS first,
	// then by time of day, then a changing pick of the rest ("andere").
	// Open suggestions of the agent (PLAN 53), shown as a number in the menu.
	let inboxCount = $state(0);
	$effect(() => {
		api
			.get<{ items: unknown[] }>('/agent/inbox')
			.then((r) => (inboxCount = r.items?.length ?? 0))
			.catch(() => (inboxCount = 0));
	});
	/** Getting to know: a new chat with the question to look at the NAS. */
	function getToKnow() {
		dialog = null;
		newChat('');
		void send(t('agent.knowAsk'));
	}
	let hints = $state<NasHint[]>([]);
	const seed = Math.floor(Math.random() * 100);
	let shuffle = $state(0);
	$effect(() => {
		api
			.get<{ suggestions: NasHint[] }>('/agent/suggestions')
			.then((r) => (hints = r.suggestions ?? []))
			.catch(() => (hints = []));
	});
	const suggestions = $derived.by<Suggestion[]>(() => {
		const hour = new Date().getHours();
		const fromNas: Suggestion[] = hints.map((h) => ({
			title: h.title || h.kind,
			sub: t('agent.suggNasSub'),
			text: t('agent.suggestNas', { title: h.title || h.kind }),
			tag:
				h.severity === 'error'
					? t('agent.tagError')
					: h.kind === 'update_available'
						? t('agent.tagUpdate')
						: t('agent.tagWarning'),
			tone: h.kind === 'update_available' ? 'info' : 'warn',
			icon: h.kind.startsWith('backup')
				? 'archive'
				: h.kind.startsWith('update')
					? 'download'
					: h.kind.startsWith('volume') || h.kind.startsWith('disk')
						? 'disk'
						: 'bell',
			color: h.kind === 'update_available' ? '#3b82f6' : '#f59e0b',
		}));
		const timed: Suggestion[] =
			hour < 11
				? [
						{
							title: t('agent.suggNight'),
							sub: t('agent.suggNightSub'),
							text: t('agent.suggestNight'),
							tag: t('agent.tagMorning'),
							tone: 'ok',
							icon: 'clock',
							color: '#0ea5e9',
						},
					]
				: hour >= 18
					? [
							{
								title: t('agent.suggHome'),
								sub: t('agent.suggHomeSub'),
								text: t('agent.suggestHome'),
								tag: t('agent.tagEvening'),
								tone: 'ok',
								icon: 'home',
								color: '#10b981',
							},
						]
					: [];
		const pool: Suggestion[] = [
			{
				title: t('agent.suggHealth'),
				sub: t('agent.suggHealthSub'),
				text: t('agent.suggestHealth'),
				icon: 'shield',
				color: '#22c55e',
			},
			{
				title: t('agent.suggFind'),
				sub: t('agent.suggFindSub'),
				text: t('agent.suggestFind'),
				icon: 'search',
				color: '#0ea5e9',
			},
			{
				title: t('agent.suggDocument'),
				sub: t('agent.suggDocumentSub'),
				text: t('agent.suggestDocument'),
				icon: 'fileText',
				color: '#8b5cf6',
			},
			{
				title: t('agent.suggBackup'),
				sub: t('agent.suggBackupSub'),
				text: t('agent.suggestBackup'),
				icon: 'archive',
				color: '#f59e0b',
			},
			{
				title: t('agent.suggUpdates'),
				sub: t('agent.suggUpdatesSub'),
				text: t('agent.suggestUpdates'),
				icon: 'download',
				color: '#3b82f6',
			},
			{
				title: t('agent.suggClean'),
				sub: t('agent.suggCleanSub'),
				text: t('agent.suggestClean'),
				icon: 'disk',
				color: '#ec4899',
			},
			{
				title: t('agent.suggApps'),
				sub: t('agent.suggAppsSub'),
				text: t('agent.suggestApps'),
				icon: 'box',
				color: '#6366f1',
			},
			{
				title: t('agent.suggEnergy'),
				sub: t('agent.suggEnergySub'),
				text: t('agent.suggestEnergy'),
				icon: 'zap',
				color: '#eab308',
			},
		];
		// The icon shows the kind; older texts carry an emoji in front.
		for (const p of pool) p.title = p.title.replace(/^\p{Extended_Pictographic}\S*\s*/u, '');
		if (chats.length === 0)
			timed.unshift({
				title: t('agent.suggKnow'),
				sub: t('agent.suggKnowSub'),
				text: t('agent.knowAsk'),
				tag: t('agent.tagNew'),
				tone: 'ok',
				icon: 'sparkle',
				color: '#8b5cf6',
			});
		const start = seed % pool.length;
		const all = [...fromNas, ...timed, ...pool.slice(start), ...pool.slice(0, start)];
		const from = (shuffle * 3) % all.length;
		return [...all.slice(from), ...all.slice(0, from)].slice(0, 3);
	});
	const currentInfo = $derived(
		current && !readOnlyBy ? chats.find((c) => c.id === current!.id && !c.author) : undefined,
	);
	const currentProject = $derived(projects.find((p) => p.id === (current?.project ?? project)));
	const menuProps = $derived({
		chats,
		projects,
		current: current?.id,
		project,
		host: session.data?.hostname ?? '',
		admin: status?.admin ?? false,
		onopen: (id: string, author?: string) => open(id, true, author ?? ''),
		onnew: () => newChat(''),
		onproject: (id: string) => {
			project = id;
			// On the phone the menu stays open and shows the project's chats.
			if (!phone && (!id || current?.project !== id)) newChat(id);
		},
		onnewproject: () => (dialog = { kind: 'project', project: null }),
		oneditproject: (p: Project) => (dialog = { kind: 'project', project: p }),
		onknowledge: () => (dialog = { kind: 'knowledge', tab: 'memory' }),
		onplanned: () => (dialog = { kind: 'knowledge', tab: 'schedules', only: true }),
		onlook: () => (dialog = { kind: 'settings', section: 'look' }),
		onsettings: () => (dialog = { kind: 'settings' }),
		inboxCount,
		oninbox: () => (dialog = { kind: 'inbox' }),
		gestures: look.gestures,
		pull: look.pull,
		compact: look.compact,
		onrefresh: () => void loadLists(),
		swipeLeft: look.swipeLeft,
		swipeRight: look.swipeRight,
		onaction: (chat: ChatInfo, action: Swipe) => {
			buzz(12);
			if (action === 'pin') void patch(chat, { pinned: !chat.pinned });
			else if (action === 'delete') dialog = { kind: 'delete', chat };
			else if (action === 'menu') dialog = { kind: 'actions', chat };
		},
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
						<button
							type="button"
							class="icon-btn"
							aria-label={t('agent.set.title')}
							title={t('agent.set.title')}
							onclick={() => (dialog = { kind: 'settings' })}><Icon name="gear" size={18} /></button
						>
						{#if phone || current}
							<button
								type="button"
								class="icon-btn"
								aria-label={t('agent.newChat')}
								title={t('agent.newChat')}
								onclick={() => newChat(phone ? '' : project)}
								><Icon name="newChat" size={18} /></button
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
						showSteps={look.steps}
						readonly={!!readOnlyBy}
						onplanned={() => (dialog = { kind: 'knowledge', tab: 'schedules' })}
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
						onshuffle={() => (shuffle += 1)}
						show={look.suggestions}
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
						micHold={look.mic === 'hold'}
						bind:this={composer}
						{busy}
						bind:attachments
						disabled={!!current?.pending || !!readOnlyBy}
						voice={!!status.voice}
						placeholder={readOnlyBy
							? t('agent.readOnlyChat', { name: readOnlyBy })
							: current?.pending
								? t('agent.inputPending')
								: t('agent.inputShort')}
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
		only={dialog.only}
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
{:else if dialog?.kind === 'actions'}
	{@const chat = dialog.chat}
	<!-- PLAN 53: everything you can do with a chat, as a sheet from below. -->
	<div class="sheet-scrim" role="presentation" onclick={() => (dialog = null)}></div>
	<div class="action-sheet" role="dialog" aria-modal="true" aria-label={chat.title}>
		<span class="grab" aria-hidden="true"></span>
		<strong class="sheet-title">{chat.title || t('agent.newChat')}</strong>
		<div class="sheet-list">
			<button
				type="button"
				onclick={() => {
					dialog = null;
					void patch(chat, { pinned: !chat.pinned });
				}}
				><span class="sq" style:--c="#3b82f6"><Icon name="pin" size={17} /></span>{chat.pinned
					? t('agent.unpin')
					: t('agent.pin')}</button
			>
			<button type="button" onclick={() => (dialog = { kind: 'rename', chat, title: chat.title })}
				><span class="sq" style:--c="#64748b"><Icon name="edit" size={17} /></span>{t(
					'agent.rename',
				)}</button
			>
			<button type="button" onclick={() => (dialog = { kind: 'move', chat })}
				><span class="sq" style:--c="#8b5cf6"><Icon name="folder" size={17} /></span>{t(
					'agent.moveToProject',
				)}</button
			>
			<a href={`/api/v1/agent/chats/${chat.id}/export`} download onclick={() => (dialog = null)}
				><span class="sq" style:--c="#14b8a6"><Icon name="download" size={17} /></span>{t(
					'agent.exportMd',
				)}</a
			>
			<a
				href={`/api/v1/agent/chats/${chat.id}/export?format=pdf`}
				download
				onclick={() => (dialog = null)}
				><span class="sq" style:--c="#0ea5e9"><Icon name="fileText" size={17} /></span>{t(
					'agent.exportPdf',
				)}</a
			>
			<button type="button" class="danger" onclick={() => (dialog = { kind: 'delete', chat })}
				><span class="sq" style:--c="#e11d48"><Icon name="trash" size={17} /></span>{t(
					'agent.delete',
				)}</button
			>
		</div>
	</div>
{:else if dialog?.kind === 'inbox'}
	<InboxPanel
		{calm}
		onclose={() => (dialog = null)}
		oncount={(n) => (inboxCount = n)}
		onask={(text) => {
			dialog = null;
			newChat('');
			void send(text);
		}}
	/>
{:else if dialog?.kind === 'settings' && status}
	<AgentSettings
		bind:look
		{status}
		{user}
		{calm}
		start={dialog.section ?? 'list'}
		onknow={getToKnow}
		onclose={() => (dialog = null)}
		onopen={(part) => (dialog = { kind: 'knowledge', tab: part, only: true })}
		chats={chats.filter((c) => !c.author).length}
		ondeleteall={() => {
			keepPinned = true;
			dialog = { kind: 'deleteAll' };
		}}
	/>
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
{:else if dialog?.kind === 'deleteAll'}
	{@const own = chats.filter((c) => !c.author)}
	{@const pinned = own.filter((c) => c.pinned).length}
	<Dialog title={t('agent.deleteAllTitle')} onclose={() => (dialog = null)}>
		<p>{t('agent.deleteAllText', { count: own.length })}</p>
		{#if pinned}
			<label class="check"
				><input type="checkbox" bind:checked={keepPinned} />
				<span>{t('agent.deleteAllKeepPinned', { count: pinned })}</span></label
			>
		{/if}
		<div class="dialog-actions">
			<button type="button" class="btn btn-danger" onclick={removeAll}
				>{t('agent.deleteAllButton')}</button
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
	.sheet-scrim {
		position: fixed;
		inset: 0;
		z-index: 70;
		background: rgb(3 6 14 / 0.5);
		backdrop-filter: blur(8px);
	}
	.action-sheet {
		position: fixed;
		z-index: 71;
		left: 0;
		right: 0;
		bottom: 0;
		max-width: 520px;
		margin: 0 auto;
		padding: 8px 14px calc(env(safe-area-inset-bottom, 0px) + 18px);
		border-radius: 26px 26px 0 0;
		color: var(--text);
		background: color-mix(in srgb, var(--surface) 82%, transparent);
		backdrop-filter: blur(30px) saturate(170%);
		border-top: 1px solid color-mix(in srgb, var(--text) 10%, transparent);
		animation: sheet-up 0.28s cubic-bezier(0.2, 0.8, 0.2, 1);
	}
	@keyframes sheet-up {
		from {
			transform: translateY(100%);
		}
	}
	@media (prefers-reduced-motion: reduce) {
		.action-sheet {
			animation: none;
		}
	}
	.grab {
		display: block;
		width: 44px;
		height: 5px;
		border-radius: 3px;
		margin: 2px auto 10px;
		background: color-mix(in srgb, var(--text) 25%, transparent);
	}
	.sheet-title {
		display: block;
		padding: 0 6px 10px;
		font-size: var(--text-md);
	}
	.sheet-list {
		border-radius: 20px;
		overflow: hidden;
		border: 1px solid color-mix(in srgb, var(--text) 9%, transparent);
	}
	.sheet-list > * {
		display: flex;
		align-items: center;
		gap: 12px;
		width: 100%;
		min-height: 54px;
		padding: 8px 12px;
		border: 0;
		border-top: 1px solid color-mix(in srgb, var(--text) 8%, transparent);
		background: none;
		color: var(--text);
		font: inherit;
		text-decoration: none;
		text-align: left;
		cursor: pointer;
	}
	.sheet-list > :first-child {
		border-top: 0;
	}
	.sheet-list .danger {
		color: var(--danger, #fb7185);
	}
	.sheet-list .sq {
		width: 34px;
		height: 34px;
		border-radius: 11px;
		display: grid;
		place-items: center;
		color: #fff;
		background: linear-gradient(145deg, var(--c), color-mix(in srgb, var(--c) 45%, #0b1020));
	}
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
	.agent {
		overflow: hidden;
		border-radius: var(--radius-tile);
	}
	.agent.app {
		border-radius: 0;
	}
	/* PLAN 53: the agent has its own lively look – a slowly moving aurora
	   behind glass. Motion off, calm or reduced motion keep it still. */
	.agent[data-bg='aurora']::before {
		inset: -12%;
		filter: blur(48px) saturate(140%);
		opacity: 0.9;
		background:
			radial-gradient(
				40% 35% at 18% 20%,
				color-mix(in srgb, var(--accent) 55%, transparent),
				transparent 70%
			),
			radial-gradient(
				35% 30% at 85% 12%,
				color-mix(in srgb, var(--ag-accent-2, var(--accent)) 45%, transparent),
				transparent 70%
			),
			radial-gradient(45% 40% at 70% 88%, rgb(34 211 238 / 0.22), transparent 70%),
			radial-gradient(
				40% 35% at 8% 92%,
				color-mix(in srgb, var(--ag-accent-2, var(--accent)) 25%, transparent),
				transparent 70%
			);
		animation: ag-aurora 28s ease-in-out infinite alternate;
	}
	.agent.calm[data-bg]::before {
		animation: none;
	}
	@keyframes ag-aurora {
		0% {
			transform: translate3d(0, 0, 0) scale(1);
		}
		50% {
			transform: translate3d(3%, -2%, 0) scale(1.06) rotate(2deg);
		}
		100% {
			transform: translate3d(-3%, 2%, 0) scale(1.03) rotate(-2deg);
		}
	}
	@media (prefers-reduced-motion: reduce) {
		.agent[data-bg]::before {
			animation: none;
		}
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
		background: color-mix(in srgb, var(--surface) calc(var(--ag-alpha, 0.55) * 100%), transparent);
		backdrop-filter: blur(26px) saturate(170%);
		border-color: color-mix(in srgb, var(--text) 10%, transparent);
		box-shadow:
			inset 0 1px 0 color-mix(in srgb, #fff 7%, transparent),
			0 20px 50px -25px rgb(0 0 0 / 0.7);
	}
	.layout {
		zoom: var(--ag-scale, 1);
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
		padding: 4px 12px calc(env(safe-area-inset-bottom, 0px) + 8px);
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
