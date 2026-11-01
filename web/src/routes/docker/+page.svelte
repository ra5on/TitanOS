<script lang="ts">
	import { poll, type Poller } from '#lib/poll.ts';
	import { tick, onMount } from 'svelte';
	import { SvelteSet } from 'svelte/reactivity';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import { session } from '#lib/session.svelte.ts';
	import {
		defaultDockerLayout,
		sanitizeDockerLayout,
		dockerGroups,
		orderedDockerGroups,
		moveDockerGroup,
		stackPrimary,
		type DockerGroup,
		type DockerView,
	} from '#lib/docker-layout.ts';
	import Icon from '#lib/components/Icon.svelte';
	import { dockerName } from '#lib/docker-name.ts';
	import {
		api,
		ApiError,
		type AppProxyStatus,
		type DockerContainer,
		type DockerStatus,
	} from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import AutoUpdateDialog from './AutoUpdateDialog.svelte';
	import TailnetDialog from './TailnetDialog.svelte';
	import { inTailnet, tailnetPort } from '#lib/app-proxy.ts';
	import { formatBytes, formatPercent } from '#lib/format.ts';
	import { apiErrorText, errorText, i18n, t } from '#lib/i18n/index.svelte.ts';
	import { storage } from '#lib/storage.svelte.ts';
	import { system } from '#lib/system.svelte.ts';
	import { addContainerConsole } from '#lib/terminal.svelte.ts';
	import { tailnetHost, type TailnetStatus } from '#lib/app-links.ts';
	import AppIcon from '#lib/components/AppIcon.svelte';
	import ContextMenu from '#lib/components/ContextMenu.svelte';
	import ContainerCard from './ContainerCard.svelte';
	import ContainerRow from './ContainerRow.svelte';
	import { isRunning, webLinks, type ContainerActions } from './container.ts';

	let tailnet = $state<TailnetStatus | null>(null);
	const tailscaleHost = $derived(tailnetHost(tailnet));
	$effect(() =>
		poll(
			async (signal) => {
				try {
					const value = await api.get<TailnetStatus>('/plugins/tailscale', signal);
					if (!signal.aborted) tailnet = value;
				} catch {
					if (!signal.aborted) tailnet = null;
				}
			},
			{ interval: 15000 },
		),
	);
	const NAME = /^[a-z0-9][a-z0-9_-]{0,31}$/;

	let status = $state<DockerStatus | null>(null);
	let autoUpdates = $state(false);
	let error = $state<{ text: string; detail: string } | null>(null);
	// What has a request on its way: `container:<id>`, `app:<name>` or `global`
	// for actions on everything. Other cards stay usable meanwhile.
	const busyKeys = new SvelteSet<string>();
	const appKey = (app: string) => `app:${app}`;
	const containerKey = (container: DockerContainer) => `container:${container.id}`;
	const containerBusy = (container: DockerContainer) =>
		busyKeys.has(containerKey(container)) ||
		(!!container.app && busyKeys.has(appKey(container.app)));
	const globalBusy = $derived(busyKeys.has('global'));
	// Requests of the dialogs (remove, import, settings).
	let dialogBusy = $state(false);
	let search = $state('');
	let filter = $state<'all' | 'running' | 'stopped' | 'updates'>('all');
	// A stack is closed until the user opens it; filters reveal matching services.
	let expanded = $state<Record<string, boolean>>({});
	// The menu of a stack (addresses, problems, links, tailnet, editor), from its icon.
	let stackMenu = $state<{ group: DockerGroup; x: number; y: number } | null>(null);
	let stackMenuWasOpen = false;
	function openStackMenu(group: DockerGroup, anchor: HTMLElement) {
		if (stackMenuWasOpen) {
			stackMenuWasOpen = false;
			stackMenu = null;
			return;
		}
		const rect = anchor.getBoundingClientRect();
		stackMenu = { group, x: rect.left, y: rect.bottom + 4 };
	}
	const stackOpen = (id: string) => expanded[id] ?? (search.trim() !== '' || filter !== 'all');
	let linkGroup = $state<DockerGroup | null>(null),
		primaryService = $state('');
	let refreshError = $state(''),
		lastRefresh = $state(''),
		refreshing = $state(false);
	let layout = $state(sanitizeDockerLayout(session.docker));
	let savedLayout = $state(sanitizeDockerLayout(session.docker));
	let editing = $state(false);
	let savingLayout = $state(false);
	let layoutError = $state('');
	let layoutSaved = $state(false);
	let dragged = $state<string | null>(null);
	function startEditing() {
		editing = true;
		layoutSaved = false;
		layoutError = '';
		search = '';
		filter = 'all';
	}
	// The view is saved right away; only the order waits for "Save layout".
	async function setView(view: DockerView) {
		if (layout.view === view) return;
		const previous = layout.view;
		layout.view = view;
		layoutError = '';
		const value = sanitizeDockerLayout({ ...savedLayout, view });
		savingLayout = true;
		try {
			await api.put('/preferences/docker', value);
			session.docker = value;
			savedLayout = value;
		} catch (err) {
			// Not saved: the page shows the view that is still stored.
			layout.view = previous;
			layoutError = t('docker.layout.viewFailed', { error: apiErrorText(err) });
		} finally {
			savingLayout = false;
		}
	}
	function move(from: string, to: string) {
		layout.order = moveDockerGroup(layout.order, groups, from, to);
	}
	function moveBy(id: string, offset: number) {
		const target = groups[groups.findIndex((group) => group.id === id) + offset];
		if (target) move(id, target.id);
	}
	function cancelLayout() {
		layout = sanitizeDockerLayout(savedLayout);
		editing = false;
		layoutError = '';
	}
	async function saveLayout() {
		savingLayout = true;
		layoutError = '';
		const value = sanitizeDockerLayout(layout);
		try {
			await api.put('/preferences/docker', value);
			session.docker = value;
			savedLayout = value;
			editing = false;
			layoutSaved = true;
		} catch (err) {
			layoutError = apiErrorText(err);
		} finally {
			savingLayout = false;
		}
	}
	let dialog = $state<
		| { kind: 'import' | 'settings' }
		| { kind: 'logs'; container: DockerContainer; text: string | null }
		| { kind: 'updates'; selected: string[]; failed: { app: string; text: string }[] }
		| { kind: 'remove'; container: DockerContainer }
		| { kind: 'removeApp'; app: string; purge: boolean }
		| null
	>(null);
	let draft = $state({ name: '', compose: '', enabled: true, volume: '' });
	let dialogError = $state<{ text: string; detail: string } | null>(null);

	const volumes = $derived((storage.status?.volumes ?? []).map((volume) => volume.name));
	const host = $derived(system.info?.network?.ipv4.split('/')[0] || location.hostname);
	const containers = $derived(status?.containers ?? []);
	const runCount = $derived(containers.filter(isRunning).length);
	const totals = $derived(
		t('docker.totals', {
			cpu: formatPercent(containers.reduce((sum, container) => sum + container.cpuPercent, 0)),
			ram: formatBytes(containers.reduce((sum, container) => sum + container.memoryBytes, 0)),
		}),
	);

	const visible = $derived(
		containers.filter(
			(container) =>
				(filter === 'all' ||
					(filter === 'updates'
						? container.updateAvailable
						: (filter === 'running') === isRunning(container))) &&
				(search === '' ||
					`${container.name} ${container.image} ${container.app}`
						.toLowerCase()
						.includes(search.toLowerCase())),
		),
	);
	const groups = $derived(orderedDockerGroups(dockerGroups(containers), layout.order));
	const visibleIDs = $derived(new Set(visible.map((container) => container.id)));
	const shownGroups = $derived(
		groups.filter((group) => group.members.some((container) => visibleIDs.has(container.id))),
	);
	const shownMembers = (group: DockerGroup) =>
		group.members.filter((container) => visibleIDs.has(container.id));
	const isStack = (group: DockerGroup) =>
		group.members.length > 1 || !!layout.primaryServices[group.id];
	const sum = (group: DockerGroup, value: (container: DockerContainer) => number) =>
		group.members.filter(isRunning).reduce((total, container) => total + value(container), 0);
	const updateCount = $derived(containers.filter((container) => container.updateAvailable).length);
	const updatable = $derived(
		containers.some((container) => container.updateAvailable && container.managed),
	);
	const checked = $derived.by(() => {
		const time = new Date(status?.updatesChecked ?? '').getTime();
		if (!time || time < 0) return t('docker.updatesNever');
		return t('docker.updatesChecked', {
			time: new Intl.DateTimeFormat(i18n.locale, { dateStyle: 'short', timeStyle: 'short' }).format(
				time,
			),
		});
	});
	const jobs = $derived(
		Object.entries(status?.jobs ?? {}).filter(([, job]) => job.state !== 'done'),
	);
	// Apps whose setup never created a container (or whose containers are
	// gone) have no card; without this they could not be removed.
	const idleApps = $derived(
		(status?.apps ?? []).filter(
			(app) =>
				!containers.some((container) => container.app === app) &&
				!jobs.some(([name]) => name === app),
		),
	);

	function problem(err: unknown): { text: string; detail: string } {
		const code = err instanceof ApiError ? err.code : 'internal';
		const detailed = code === 'invalid_compose' || code === 'docker_failed';
		return {
			text: errorText(code),
			detail: detailed && err instanceof ApiError ? err.message : '',
		};
	}

	// Refreshes go through the poller: never two at once, and one asked for
	// during a run follows it, so an older answer cannot win.
	let poller: Poller | undefined;
	const load = () => poller?.now() ?? Promise.resolve();
	async function fetchStatus() {
		refreshing = true;
		try {
			status = await api.get<DockerStatus>('/docker');
			refreshError = '';
			lastRefresh = new Date().toISOString();
		} catch (err) {
			refreshError = problem(err).text;
		} finally {
			refreshing = false;
		}
	}
	function editLinks(group: DockerGroup) {
		linkGroup = group;
		primaryService = savedLayout.primaryServices[group.id] ?? '';
		layoutError = '';
	}
	async function saveLinks() {
		if (!linkGroup || savingLayout) return;
		const primaryServices = { ...savedLayout.primaryServices };
		if (primaryService) primaryServices[linkGroup.id] = primaryService;
		else delete primaryServices[linkGroup.id];
		const value = sanitizeDockerLayout({ ...savedLayout, primaryServices });
		savingLayout = true;
		layoutError = '';
		try {
			await api.put('/preferences/docker', value);
			session.docker = value;
			savedLayout = value;
			layout = value;
			linkGroup = null;
		} catch (err) {
			layoutError = problem(err).text;
		} finally {
			savingLayout = false;
		}
	}
	async function showProblems(group: DockerGroup) {
		search = '';
		filter = 'all';
		expanded[group.id] = true;
		await tick();
		const container = group.members.find((c) => !isRunning(c) || c.state === 'restarting');
		if (container)
			document
				.getElementById(`docker-container-${encodeURIComponent(container.name)}`)
				?.scrollIntoView({ block: 'nearest' });
	}
	onMount(() => {
		const current = (poller = poll(fetchStatus, { interval: 3000 }));
		void loadProxy();
		return () => {
			current();
			if (poller === current) poller = undefined;
		};
	});

	// Apps in the tailnet (their own device, appproxy): read when the page
	// opens and after "Im Tailnet veröffentlichen" changed something.
	let proxy = $state<AppProxyStatus | null>(null);
	let tailnetDialog = $state<{ app: string; mode: 'publish' | 'remove'; port: number } | null>(
		null,
	);
	async function loadProxy() {
		try {
			proxy = await api.get<AppProxyStatus>('/docker/proxy');
		} catch {
			proxy = null;
		}
	}
	const appInTailnet = (app: string) => inTailnet(proxy?.apps?.find((route) => route.app === app));
	function openTailnet(app: string) {
		const members = containers.filter((container) => container.app === app);
		const primary = stackPrimary({ id: `app:${app}`, name: app, members }, layout.primaryServices);
		tailnetDialog = {
			app,
			mode: appInTailnet(app) ? 'remove' : 'publish',
			port: tailnetPort(members, primary),
		};
	}

	// The failed action, for "Retry" next to its error.
	let failedAction = $state<{ key: string; action: () => Promise<unknown>; label: string } | null>(
		null,
	);
	async function run(key: string, action: () => Promise<unknown>, label = '') {
		if (busyKeys.has(key)) return;
		busyKeys.add(key);
		error = null;
		failedAction = null;
		try {
			await action();
		} catch (err) {
			const failed = problem(err);
			// Says which container or app it was, as other cards stay usable.
			error = label ? { ...failed, text: `${label}: ${failed.text}` } : failed;
			failedAction = { key, action, label };
		}
		await load();
		busyKeys.delete(key);
	}

	// Logs: only the answer to the latest request for the open container
	// counts; a late answer for another container is dropped.
	let logsRequest = 0;
	let logsError = $state(''),
		logsFollow = $state(false),
		logsSearch = $state(''),
		logsLoading = $state(false);
	let logsBox: HTMLElement | undefined = $state();
	const logsFor = (container: DockerContainer) =>
		dialog?.kind === 'logs' && dialog.container.id === container.id;
	async function showLogs(container: DockerContainer, again = false) {
		const request = ++logsRequest;
		if (!again || !logsFor(container)) {
			dialog = { kind: 'logs', container, text: null };
			logsFollow = false;
			logsSearch = '';
		}
		logsError = '';
		logsLoading = true;
		try {
			const res = await api.get<{ logs: string }>(
				`/docker/containers/${container.id}/logs?tail=300`,
			);
			if (request !== logsRequest || !logsFor(container) || dialog?.kind !== 'logs') return;
			dialog.text = res.logs;
			if (logsFollow) {
				await tick();
				if (logsBox) logsBox.scrollTop = logsBox.scrollHeight;
			}
		} catch (err) {
			if (request === logsRequest && logsFor(container)) logsError = problem(err).text;
		} finally {
			if (request === logsRequest) logsLoading = false;
		}
	}
	// Following fetches the logs again every three seconds while the dialog is open.
	$effect(() => {
		const container = dialog?.kind === 'logs' ? dialog.container : null;
		if (!container || !logsFollow) return;
		return poll(() => showLogs(container, true), { interval: 3000, immediate: false });
	});
	type LogPart = { text: string; hit: boolean };
	const logParts = $derived.by<LogPart[]>(() => {
		const text = dialog?.kind === 'logs' ? (dialog.text ?? '') : '';
		const query = logsSearch.trim().toLowerCase();
		if (!query) return [{ text, hit: false }];
		const parts: LogPart[] = [];
		const lower = text.toLowerCase();
		let at = 0;
		for (let found = lower.indexOf(query); found >= 0; found = lower.indexOf(query, at)) {
			if (found > at) parts.push({ text: text.slice(at, found), hit: false });
			parts.push({ text: text.slice(found, found + query.length), hit: true });
			at = found + query.length;
		}
		if (at < text.length) parts.push({ text: text.slice(at), hit: false });
		return parts;
	});
	const logHits = $derived(logParts.filter((part) => part.hit).length);
	$effect(() => {
		// The first hit comes into view when the search changes.
		if (!logsSearch.trim() || !logsBox) return;
		void logHits;
		tick().then(() => logsBox?.querySelector('mark')?.scrollIntoView({ block: 'nearest' }));
	});
	function downloadLogs(container: DockerContainer, text: string) {
		const url = URL.createObjectURL(new Blob([text], { type: 'text/plain;charset=utf-8' }));
		const anchor = document.createElement('a');
		anchor.href = url;
		anchor.download = `${dockerName(container, 'distinct').replace(/[^\w.-]+/g, '_')}-logs.txt`;
		anchor.click();
		setTimeout(() => URL.revokeObjectURL(url), 1000);
	}

	// "Update all" asks which apps first; all with an update are ticked.
	const updateApps = $derived.by(() => {
		const apps = new Map<string, string[]>();
		for (const container of containers)
			if (container.updateAvailable && container.managed && container.app)
				apps.set(container.app, [
					...(apps.get(container.app) ?? []),
					dockerName(container, 'distinct'),
				]);
		return [...apps].map(([app, names]) => ({ app, names }));
	});
	function askUpdates() {
		dialogError = null;
		dialog = { kind: 'updates', selected: updateApps.map((entry) => entry.app), failed: [] };
	}
	async function applyUpdates() {
		if (dialog?.kind !== 'updates' || dialogBusy) return;
		const chosen = [...dialog.selected];
		const failed: { app: string; text: string }[] = [];
		dialogBusy = true;
		busyKeys.add('global');
		for (const app of chosen) {
			busyKeys.add(appKey(app));
			try {
				await api.post(`/docker/apps/${encodeURIComponent(app)}/update`);
			} catch (err) {
				failed.push({ app, text: problem(err).text });
			} finally {
				busyKeys.delete(appKey(app));
			}
		}
		busyKeys.delete('global');
		dialogBusy = false;
		await load();
		if (dialog?.kind !== 'updates') return;
		// Started ones are done here; failed ones stay ticked for another try.
		if (failed.length === 0) dialog = null;
		else {
			dialog.failed = failed;
			dialog.selected = failed.map((entry) => entry.app);
		}
	}

	async function remove(container: DockerContainer) {
		dialogBusy = true;
		dialogError = null;
		busyKeys.add(containerKey(container));
		try {
			await api.delete(`/docker/containers/${encodeURIComponent(container.id)}`);
			dialog = null;
			await load();
		} catch (err) {
			dialogError = problem(err);
		} finally {
			dialogBusy = false;
			busyKeys.delete(containerKey(container));
		}
	}

	function actionsFor(container: DockerContainer): ContainerActions {
		return {
			onaction: (action) =>
				run(
					containerKey(container),
					() => api.post(`/docker/containers/${container.id}/${action}`),
					dockerName(container, 'distinct'),
				),
			onlogs: () => showLogs(container),
			onremove: () => {
				dialogError = null;
				container.managed ? askRemoveApp(container.app) : (dialog = { kind: 'remove', container });
			},
			onconsole: () => addContainerConsole(container.id, dockerName(container, 'distinct')),
			onupdate: () =>
				run(
					appKey(container.app),
					() => api.post(`/docker/apps/${container.app}/update`),
					container.app,
				),
			ontailnet: proxy ? () => openTailnet(container.app) : undefined,
			tailnet: appInTailnet(container.app),
		};
	}

	function askRemoveApp(app: string) {
		dialogError = null;
		dialog = { kind: 'removeApp', app, purge: false };
	}

	async function removeApp(app: string, purge: boolean) {
		dialogBusy = true;
		dialogError = null;
		busyKeys.add(appKey(app));
		try {
			await api.delete(
				`/docker/apps/${encodeURIComponent(app)}${purge ? `?deleteData=${encodeURIComponent(app)}` : ''}`,
			);
			dialog = null;
			await load();
		} catch (err) {
			dialogError = problem(err);
		} finally {
			dialogBusy = false;
			busyKeys.delete(appKey(app));
		}
	}

	function open(kind: 'import' | 'settings') {
		draft = {
			name: '',
			compose: '',
			enabled: status?.settings.enabled ?? true,
			volume: status?.settings.volume || volumes[0] || '',
		};
		dialogError = null;
		dialog = { kind };
	}

	async function submit(event: SubmitEvent) {
		event.preventDefault();
		// One request at a time: a second click or Enter must not import twice.
		if (dialogBusy) return;
		dialogBusy = true;
		dialogError = null;
		try {
			if (dialog?.kind === 'import') {
				if (!NAME.test(draft.name))
					return void (dialogError = { text: errorText('invalid_app_name'), detail: '' });
				await api.post('/docker/apps', { name: draft.name, compose: draft.compose });
			} else {
				await api.put('/docker/settings', { enabled: draft.enabled, volume: draft.volume });
			}
			dialog = null;
			await load();
		} catch (err) {
			dialogError = problem(err);
		} finally {
			dialogBusy = false;
		}
	}

	// "Import compose file" on the Apps page leads here.
	$effect(() => {
		if (status?.running && location.hash === '#import') {
			history.replaceState(history.state, '', location.pathname);
			open('import');
		}
	});

	// The menus of the head: the gear (settings, LAN, updates, layout) and the
	// arrow next to "Install app" (catalog, compose file, own container).
	let pageMenu = $state<'settings' | 'install' | null>(null);
	let pageMenuAt = $state<{ x: number; y: number } | null>(null);
	let pageMenuWasOpen = false;
	function openPageMenu(kind: 'settings' | 'install', anchor: HTMLElement) {
		if (pageMenuWasOpen) {
			pageMenuWasOpen = false;
			pageMenu = null;
			return;
		}
		const rect = anchor.getBoundingClientRect();
		pageMenuAt = { x: Math.max(8, rect.right - 280), y: rect.bottom + 4 };
		pageMenu = kind;
	}
	function pick(action: () => void) {
		pageMenu = null;
		action();
	}
	const checkUpdates = () =>
		run('global', async () => (status = await api.post<DockerStatus>('/docker/updates/check')));
	// Start and stop everything, one container after the other like by hand.
	let confirmStopAll = $state(false);
	const startAll = () =>
		run('global', async () => {
			for (const container of containers.filter((entry) => !isRunning(entry)))
				await api.post(`/docker/containers/${encodeURIComponent(container.id)}/start`);
		});
	function stopAll() {
		confirmStopAll = false;
		void run('global', async () => {
			for (const container of containers.filter(isRunning))
				await api.post(`/docker/containers/${encodeURIComponent(container.id)}/stop`);
		});
	}

	const enable = () =>
		run('global', () =>
			api.put('/docker/settings', { enabled: true, volume: draft.volume || volumes[0] }),
		);
</script>

<PageHead title={t('docker.title')}>
	{#snippet actions()}
		{#if status?.running && containers.length > 0}
			<span class="search-box"
				><Icon name="search" size={15} /><input
					class="field search"
					type="search"
					disabled={editing}
					bind:value={search}
					oninput={() => (expanded = {})}
					placeholder={t('docker.search')}
					aria-label={t('docker.search')}
				/></span
			>
			<div class="segmented icons" role="group" aria-label={t('docker.layout.view')}>
				{#each [['list', t('docker.layout.list'), 'list'], ['cards', t('docker.layout.cards'), 'grid']] as const as [id, label, icon] (id)}
					<button
						type="button"
						aria-pressed={layout.view === id}
						aria-label={label}
						title={label}
						disabled={savingLayout}
						onclick={() => setView(id)}><Icon name={icon} size={16} /></button
					>
				{/each}
			</div>
		{/if}
		<button
			type="button"
			class="btn icon-btn"
			aria-label={t('docker.pageMenu')}
			title={t('docker.pageMenu')}
			aria-haspopup="menu"
			aria-expanded={!!pageMenu}
			onpointerdown={() => (pageMenuWasOpen = pageMenu === 'settings')}
			onclick={(event) => openPageMenu('settings', event.currentTarget)}
			><Icon name="gear" size={17} /></button
		>
		{#if status?.settings.enabled}
			<span class="split">
				<a class="btn btn-primary" href="/apps"
					><Icon name="plus" size={15} />{t('docker.installApp')}</a
				><button
					type="button"
					class="btn btn-primary"
					aria-label={t('docker.installMenu')}
					title={t('docker.installMenu')}
					aria-haspopup="menu"
					aria-expanded={pageMenu === 'install'}
					onpointerdown={() => (pageMenuWasOpen = pageMenu === 'install')}
					onclick={(event) => openPageMenu('install', event.currentTarget)}
					><Icon name="chevronDown" size={15} /></button
				>
			</span>
		{/if}
	{/snippet}
</PageHead>

{#if status?.running && containers.length > 0}
	<div class="status-line">
		<div class="filters" role="group" aria-label={t('docker.filter')}>
			{#each [['all', containers.length, ''], ['running', runCount, 'ok'], ['stopped', containers.length - runCount, 'off'], ['updates', updateCount, 'warn']] as const as [id, count, tone] (id)}
				{#if id !== 'updates' || count > 0 || filter === 'updates'}
					<button
						type="button"
						class="filter {tone}"
						aria-pressed={filter === id}
						disabled={editing}
						onclick={() => {
							filter = id;
							expanded = {};
						}}
						>{#if tone}<span class="dot {tone}" aria-hidden="true"></span>{/if}{#if id === 'all'}{t(
								'docker.filterAll',
							)} <b class="mono">{count}</b>{:else}<b class="mono">{count}</b>
							{t(
								id === 'running'
									? 'docker.filterRunningCount'
									: id === 'stopped'
										? 'docker.filterStoppedCount'
										: 'docker.filterUpdatesCount',
							)}{/if}</button
					>
				{/if}
			{/each}
		</div>
		<p class="meta">
			<span class="mono">{totals}</span><span>{checked}</span><button
				type="button"
				class="icon-link"
				aria-label={t('docker.checkUpdates')}
				title={t('docker.checkUpdates')}
				onclick={checkUpdates}
				disabled={globalBusy}><Icon name="refresh" size={14} /></button
			>
		</p>
	</div>
{/if}

{#if pageMenu && pageMenuAt}
	<ContextMenu
		label={t(pageMenu === 'settings' ? 'docker.pageMenu' : 'docker.installMenu')}
		x={pageMenuAt.x}
		y={pageMenuAt.y}
		onclose={() => (pageMenu = null)}
	>
		{#if pageMenu === 'settings'}
			<div class="menu-title">{t('docker.title')}</div>
			{#if status?.settings.enabled}
				<button type="button" role="menuitem" onclick={() => pick(() => open('settings'))}
					><Icon name="gear" size={16} /><span
						>{t('docker.settings')}<small>{t('docker.settingsHint')}</small></span
					></button
				>
			{/if}
			<a role="menuitem" href="/docker/networks" onclick={() => (pageMenu = null)}
				><Icon name="globe" size={16} /><span
					>{t('lan.title')}<small>{t('docker.lanHint')}</small></span
				></a
			>
			{#if status?.settings.enabled}
				<button
					type="button"
					role="menuitem"
					disabled={!status.running}
					onclick={() => pick(() => (autoUpdates = true))}
					><Icon name="clock" size={16} /><span
						>{t('autoUpdate.button')}<small>{t('docker.autoUpdateHint')}</small></span
					></button
				>
			{/if}
			{#if containers.length > 0}
				<hr />
				<button type="button" role="menuitem" disabled={editing} onclick={() => pick(startEditing)}
					><Icon name="edit" size={16} /><span
						>{t('docker.layout.edit')}<small>{t('docker.layoutHint')}</small></span
					></button
				>
			{/if}
		{:else}
			<a role="menuitem" href="/apps" onclick={() => (pageMenu = null)}
				><Icon name="grid" size={16} /><span
					>{t('docker.fromCatalog')}<small>{t('docker.fromCatalogHint')}</small></span
				></a
			>
			<button
				type="button"
				role="menuitem"
				disabled={!status?.running}
				onclick={() => pick(() => open('import'))}
				><Icon name="fileText" size={16} /><span
					>{t('docker.import')}<small>{t('docker.importHint')}</small></span
				></button
			>
			<a role="menuitem" href="/apps/new" onclick={() => (pageMenu = null)}
				><Icon name="box" size={16} /><span
					>{t('docker.ownContainer')}<small>{t('docker.ownContainerHint')}</small></span
				></a
			>
		{/if}
	</ContextMenu>
{/if}

{#if error}
	<div class="notice-error" role="alert">
		<div class="error-line">
			<span>{error.text}</span>{#if failedAction}{@const retry = failedAction}<button
					type="button"
					class="btn"
					disabled={busyKeys.has(retry.key)}
					onclick={() => run(retry.key, retry.action, retry.label)}>{t('common.retry')}</button
				>{/if}
		</div>
		{#if error.detail}<pre>{error.detail}</pre>{/if}
	</div>
{/if}

{#if refreshError}<div class="notice-warn" role="alert">
		<span
			>{refreshError}
			{lastRefresh
				? t('docker.stale', { time: new Date(lastRefresh).toLocaleTimeString() })
				: t('docker.noCurrentData')}</span
		><button class="btn" disabled={refreshing} onclick={load}>{t('common.retry')}</button>
	</div>{/if}
{#if !status && !refreshError}<p class="muted loading" role="status">{t('common.loading')}</p>{/if}
{#if status && !status.settings.enabled}
	<section class="tile setup">
		<h2>{t('docker.setupTitle')}</h2>
		<p class="muted">{t('docker.setupText')}</p>
		{#if volumes.length === 0}
			<p class="notice-warn">{t('docker.setupNeedVolume')}</p>
		{:else}
			<div class="setup-row">
				<label
					><span>{t('docker.volume')}</span>
					<select class="field" bind:value={draft.volume}
						>{#each volumes as volume (volume)}<option value={volume}>{volume}</option
							>{/each}</select
					>
				</label>
				<button type="button" class="btn btn-primary" onclick={enable} disabled={globalBusy}
					>{t('docker.enable')}</button
				>
			</div>
		{/if}
	</section>
{:else if status && !status.running}
	<p class="notice-warn" role="status">
		{volumes.includes(status.settings.volume) &&
		storage.status?.volumes.find((volume) => volume.name === status?.settings.volume)?.mounted
			? t('docker.starting')
			: t('docker.waitingVolume', { volume: status.settings.volume })}
	</p>
{:else if status}
	{#each jobs as [app, job] (app)}
		<div class={job.state === 'failed' ? 'notice-error' : 'notice-warn'} role="status">
			{t(job.state === 'failed' ? 'docker.jobFailed' : 'docker.jobRunning', { app })}
			{#if job.state === 'failed' && job.output}<pre>{job.output}</pre>{/if}
			{#if job.state === 'failed'}
				<div class="buttons job-actions">
					<button
						type="button"
						class="btn"
						disabled={busyKeys.has(appKey(app))}
						onclick={() => run(appKey(app), () => api.post(`/docker/apps/${app}/update`), app)}
						>{t('common.retry')}</button
					>
					<a class="btn" href="/docker/apps/{app}">{t('docker.edit')}</a>
					<button
						type="button"
						class="btn btn-danger-outline"
						disabled={busyKeys.has(appKey(app))}
						onclick={() => askRemoveApp(app)}>{t('docker.remove')}</button
					>
					<button
						type="button"
						class="btn"
						disabled={busyKeys.has(appKey(app))}
						onclick={() => run(appKey(app), () => api.delete(`/docker/apps/${app}/job`), app)}
						>{t('docker.jobDismiss')}</button
					>
				</div>
			{/if}
		</div>
	{/each}

	{#each idleApps as app (app)}
		<div class="notice-warn" role="status">
			{t('docker.appIdle', { app, path: `/config/apps/${app}/compose.yaml` })}
			<div class="buttons job-actions">
				<button
					type="button"
					class="btn"
					disabled={busyKeys.has(appKey(app))}
					onclick={() => run(appKey(app), () => api.post(`/docker/apps/${app}/update`), app)}
					>{t('docker.appStart')}</button
				>
				<a class="btn" href="/docker/apps/{app}">{t('docker.edit')}</a>
				<button
					type="button"
					class="btn btn-danger-outline"
					disabled={busyKeys.has(appKey(app))}
					onclick={() => askRemoveApp(app)}>{t('docker.remove')}</button
				>
			</div>
		</div>
	{/each}

	{#if containers.length > 0}
		{#if layoutError && !editing}<p class="notice-error" role="alert">{layoutError}</p>{/if}
		{#if layoutSaved}<p class="muted saved" role="status">{t('docker.layout.saved')}</p>{/if}
	{/if}

	{#if editing}
		<section class="tile layout-editor" aria-label={t('docker.layout.edit')}>
			<p>{t('docker.layout.hint')}</p>
			<label class="check"
				><input type="checkbox" bind:checked={layout.details} disabled={savingLayout} />{t(
					'docker.layout.details',
				)}</label
			>
			<ol class="order">
				{#each groups as group, index (group.id)}
					<li
						class:dragging={dragged === group.id}
						ondragover={(event) => {
							if (dragged && !savingLayout) event.preventDefault();
						}}
						ondrop={(event) => {
							event.preventDefault();
							if (dragged && !savingLayout) move(dragged, group.id);
							dragged = null;
						}}
					>
						<button
							type="button"
							class="btn grip"
							draggable={!savingLayout}
							disabled={savingLayout}
							aria-label={t('dashboard.move', { tile: group.name })}
							ondragstart={(event) => {
								dragged = group.id;
								event.dataTransfer?.setData('text/plain', group.id);
							}}
							ondragend={() => (dragged = null)}><Icon name="grip" size={16} /></button
						>
						<span class="order-name">{group.name}</span>
						{#if isStack(group)}<span class="chip">{t('docker.stack')}</span>{/if}
						<button
							type="button"
							class="btn arrow"
							disabled={savingLayout || index === 0}
							aria-label={t('docker.layout.before', { name: group.name })}
							onclick={() => moveBy(group.id, -1)}>↑</button
						>
						<button
							type="button"
							class="btn arrow"
							disabled={savingLayout || index === groups.length - 1}
							aria-label={t('docker.layout.after', { name: group.name })}
							onclick={() => moveBy(group.id, 1)}>↓</button
						>
					</li>
				{/each}
			</ol>
			{#if layoutError}<p class="notice-error" role="alert">{layoutError}</p>{/if}
			<div class="buttons">
				<button
					type="button"
					class="btn"
					disabled={savingLayout}
					onclick={() => {
						layout = { ...defaultDockerLayout(), view: layout.view };
					}}>{t('dashboard.restore')}</button
				>
				<span class="spacer"></span>
				<button type="button" class="btn" disabled={savingLayout} onclick={cancelLayout}
					>{t('common.cancel')}</button
				>
				<button type="button" class="btn btn-primary" disabled={savingLayout} onclick={saveLayout}
					>{t('docker.layout.save')}</button
				>
			</div>
		</section>
	{/if}

	{#if shownGroups.length > 0}
		{#if layout.view === 'list'}
			<div class="list" role="list" aria-label={t('docker.title')}>
				{#each shownGroups as group (group.id)}
					{#if isStack(group)}
						<div class="stack" role="listitem" aria-label="{t('docker.stack')} {group.name}">
							{@render stackHead(group)}
							{#if stackOpen(group.id)}
								<div class="members" role="list">
									{#each shownMembers(group) as container (container.name)}
										{@render row(container, true)}
									{/each}
								</div>
							{/if}
						</div>
					{:else}
						{@render row(group.members[0], false)}
					{/if}
				{/each}
			</div>
		{:else}
			<div class="cards">
				{#each shownGroups as group (group.id)}
					{#if isStack(group)}
						<section class="tile stack-card" aria-label="{t('docker.stack')} {group.name}">
							<div class="list">
								{@render stackHead(group)}
								{#if stackOpen(group.id)}
									<div class="members" role="list">
										{#each shownMembers(group) as container (container.name)}
											{@render row(container, true)}
										{/each}
									</div>
								{/if}
							</div>
						</section>
					{:else}
						{@const container = group.members[0]}
						<ContainerCard
							{container}
							{host}
							{tailscaleHost}
							busy={containerBusy(container)}
							details={layout.details}
							{...actionsFor(container)}
						/>
					{/if}
				{/each}
			</div>
		{/if}
	{/if}
	{#if containers.length === 0 && jobs.length === 0 && idleApps.length === 0}
		<EmptyState title={t('docker.empty')} />
	{:else if visible.length === 0 && containers.length > 0}
		<EmptyState title={t('docker.noMatch')} />
	{/if}
	{#if containers.length > 0}
		<div class="bulk">
			<button
				type="button"
				class="btn"
				disabled={globalBusy || editing || runCount === containers.length}
				onclick={startAll}><Icon name="play" size={15} />{t('docker.startAll')}</button
			>
			<button
				type="button"
				class="btn"
				disabled={globalBusy || editing || runCount === 0}
				onclick={() => (confirmStopAll = true)}
				><Icon name="stop" size={15} />{t('docker.stopAll')}</button
			>
			<button type="button" class="btn" disabled={globalBusy} onclick={checkUpdates}
				><Icon name="refresh" size={15} />{t('docker.checkUpdates')}</button
			>
			{#if updatable}
				<button type="button" class="btn" onclick={askUpdates} disabled={globalBusy}
					><Icon name="download" size={15} />{t('docker.updateAllCount', {
						count: updateApps.length,
					})}</button
				>
			{/if}
			<span class="spacer"></span>
			<span class="muted engine"
				>{t('docker.version', { version: status.version })} · {t('docker.onVolume', {
					volume: status.settings.volume,
				})}</span
			>
		</div>
	{/if}
{/if}

{#if confirmStopAll}
	<Dialog title={t('docker.stopAllTitle')} onclose={() => (confirmStopAll = false)}>
		<p>{t('docker.stopAllText', { count: runCount })}</p>
		<div class="dialog-actions">
			<button type="button" class="btn btn-warn" onclick={stopAll}>{t('docker.stopAll')}</button
			><button type="button" class="btn" onclick={() => (confirmStopAll = false)}
				>{t('common.cancel')}</button
			>
		</div>
	</Dialog>
{/if}

{#snippet row(container: DockerContainer, member: boolean)}
	<ContainerRow
		{container}
		{host}
		{tailscaleHost}
		busy={containerBusy(container)}
		{member}
		details={layout.details}
		{...actionsFor(container)}
	/>
{/snippet}

{#snippet stackHead(group: DockerGroup)}
	{@const running = group.members.filter(isRunning).length}
	{@const summary = t('docker.stackSummary', { running, total: group.members.length })}
	<!-- A stack is a tile like a container: the icon opens its menu, the
	     name unfolds the services as tiles behind it. -->
	<div
		class="stack-head"
		class:stopped={running === 0}
		title={search.trim() || filter !== 'all'
			? t('docker.stackMatches', { count: shownMembers(group).length, total: group.members.length })
			: undefined}
		oncontextmenu={(event) => {
			event.preventDefault();
			stackMenu = { group, x: event.clientX, y: event.clientY };
		}}
		role="presentation"
	>
		<button
			type="button"
			class="stack-icon"
			aria-label={t('docker.actionsFor', { name: group.name })}
			aria-haspopup="menu"
			onpointerdown={() => (stackMenuWasOpen = stackMenu?.group.id === group.id)}
			onclick={(event) => openStackMenu(group, event.currentTarget)}
			><AppIcon
				src={group.members.find((container) => container.icon)?.icon ?? ''}
				name={group.name}
			/></button
		>
		<button
			type="button"
			class="stack-toggle"
			aria-expanded={stackOpen(group.id)}
			onclick={() => (expanded[group.id] = !stackOpen(group.id))}
			><span class="stack-name">{group.name}</span><span class="sr-only">{t('docker.stack')}</span
			>{#if group.members.some((container) => container.updateAvailable)}<span class="chip warn"
					>{t('docker.update')}</span
				>{/if}<span class="chevron" class:folded={!stackOpen(group.id)}
				><Icon name="chevronDown" size={14} /></span
			></button
		>
		<span class="stack-values"
			><span class="sr-only">{summary}, {t('docker.cpu')}</span><span aria-hidden="true"
				>{`${running}/${group.members.length} · `}</span
			>{running ? formatPercent(sum(group, (container) => container.cpuPercent)) : '–'}<span
				class="sr-only">, {t('docker.ram')}</span
			><span aria-hidden="true">{' · '}</span>{running
				? formatBytes(sum(group, (container) => container.memoryBytes))
				: '–'}</span
		>
	</div>
{/snippet}

{#if stackMenu}
	{@const group = stackMenu.group}
	{@const primary = stackPrimary(group, layout.primaryServices)}
	{@const links = primary ? webLinks(primary, host, tailscaleHost) : []}
	{@const app = group.members.find((c) => c.managed && c.app)?.app ?? ''}
	{@const problemCount = group.members.filter(
		(c) => !isRunning(c) || c.state === 'restarting',
	).length}
	<ContextMenu
		label={t('docker.actionsFor', { name: group.name })}
		x={stackMenu.x}
		y={stackMenu.y}
		onclose={() => (stackMenu = null)}
	>
		{#if links.length > 0}
			{#each links as link, index (`${index}:${link.href}`)}<a
					role="menuitem"
					href={link.href}
					target="_blank"
					rel="noreferrer"
					title={link.label}
					onclick={() => (stackMenu = null)}
					><Icon name={link.icon} size={16} /><span>{link.text}</span></a
				>{/each}
			<hr />
		{/if}
		{#if layout.primaryServices[group.id] && !primary}<p class="menu-title">
				{t('docker.primaryMissing', { service: layout.primaryServices[group.id] })}
			</p>{/if}
		{#if problemCount}
			<button
				type="button"
				role="menuitem"
				onclick={() => {
					stackMenu = null;
					showProblems(group);
				}}
				><Icon name="activity" size={16} /><span
					>{t('docker.stackProblems', { count: problemCount })}</span
				></button
			>
		{/if}
		<button
			type="button"
			role="menuitem"
			disabled={editing || savingLayout}
			onclick={() => {
				stackMenu = null;
				editLinks(group);
			}}><Icon name="globe" size={16} /><span>{t('docker.stackLinks')}</span></button
		>
		{#if proxy && app}
			<button
				type="button"
				role="menuitem"
				disabled={editing}
				onclick={() => {
					stackMenu = null;
					openTailnet(app);
				}}
				><Icon name="tailnet" size={16} /><span
					>{t(appInTailnet(app) ? 'docker.tailnetUnpublish' : 'docker.tailnetPublish')}</span
				></button
			>
		{/if}
		{#if app}
			<a role="menuitem" href="/docker/apps/{app}" onclick={() => (stackMenu = null)}
				><Icon name="edit" size={16} /><span>{t('docker.edit')}</span></a
			>
		{/if}
	</ContextMenu>
{/if}

{#if linkGroup}
	<Dialog
		title={t('docker.stackLinksTitle', { name: linkGroup.name })}
		locked={savingLayout}
		onclose={() => (linkGroup = null)}
	>
		<p class="text">{t('docker.stackLinksHint')}</p>
		<label class="field-row"
			><span>{t('docker.primaryService')}</span><select
				class="field"
				bind:value={primaryService}
				disabled={savingLayout}
			>
				<option value="">{t('docker.primaryAuto')}</option>
				{#if primaryService && !linkGroup.members.some((c) => c.service === primaryService)}<option
						value={primaryService}>{primaryService} · {t('docker.primaryUnavailable')}</option
					>{/if}
				{#each [...new Set(linkGroup.members
							.map((c) => c.service)
							.filter(Boolean))] as service}<option value={service}>{service}</option>{/each}
			</select></label
		>
		{#if layoutError}<p class="notice-error" role="alert">{layoutError}</p>{/if}
		<div class="dialog-actions">
			<button class="btn" disabled={savingLayout} onclick={() => (linkGroup = null)}
				>{t('common.cancel')}</button
			><button class="btn btn-primary" disabled={savingLayout} onclick={saveLinks}
				>{t('common.save')}</button
			>
		</div>
	</Dialog>
{/if}
{#if dialog?.kind === 'logs'}
	{@const container = dialog.container}
	{@const text = dialog.text}
	<Dialog
		title={t('docker.logsTitle', { name: dockerName(container, 'distinct') })}
		wide
		onclose={() => (dialog = null)}
	>
		<div class="logs-tools">
			<input
				class="field logs-search"
				type="search"
				bind:value={logsSearch}
				placeholder={t('docker.logsSearch')}
				aria-label={t('docker.logsSearch')}
			/>
			{#if logsSearch.trim()}<span class="muted mono" role="status"
					>{t('docker.logsHits', { count: logHits })}</span
				>{/if}
			<label class="check"
				><input type="checkbox" bind:checked={logsFollow} /><span>{t('docker.logsFollow')}</span
				></label
			>
		</div>
		{#if logsError}<div class="notice-error logs-error" role="alert">
				<span>{logsError}</span><button
					type="button"
					class="btn"
					disabled={logsLoading}
					onclick={() => showLogs(container, true)}>{t('common.retry')}</button
				>
			</div>{/if}
		<pre class="logs" bind:this={logsBox}>{#if text === null}…{:else if !text}{t(
					'docker.logsEmpty',
				)}{:else}{#each logParts as part, index (index)}{#if part.hit}<mark>{part.text}</mark
						>{:else}{part.text}{/if}{/each}{/if}</pre>
		<div class="dialog-actions">
			<button
				type="button"
				class="btn"
				disabled={!text}
				onclick={() => text && downloadLogs(container, text)}
				><Icon name="download" size={16} />{t('docker.logsDownload')}</button
			>
			<button
				type="button"
				class="btn"
				disabled={logsLoading}
				onclick={() => showLogs(container, true)}>{t('docker.logsRefresh')}</button
			>
			<button type="button" class="btn btn-primary" onclick={() => (dialog = null)}
				>{t('common.done')}</button
			>
		</div>
	</Dialog>
{:else if dialog?.kind === 'updates'}
	{@const target = dialog}
	<Dialog title={t('docker.updatesTitle')} locked={dialogBusy} onclose={() => (dialog = null)}>
		<p class="text">{t('docker.updatesText')}</p>
		<div class="update-list">
			{#each updateApps as entry (entry.app)}
				<label class="check"
					><input
						type="checkbox"
						disabled={dialogBusy}
						checked={target.selected.includes(entry.app)}
						onchange={(event) =>
							(target.selected = event.currentTarget.checked
								? [...target.selected, entry.app]
								: target.selected.filter((app) => app !== entry.app))}
					/>
					<span
						><span class="mono">{entry.app}</span
						>{#if entry.names.length > 1 || entry.names[0] !== entry.app}
							<span class="muted">· {entry.names.join(', ')}</span>{/if}</span
					></label
				>
			{:else}<p class="muted">{t('docker.updatesNone')}</p>{/each}
		</div>
		{#if target.failed.length}<div class="notice-error" role="alert">
				{t('docker.updatesFailed')}
				<ul>
					{#each target.failed as entry (entry.app)}<li>
							<span class="mono">{entry.app}</span>: {entry.text}
						</li>{/each}
				</ul>
			</div>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={dialogBusy} onclick={() => (dialog = null)}
				>{t('common.cancel')}</button
			>
			<button
				type="button"
				class="btn btn-primary"
				disabled={dialogBusy || target.selected.length === 0}
				onclick={applyUpdates}
				>{t(dialogBusy ? 'docker.updatesRunning' : 'docker.updatesSubmit', {
					count: target.selected.length,
				})}</button
			>
		</div>
	</Dialog>
{:else if dialog?.kind === 'remove'}
	{@const container = dialog.container}
	<Dialog
		title={t('docker.removeTitle', { name: container.name })}
		locked={dialogBusy}
		onclose={() => (dialog = null)}
	>
		<p class="text">{t('docker.removeContainer')}</p>
		{#if dialogError}<p class="notice-error" role="alert">{dialogError.text}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={dialogBusy} onclick={() => (dialog = null)}
				>{t('common.cancel')}</button
			>
			<button
				type="button"
				class="btn btn-warn"
				disabled={dialogBusy}
				onclick={() => remove(container)}>{t('docker.removeSubmit')}</button
			>
		</div>
	</Dialog>
{:else if dialog?.kind === 'removeApp'}
	{@const app = dialog.app}
	<Dialog
		title={t('docker.removeTitle', { name: app })}
		locked={dialogBusy}
		onclose={() => (dialog = null)}
	>
		{@const path = `/mnt/${status?.settings.volume}/appdata/${app}`}
		<p class="text">
			{t(dialog.purge ? 'docker.removeAppPurge' : 'docker.removeApp', { app, path })}
		</p>
		{#if dialogError}<p class="notice-error" role="alert">{dialogError.text}</p>{/if}
		<label class="check"
			><input type="checkbox" bind:checked={dialog.purge} disabled={dialogBusy} /><span
				>{t('docker.removeData', { path })}</span
			></label
		>
		{#if dialog.purge}
			<p class="notice-error">{t('docker.removeDataWarning')}</p>
		{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={dialogBusy} onclick={() => (dialog = null)}
				>{t('common.cancel')}</button
			>
			<button
				type="button"
				disabled={dialogBusy}
				class="btn {dialog.purge ? 'btn-danger' : 'btn-warn'}"
				onclick={() => dialog?.kind === 'removeApp' && removeApp(app, dialog.purge)}
				>{t(dialog.purge ? 'docker.removeDataSubmit' : 'docker.removeSubmit')}</button
			>
		</div>
	</Dialog>
{:else if dialog}
	{@const kind = dialog.kind}
	<Dialog
		title={t(kind === 'import' ? 'docker.importTitle' : 'docker.settingsTitle')}
		wide={kind === 'import'}
		locked={dialogBusy}
		onclose={() => (dialog = null)}
	>
		<form onsubmit={submit}>
			{#if kind === 'import'}
				<p class="notice-warn">{t('docker.importWarning')}</p>
				<label
					><span>{t('docker.importName')}</span>
					<!-- svelte-ignore a11y_autofocus -->
					<input
						class="field mono name-field"
						type="text"
						bind:value={draft.name}
						maxlength="32"
						autocapitalize="none"
						spellcheck="false"
						required
						autofocus
					/>
				</label>
				<label
					><span>{t('docker.importCompose')}</span>
					<textarea
						class="field mono"
						rows="14"
						bind:value={draft.compose}
						spellcheck="false"
						required
						placeholder={'services:\n  web:\n    image: …'}></textarea>
					<small
						>{t('docker.importHint', {
							path: `/mnt/${status?.settings.volume}/appdata/${draft.name || '<name>'}`,
						})}</small
					>
				</label>
			{:else}
				<label class="check"
					><input type="checkbox" bind:checked={draft.enabled} /><span>{t('docker.enabled')}</span
					></label
				>
				<label
					><span>{t('docker.volume')}</span>
					<select class="field" bind:value={draft.volume} disabled={!draft.enabled}
						>{#each volumes as volume (volume)}<option value={volume}>{volume}</option
							>{/each}</select
					>
				</label>
				<p class="muted note">{t('docker.disableNote')}</p>
			{/if}
			{#if dialogError}
				<div class="notice-error" role="alert">
					{dialogError.text}{#if dialogError.detail}<pre>{dialogError.detail}</pre>{/if}
				</div>
			{/if}
			<div class="dialog-actions">
				<button type="button" class="btn" onclick={() => (dialog = null)} disabled={dialogBusy}
					>{t('common.cancel')}</button
				>
				<button type="submit" class="btn btn-primary" disabled={dialogBusy}
					>{t(kind === 'import' ? 'docker.importSubmit' : 'docker.save')}</button
				>
			</div>
		</form>
	</Dialog>
{/if}

{#if tailnetDialog}<TailnetDialog
		app={tailnetDialog.app}
		mode={tailnetDialog.mode}
		port={tailnetDialog.port}
		onclose={() => (tailnetDialog = null)}
		onchanged={() => {
			void loadProxy();
			void load();
		}}
	/>{/if}
{#if autoUpdates}<AutoUpdateDialog
		apps={[...new Set(containers.filter((c) => c.managed && c.app).map((c) => c.app))].sort()}
		onclose={() => (autoUpdates = false)}
	/>{/if}

<style>
	.field-row {
		display: flex;
		flex-direction: column;
		gap: 6px;
	}
	.notice-warn {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2);
	}

	.buttons {
		display: flex;
		flex-wrap: wrap;
		gap: 10px;
	}
	.job-actions {
		margin-top: 10px;
	}
	.setup {
		display: flex;
		flex-direction: column;
		gap: 14px;
		padding: var(--space-5);
		font-size: var(--text-md);
	}
	.setup h2 {
		font-size: var(--text-lg);
		font-weight: 600;
	}
	.setup-row {
		display: flex;
		flex-wrap: wrap;
		align-items: flex-end;
		gap: var(--space-3);
	}
	.setup-row .field {
		min-width: 14rem;
	}
	label {
		display: flex;
		flex-direction: column;
		gap: 6px;
		font-size: var(--text-md);
	}
	.search-box {
		position: relative;
		display: inline-flex;
		align-items: center;
		width: 15rem;
		max-width: 100%;
		color: var(--muted);
	}
	.search-box :global(svg) {
		position: absolute;
		left: 10px;
		pointer-events: none;
	}
	.search {
		width: 100%;
		padding-left: 32px;
	}
	.segmented.icons button {
		padding: 0 8px;
	}
	.icon-btn {
		width: var(--control-height);
		padding: 0;
	}
	.split {
		display: inline-flex;
	}
	.split > :first-child {
		border-top-right-radius: 0;
		border-bottom-right-radius: 0;
	}
	.split > :last-child {
		width: 2.25rem;
		padding: 0;
		border-top-left-radius: 0;
		border-bottom-left-radius: 0;
		border-left: 1px solid color-mix(in srgb, var(--on-accent) 25%, transparent);
	}
	.status-line {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2) var(--space-3);
	}
	.filters {
		display: flex;
		flex-wrap: wrap;
		gap: 4px;
	}
	.filter {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		min-height: 2rem;
		padding: 0 10px;
		background: transparent;
		border: 1px solid transparent;
		border-radius: 999px;
		color: var(--muted);
		font: inherit;
		font-size: var(--text-md);
		cursor: pointer;
	}
	.filter b {
		color: var(--text);
		font-weight: 600;
	}
	.filter.warn,
	.filter.warn b {
		color: var(--warn-text);
	}
	.filter:hover:not(:disabled) {
		color: var(--text);
	}
	.filter[aria-pressed='true'] {
		background: var(--chip);
		border-color: var(--border);
		color: var(--text);
	}
	.filter .dot.off {
		background: var(--status-inactive);
	}
	.meta {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 4px 12px;
		margin: 0 0 0 auto;
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.icon-link {
		display: inline-flex;
		padding: 2px;
		background: none;
		border: 0;
		color: var(--accent);
		cursor: pointer;
	}
	.icon-link:disabled {
		opacity: 0.5;
		cursor: default;
	}
	.bulk {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2);
		padding-top: var(--space-3);
		border-top: 1px solid var(--border);
	}
	.bulk .spacer {
		flex: 1;
	}
	.engine {
		font-size: var(--text-sm);
	}
	:global(.dropdown-panel) [role^='menuitem'] small {
		display: block;
		color: var(--muted);
		font-size: var(--text-sm);
		white-space: normal;
	}
	@media (max-width: 760px) {
		.search-box {
			flex: 1 1 10rem;
			width: auto;
		}
		.meta {
			margin-left: 0;
		}
	}
	.segmented {
		display: flex;
		min-width: 0;
		max-width: 100%;
		overflow-x: auto;
		scrollbar-width: thin;
		gap: 2px;
		padding: 3px;
		background: var(--panel-bg);
		border: 1px solid var(--panel-border);
		border-radius: var(--radius-ctl);
	}
	.segmented button {
		flex: none;
		display: inline-flex;
		align-items: center;
		gap: 7px;
		height: 1.875rem;
		padding: 0 10px;
		background: transparent;
		border: 0;
		border-radius: var(--radius-small);
		color: var(--muted);
		font: inherit;
		font-size: var(--text-base);
		cursor: pointer;
	}
	.segmented button:hover:not(:disabled) {
		color: var(--text);
	}
	.segmented button[aria-pressed='true'] {
		background: var(--chip);
		color: var(--text);
		font-weight: 500;
	}
	.saved {
		font-size: var(--text-base);
	}

	.layout-editor {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
		padding: var(--tile-pad);
		font-size: var(--text-md);
	}
	.order {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
		max-height: 22rem;
		overflow-y: auto;
		margin: 0;
		padding: 0;
		list-style: none;
	}
	.order li {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-1) 6px;
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
	}
	.order li.dragging {
		border-style: dashed;
		border-color: var(--accent);
	}
	.order-name {
		flex: 1 1 auto;
		min-width: 0;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
		font-weight: 500;
	}
	.grip {
		padding: 0 var(--space-2);
		cursor: grab;
	}
	.arrow {
		width: 2.25rem;
		padding: 0;
	}
	.spacer {
		flex: 1 1 auto;
	}

	/* The list: tiles of one fixed width, as many side by side as fit
	   (owner's design of 30 Oct 2026). A stack is a tile too; its services
	   follow as tiles of their own when it is unfolded. */
	.list {
		display: grid;
		grid-template-columns: repeat(auto-fill, 15rem);
		gap: 10px;
	}
	.stack,
	.members {
		display: contents;
	}
	.stack-card .list {
		padding: var(--tile-pad);
	}
	.stack-head {
		display: grid;
		grid-template-columns: 30px minmax(0, 1fr);
		grid-template-rows: auto auto;
		align-content: center;
		column-gap: 10px;
		row-gap: 1px;
		height: 3.25rem;
		padding: 0 var(--space-3) 0 14px;
		box-sizing: border-box;
		border: 1px solid var(--panel-border);
		border-radius: 14px;
		background: var(--panel-bg);
		-webkit-backdrop-filter: var(--panel-backdrop);
		backdrop-filter: var(--panel-backdrop);
		box-shadow:
			inset 4px 0 0 var(--accent),
			var(--panel-shadow);
		font-size: var(--text-md);
	}
	.stack-head.stopped {
		box-shadow:
			inset 4px 0 0 var(--muted),
			var(--panel-shadow);
	}
	.stack-icon {
		grid-row: 1 / span 2;
		align-self: center;
		display: grid;
		place-items: center;
		width: 30px;
		height: 30px;
		padding: 0;
		overflow: hidden;
		border: 0;
		border-radius: var(--radius-ctl);
		background: none;
		color: var(--text);
		font-size: var(--text-sm);
		font-weight: 600;
		cursor: pointer;
	}
	.stack-icon :global(img) {
		padding: 0;
	}
	.stack-icon:hover,
	.stack-icon:focus-visible {
		outline: 2px solid color-mix(in srgb, var(--accent) 50%, transparent);
	}
	.stack-toggle {
		display: flex;
		align-items: center;
		gap: 6px;
		min-width: 0;
		padding: 0;
		background: transparent;
		border: 0;
		color: var(--text);
		font: inherit;
		text-align: left;
		cursor: pointer;
	}
	.stack-toggle .chip {
		flex: none;
		font-size: var(--text-xs);
	}
	.chevron {
		display: inline-flex;
		flex: none;
		color: var(--muted);
		transition: transform 0.15s ease;
	}
	.chevron.folded {
		transform: rotate(-90deg);
	}
	.stack-name {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
		font-weight: 600;
	}
	.stack-values {
		overflow: hidden;
		white-space: nowrap;
		color: var(--muted);
		font-family: var(--font-mono);
		font-size: var(--text-sm);
	}

	.cards {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(17rem, 1fr));
		gap: var(--gap);
	}
	.stack-card {
		grid-column: 1 / -1;
		position: relative;
		padding-bottom: var(--space-1);
	}

	@media (prefers-reduced-motion: reduce) {
		.chevron {
			transition: none;
		}
	}
	/* Phones: one tile per line. */
	@media (max-width: 760px) {
		.list {
			grid-template-columns: minmax(0, 1fr);
		}
	}
	@media (max-width: 760px) {
		.search {
			max-width: none;
		}
		.view-tools {
			margin-left: 0;
		}
		.segmented button {
			height: 2.5rem;
		}
	}

	form {
		display: flex;
		flex-direction: column;
		gap: var(--space-4);
	}
	.name-field {
		max-width: 18rem;
	}
	textarea {
		height: auto;
		padding: 0.625rem 0.75rem;
		font-size: var(--text-base);
		line-height: 1.5;
		resize: vertical;
	}
	small,
	.note {
		font-size: var(--text-sm);
		color: var(--muted);
	}
	.check {
		flex-direction: row;
		align-items: center;
		gap: 10px;
	}
	.check input {
		width: 18px;
		height: 18px;
		margin: 0;
	}
	.text {
		font-size: var(--text-md);
	}
	pre {
		margin: var(--space-2) 0 0;
		font-family: var(--font-mono);
		font-size: var(--text-sm);
		white-space: pre-wrap;
		word-break: break-word;
	}
	.logs {
		max-height: 60vh;
		margin: 0;
		padding: var(--space-3) 14px;
		overflow: auto;
		background: var(--bg);
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		line-height: 1.5;
	}
	.error-line {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-2);
	}
	.logs mark {
		background: var(--warn-bg);
		color: var(--warn-text);
		outline: 1px solid var(--warn-border);
		border-radius: var(--radius-small);
	}
	.logs-tools {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2) var(--space-4);
		margin-bottom: 10px;
	}
	.logs-search {
		flex: 1 1 14rem;
		min-width: 0;
	}
	.logs-error {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-2);
		margin-bottom: 10px;
	}
	.update-list {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		max-height: 50vh;
		overflow: auto;
		margin: var(--space-3) 0;
	}
	.notice-error ul {
		margin: 6px 0 0;
		padding-left: 1.1rem;
	}
</style>
