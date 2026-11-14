// The KI-Agent's page, first part: what the NAS says about the agent, the
// chats and projects, the open chat with what streams in, the look, and the
// requests that load, open, send and change chats. The menus, gestures and
// suggestions on top of this are in agent-page.svelte.ts.
import { page } from '$app/state';
import { goto } from '$app/navigation';
import { api, apiFetch, responseError, setAgentProfile } from '#lib/api.ts';
import { agentAppMode } from '#lib/agent-app.ts';
import { agentFull } from '#lib/agent-full.svelte.ts';
import { clearAgentNotifications } from '#lib/web-push.ts';
import { canSpeak, speak, stopSpeaking } from '#lib/agent-speech.svelte.ts';
import { apiErrorText, i18n, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
import type { FeatureViewId, FeaturesPublic } from '#lib/agent-features.ts';
import { session } from '#lib/session.svelte.ts';
import {
	defaultLook,
	follow,
	whereLine,
	sanitizeLook,
	type AgentEvent,
	type Chat,
	type ChatInfo,
	type Entry,
	type Location,
	type Look,
	type Pending,
	type Project,
	type Status,
	type AgentView,
} from '#lib/agent.ts';
import type { ChatSheet } from './chat-sheets.ts';

export const LOOK_KEY = 'titanos.agent.look';
// PLAN 68: the side bar can be folded away (per device).
const FOLD_KEY = 'titanos.agent.side';

/** The window on top of the chat: a dialog, a panel or a sheet. */
export type AgentDialog = AgentChats['dialog'];

export class AgentChats {
	readLook = (): Look => {
		try {
			return sanitizeLook(JSON.parse(localStorage.getItem(LOOK_KEY) ?? 'null'));
		} catch {
			return defaultLook();
		}
	};
	status = $state<Status | null>(null);
	chats = $state<ChatInfo[]>([]);
	// PLAN 72: archived chats are only in the Archiv.
	listedChats = $derived(this.chats.filter((c) => !c.archived));
	projects = $state<Project[]>([]);
	locations = $state<Location[]>([]);
	current = $state<Chat | null>(null);
	/** The author when the open chat belongs to another account (read-only). */
	readOnlyBy = $state('');
	project = $state('');
	error = $state('');
	busy = $state(false);
	deciding = $state(false);
	live = $state<{ thinking: string; content: string; step: Entry | null }>({
		thinking: '',
		content: '',
		step: null,
	});
	model = $state('');
	think = $state('');
	look = $state<Look>(this.readLook());
	dialog = $state<
		| { kind: 'project'; project: Project | null }
		| { kind: 'notes' }
		/** A project: its chats, files and memory (from the page Wissen). */
		| { kind: 'sheet'; project: Project }
		| { kind: 'nas' }
		/** Project, model and thinking level of the chat (PLAN 90-4). */
		| { kind: 'head' }
		| { kind: 'feature'; id: FeatureViewId }
		| ChatSheet
		| null
	>(null);
	// PLAN 90-4: Geplant, Gedächtnis, Wissen and Einstellungen are pages in
	// the place of the chat ('chat' without an open chat is the start page).
	// The address names the page (/agent?page=memory&part=…), so "back"
	// works and a window comes back where it was.
	/**
	 * PLAN 91: the profile of Home Assistant instead of the own account
	 * (/agent?profile=assist, owners and administrators). Set before the
	 * first request; every address of the page keeps it.
	 */
	profile: '' | 'assist' = (() => {
		const p = page.url.searchParams.get('profile') === 'assist' ? 'assist' : '';
		setAgentProfile(p);
		return p;
	})();
	go = (path: string) =>
		goto(this.profile ? `${path}${path.includes('?') ? '&' : '?'}profile=${this.profile}` : path);
	view = $state<AgentView>('chat');
	part = $state('');
	show = (view: AgentView, part = '') => {
		this.menuOpen = false;
		if (this.dialog?.kind === 'feature') this.dialog = null;
		const chat = this.current && !this.readOnlyBy ? `&chat=${this.current.id}` : '';
		if (view === 'chat') void this.go(chat ? `/agent?${chat.slice(1)}` : '/agent');
		else
			void this.go(`/agent?page=${view}${part ? `&part=${encodeURIComponent(part)}` : ''}${chat}`);
	};
	/** A short confirmation above the input ("Gemerkt."), gone after a while. */
	notice = $state('');
	noticeTimer: ReturnType<typeof setTimeout> | undefined;
	say = (text: string) => {
		this.notice = text;
		clearTimeout(this.noticeTimer);
		this.noticeTimer = setTimeout(() => (this.notice = ''), 4000);
	};
	/** The search in the open chat (PLAN 90-4); null: closed. */
	find = $state<string | null>(null);
	// PLAN 54: which functions are there for this account, and their views
	// in the menu (only what is on; the others stay under Funktionen).
	features = $state<FeaturesPublic | null>(null);
	protectMe = $state<{
		guard?: boolean;
		handbook?: boolean;
		emergency?: boolean;
		alerts?: number;
		requests?: number;
	} | null>(null);
	composer = $state<{ fill: (text: string) => void }>();
	attachments = $state<{ name: string; file?: File; ref?: string }[]>([]);
	phone = $state(matchMedia('(max-width: 760px)').matches);
	stream: AbortController | null = null;

	menuOpen = $state(false);

	// Installed as an app and on the phone it is always full screen; on the
	// desktop inside TitanOS full screen is a choice per device (PLAN 47/48).
	installed = $derived(agentAppMode(page.url));
	app = $derived(this.installed || this.phone || agentFull.on);
	/** Back to TitanOS: a full load, so the TitanOS manifest is there again. */
	toTitanOS = () => location.assign('/');
	user = $derived(session.data?.user?.name ?? '');
	usable = $derived((this.status?.models ?? []).filter((m) => m.capabilities.includes('tools')));
	contextNow = $derived(
		this.status?.context ||
			this.status?.running?.find((r) => r.name === this.model)?.contextLength ||
			0,
	);
	// PLAN 53: the look and the app settings belong to the account and live on
	// the NAS; only device things (vibration, screen awake, text size) stay
	// in this browser. The local copy shows the page before the answer comes.
	prefsLoaded = false;
	prefsTimer: ReturnType<typeof setTimeout> | undefined;
	calm = $derived(
		this.look.calm ||
			this.look.motion !== 'full' ||
			(typeof matchMedia !== 'undefined' && matchMedia('(prefers-reduced-motion: reduce)').matches),
	);
	buzz = (pattern: number | number[]) => {
		if (this.look.vibrate && !this.calm && 'vibrate' in navigator) navigator.vibrate(pattern);
	};

	/** The agent's own page is shown (on, this account may use it, volume there). */
	ready = $derived(!!this.status?.enabled && this.status.allowed && this.status.volume);
	/** The state could not be read: the page offers to try again. */
	statusFailed = $state(false);
	loadStatus = async () => {
		try {
			this.status = await api.get<Status>('/agent/status');
			this.statusFailed = false;
			if (!this.model) this.model = this.status.model || this.usable[0]?.name || '';
			if (!this.think) this.think = this.status.think || '';
		} catch (e) {
			this.error = apiErrorText(e);
			this.statusFailed = true;
		}
	};
	loadLists = async () => {
		try {
			const [c, p] = await Promise.all([
				api.get<{ chats: ChatInfo[] }>('/agent/chats'),
				api.get<{ projects: Project[] }>('/agent/projects'),
			]);
			this.chats = c.chats;
			this.projects = p.projects;
		} catch (e) {
			this.error = apiErrorText(e);
		}
		api
			.get<{ schedules?: unknown[] }>('/agent/schedules')
			.then((r) => (this.plannedCount = r.schedules?.length ?? 0))
			.catch(() => (this.plannedCount = 0));
	};
	plannedCount = $state(0);
	ollamaUrl = $state('');
	whereText = $derived(
		whereLine({
			cluster: this.status?.source === 'cluster',
			web: !!this.status?.web,
			ollama: this.ollamaUrl,
			here: location.hostname,
		})
			.map((p) => t(p.key as MessageKey, p.params))
			.join(' · '),
	);
	readFolded = () => {
		try {
			return localStorage.getItem(FOLD_KEY) === 'folded';
		} catch {
			return false;
		}
	};
	folded = $state(this.readFolded());
	setFolded = (on: boolean) => {
		this.folded = on;
		try {
			if (on) localStorage.setItem(FOLD_KEY, 'folded');
			else localStorage.removeItem(FOLD_KEY);
		} catch {
			// Only for this page view.
		}
	};
	loadLocations = async () => {
		if (!this.status?.admin) return;
		try {
			const s = await api.get<{ locations?: Location[]; ollama?: string }>('/agent/settings');
			this.locations = s.locations ?? [];
			this.ollamaUrl = s.ollama ?? '';
		} catch {
			this.locations = [];
		}
	};

	listsReady = $state(false);
	open = async (id: string, push = true, author = '') => {
		this.menuOpen = false;
		this.find = null;
		stopSpeaking();
		this.stream?.abort();
		this.busy = false;
		this.live = { thinking: '', content: '', step: null };
		this.error = '';
		try {
			// Another account's chat in a shared project is only read (PLAN 51).
			const fetched = await api.get<Chat & { author?: string; readOnly?: boolean }>(
				`/agent/chats/${id}` + (author ? `?author=${encodeURIComponent(author)}` : ''),
			);
			this.readOnlyBy = fetched.readOnly ? (fetched.author ?? author) : '';
			this.current = fetched;
			if (this.readOnlyBy) {
				if (fetched.project) this.project = fetched.project;
				if (push) void this.go(`/agent?chat=${id}&author=${encodeURIComponent(this.readOnlyBy)}`);
				return;
			}
			if (this.current.model) this.model = this.current.model;
			if (this.current.think) this.think = this.current.think;
			if (this.current.project) this.project = this.current.project;
			if (push) void this.go(`/agent?chat=${id}`);
			const listed = this.chats.find((c) => c.id === id);
			if (this.current.state === 'running' || listed?.state === 'running') this.watch(id);
		} catch (e) {
			this.error = apiErrorText(e);
		}
	};

	newChat = (inProject = this.project) => {
		this.menuOpen = false;
		this.readOnlyBy = '';
		stopSpeaking();
		this.stream?.abort();
		this.find = null;
		this.current = null;
		this.busy = false;
		this.project = inProject;
		this.live = { thinking: '', content: '', step: null };
		void this.go('/agent');
	};

	watch = (id: string) => {
		this.stream?.abort();
		const controller = new AbortController();
		this.stream = controller;
		this.busy = true;
		this.live = { thinking: '', content: '', step: null };
		void follow(id, (e) => this.onEvent(id, e), controller.signal)
			.catch(() => {})
			.finally(() => {
				if (this.stream === controller) this.finish(id);
			});
	};

	onEvent = (id: string, e: AgentEvent) => {
		if (!this.current || this.current.id !== id) return;
		switch (e.type) {
			case 'thinking':
				this.live.thinking += e.text;
				break;
			case 'content':
				this.live.content += e.text;
				break;
			case 'step':
				this.live.step = e.step;
				break;
			case 'entry':
				if (!this.current.entries.some((x) => x.id === e.entry.id))
					this.current.entries.push(e.entry);
				if (e.entry.role === 'assistant')
					this.live = { thinking: '', content: '', step: this.live.step };
				if (e.entry.role === 'tool') this.live.step = null;
				break;
			case 'reset':
				// The round is asked again; what streamed in does not count.
				this.live = { thinking: '', content: '', step: this.live.step };
				break;
			case 'plan':
				this.current.plan = e.plan;
				break;
			case 'approval':
				this.current.pending = e.pending;
				this.current.state = 'waiting';
				this.buzz([60, 40, 60]);
				break;
			case 'state':
				this.current.state = e.state;
				break;
		}
	};

	finish = async (id: string) => {
		this.busy = false;
		this.stream = null;
		this.live = { thinking: '', content: '', step: null };
		try {
			const fresh = await api.get<Chat>(`/agent/chats/${id}`);
			if (this.current?.id === id) this.current = fresh;
			if (fresh.state === 'idle') this.buzz(40);
			// Read the new answer aloud if wished (Darstellung, PLAN 51).
			if (
				this.look.read &&
				this.current?.id === id &&
				document.visibilityState === 'visible' &&
				canSpeak()
			) {
				const last = [...fresh.entries]
					.reverse()
					.find((e) => e.role === 'assistant' && e.content?.trim());
				if (last) speak(last.id, last.content ?? '', i18n.locale);
			}
			if (this.current?.id === id) this.seen(id, true);
		} catch {
			// The list reload below shows the state.
		}
		void this.loadLists();
	};

	/**
	 * Asks a message of the open chat again (PLAN 90-4): with a text it is
	 * changed first ("Bearbeiten"), without one the same is asked ("Neu
	 * erzeugen"). The chat forgets everything after it.
	 */
	resend = async (entry: Entry, text = '') => {
		if (!this.current || this.busy) return;
		this.error = '';
		stopSpeaking();
		try {
			const chat = await api.post<Chat>(`/agent/chats/${this.current.id}/messages`, {
				message: text,
				replace: entry.id,
				model: this.model,
				think: this.think,
			});
			this.current = chat;
			this.watch(chat.id);
		} catch (e) {
			this.error = apiErrorText(e);
		}
	};

	send = async (text: string) => {
		this.error = '';
		const files = this.attachments;
		try {
			let target = this.current;
			const uploads = files.filter((a) => a.file);
			if (!target && uploads.length) {
				// An empty chat first, so the files are there before it runs.
				target = await api.post<Chat>('/agent/chats', {
					draft: true,
					project: this.project,
					model: this.model,
					think: this.think,
				});
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
			this.attachments = [];
			if (target) {
				const chat = await api.post<Chat>(`/agent/chats/${target.id}/messages`, {
					message,
					model: this.model,
					think: this.think,
				});
				this.current = chat;
				if (!this.chats.some((c) => c.id === chat.id)) this.chats = [chat, ...this.chats];
				void this.go(`/agent?chat=${chat.id}`);
				this.watch(chat.id);
			} else {
				const chat = await api.post<Chat>('/agent/chats', {
					message,
					project: this.project,
					model: this.model,
					think: this.think,
				});
				this.current = chat;
				this.chats = [chat, ...this.chats];
				void this.go(`/agent?chat=${chat.id}`);
				this.watch(chat.id);
			}
		} catch (e) {
			this.attachments = files;
			this.error = apiErrorText(e);
		}
	};

	undo = async (step: Entry) => {
		if (!this.current) return;
		this.error = '';
		try {
			await api.post(`/agent/chats/${this.current.id}/undo`, { step: step.id });
			this.current = await api.get<Chat>(`/agent/chats/${this.current.id}`);
		} catch (e) {
			this.error = apiErrorText(e);
		}
	};

	// Read state (PLAN 48): while a chat is on screen the page says so every
	// 15 seconds. The NAS then counts it as read on all devices and sends no
	// push message for it; notifications of it on this device go away.
	seen = (id: string, visible: boolean) => {
		void apiFetch(`/agent/chats/${id}/seen`, {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({ visible }),
			keepalive: !visible,
		}).catch(() => {});
		if (visible) {
			const listed = this.chats.find((c) => c.id === id);
			if (listed?.unread) listed.unread = false;
			void clearAgentNotifications(id);
		}
	};

	decide = async (p: Pending, approve: boolean) => {
		if (!this.current) return;
		this.deciding = true;
		this.error = '';
		try {
			await api.post(`/agent/chats/${this.current.id}/decide`, { id: p.id, approve });
			this.current.pending = null;
			this.watch(this.current.id);
		} catch (e) {
			this.error = apiErrorText(e);
		} finally {
			this.deciding = false;
		}
	};

	stop = async (id = this.current?.id) => {
		if (!id) return;
		try {
			await api.post(`/agent/chats/${id}/stop`);
		} catch (e) {
			this.error = apiErrorText(e);
		}
	};

	patch = async (chat: ChatInfo, body: Record<string, unknown>) => {
		try {
			await api.patch(`/agent/chats/${chat.id}`, body);
			await this.loadLists();
			if (this.current?.id === chat.id)
				this.current = await api.get<Chat>(`/agent/chats/${chat.id}`);
		} catch (e) {
			this.error = apiErrorText(e);
		}
	};
	remove = async (chat: ChatInfo) => {
		try {
			await api.delete(`/agent/chats/${chat.id}`);
			if (this.current?.id === chat.id) this.newChat();
			await this.loadLists();
		} catch (e) {
			this.error = apiErrorText(e);
		}
		this.dialog = null;
	};

	// "Alle Chats löschen" (PLAN 56): the own chats, pinned ones stay if wished.
	keepPinned = $state(true);
	removeAll = async () => {
		try {
			await api.delete(`/agent/chats${this.keepPinned ? '?keepPinned=true' : ''}`);
			if (this.current && !(this.keepPinned && this.current.pinned)) this.newChat();
			await this.loadLists();
		} catch (e) {
			this.error = apiErrorText(e);
		}
		this.dialog = null;
	};
}
