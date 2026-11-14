// The KI-Agent's page, second part: the views of the functions in the menu,
// the settings beneath a window, the phone's keyboard and edge swipes, the
// suggestions of the start page, what the side bar gets, and the effects
// that load and keep everything current. Built on agent-chats.svelte.ts.
import { page } from '$app/state';
import { untrack } from 'svelte';
import { api, setAgentProfile } from '#lib/api.ts';
import type { IconName } from '#lib/components/Icon.svelte';
import { nasSpeech, speechPrefs } from '#lib/agent-speech.svelte.ts';
import { t } from '#lib/i18n/index.svelte.ts';
import { pageTitle } from '#lib/page-title.svelte.ts';
import { loadMine, type FeatureViewId } from '#lib/agent-features.ts';
import { inWindow } from '#lib/desktop/frame.ts';
import {
	accountLook,
	DEVICE_LOOK,
	sanitizeLook,
	type ChatInfo,
	type Project,
	type Suggestion,
	type Swipe,
	type NasHint,
	AGENT_VIEWS,
	type AgentView,
} from '#lib/agent.ts';
import { AgentChats, LOOK_KEY } from './agent-chats.svelte.ts';

export class AgentPage extends AgentChats {
	featureItems = $derived.by(() => {
		const f = this.features;
		const admin = !!this.status?.admin;
		const out: { id: FeatureViewId; label: string; icon: IconName; count?: number }[] = [];
		if (!f) return out;
		if (f.paper) out.push({ id: 'paper', label: t('agent.f.menu.paper'), icon: 'list' });
		if (f.deadlines)
			out.push({ id: 'deadlines', label: t('agent.f.menu.deadlines'), icon: 'clock' });
		if (this.protectMe?.guard)
			out.push({
				id: 'guard',
				label: t('agent.f.menu.guard'),
				icon: 'shield',
				count: this.protectMe.alerts,
			});
		if (this.protectMe?.handbook)
			out.push({ id: 'handbook', label: t('agent.f.menu.handbook'), icon: 'fileText' });
		if (f.emergency && (admin || this.protectMe?.emergency))
			out.push({
				id: 'emergency',
				label: t('agent.f.menu.emergency'),
				icon: 'key',
				count: this.protectMe?.requests,
			});
		if (f.advisor && admin)
			out.push({ id: 'advisor', label: t('agent.f.menu.advisor'), icon: 'chip' });
		if (f.buddy && admin) out.push({ id: 'buddy', label: t('agent.f.menu.buddy'), icon: 'users' });
		if (f.catalog) out.push({ id: 'catalog', label: t('agent.f.menu.catalog'), icon: 'zap' });
		return out;
	});
	/** A page beside the chat is shown (not the chat or the start page). */
	onPage = $derived(this.view !== 'chat');

	keyboard = $state(false);

	// On the phone a swipe from the left edge opens the menu, a swipe to
	// the left closes it again.
	swipeStart: { x: number; y: number } | null = null;
	// Swipes from the edge (PLAN 53, Einstellungen › Darstellung und Gesten):
	// from the left the chats, from the right the settings; back again.
	touchStart = (e: TouchEvent) => {
		const p = e.touches[0];
		const edges = this.look.gestures && this.look.edge;
		const left = p.clientX <= 24;
		const right = p.clientX >= innerWidth - 24;
		this.swipeStart =
			this.phone && !this.dialog && (this.menuOpen || (edges && (left || right)))
				? { x: p.clientX, y: p.clientY }
				: null;
	};
	touchMove = (e: TouchEvent) => {
		if (!this.swipeStart) return;
		const p = e.touches[0];
		const dx = p.clientX - this.swipeStart.x;
		if (Math.abs(p.clientY - this.swipeStart.y) > Math.abs(dx)) this.swipeStart = null;
		else if (!this.menuOpen && dx > 50) {
			this.swipeStart = null;
			this.menuOpen = true;
			this.buzz(8);
		} else if (!this.menuOpen && dx < -50) {
			this.swipeStart = null;
			this.show('settings');
			this.buzz(8);
		} else if (this.menuOpen && dx < -50) {
			this.swipeStart = null;
			this.menuOpen = false;
		}
	};

	// PLAN 53: suggestions that fit the moment – warnings of the NAS first,
	// then by time of day, then a changing pick of the rest ("andere").
	// Open suggestions of the agent (PLAN 53), shown as a number in the menu.
	inboxCount = $state(0);
	/** Getting to know: a new chat with the question to look at the NAS. */
	getToKnow = () => {
		this.dialog = null;
		this.newChat('');
		void this.send(t('agent.knowAsk'));
	};
	hints = $state<NasHint[]>([]);
	seed = Math.floor(Math.random() * 100);
	shuffle = $state(0);
	suggestions = $derived.by<Suggestion[]>(() => {
		const hour = new Date().getHours();
		const fromNas: Suggestion[] = this.hints.map((h) => ({
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
		if (this.chats.length === 0)
			timed.unshift({
				title: t('agent.suggKnow'),
				sub: t('agent.suggKnowSub'),
				text: t('agent.knowAsk'),
				tag: t('agent.tagNew'),
				tone: 'ok',
				icon: 'sparkle',
				color: '#8b5cf6',
			});
		const start = this.seed % pool.length;
		const all = [...fromNas, ...timed, ...pool.slice(start), ...pool.slice(0, start)];
		const from = (this.shuffle * 3) % all.length;
		return [...all.slice(from), ...all.slice(0, from)].slice(0, 3);
	});
	currentInfo = $derived(
		this.current && !this.readOnlyBy
			? this.chats.find((c) => c.id === this.current!.id && !c.author)
			: undefined,
	);
	currentProject = $derived(
		this.projects.find((p) => p.id === (this.current?.project ?? this.project)),
	);
	menuProps = $derived({
		chats: this.chats,
		projects: this.projects,
		current: this.current?.id,
		project: this.project,
		admin: this.status?.admin ?? false,
		view: this.current || this.onPage ? this.view : ('start' as const),
		onshow: (view: AgentView) => {
			if (view === 'chat') this.newChat('');
			else this.show(view);
		},
		onsearchmemory: (query: string) => this.show('memory', query),
		waitingCount: this.inboxCount + this.chats.filter((c) => c.state === 'waiting').length,
		onopen: (id: string, author?: string) => {
			if (this.dialog?.kind === 'feature') this.dialog = null;
			this.open(id, true, author ?? '');
		},
		onnew: () => {
			if (this.dialog?.kind === 'feature') this.dialog = null;
			this.newChat('');
		},
		onproject: (id: string) => {
			this.project = id;
			// On the phone the menu stays open and shows the project's chats.
			if (!this.phone && (!id || this.current?.project !== id)) this.newChat(id);
		},
		onnewproject: () => (this.dialog = { kind: 'project', project: null }),
		oneditproject: (p: Project) => (this.dialog = { kind: 'project', project: p }),
		features: this.featureItems,
		onfeature: (id: FeatureViewId) => {
			this.menuOpen = false;
			this.dialog = { kind: 'feature', id };
		},
		onnotes: () => {
			this.menuOpen = false;
			this.dialog = { kind: 'notes' };
		},
		plannedCount: this.plannedCount,
		onarchive: () => {
			this.menuOpen = false;
			this.dialog = { kind: 'archive' };
		},
		onfold: this.phone ? undefined : () => this.setFolded(true),
		gestures: this.look.gestures,
		pull: this.look.pull,
		compact: this.look.compact,
		onrefresh: () => void this.loadLists(),
		swipeLeft: this.look.swipeLeft,
		swipeRight: this.look.swipeRight,
		onaction: (chat: ChatInfo, action: Swipe) => {
			this.buzz(12);
			if (action === 'pin') void this.patch(chat, { pinned: !chat.pinned });
			else if (action === 'archive') void this.patch(chat, { archived: !chat.archived });
			else if (action === 'delete') this.dialog = { kind: 'delete', chat };
			else if (action === 'menu') this.dialog = { kind: 'actions', chat };
		},
		ontitanos: this.phone || this.installed ? this.toTitanOS : undefined,
		profile: this.profile,
	});

	constructor() {
		super();
		$effect(() => {
			if (!this.status?.enabled || !this.status.allowed) return;
			loadMine()
				.then((m) => (this.features = m.nas))
				.catch(() => (this.features = null));
			api
				.get<AgentChats['protectMe']>('/agent/protect/me')
				.then((p) => (this.protectMe = p))
				.catch(() => (this.protectMe = null));
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
			// The views of "Schutz" work without the agent (it may be off).
			const always: FeatureViewId[] = ['guard', 'handbook', 'emergency'];
			if (id && known.includes(id) && this.status && (this.status.enabled || always.includes(id)))
				this.dialog = { kind: 'feature', id };
		});
		// PLAN 92: the speech services of the NAS, when the agent may use them.
		$effect(() => {
			if (!this.status?.enabled || !this.status.allowed) return;
			api
				.get<{ say: boolean; hear: boolean }>('/agent/speech')
				.then((r) => ((nasSpeech.say = !!r.say), (nasSpeech.hear = !!r.hear)))
				.catch(() => ((nasSpeech.say = false), (nasSpeech.hear = false)));
		});
		// PLAN 91: leaving the page ends the profile for other pages' requests.
		$effect(() => () => setAgentProfile(''));
		// PLAN 90-4: the address names the page.
		$effect(() => {
			const name = page.url.searchParams.get('page') as AgentView | null;
			this.view = name && AGENT_VIEWS.includes(name) ? name : 'chat';
			this.part = page.url.searchParams.get('part') ?? '';
		});
		// PLAN 59: on the phone the app follows the visible part of the screen.
		// With the keyboard open iOS keeps the page height and pans instead; the
		// app then sits in what is visible and the composer right on the
		// keyboard, without the gap of the home indicator.
		$effect(() => {
			const vv = window.visualViewport;
			if (!vv || !(this.app && this.phone)) return;
			const root = document.documentElement;
			const update = () => {
				// Pinch zoom also shrinks the visible part: then nothing moves.
				if (Math.abs(vv.scale - 1) > 0.01) return;
				root.style.setProperty('--ag-vvh', `${vv.height}px`);
				root.style.setProperty('--ag-vvtop', `${vv.offsetTop}px`);
				this.keyboard = root.clientHeight - vv.height > 120;
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
				this.keyboard = false;
			};
		});

		$effect(() => {
			const query = matchMedia('(max-width: 760px)');
			const change = () => {
				this.phone = query.matches;
				if (!this.phone) this.menuOpen = false;
			};
			query.addEventListener('change', change);
			return () => query.removeEventListener('change', change);
		});
		$effect(() => {
			// In a window the title bar names the program already.
			const names = {
				planned: t('agent.planned'),
				memory: t('agent.memory'),
				knowledge: t('agent.knowledge'),
				settings: t('agent.set.title'),
			};
			if (this.view !== 'chat') {
				pageTitle.value = inWindow ? names[this.view] : `${names[this.view]} · ${t('nav.agent')}`;
				return () => (pageTitle.value = '');
			}
			pageTitle.value = this.current?.title
				? inWindow
					? this.current.title
					: `${this.current.title} · ${t('nav.agent')}`
				: t('nav.agent');
			return () => (pageTitle.value = '');
		});
		$effect(() => {
			api
				.get<Record<string, unknown>>('/agent/prefs')
				// Saved only after a successful read, so a hiccup never overwrites
				// the account's settings with this device's copy.
				.then((saved) => {
					if (saved && Object.keys(saved).length) {
						const device = Object.fromEntries(DEVICE_LOOK.map((k) => [k, this.look[k]]));
						this.look = sanitizeLook({ ...this.look, ...saved, ...device });
					}
					this.prefsLoaded = true;
				})
				.catch(() => {});
		});
		$effect(() => {
			const value = JSON.stringify(this.look);
			try {
				localStorage.setItem(LOOK_KEY, value);
			} catch {
				// Only remembered while the page is open.
			}
			if (!this.prefsLoaded) return;
			const body = accountLook(this.look);
			clearTimeout(this.prefsTimer);
			this.prefsTimer = setTimeout(() => void api.put('/agent/prefs', body).catch(() => {}), 600);
		});
		$effect(() => {
			speechPrefs.rate = this.look.rate;
		});
		// Keep the screen on while the agent works (device setting, PLAN 53).
		$effect(() => {
			const working = this.look.keepAwake && this.chats.some((c) => c.state === 'running');
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

		$effect(() => {
			void (async () => {
				await this.loadStatus();
				if (this.status?.allowed && this.status.enabled) {
					await this.loadLists();
					void this.loadLocations();
					const id = page.url.searchParams.get('chat');
					if (id) void this.open(id, false, page.url.searchParams.get('author') ?? '');
					else if (
						!page.url.searchParams.has('ask') &&
						!page.url.searchParams.has('attach') &&
						!page.url.searchParams.has('page')
					) {
						// PLAN 53: what opens first is a setting of the account.
						const saved = await api
							.get<{ start?: string }>('/agent/prefs')
							.catch(() => ({}) as { start?: string });
						const start = saved.start ?? this.look.start;
						const last = [...this.chats]
							.filter((c) => !c.author)
							.sort((a, b) => b.updated.localeCompare(a.updated))[0];
						if (start === 'last' && last) void this.open(last.id, false);
						else if (start === 'overview' && this.phone) this.menuOpen = true;
					}
					// From the file manager, the dashboard or a notification.
					const attach = page.url.searchParams.get('attach');
					if (attach)
						this.attachments = attach
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
					if (ask || attach) queueMicrotask(() => this.composer?.fill(ask ?? ''));
					// Ollama loads the model now, not with the first message.
					void api.post('/agent/preload', { model: this.model }).catch(() => {});
					this.listsReady = true;
				}
			})();
			return () => this.stream?.abort();
		});

		// PLAN 75-16: a notification tapped while the agent is already open moves
		// it to its chat (/agent?chat=…); the effect above reads the address only
		// once, after its awaits.
		$effect(() => {
			const id = page.url.searchParams.get('chat');
			const author = page.url.searchParams.get('author') ?? '';
			if (!this.listsReady || !id || id === untrack(() => this.current?.id)) return;
			untrack(() => void this.open(id, false, author));
		});

		$effect(() => {
			const id = this.current?.id;
			if (!id || this.readOnlyBy) return;
			const visible = () => document.visibilityState === 'visible';
			const ping = () => this.seen(id, visible());
			ping();
			const timer = setInterval(() => visible() && this.seen(id, true), 15000);
			document.addEventListener('visibilitychange', ping);
			// iOS may close the app without a visibility change (swiped away).
			const gone = () => this.seen(id, false);
			window.addEventListener('pagehide', gone);
			return () => {
				clearInterval(timer);
				document.removeEventListener('visibilitychange', ping);
				window.removeEventListener('pagehide', gone);
				this.seen(id, false);
			};
		});
		$effect(() => {
			api
				.get<{ items: unknown[] }>('/agent/inbox')
				.then((r) => (this.inboxCount = r.items?.length ?? 0))
				.catch(() => (this.inboxCount = 0));
		});
		$effect(() => {
			api
				.get<{ suggestions: NasHint[] }>('/agent/suggestions')
				.then((r) => (this.hints = r.suggestions ?? []))
				.catch(() => (this.hints = []));
		});
	}
}
