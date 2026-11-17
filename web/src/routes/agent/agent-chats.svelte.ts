// The KI-Agent's page, first part: what the NAS says about the agent, the
// chats and projects, the runs of the chats with what streams in, the look,
// and the requests that load, open, send and change chats. The menus,
// gestures and suggestions on top of this are in agent-page.svelte.ts.
import { page } from '$app/state';
import { goto } from '$app/navigation';
import { SvelteMap, SvelteSet } from 'svelte/reactivity';
import { api, ApiError, apiFetch, responseError, setAgentProfile } from '#lib/api.ts';
import { agentAppMode } from '#lib/agent-app.ts';
import { agentFull } from '#lib/agent-full.svelte.ts';
import { clearAgentNotifications } from '#lib/web-push.ts';
import { canSpeak, speak, stopSpeaking } from '#lib/agent-speech.svelte.ts';
import { apiErrorText, i18n, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
import type { FeatureViewId, FeaturesPublic } from '#lib/agent-features.ts';
import { session } from '#lib/session.svelte.ts';
import {
	activeSchedules,
	defaultLook,
	follow,
	mergeEntries,
	whereLine,
	sanitizeLook,
	LOOK_KEY,
	type AgentEvent,
	type Chat,
	type ChatInfo,
	type Entry,
	type Location,
	type Look,
	type Pending,
	type Project,
	type Schedule,
	type Status,
	type AgentView,
} from '#lib/agent.ts';
import type { ChatSheet } from './chat-sheets.ts';

export { LOOK_KEY };
// PLAN 68: the side bar can be folded away (per device).
const FOLD_KEY = 'titanos.agent.side';

/** The window on top of the chat: a dialog, a panel or a sheet. */
export type AgentDialog = AgentChats['dialog'];

export type Autonomy = 'direct' | 'plan' | 'ask';
const AUTONOMY_KEY = 'titanos.agent.autonomy';
function savedAutonomy(): Autonomy {
	try {
		const v = localStorage.getItem(AUTONOMY_KEY);
		return v === 'plan' || v === 'ask' ? v : 'direct';
	} catch {
		return 'direct';
	}
}

/** What streams in while a chat runs: thoughts, text and the step. */
export type Live = { thinking: string; content: string; step: Entry | null };
const idle = (): Live => ({ thinking: '', content: '', step: null });
const IDLE: Live = Object.freeze(idle());

/**
 * The run of one chat as this page knows it (1.01): runs are kept per chat,
 * so one goes on while another chat is open, and its chat shows it again
 * when opened. Only the open chat is followed (one stream: on plain HTTP a
 * browser has few connections per address); a run elsewhere is asked for
 * through /agent/runs until it ends.
 */
export class ChatRun {
	busy = $state(false);
	live = $state<Live>(idle());
	deciding = $state(false);
	/** Times the chat said "running" while no run answered (at most eight). */
	retries = 0;
	retryTimer: ReturnType<typeof setTimeout> | undefined;
}

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
	/** The open chat's id; changes only when another chat is opened. */
	currentId = $derived(this.current?.id ?? '');
	/** The author when the open chat belongs to another account (read-only). */
	readOnlyBy = $state('');
	project = $state('');
	error = $state('');
	/** The runs this page knows, per chat. */
	runs = new SvelteMap<string, ChatRun>();
	/** The run of the open chat. */
	openRun = $derived(this.current ? this.runs.get(this.current.id) : undefined);
	busy = $derived(this.openRun?.busy ?? false);
	deciding = $derived(this.openRun?.deciding ?? false);
	live = $derived<Live>(this.openRun?.live ?? IDLE);
	/** The run of a chat, made when it is first needed (never in a $derived). */
	run = (id: string): ChatRun => {
		let run = this.runs.get(id);
		if (!run) {
			run = new ChatRun();
			this.runs.set(id, run);
		}
		return run;
	};
	/** The model and thinking level chosen in this chat; empty follows the
	 *  settings of the agent (owner 17 Nov 2026: a new default level only
	 *  counted after a restart, because the page sent its copy of the old
	 *  default with every message and the NAS kept it as the chat's own). */
	model = $state('');
	think = $state('');
	/** What a message sends: only a choice made in the chat. Without a
	 *  model in the settings the first usable one goes, as before. */
	choice = (): { model?: string; think?: string } => {
		const model = this.model || (this.status?.model ? '' : this.shownModel);
		return { ...(model && { model }), ...(this.think && { think: this.think }) };
	};
	/** How the agent works in this chat (owner 15 Nov 2026): start right
	 *  away, show the plan first, or ask before every change. A new chat
	 *  takes what was chosen last on this device. */
	autonomy = $state<Autonomy>(savedAutonomy());
	setAutonomy = (value: Autonomy) => {
		this.autonomy = value;
		try {
			localStorage.setItem(AUTONOMY_KEY, value);
		} catch {
			// private mode: the choice holds for this visit
		}
		const info = this.chats.find((c) => c.id === this.current?.id && !c.author);
		if (info && !this.readOnlyBy) void this.patch(info, { autonomy: value });
	};
	look = $state<Look>(this.readLook());
	dialog = $state<
		| { kind: 'project'; project: Project | null }
		| { kind: 'notes' }
		/** A project: its chats, files and memory (from the page Wissen). */
		| { kind: 'sheet'; project: Project }
		| { kind: 'nas' }
		| { kind: 'talk' }
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
		if (view !== 'chat') this.fresh = false;
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
	/** The message being typed: it outlives the input field. */
	draft = $state('');
	attachments = $state<{ name: string; file?: File; ref?: string }[]>([]);
	phone = $state(matchMedia('(max-width: 760px)').matches);
	/** The one stream this page follows: the run of the open chat. */
	stream: { id: string; controller: AbortController } | null = null;

	menuOpen = $state(false);

	// Installed as an app and on the phone it is always full screen; on the
	// desktop inside TitanOS full screen is a choice per device (PLAN 47/48).
	installed = $derived(agentAppMode(page.url));
	app = $derived(this.installed || this.phone || agentFull.on);
	/** Back to TitanOS: a full load, so the TitanOS manifest is there again. */
	toTitanOS = () => location.assign('/');
	user = $derived(session.data?.user?.name ?? '');
	usable = $derived((this.status?.models ?? []).filter((m) => m.capabilities.includes('tools')));
	/** The model the chat runs with: its own, or the settings' default. */
	shownModel = $derived(this.model || this.status?.model || this.usable[0]?.name || '');
	/** The thinking level the chat runs with, as the model offers it. */
	shownThink = $derived.by(() => {
		const want = this.think || this.status?.think || '';
		const m = this.usable.find((x) => x.name === this.shownModel);
		if (m && want && !(m.levels ?? []).includes(want)) return m.defaultLevel ?? '';
		return want;
	});
	contextNow = $derived(
		this.status?.context ||
			this.status?.running?.find((r) => r.name === this.shownModel)?.contextLength ||
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
			.get<{ schedules?: Schedule[] }>('/agent/schedules')
			.then((r) => (this.plannedCount = activeSchedules(r.schedules)))
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
	/** A run no longer followed: its stream and its retries are gone; it goes
	 *  on on the NAS. What streamed in stays until it is followed again (the
	 *  snapshot then replaces it). */
	private rest(id: string) {
		const run = this.runs.get(id);
		if (!run) return;
		clearTimeout(run.retryTimer);
		run.retries = 0;
	}
	/** Lets go of the followed stream so that its end does nothing more
	 *  (0.99: the old stream's end read the last answer aloud again and wrote
	 *  an older copy of the chat over the new one). Its run goes on and is
	 *  followed again, with a snapshot, when its chat is opened (1.01). */
	detach = () => {
		const old = this.stream;
		this.stream = null;
		if (!old) return;
		this.rest(old.id);
		old.controller.abort();
	};
	/** The state of a chat in the list ("arbeitet" in the side bar). */
	private listState(id: string, state: Chat['state']) {
		const listed = this.chats.find((c) => c.id === id && !c.author);
		if (listed && listed.state !== state) listed.state = state;
	}
	/** A run was started from here: the list and the chat's run know it at
	 *  once (the answer of the NAS is the chat as it was before the run). */
	private started(chat: Chat) {
		if (this.chats.some((c) => c.id === chat.id && !c.author)) this.listState(chat.id, 'running');
		else this.chats = [{ ...chat, state: 'running' }, ...this.chats];
		this.run(chat.id).busy = true;
	}
	/** The text of a refused start: the limit of chats at once names its number. */
	startError = (e: unknown): string => {
		if (!(e instanceof ApiError) || e.code !== 'agent_limit') return apiErrorText(e);
		const n = this.status?.maxRuns ?? 1;
		return n <= 1 ? t('agent.runs.limitOne') : t('agent.runs.limitMany', { n });
	};
	/** Counts the chats asked for: an answer that comes late for a chat
	 *  left in the meantime is dropped (two quick taps showed the first). */
	private opened = 0;
	open = async (id: string, push = true, author = '') => {
		const turn = ++this.opened;
		this.fresh = false;
		this.menuOpen = false;
		this.find = null;
		stopSpeaking();
		this.detach();
		this.error = '';
		try {
			// Another account's chat in a shared project is only read (PLAN 51).
			const fetched = await api.get<Chat & { author?: string; readOnly?: boolean }>(
				`/agent/chats/${id}` + (author ? `?author=${encodeURIComponent(author)}` : ''),
			);
			if (turn !== this.opened) return;
			this.readOnlyBy = fetched.readOnly ? (fetched.author ?? author) : '';
			this.current = fetched;
			if (fetched.titling) this.followTitle(id);
			if (this.readOnlyBy) {
				if (fetched.project) this.project = fetched.project;
				if (push) void this.go(`/agent?chat=${id}&author=${encodeURIComponent(this.readOnlyBy)}`);
				return;
			}
			// The chat's own choice; without one it follows the settings.
			this.model = (this.current.modelOwn && this.current.model) || '';
			this.think = (this.current.thinkOwn && this.current.think) || '';
			this.autonomy = (this.current.autonomy as Autonomy) || 'direct';
			if (this.current.project) this.project = this.current.project;
			if (push) void this.go(`/agent?chat=${id}`);
			const listed = this.chats.find((c) => c.id === id);
			// A run that went on while another chat was open is followed
			// again; its snapshot brings what happened meanwhile (1.01).
			if (
				this.current.state === 'running' ||
				listed?.state === 'running' ||
				this.runs.get(id)?.busy
			)
				this.watch(id);
		} catch (e) {
			if (turn === this.opened) this.error = apiErrorText(e);
		}
	};

	/** 1.07: the model writes the chat's title after the first answer; the
	 *  page asks a few times until it is there, then shows it in the head
	 *  and the list. When it does not come, the question stays the title. */
	followTitle = (id: string, tries = 0) => {
		if (tries >= 8) return;
		setTimeout(
			async () => {
				try {
					const c = await api.get<Chat>(`/agent/chats/${id}`);
					if (c.titling) return this.followTitle(id, tries + 1);
					if (this.current?.id === id) this.current.title = c.title;
					const listed = this.chats.find((x) => x.id === id);
					if (listed) listed.title = c.title;
				} catch {
					// The list shows it the next time it is read.
				}
			},
			Math.min(1000 * 2 ** tries, 8000),
		);
	};

	/** "Neuer Chat" was asked for: an empty chat to write in, not the start
	 *  page with the state of the NAS and everything that is planned. */
	fresh = $state(false);
	freshChat = (inProject = this.project) => {
		this.newChat(inProject);
		this.fresh = true;
	};
	/** navigate: false when the address already says it ("back", 1.01). */
	newChat = (inProject = this.project, navigate = true) => {
		this.fresh = false;
		this.menuOpen = false;
		this.readOnlyBy = '';
		this.opened++;
		stopSpeaking();
		this.detach();
		this.find = null;
		this.current = null;
		this.project = inProject;
		// A new chat follows the settings until something is chosen in it.
		this.model = '';
		this.think = '';
		if (navigate) void this.go('/agent');
	};

	watch = (id: string) => {
		const run = this.run(id);
		const old = this.stream;
		const controller = new AbortController();
		this.stream = { id, controller };
		// Another chat's run goes on without this page (1.01).
		if (old && old.id !== id) this.rest(old.id);
		old?.controller.abort();
		clearTimeout(run.retryTimer);
		run.busy = true;
		this.listState(id, 'running');
		// What streamed in before stays until the stream says more: a snapshot
		// replaces it (no flicker when the page follows the run again); a
		// titand without snapshots, or the end, clears it first.
		let first = true;
		void follow(
			id,
			(e) => {
				if (first && e.type !== 'snapshot') run.live = idle();
				first = false;
				// The stream delivers: the next break starts counting anew. Its
				// bare end does not count – a chat that says "running" while no
				// run answers would otherwise be asked twice a second for ever.
				if (e.type !== 'end') run.retries = 0;
				this.onEvent(id, e);
			},
			controller.signal,
		)
			.catch(() => {})
			.finally(() => {
				if (this.stream?.controller === controller) {
					this.stream = null;
					void this.finish(id);
				}
			});
	};

	onEvent = (id: string, e: AgentEvent) => {
		const run = this.runs.get(id);
		if (!run) return;
		// The chat itself changes only while it is open; what streams in
		// belongs to its run.
		const chat = this.current?.id === id ? this.current : null;
		switch (e.type) {
			case 'snapshot':
				// What the run did before this page followed it (1.01): the text
				// is replaced, the entries take their place by id.
				run.live = { thinking: e.thinking ?? '', content: e.content ?? '', step: e.step ?? null };
				if (chat) {
					chat.entries = mergeEntries(chat.entries, e.entries ?? []);
					if (e.state) chat.state = e.state;
					chat.pending = e.pending ?? null;
					if (e.plan) chat.plan = e.plan;
				}
				break;
			case 'thinking':
				run.live.thinking += e.text;
				break;
			case 'content':
				run.live.content += e.text;
				break;
			case 'step':
				run.live.step = e.step;
				break;
			case 'entry':
				if (chat && !chat.entries.some((x) => x.id === e.entry.id)) chat.entries.push(e.entry);
				if (e.entry.role === 'assistant')
					run.live = { thinking: '', content: '', step: run.live.step };
				if (e.entry.role === 'tool') run.live.step = null;
				break;
			case 'reset':
				// The round is asked again; what streamed in does not count.
				run.live = { thinking: '', content: '', step: run.live.step };
				break;
			case 'plan':
				if (chat) chat.plan = e.plan;
				break;
			case 'approval':
				if (chat) {
					chat.pending = e.pending;
					chat.state = 'waiting';
				}
				this.buzz([60, 40, 60]);
				break;
			case 'state':
				if (chat) chat.state = e.state;
				break;
		}
	};

	/** The stream ended. The chat is read first and shown, then the working
	 *  state goes: who waits for the end finds the answer there. A run that
	 *  still works (the connection broke) is followed again (0.99: the page
	 *  stood still until it was loaded anew). */
	finish = async (id: string) => {
		const run = this.run(id);
		const again = (ms: number) => {
			clearTimeout(run.retryTimer);
			run.retryTimer = setTimeout(() => {
				if (this.current?.id !== id || this.stream) return;
				if (run.busy) this.watch(id);
				else void this.finish(id);
			}, ms);
		};
		try {
			const fresh = await api.get<Chat>(`/agent/chats/${id}`);
			if (this.stream?.id === id) return; // followed again in the meantime
			if (this.current?.id === id) this.current = fresh;
			if (fresh.titling) this.followTitle(id);
			if (fresh.state === 'running') {
				if (this.current?.id === id && run.retries < 8) {
					run.retries++;
					again(Math.min(500 * 2 ** (run.retries - 1), 8000));
					return;
				}
				if (this.current?.id !== id) {
					// Another chat was opened meanwhile: this run goes on there,
					// and /agent/runs tells when it ends (1.01).
					run.retries = 0;
					run.live = idle();
					void this.loadLists();
					return;
				}
			}
			run.retries = 0;
			run.busy = false;
			run.live = idle();
			if (fresh.state === 'idle') this.buzz(40);
			// Read the new answer aloud if wished (Darstellung, PLAN 51).
			if (
				this.look.read &&
				// The Sprechmodus reads the answer itself.
				this.dialog?.kind !== 'talk' &&
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
			// The NAS did not answer: the state is not known. Ask again a few
			// times; after that the chat shows what it has, not working.
			if (this.current?.id === id && run.retries < 8) {
				run.retries++;
				again(Math.min(1000 * 2 ** (run.retries - 1), 10000));
				return;
			}
			run.retries = 0;
			run.busy = false;
			run.live = idle();
		}
		void this.loadLists();
	};

	/** Chats the NAS named as running in an earlier answer of /agent/runs. */
	private seenRunning = new Set<string>();
	/** The chats of the account that run on the NAS, while one runs that
	 *  this page does not follow (1.01): its end updates the list, and its
	 *  chat shows the answer when opened. Only the RAM of titand is asked.
	 *  True while one of them still runs. */
	checkRuns = async (): Promise<boolean> => {
		let now: Set<string>;
		try {
			now = new Set((await api.get<{ runs?: string[] }>('/agent/runs')).runs ?? []);
		} catch {
			// No answer counts as a quiet one: the asking ends after a while.
			return false;
		}
		const followed = this.stream?.id;
		let ended = false;
		let still = false;
		for (const [id, run] of this.runs) {
			if (id === followed || !run.busy) continue;
			if (now.has(id)) still = true;
			else {
				run.busy = false;
				run.live = idle();
				ended = true;
			}
		}
		for (const c of this.chats) {
			if (c.author || c.id === followed || c.state !== 'running') continue;
			if (now.has(c.id)) {
				still = true;
				this.seenRunning.add(c.id);
			} else if (this.seenRunning.delete(c.id)) ended = true;
			// A chat that says "running" without ever having a run here is
			// not read again and again (the stored state of a run titand did
			// not finish; it is marked "interrupted" at the next start).
		}
		// Started elsewhere (another device, a planned task): shown as running.
		for (const id of now) if (id !== followed) this.listState(id, 'running');
		if (ended) await this.loadLists();
		return still;
	};

	/**
	 * Asks a message of the open chat again (PLAN 90-4): with a text it is
	 * changed first ("Bearbeiten"), without one the same is asked ("Neu
	 * erzeugen"). The chat forgets everything after it.
	 */
	resend = async (entry: Entry, text = '') => {
		if (!this.current) return;
		const id = this.current.id;
		if (this.runs.get(id)?.busy || this.sendingIn.has(id)) return;
		this.sendingIn.add(id);
		this.error = '';
		stopSpeaking();
		try {
			const chat = await api.post<Chat>(`/agent/chats/${id}/messages`, {
				message: text,
				replace: entry.id,
				...this.choice(),
				autonomy: this.autonomy,
			});
			this.started(chat);
			if (this.current?.id !== id) return;
			this.current = chat;
			this.watch(chat.id);
		} catch (e) {
			this.error = this.startError(e);
		} finally {
			this.sendingIn.delete(id);
		}
	};

	/** Messages on their way to the NAS, by chat ('' a new one): a second
	 *  tap sends nothing (two chats with the same question, or an error for
	 *  the second). */
	sendingIn = new SvelteSet<string>();
	/**
	 * Sends a message; false when nothing was sent (the text stays in the
	 * input then). While one is on its way or the chat's run works, nothing
	 * is sent; another chat may run meanwhile (1.01).
	 */
	send = async (text: string): Promise<boolean> => {
		// The chat the message belongs to; the person may open another one
		// while it is sent (a slow upload) – the view then stays there.
		const from = this.current?.id ?? null;
		const key = from ?? '';
		if (this.sendingIn.has(key) || (from && this.runs.get(from)?.busy)) return false;
		this.sendingIn.add(key);
		this.error = '';
		const files = this.attachments;
		const turn = this.opened;
		const here = () => (this.current?.id ?? null) === from && turn === this.opened;
		try {
			let target = this.current;
			const uploads = files.filter((a) => a.file);
			if (!target && uploads.length) {
				// An empty chat first, so the files are there before it runs.
				target = await api.post<Chat>('/agent/chats', {
					draft: true,
					project: this.project,
					...this.choice(),
					autonomy: this.autonomy,
				});
			}
			// 1.01: the NAS keeps a file under another name when the chat has
			// one of that name already; the message names the one it kept.
			const kept = new Map<object, string>();
			for (const a of uploads) {
				const res = await apiFetch(
					`/agent/chats/${target!.id}/attachments?name=${encodeURIComponent(a.name)}`,
					{ method: 'POST', body: a.file },
				);
				if (!res.ok) throw await responseError(res);
				const saved = (await res.json().catch(() => null)) as { name?: string } | null;
				if (saved?.name) kept.set(a, saved.name);
			}
			const refs = files.map((a) =>
				a.file
					? `[Anhang: ${kept.get(a) ?? a.name}]`
					: `[Datei: ${(a.ref ?? '').replace(':', '/')}]`,
			);
			const message = refs.length ? `${refs.join('\n')}\n\n${text}` : text;
			this.attachments = [];
			if (target) {
				const chat = await api.post<Chat>(`/agent/chats/${target.id}/messages`, {
					message,
					...this.choice(),
					autonomy: this.autonomy,
				});
				this.started(chat);
				if (here()) {
					this.current = chat;
					void this.go(`/agent?chat=${chat.id}`);
					this.watch(chat.id);
				}
			} else {
				const chat = await api.post<Chat>('/agent/chats', {
					message,
					project: this.project,
					...this.choice(),
					autonomy: this.autonomy,
				});
				this.started(chat);
				if (here()) {
					this.current = chat;
					void this.go(`/agent?chat=${chat.id}`);
					this.watch(chat.id);
				}
			}
			return true;
		} catch (e) {
			this.attachments = files;
			this.error = this.startError(e);
			return false;
		} finally {
			this.sendingIn.delete(key);
		}
	};

	undo = async (step: Entry) => {
		if (!this.current) return;
		this.error = '';
		const id = this.current.id;
		try {
			await api.post(`/agent/chats/${id}/undo`, { step: step.id });
			const fresh = await api.get<Chat>(`/agent/chats/${id}`);
			if (this.current?.id === id) this.current = fresh;
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
		const id = this.current.id;
		// The answer belongs to this chat's run: another chat that waits keeps
		// its step (1.01).
		const run = this.run(id);
		if (run.deciding) return;
		run.deciding = true;
		this.error = '';
		try {
			await api.post(`/agent/chats/${id}/decide`, { id: p.id, approve });
			this.listState(id, 'running');
			run.busy = true;
			// The answer belongs to the chat it was given in, also when
			// another one was opened while it was on its way.
			if (this.current?.id !== id) return;
			this.current.pending = null;
			this.watch(id);
		} catch (e) {
			this.error = this.startError(e);
			// Decided on another device meanwhile: show the chat as it is.
			try {
				const fresh = await api.get<Chat>(`/agent/chats/${id}`);
				if (this.current?.id === id) this.current = fresh;
			} catch {
				// The error above stays.
			}
		} finally {
			run.deciding = false;
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
			if (this.current?.id === chat.id) {
				const fresh = await api.get<Chat>(`/agent/chats/${chat.id}`);
				if (this.current?.id === chat.id) this.current = fresh;
			}
		} catch (e) {
			this.error = apiErrorText(e);
		}
	};
	remove = async (chat: ChatInfo) => {
		try {
			await api.delete(`/agent/chats/${chat.id}`);
			if (this.current?.id === chat.id) this.newChat();
			this.runs.delete(chat.id);
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
