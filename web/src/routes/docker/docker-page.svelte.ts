// Docker › Container: the page's state and actions – the apps as rows, their
// figures and filters, requests on their way, the menus and the dialogs.
// The page and its parts (AppTable, PageMenu, DockerDialogs) get this one object.
import { poll, type Poller } from '#lib/poll.ts';
import { onMount } from 'svelte';
import { SvelteSet } from 'svelte/reactivity';
import { goto } from '$app/navigation';
import { phoneView } from '#lib/desktop/mode.svelte.ts';
import { session } from '#lib/session.svelte.ts';
import {
	sanitizeDockerLayout,
	dockerGroups,
	arrangedDockerGroups,
	stackPrimary,
	type DockerGroup,
} from '#lib/docker-layout.ts';
import { dockerName } from '#lib/docker-name.ts';
import {
	api,
	ApiError,
	type AppProxyStatus,
	type DockerContainer,
	type DockerStatus,
} from '#lib/api.ts';
import { inTailnet, tailnetPort } from '#lib/app-proxy.ts';
import { errorText, i18n, t } from '#lib/i18n/index.svelte.ts';
import { storage } from '#lib/storage.svelte.ts';
import { system } from '#lib/system.svelte.ts';
import { tailnetHost, type TailnetStatus } from '#lib/app-links.ts';
import { isRunning, portLabels } from './container.ts';

export type RowState = 'running' | 'partial' | 'stopped' | 'restarting' | 'error';

export class DockerPage {
	tailnet = $state<TailnetStatus | null>(null);
	tailscaleHost = $derived(tailnetHost(this.tailnet));
	NAME = /^[a-z0-9][a-z0-9_-]{0,31}$/;

	status = $state<DockerStatus | null>(null);
	autoUpdates = $state(false);
	error = $state<{ text: string; detail: string } | null>(null);
	// What has a request on its way: `container:<id>`, `app:<name>` or `global`
	// for actions on everything. Other rows stay usable meanwhile.
	busyKeys = new SvelteSet<string>();
	appKey = (app: string) => `app:${app}`;
	containerKey = (container: DockerContainer) => `container:${container.id}`;
	globalBusy = $derived(this.busyKeys.has('global'));
	// Requests of the dialogs (remove, import, settings).
	dialogBusy = $state(false);
	search = $state('');
	filter = $state<'all' | 'running' | 'stopped' | 'updates'>('all');
	linkGroup = $state<DockerGroup | null>(null);
	primaryService = $state('');
	refreshError = $state('');
	lastRefresh = $state('');
	refreshing = $state(false);
	// The stored order and the main service of each stack (no view settings any more).
	layout = $state(sanitizeDockerLayout(session.docker));
	savingLayout = $state(false);
	layoutError = $state('');
	dialog = $state<
		| { kind: 'import' | 'settings' }
		| { kind: 'logs'; container: DockerContainer }
		| { kind: 'updates'; selected: string[]; failed: { app: string; text: string }[] }
		| { kind: 'remove'; container: DockerContainer }
		| { kind: 'removeApp'; app: string }
		| null
	>(null);
	draft = $state({ name: '', compose: '', enabled: true, volume: '' });
	dialogError = $state<{ text: string; detail: string } | null>(null);

	volumes = $derived((storage.status?.volumes ?? []).map((volume) => volume.name));
	host = $derived(system.info?.network?.ipv4.split('/')[0] || location.hostname);
	containers = $derived(this.status?.containers ?? []);
	runCount = $derived(this.containers.filter(isRunning).length);
	groups = $derived(arrangedDockerGroups(dockerGroups(this.containers), this.layout, i18n.locale));

	// ---- One row per app ----

	rowState = (group: DockerGroup): RowState => {
		const m = group.members;
		if (m.some((c) => c.state === 'dead') || (this.jobFailed(group.name) && !m.some(isRunning)))
			return 'error';
		if (m.some((c) => c.state === 'restarting')) return 'restarting';
		const running = m.filter((c) => c.state === 'running').length;
		if (running === m.length) return 'running';
		return running ? 'partial' : 'stopped';
	};
	jobFailed = (app: string) => this.status?.jobs?.[app]?.state === 'failed';
	running = (group: DockerGroup) => group.members.filter(isRunning);
	groupCpu = (group: DockerGroup) => this.running(group).reduce((s, c) => s + c.cpuPercent, 0);
	groupRam = (group: DockerGroup) => this.running(group).reduce((s, c) => s + c.memoryBytes, 0);
	groupPorts = (group: DockerGroup) => [...new Set(group.members.flatMap(portLabels))];
	groupUpdate = (group: DockerGroup) => group.members.some((c) => c.updateAvailable);
	groupApp = (group: DockerGroup) => group.members.find((c) => c.managed && c.app)?.app ?? '';
	groupBusy = (group: DockerGroup) =>
		this.busyKeys.has(this.appKey(group.name)) ||
		group.members.some((c) => this.busyKeys.has(this.containerKey(c)));
	subLine = (group: DockerGroup): string => {
		const host = group.members.some((c) => c.hostNetwork) ? t('docker.table.hostNetwork') : '';
		const base =
			group.members.length > 1
				? t('docker.table.containers', { count: group.members.length })
				: (group.members[0]?.image ?? '');
		return [base, host].filter(Boolean).join(' · ');
	};
	stateOf = $derived(new Map(this.groups.map((g) => [g.id, this.rowState(g)])));
	isOn = (group: DockerGroup) => {
		const s = this.stateOf.get(group.id);
		return s === 'running' || s === 'partial';
	};
	counts = $derived({
		all: this.groups.length,
		running: this.groups.filter(this.isOn).length,
		stopped: this.groups.filter((g) => this.stateOf.get(g.id) === 'stopped').length,
		updates: this.groups.filter(this.groupUpdate).length,
	});
	shownGroups = $derived(
		this.groups.filter((group) => {
			if (this.filter === 'running' && !this.isOn(group)) return false;
			if (this.filter === 'stopped' && this.stateOf.get(group.id) !== 'stopped') return false;
			if (this.filter === 'updates' && !this.groupUpdate(group)) return false;
			const q = this.search.trim().toLowerCase();
			return (
				!q ||
				[group.name, ...group.members.flatMap((c) => [c.name, c.image, c.service])]
					.join(' ')
					.toLowerCase()
					.includes(q)
			);
		}),
	);
	totalCpu = $derived(this.containers.reduce((s, c) => s + (isRunning(c) ? c.cpuPercent : 0), 0));
	totalRam = $derived(this.containers.reduce((s, c) => s + (isRunning(c) ? c.memoryBytes : 0), 0));
	// A colour of its own for an app without an icon, from its name.
	hue = (name: string) => {
		let h = 0;
		for (const ch of name) h = (h * 31 + ch.charCodeAt(0)) % 360;
		return h;
	};

	updatable = $derived(
		this.containers.some((container) => container.updateAvailable && container.managed),
	);
	checked = $derived.by(() => {
		const time = new Date(this.status?.updatesChecked ?? '').getTime();
		if (!time || time < 0) return t('docker.updatesNever');
		return t('docker.updatesChecked', {
			time: new Intl.DateTimeFormat(i18n.locale, { dateStyle: 'short', timeStyle: 'short' }).format(
				time,
			),
		});
	});
	jobs = $derived(
		Object.entries(this.status?.jobs ?? {}).filter(([, job]) => job.state !== 'done'),
	);
	// Apps whose setup never created a container (or whose containers are
	// gone) have no row; without this they could not be removed.
	idleApps = $derived(
		(this.status?.apps ?? []).filter(
			(app) =>
				!this.containers.some((container) => container.app === app) &&
				!this.jobs.some(([name]) => name === app),
		),
	);

	problem = (err: unknown): { text: string; detail: string } => {
		const code = err instanceof ApiError ? err.code : 'internal';
		const detailed = code === 'invalid_compose' || code === 'docker_failed';
		return {
			text: errorText(code),
			detail: detailed && err instanceof ApiError ? err.message : '',
		};
	};

	// Refreshes go through the poller: never two at once, and one asked for
	// during a run follows it, so an older answer cannot win.
	poller: Poller | undefined;
	load = () => this.poller?.now() ?? Promise.resolve();
	fetchStatus = async () => {
		this.refreshing = true;
		try {
			this.status = await api.get<DockerStatus>('/docker');
			this.refreshError = '';
			this.lastRefresh = new Date().toISOString();
		} catch (err) {
			this.refreshError = this.problem(err).text;
		} finally {
			this.refreshing = false;
		}
	};
	editLinks = (group: DockerGroup) => {
		this.linkGroup = group;
		this.primaryService = this.layout.primaryServices[group.id] ?? '';
		this.layoutError = '';
	};
	saveLinks = async () => {
		if (!this.linkGroup || this.savingLayout) return;
		const primaryServices = { ...this.layout.primaryServices };
		if (this.primaryService) primaryServices[this.linkGroup.id] = this.primaryService;
		else delete primaryServices[this.linkGroup.id];
		// The rest of the stored preference (older view fields too) stays as it is.
		const value = sanitizeDockerLayout({ ...this.layout, primaryServices });
		this.savingLayout = true;
		this.layoutError = '';
		try {
			await api.put('/preferences/docker', value);
			session.docker = value;
			this.layout = value;
			this.linkGroup = null;
		} catch (err) {
			this.layoutError = this.problem(err).text;
		} finally {
			this.savingLayout = false;
		}
	};

	// Apps in the tailnet (their own device, appproxy): read when the page
	// opens and after "Im Tailnet veröffentlichen" changed something.
	proxy = $state<AppProxyStatus | null>(null);
	tailnetDialog = $state<{ app: string; mode: 'publish' | 'remove'; port: number } | null>(null);
	loadProxy = async () => {
		try {
			this.proxy = await api.get<AppProxyStatus>('/docker/proxy');
		} catch {
			this.proxy = null;
		}
	};
	appInTailnet = (app: string) => inTailnet(this.proxy?.apps?.find((route) => route.app === app));
	openTailnet = (app: string) => {
		const members = this.containers.filter((container) => container.app === app);
		const primary = stackPrimary(
			{ id: `app:${app}`, name: app, members },
			this.layout.primaryServices,
		);
		this.tailnetDialog = {
			app,
			mode: this.appInTailnet(app) ? 'remove' : 'publish',
			port: tailnetPort(members, primary),
		};
	};

	// The failed action, for "Retry" next to its error.
	failedAction = $state<{ key: string; action: () => Promise<unknown>; label: string } | null>(
		null,
	);
	run = async (key: string, action: () => Promise<unknown>, label = '') => {
		if (this.busyKeys.has(key)) return;
		this.busyKeys.add(key);
		this.error = null;
		this.failedAction = null;
		try {
			await action();
		} catch (err) {
			const failed = this.problem(err);
			// Says which app it was, as other rows stay usable.
			this.error = label ? { ...failed, text: `${label}: ${failed.text}` } : failed;
			this.failedAction = { key, action, label };
		}
		await this.load();
		this.busyKeys.delete(key);
	};

	/** Start, stop or restart every container of a row, one after the other like by hand. */
	groupAction = (group: DockerGroup, action: 'start' | 'stop' | 'restart') => {
		const targets = group.members.filter((c) =>
			action === 'start' ? !isRunning(c) : action === 'stop' ? isRunning(c) : true,
		);
		void this.run(
			this.appKey(group.name),
			async () => {
				for (const c of targets)
					await api.post(`/docker/containers/${encodeURIComponent(c.id)}/${action}`);
			},
			group.name,
		);
	};
	updateApp = (app: string) => {
		void this.run(
			this.appKey(app),
			() => api.post(`/docker/apps/${encodeURIComponent(app)}/update`),
			app,
		);
	};
	removeGroup = (group: DockerGroup) => {
		this.dialogError = null;
		const app = this.groupApp(group);
		if (app) this.askRemoveApp(app);
		else this.dialog = { kind: 'remove', container: group.members[0] };
	};
	/** PLAN 63: the app in its remote view window (the desktop opens it). */
	openRemote = (app: string) => {
		window.parent.postMessage(
			{ type: 'titanos-open-program', key: 'docker:' + app },
			location.origin,
		);
	};

	// "Update all" asks which apps first; all with an update are ticked.
	updateApps = $derived.by(() => {
		const apps = new Map<string, string[]>();
		for (const container of this.containers)
			if (container.updateAvailable && container.managed && container.app)
				apps.set(container.app, [
					...(apps.get(container.app) ?? []),
					dockerName(container, 'distinct'),
				]);
		return [...apps].map(([app, names]) => ({ app, names }));
	});
	askUpdates = () => {
		this.dialogError = null;
		this.dialog = {
			kind: 'updates',
			selected: this.updateApps.map((entry) => entry.app),
			failed: [],
		};
	};
	applyUpdates = async () => {
		if (this.dialog?.kind !== 'updates' || this.dialogBusy) return;
		const chosen = [...this.dialog.selected];
		const failed: { app: string; text: string }[] = [];
		this.dialogBusy = true;
		this.busyKeys.add('global');
		for (const app of chosen) {
			this.busyKeys.add(this.appKey(app));
			try {
				await api.post(`/docker/apps/${encodeURIComponent(app)}/update`);
			} catch (err) {
				failed.push({ app, text: this.problem(err).text });
			} finally {
				this.busyKeys.delete(this.appKey(app));
			}
		}
		this.busyKeys.delete('global');
		this.dialogBusy = false;
		await this.load();
		if (this.dialog?.kind !== 'updates') return;
		// Started ones are done here; failed ones stay ticked for another try.
		if (failed.length === 0) this.dialog = null;
		else {
			this.dialog.failed = failed;
			this.dialog.selected = failed.map((entry) => entry.app);
		}
	};

	remove = async (container: DockerContainer) => {
		this.dialogBusy = true;
		this.dialogError = null;
		this.busyKeys.add(this.containerKey(container));
		try {
			await api.delete(`/docker/containers/${encodeURIComponent(container.id)}`);
			this.dialog = null;
			await this.load();
		} catch (err) {
			this.dialogError = this.problem(err);
		} finally {
			this.dialogBusy = false;
			this.busyKeys.delete(this.containerKey(container));
		}
	};

	askRemoveApp = (app: string) => {
		this.dialogError = null;
		this.dialog = { kind: 'removeApp', app };
	};

	removeApp = async (app: string, purge: boolean) => {
		this.dialogBusy = true;
		this.dialogError = null;
		this.busyKeys.add(this.appKey(app));
		try {
			await api.delete(
				`/docker/apps/${encodeURIComponent(app)}${purge ? `?deleteData=${encodeURIComponent(app)}` : ''}`,
			);
			this.dialog = null;
			await this.load();
		} catch (err) {
			this.dialogError = this.problem(err);
		} finally {
			this.dialogBusy = false;
			this.busyKeys.delete(this.appKey(app));
		}
	};

	open = (kind: 'import' | 'settings') => {
		this.draft = {
			name: '',
			compose: '',
			enabled: this.status?.settings.enabled ?? true,
			volume: this.status?.settings.volume || this.volumes[0] || '',
		};
		this.dialogError = null;
		this.dialog = { kind };
	};

	submit = async (event: SubmitEvent) => {
		event.preventDefault();
		// One request at a time: a second click or Enter must not import twice.
		if (this.dialogBusy) return;
		this.dialogBusy = true;
		this.dialogError = null;
		try {
			if (this.dialog?.kind === 'import') {
				if (!this.NAME.test(this.draft.name))
					return void (this.dialogError = { text: errorText('invalid_app_name'), detail: '' });
				await api.post('/docker/apps', { name: this.draft.name, compose: this.draft.compose });
			} else {
				await api.put('/docker/settings', {
					enabled: this.draft.enabled,
					volume: this.draft.volume,
				});
			}
			this.dialog = null;
			await this.load();
		} catch (err) {
			this.dialogError = this.problem(err);
		} finally {
			this.dialogBusy = false;
		}
	};

	// The menus of the tool bar: ⋯ (everything at once, settings, LAN,
	// auto updates) and the arrow next to "Install app" (catalog, compose file,
	// own container); the menu of a row.
	pageMenu = $state<'more' | 'install' | null>(null);
	pageMenuAt = $state<{ x: number; y: number } | null>(null);
	pageMenuWasOpen = false;
	openPageMenu = (kind: 'more' | 'install', anchor: HTMLElement) => {
		if (this.pageMenuWasOpen) {
			this.pageMenuWasOpen = false;
			this.pageMenu = null;
			return;
		}
		const rect = anchor.getBoundingClientRect();
		this.pageMenuAt = { x: Math.max(8, rect.right - 280), y: rect.bottom + 4 };
		this.pageMenu = kind;
	};
	// The action reads what the menu was opened for before the menu closes.
	pick = (action: () => void) => {
		action();
		this.pageMenu = null;
		this.rowMenu = null;
	};
	rowMenu = $state<{ group: DockerGroup; x: number; y: number } | null>(null);
	rowMenuWasOpen = '';
	openRowMenu = (group: DockerGroup, anchor: HTMLElement) => {
		if (this.rowMenuWasOpen === group.id) {
			this.rowMenuWasOpen = '';
			this.rowMenu = null;
			return;
		}
		const rect = anchor.getBoundingClientRect();
		this.rowMenu = { group, x: Math.max(8, rect.right - 260), y: rect.bottom + 4 };
	};
	/** A click on a row opens the app's own page (not on its buttons and links). */
	phone = $derived(phoneView());
	// PLAN 75-19 (style W4): a wide window shows the selected app at the
	// right; a click selects, a double click or the name opens its page. A
	// narrow window (and the phone) opens the page right away as before.
	wide = $state(matchMedia('(min-width: 1000px)').matches);
	selectedId = $state<string | null>(null);
	selected = $derived(
		this.wide && !this.phone
			? (this.shownGroups.find((g) => g.id === this.selectedId) ?? null)
			: null,
	);
	// Apps of several containers open downward to show them (style E3).
	expanded = new SvelteSet<string>();
	toggleMembers = (group: DockerGroup) => {
		if (this.expanded.has(group.id)) this.expanded.delete(group.id);
		else this.expanded.add(group.id);
	};

	openGroup = (event: MouseEvent, group: DockerGroup) => {
		const app = this.groupApp(group);
		if (app) void goto(`/docker/apps/${encodeURIComponent(app)}`);
		else this.rowMenu = { group, x: event.clientX, y: event.clientY };
	};
	rowClick = (event: MouseEvent, group: DockerGroup) => {
		if ((event.target as Element).closest('button, a, input')) return;
		if (this.wide && !this.phone) {
			this.selectedId = group.id;
			return;
		}
		this.openGroup(event, group);
	};
	rowOpen = (event: MouseEvent, group: DockerGroup) => {
		if ((event.target as Element).closest('button, a, input')) return;
		if (this.wide && !this.phone) this.openGroup(event, group);
	};
	rowKey = (event: KeyboardEvent, group: DockerGroup) => {
		if (event.key !== 'Enter' || (event.target as Element) !== event.currentTarget) return;
		const box = (event.currentTarget as Element).getBoundingClientRect();
		this.openGroup(
			new MouseEvent('click', { clientX: box.x + 40, clientY: box.y + box.height }),
			group,
		);
	};
	memberState = (c: DockerContainer) =>
		c.state === 'running'
			? 'running'
			: c.state === 'restarting'
				? 'restarting'
				: c.state === 'dead'
					? 'error'
					: 'stopped';

	checkUpdates = () =>
		this.run(
			'global',
			async () => (this.status = await api.post<DockerStatus>('/docker/updates/check')),
		);
	// Start and stop everything, one container after the other like by hand.
	confirmStopAll = $state(false);
	startAll = () =>
		this.run('global', async () => {
			for (const container of this.containers.filter((entry) => !isRunning(entry)))
				await api.post(`/docker/containers/${encodeURIComponent(container.id)}/start`);
		});
	stopAll = () => {
		this.confirmStopAll = false;
		void this.run('global', async () => {
			for (const container of this.containers.filter(isRunning))
				await api.post(`/docker/containers/${encodeURIComponent(container.id)}/stop`);
		});
	};

	enable = () =>
		this.run('global', () =>
			api.put('/docker/settings', { enabled: true, volume: this.draft.volume || this.volumes[0] }),
		);
	// PLAN 64: the phone's full-screen view (in a window: inWindow).
	// A narrow window: the filters get a row of their own under the tool bar.
	roomy = $state(matchMedia('(min-width: 760px)').matches);
	FILTERS = [
		['all', 'docker.table.filterAll'],
		['running', 'docker.table.filterRunning'],
		['stopped', 'docker.table.filterStopped'],
		['updates', 'docker.table.filterUpdates'],
	] as const;

	constructor() {
		$effect(() =>
			poll(
				async (signal) => {
					try {
						const value = await api.get<TailnetStatus>('/plugins/tailscale', signal);
						if (!signal.aborted) this.tailnet = value;
					} catch {
						if (!signal.aborted) this.tailnet = null;
					}
				},
				{ interval: 15000 },
			),
		);
		onMount(() => {
			const current = (this.poller = poll(this.fetchStatus, { interval: 3000 }));
			void this.loadProxy();
			return () => {
				current();
				if (this.poller === current) this.poller = undefined;
			};
		});

		// "Import compose file" on the Apps page leads here.
		$effect(() => {
			if (this.status?.running && location.hash === '#import') {
				history.replaceState(history.state, '', location.pathname);
				this.open('import');
			}
		});
		$effect(() => {
			const query = matchMedia('(min-width: 1000px)');
			const change = () => (this.wide = query.matches);
			query.addEventListener('change', change);
			return () => query.removeEventListener('change', change);
		});
		$effect(() => {
			const query = matchMedia('(min-width: 760px)');
			const change = () => (this.roomy = query.matches);
			query.addEventListener('change', change);
			return () => query.removeEventListener('change', change);
		});
	}
}
