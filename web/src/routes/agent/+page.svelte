<script lang="ts">
	// The KI-Agent (PLAN 44; look of PLAN 68, the design language of every
	// window): its side bar on the left, the chat on the right with title,
	// project and model at the top. Colors come from the TitanOS theme. On
	// the phone the agent is an app of its own whose side bar slides in from
	// the left, with the way back to TitanOS. Installed from here it is an
	// app of its own too (/agent.webmanifest, start /agent?app=1).
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
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
	import { isChatSheet, type ChatSheet } from './chat-sheets.ts';
	import { api, apiFetch, responseError } from '#lib/api.ts';
	import type { IconName } from '#lib/components/Icon.svelte';
	import { agentAppMode } from '#lib/agent-app.ts';
	import { agentFull, setAgentFull } from '#lib/agent-full.svelte.ts';
	import { clearAgentNotifications } from '#lib/web-push.ts';
	import { canSpeak, speak, speechPrefs, stopSpeaking } from '#lib/agent-speech.svelte.ts';
	import { apiErrorText, i18n, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { pageTitle } from '#lib/page-title.svelte.ts';
	import { loadMine, type FeatureViewId, type FeaturesPublic } from '#lib/agent-features.ts';
	import FeatureView from '#lib/components/agent/features/FeatureView.svelte';
	import FeatureHost from '#lib/components/agent/features/FeatureHost.svelte';
	import { session } from '#lib/session.svelte.ts';
	import { inWindow } from '#lib/desktop/frame.ts';
	import {
		accountLook,
		defaultLook,
		DEVICE_LOOK,
		follow,
		whereLine,
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
	// PLAN 72: archived chats are only in the Archiv.
	const listedChats = $derived(chats.filter((c) => !c.archived));
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
		| { kind: 'settings'; section?: 'list' | 'speech' | 'look' | 'models' | 'initiative' | 'mind' }
		| { kind: 'inbox' }
		| { kind: 'notes' }
		| { kind: 'sheet'; project: Project }
		| { kind: 'nas' }
		| { kind: 'feature'; id: FeatureViewId }
		| ChatSheet
		| null
	>(null);
	// PLAN 54: which functions are there for this account, and their views
	// in the menu (only what is on; the others stay under Funktionen).
	let features = $state<FeaturesPublic | null>(null);
	let protectMe = $state<{
		guard?: boolean;
		handbook?: boolean;
		emergency?: boolean;
		alerts?: number;
		requests?: number;
	} | null>(null);
	$effect(() => {
		if (!status?.enabled || !status.allowed) return;
		loadMine()
			.then((m) => (features = m.nas))
			.catch(() => (features = null));
		api
			.get<typeof protectMe>('/agent/protect/me')
			.then((p) => (protectMe = p))
			.catch(() => (protectMe = null));
	});
	const featureItems = $derived.by(() => {
		const f = features;
		const admin = !!status?.admin;
		const out: { id: FeatureViewId; label: string; icon: IconName; count?: number }[] = [];
		if (!f) return out;
		if (f.paper) out.push({ id: 'paper', label: t('agent.f.menu.paper'), icon: 'list' });
		if (f.deadlines)
			out.push({ id: 'deadlines', label: t('agent.f.menu.deadlines'), icon: 'clock' });
		if (protectMe?.guard)
			out.push({
				id: 'guard',
				label: t('agent.f.menu.guard'),
				icon: 'shield',
				count: protectMe.alerts,
			});
		if (protectMe?.handbook)
			out.push({ id: 'handbook', label: t('agent.f.menu.handbook'), icon: 'fileText' });
		if (f.emergency && (admin || protectMe?.emergency))
			out.push({
				id: 'emergency',
				label: t('agent.f.menu.emergency'),
				icon: 'key',
				count: protectMe?.requests,
			});
		if (f.advisor && admin)
			out.push({ id: 'advisor', label: t('agent.f.menu.advisor'), icon: 'chip' });
		if (f.buddy && admin) out.push({ id: 'buddy', label: t('agent.f.menu.buddy'), icon: 'users' });
		if (f.catalog) out.push({ id: 'catalog', label: t('agent.f.menu.catalog'), icon: 'zap' });
		return out;
	});
	// Links of notifications open a view: /agent?feature=guard (PLAN 54).
	$effect(() => {
		const id = page.url.searchParams.get('feature') as FeatureViewId | null;
		const known: FeatureViewId[] = [
			'paper',
			'deadlines',
			'guard',
			'handbook',
			'emergency',
			'advisor',
			'buddy',
			'catalog',
		];
		if (id && known.includes(id) && status?.enabled) dialog = { kind: 'feature', id };
	});
	/** The settings beneath a window opened from them. */
	let under = $state<Extract<NonNullable<typeof dialog>, { kind: 'settings' }> | null>(null);
	const settingsShown = $derived(
		dialog?.kind === 'settings' ? dialog : under?.kind === 'settings' ? under : null,
	);
	/** The chat's action sheet takes the focus and gives it back (PLAN 59). */
	function openOver(next: NonNullable<typeof dialog>) {
		if (dialog?.kind === 'settings') under = dialog;
		dialog = next;
	}
	// Closing the window on top brings the settings back.
	$effect(() => {
		if (dialog === null && under) {
			dialog = under;
			under = null;
		}
	});
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

	// PLAN 59: on the phone the app follows the visible part of the screen.
	// With the keyboard open iOS keeps the page height and pans instead; the
	// app then sits in what is visible and the composer right on the
	// keyboard, without the gap of the home indicator.
	let keyboard = $state(false);
	$effect(() => {
		const vv = window.visualViewport;
		if (!vv || !(app && phone)) return;
		const root = document.documentElement;
		const update = () => {
			// Pinch zoom also shrinks the visible part: then nothing moves.
			if (Math.abs(vv.scale - 1) > 0.01) return;
			root.style.setProperty('--ag-vvh', `${vv.height}px`);
			root.style.setProperty('--ag-vvtop', `${vv.offsetTop}px`);
			keyboard = root.clientHeight - vv.height > 120;
			// The page behind the app cannot scroll; what iOS panned goes back.
			if (vv.offsetTop > 0 && root.scrollHeight <= root.clientHeight + 1) window.scrollTo(0, 0);
		};
		update();
		vv.addEventListener('resize', update);
		vv.addEventListener('scroll', update);
		return () => {
			vv.removeEventListener('resize', update);
			vv.removeEventListener('scroll', update);
			root.style.removeProperty('--ag-vvh');
			root.style.removeProperty('--ag-vvtop');
			keyboard = false;
		};
	});

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
		// In a window the title bar names the program already.
		pageTitle.value = current?.title
			? inWindow
				? current.title
				: `${current.title} · ${t('nav.agent')}`
			: t('nav.agent');
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
		api
			.get<{ schedules?: unknown[] }>('/agent/schedules')
			.then((r) => (plannedCount = r.schedules?.length ?? 0))
			.catch(() => (plannedCount = 0));
	}
	let plannedCount = $state(0);
	let ollamaUrl = $state('');
	const whereText = $derived(
		whereLine({
			cluster: status?.source === 'cluster',
			web: !!status?.web,
			ollama: ollamaUrl,
			here: location.hostname,
		})
			.map((p) => t(p.key as MessageKey, p.params))
			.join(' · '),
	);
	// PLAN 68: the side bar can be folded away (per device).
	const FOLD_KEY = 'titanos.agent.side';
	let folded = $state(readFolded());
	function readFolded() {
		try {
			return localStorage.getItem(FOLD_KEY) === 'folded';
		} catch {
			return false;
		}
	}
	function setFolded(on: boolean) {
		folded = on;
		try {
			if (on) localStorage.setItem(FOLD_KEY, 'folded');
			else localStorage.removeItem(FOLD_KEY);
		} catch {
			// Only for this page view.
		}
	}
	async function loadLocations() {
		if (!status?.admin) return;
		try {
			const s = await api.get<{ locations?: Location[]; ollama?: string }>('/agent/settings');
			locations = s.locations ?? [];
			ollamaUrl = s.ollama ?? '';
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
		admin: status?.admin ?? false,
		onopen: (id: string, author?: string) => {
			if (dialog?.kind === 'feature') dialog = null;
			open(id, true, author ?? '');
		},
		onnew: () => {
			if (dialog?.kind === 'feature') dialog = null;
			newChat('');
		},
		onproject: (id: string) => {
			project = id;
			// On the phone the menu stays open and shows the project's chats.
			if (!phone && (!id || current?.project !== id)) newChat(id);
		},
		onnewproject: () => (dialog = { kind: 'project', project: null }),
		oneditproject: (p: Project) => (dialog = { kind: 'project', project: p }),
		onknowledge: () => (dialog = { kind: 'knowledge', tab: 'memory' }),
		onplanned: () => (dialog = { kind: 'knowledge', tab: 'schedules', only: true }),
		features: featureItems,
		onfeature: (id: FeatureViewId) => {
			menuOpen = false;
			dialog = { kind: 'feature', id };
		},
		onnotes: () => {
			menuOpen = false;
			dialog = { kind: 'notes' };
		},
		onsettings: () => (dialog = { kind: 'settings' }),
		settingsOn: !!settingsShown,
		plannedCount,
		onarchive: () => {
			menuOpen = false;
			dialog = { kind: 'archive' };
		},
		onfold: phone ? undefined : () => setFolded(true),
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
			else if (action === 'archive') void patch(chat, { archived: !chat.archived });
			else if (action === 'delete') dialog = { kind: 'delete', chat };
			else if (action === 'menu') dialog = { kind: 'actions', chat };
		},
		ontitanos: phone || installed ? toTitanOS : undefined,
	});

	// PLAN 67-1: the menu bar of the desktop – the same actions as the agent's own menu.
	import { registerMenuCommands } from '#lib/menu-commands.svelte.ts';
	const agentOn = () => !!status?.enabled && !!status.allowed;
	registerMenuCommands({
		'agent.newChat': { run: () => newChat(''), enabled: agentOn },
		'agent.project': {
			run: () => (dialog = { kind: 'project', project: null }),
			enabled: agentOn,
		},
		'agent.notes': { run: () => (dialog = { kind: 'notes' }), enabled: agentOn },
		'agent.knowledge': {
			run: () => (dialog = { kind: 'knowledge', tab: 'memory' }),
			enabled: agentOn,
		},
		'agent.inbox': { run: () => (dialog = { kind: 'inbox' }), enabled: agentOn },
		'agent.projects': {
			items: () => projects.map((p) => ({ id: p.id, text: p.name, checked: project === p.id })),
			run: (id) => {
				if (id && projects.some((p) => p.id === id)) newChat(id);
			},
		},
		'agent.model': {
			items: () =>
				(status?.models ?? []).map((m) => ({
					id: m.name,
					text: m.name,
					checked: model === m.name,
				})),
			run: (id) => {
				if (id && (status?.models ?? []).some((m) => m.name === id)) model = id;
			},
		},
		'agent.settings': { run: () => (dialog = { kind: 'settings' }), enabled: agentOn },
	});
</script>

<svelte:window ontouchstart={touchStart} ontouchmove={touchMove} />

{#snippet chatMenu(info: ChatInfo | null)}
	<span class="more">
		<Dropdown label={t('ui.more')} icon="more" align="right">
			{#snippet children(close)}
				{#if info}
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
					<button
						type="button"
						role="menuitem"
						onclick={() => {
							close();
							void patch(info, { archived: !info.archived });
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
						dialog = { kind: 'settings' };
					}}><Icon name="gear" size={16} /><span>{t('agent.set.title')}</span></button
				>
				{#if info}
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
				{/if}
			{/snippet}
		</Dropdown>
	</span>
{/snippet}

<div
	class="agent"
	class:app
	class:phone
	class:keyboard
	class:calm
	class:framed={inWindow}
	data-size={look.fontSize}
>
	{#if error}<div class="notice-error" role="alert">{error}</div>{/if}

	{#if !status}
		<p class="muted pad">{t('agent.loading')}</p>
	{:else if !status.enabled}
		<div class="tile ui-card empty-state">
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
		<div class="tile ui-card empty-state">
			<h1>{t('agent.notAllowedTitle')}</h1>
			<p class="muted">{t('agent.notAllowed')}</p>
			{#if phone || installed}<button type="button" class="btn" onclick={toTitanOS}
					>{t('agent.toTitanOS')}</button
				>{/if}
		</div>
	{:else if !status.volume}
		<div class="tile ui-card empty-state">
			<h1>{t('agent.noVolumeTitle')}</h1>
			<p class="muted">{t('agent.noVolume')}</p>
			{#if phone || installed}<button type="button" class="btn" onclick={toTitanOS}
					>{t('agent.toTitanOS')}</button
				>{/if}
		</div>
	{:else}
		<div class="layout" class:folded={folded && !phone}>
			{#if !phone && !folded}
				<aside class="side"><AgentMenu {...menuProps} /></aside>
			{/if}
			<section class="main" aria-label={current?.title ?? t('agent.newChat')}>
				{#if dialog?.kind === 'feature' && !phone}
					<!-- PLAN 72: a function's view as a page here, the side bar stays. -->
					<FeatureHost id={dialog.id} {calm} onclose={() => (dialog = null)} />
				{/if}
				{#if phone}
					<header class="top">
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
						<strong class="app-name">{t('nav.agent')}</strong>
						<span class="fill"></span>
						<button
							type="button"
							class="icon-btn"
							aria-label={t('agent.newChat')}
							title={t('agent.newChat')}
							onclick={() => newChat('')}><Icon name="plus" size={20} /></button
						>
						{@render chatMenu(currentInfo ?? null)}
					</header>
					<div class="chat-head">
						{#if current}<h1>{current.title}</h1>{/if}
						{#if current && currentProject}<span class="chip proj"
								>{currentProject.icon ?? ''} {currentProject.name}</span
							>{/if}
						<ModelPicker models={status.models} bind:model bind:think small {calm} />
					</div>
				{:else}
					<header class="top">
						{#if folded}
							<button
								type="button"
								class="tool-btn"
								aria-label={t('agent.sideUnfold')}
								title={t('agent.sideUnfold')}
								onclick={() => setFolded(false)}><Icon name="sidebar" size={17} /></button
							>
						{/if}
						<h1>{current?.title || t('agent.newChat')}</h1>
						{#if current && currentProject}<span class="chip proj"
								>{currentProject.icon ?? ''} {currentProject.name}</span
							>{/if}
						<span class="fill"></span>
						<ModelPicker models={status.models} bind:model bind:think small {calm} />
						{#if currentInfo}
							<button
								type="button"
								class="tool-btn"
								class:on={currentInfo.pinned}
								aria-label={currentInfo.pinned ? t('agent.unpin') : t('agent.pin')}
								aria-pressed={currentInfo.pinned}
								title={currentInfo.pinned ? t('agent.unpin') : t('agent.pin')}
								onclick={() => patch(currentInfo, { pinned: !currentInfo.pinned })}
								><Icon name="pin" size={16} /></button
							>
						{/if}
						{#if !installed && !inWindow}
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
						chats={listedChats}
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
					<p class="where">{whereText}</p>
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

{#if settingsShown && status}
	<!-- The settings stay open beneath a window they open (PLAN 59), and
	     are there again when it closes. -->
	<AgentSettings
		bind:look
		{status}
		{user}
		{calm}
		start={settingsShown.section ?? 'list'}
		onknow={getToKnow}
		onclose={() => ((dialog = null), (under = null))}
		onopen={(part) => openOver({ kind: 'knowledge', tab: part, only: true })}
		chats={chats.filter((c) => !c.author).length}
		ondeleteall={() => {
			keepPinned = true;
			openOver({ kind: 'deleteAll' });
		}}
	/>
{/if}
{#if dialog?.kind === 'feature'}
	{#if phone}<FeatureView id={dialog.id} {calm} onclose={() => (dialog = null)} />{/if}
{:else if dialog?.kind === 'project'}
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
			under = null;
			dialog = null;
			void open(id);
		}}
	/>
{:else if isChatSheet(dialog)}
	<ChatSheets
		sheet={dialog}
		{chats}
		{projects}
		bind:keepPinned
		set={(next) => (dialog = next)}
		onopen={(id) => open(id)}
		onpatch={patch}
		onremove={remove}
		onremoveall={removeAll}
	/>
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
{:else if dialog?.kind === 'notes'}
	<NotesPanel {calm} onclose={() => (dialog = null)} />
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
		chats={listedChats}
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
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.fill {
		flex: 1;
	}
	.proj {
		flex: none;
		max-width: 40%;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
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
