<script lang="ts">
	// Docker › Container (PLAN 67, owner's decision of 7 Nov 2026): one table
	// of all apps – a Compose stack is one row – with state as text, CPU, RAM,
	// ports and updates; Start/Stop and a ⋯ menu per row, the app's own page
	// on a click. Above it the filters and four figures. The earlier card and
	// tile views are gone; the stored docker preference is still read (order
	// and main services of stacks).
	import { poll, type Poller } from '#lib/poll.ts';
	import { onMount } from 'svelte';
	import { SvelteSet } from 'svelte/reactivity';
	import { goto } from '$app/navigation';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import { inWindow, phoneView } from '#lib/desktop/mode.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import {
		sanitizeDockerLayout,
		dockerGroups,
		arrangedDockerGroups,
		stackPrimary,
		type DockerGroup,
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
	import ComposeSettingsDialog from './ComposeSettingsDialog.svelte';
	import LogsDialog from './LogsDialog.svelte';
	import RemoveAppDialog from './RemoveAppDialog.svelte';
	import TailnetDialog from './TailnetDialog.svelte';
	import UpdatesDialog from './UpdatesDialog.svelte';
	import { inTailnet, tailnetPort } from '#lib/app-proxy.ts';
	import { formatBytes, formatPercent } from '#lib/format.ts';
	import { errorText, i18n, t } from '#lib/i18n/index.svelte.ts';
	import { storage } from '#lib/storage.svelte.ts';
	import { system } from '#lib/system.svelte.ts';
	import { addContainerConsole } from '#lib/terminal.svelte.ts';
	import { tailnetHost, type TailnetStatus } from '#lib/app-links.ts';
	import AppIcon from '#lib/components/AppIcon.svelte';
	import ContextMenu from '#lib/components/ContextMenu.svelte';
	import { isRunning, portLabels, webLinks } from './container.ts';

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
	// for actions on everything. Other rows stay usable meanwhile.
	const busyKeys = new SvelteSet<string>();
	const appKey = (app: string) => `app:${app}`;
	const containerKey = (container: DockerContainer) => `container:${container.id}`;
	const globalBusy = $derived(busyKeys.has('global'));
	// Requests of the dialogs (remove, import, settings).
	let dialogBusy = $state(false);
	let search = $state('');
	let filter = $state<'all' | 'running' | 'stopped' | 'updates'>('all');
	let linkGroup = $state<DockerGroup | null>(null),
		primaryService = $state('');
	let refreshError = $state(''),
		lastRefresh = $state(''),
		refreshing = $state(false);
	// The stored order and the main service of each stack (no view settings any more).
	let layout = $state(sanitizeDockerLayout(session.docker));
	let savingLayout = $state(false);
	let layoutError = $state('');
	let dialog = $state<
		| { kind: 'import' | 'settings' }
		| { kind: 'logs'; container: DockerContainer }
		| { kind: 'updates'; selected: string[]; failed: { app: string; text: string }[] }
		| { kind: 'remove'; container: DockerContainer }
		| { kind: 'removeApp'; app: string }
		| null
	>(null);
	let draft = $state({ name: '', compose: '', enabled: true, volume: '' });
	let dialogError = $state<{ text: string; detail: string } | null>(null);

	const volumes = $derived((storage.status?.volumes ?? []).map((volume) => volume.name));
	const host = $derived(system.info?.network?.ipv4.split('/')[0] || location.hostname);
	const containers = $derived(status?.containers ?? []);
	const runCount = $derived(containers.filter(isRunning).length);
	const groups = $derived(arrangedDockerGroups(dockerGroups(containers), layout, i18n.locale));

	// ---- One row per app ----

	type RowState = 'running' | 'partial' | 'stopped' | 'restarting' | 'error';
	function rowState(group: DockerGroup): RowState {
		const m = group.members;
		if (m.some((c) => c.state === 'dead') || (jobFailed(group.name) && !m.some(isRunning)))
			return 'error';
		if (m.some((c) => c.state === 'restarting')) return 'restarting';
		const running = m.filter((c) => c.state === 'running').length;
		if (running === m.length) return 'running';
		return running ? 'partial' : 'stopped';
	}
	const jobFailed = (app: string) => status?.jobs?.[app]?.state === 'failed';
	const running = (group: DockerGroup) => group.members.filter(isRunning);
	const groupCpu = (group: DockerGroup) => running(group).reduce((s, c) => s + c.cpuPercent, 0);
	const groupRam = (group: DockerGroup) => running(group).reduce((s, c) => s + c.memoryBytes, 0);
	const groupPorts = (group: DockerGroup) => [...new Set(group.members.flatMap(portLabels))];
	const groupUpdate = (group: DockerGroup) => group.members.some((c) => c.updateAvailable);
	const groupApp = (group: DockerGroup) => group.members.find((c) => c.managed && c.app)?.app ?? '';
	const groupBusy = (group: DockerGroup) =>
		busyKeys.has(appKey(group.name)) || group.members.some((c) => busyKeys.has(containerKey(c)));
	function subLine(group: DockerGroup): string {
		if (group.members.length > 1)
			return t('docker.table.containers', { count: group.members.length });
		return group.members[0]?.image ?? '';
	}
	const stateOf = $derived(new Map(groups.map((g) => [g.id, rowState(g)])));
	const isOn = (group: DockerGroup) => {
		const s = stateOf.get(group.id);
		return s === 'running' || s === 'partial';
	};
	const counts = $derived({
		all: groups.length,
		running: groups.filter(isOn).length,
		stopped: groups.filter((g) => stateOf.get(g.id) === 'stopped').length,
		updates: groups.filter(groupUpdate).length,
	});
	const shownGroups = $derived(
		groups.filter((group) => {
			if (filter === 'running' && !isOn(group)) return false;
			if (filter === 'stopped' && stateOf.get(group.id) !== 'stopped') return false;
			if (filter === 'updates' && !groupUpdate(group)) return false;
			const q = search.trim().toLowerCase();
			return (
				!q ||
				[group.name, ...group.members.flatMap((c) => [c.name, c.image, c.service])]
					.join(' ')
					.toLowerCase()
					.includes(q)
			);
		}),
	);
	const totalCpu = $derived(containers.reduce((s, c) => s + (isRunning(c) ? c.cpuPercent : 0), 0));
	const totalRam = $derived(containers.reduce((s, c) => s + (isRunning(c) ? c.memoryBytes : 0), 0));
	// A colour of its own for an app without an icon, from its name.
	function hue(name: string) {
		let h = 0;
		for (const ch of name) h = (h * 31 + ch.charCodeAt(0)) % 360;
		return h;
	}

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
	// gone) have no row; without this they could not be removed.
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
		primaryService = layout.primaryServices[group.id] ?? '';
		layoutError = '';
	}
	async function saveLinks() {
		if (!linkGroup || savingLayout) return;
		const primaryServices = { ...layout.primaryServices };
		if (primaryService) primaryServices[linkGroup.id] = primaryService;
		else delete primaryServices[linkGroup.id];
		// The rest of the stored preference (older view fields too) stays as it is.
		const value = sanitizeDockerLayout({ ...layout, primaryServices });
		savingLayout = true;
		layoutError = '';
		try {
			await api.put('/preferences/docker', value);
			session.docker = value;
			layout = value;
			linkGroup = null;
		} catch (err) {
			layoutError = problem(err).text;
		} finally {
			savingLayout = false;
		}
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
			// Says which app it was, as other rows stay usable.
			error = label ? { ...failed, text: `${label}: ${failed.text}` } : failed;
			failedAction = { key, action, label };
		}
		await load();
		busyKeys.delete(key);
	}

	/** Start, stop or restart every container of a row, one after the other like by hand. */
	function groupAction(group: DockerGroup, action: 'start' | 'stop' | 'restart') {
		const targets = group.members.filter((c) =>
			action === 'start' ? !isRunning(c) : action === 'stop' ? isRunning(c) : true,
		);
		void run(
			appKey(group.name),
			async () => {
				for (const c of targets)
					await api.post(`/docker/containers/${encodeURIComponent(c.id)}/${action}`);
			},
			group.name,
		);
	}
	function updateApp(app: string) {
		void run(appKey(app), () => api.post(`/docker/apps/${encodeURIComponent(app)}/update`), app);
	}
	function removeGroup(group: DockerGroup) {
		dialogError = null;
		const app = groupApp(group);
		if (app) askRemoveApp(app);
		else dialog = { kind: 'remove', container: group.members[0] };
	}
	/** PLAN 63: the app in its remote view window (the desktop opens it). */
	function openRemote(app: string) {
		window.parent.postMessage(
			{ type: 'titanos-open-program', key: 'docker:' + app },
			location.origin,
		);
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

	function askRemoveApp(app: string) {
		dialogError = null;
		dialog = { kind: 'removeApp', app };
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

	// The menus of the tool bar: ⋯ (everything at once, settings, LAN,
	// auto updates) and the arrow next to "Install app" (catalog, compose file,
	// own container); the menu of a row.
	let pageMenu = $state<'more' | 'install' | null>(null);
	let pageMenuAt = $state<{ x: number; y: number } | null>(null);
	let pageMenuWasOpen = false;
	function openPageMenu(kind: 'more' | 'install', anchor: HTMLElement) {
		if (pageMenuWasOpen) {
			pageMenuWasOpen = false;
			pageMenu = null;
			return;
		}
		const rect = anchor.getBoundingClientRect();
		pageMenuAt = { x: Math.max(8, rect.right - 280), y: rect.bottom + 4 };
		pageMenu = kind;
	}
	// The action reads what the menu was opened for before the menu closes.
	function pick(action: () => void) {
		action();
		pageMenu = null;
		rowMenu = null;
	}
	let rowMenu = $state<{ group: DockerGroup; x: number; y: number } | null>(null);
	let rowMenuWasOpen = '';
	function openRowMenu(group: DockerGroup, anchor: HTMLElement) {
		if (rowMenuWasOpen === group.id) {
			rowMenuWasOpen = '';
			rowMenu = null;
			return;
		}
		const rect = anchor.getBoundingClientRect();
		rowMenu = { group, x: Math.max(8, rect.right - 260), y: rect.bottom + 4 };
	}
	/** A click on a row opens the app's own page (not on its buttons and links). */
	function rowClick(event: MouseEvent, group: DockerGroup) {
		if ((event.target as Element).closest('button, a, input')) return;
		const app = groupApp(group);
		if (app) void goto(`/docker/apps/${encodeURIComponent(app)}`);
		else rowMenu = { group, x: event.clientX, y: event.clientY };
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
	// PLAN 64: the phone's full-screen view (in a window: inWindow).
	const phone = $derived(phoneView());
	// A narrow window: the filters get a row of their own under the tool bar.
	let roomy = $state(matchMedia('(min-width: 760px)').matches);
	$effect(() => {
		const query = matchMedia('(min-width: 760px)');
		const change = () => (roomy = query.matches);
		query.addEventListener('change', change);
		return () => query.removeEventListener('change', change);
	});
	const FILTERS = [
		['all', 'docker.table.filterAll'],
		['running', 'docker.table.filterRunning'],
		['stopped', 'docker.table.filterStopped'],
		['updates', 'docker.table.filterUpdates'],
	] as const;

	// PLAN 67-1: the menu bar of the desktop runs the same actions as the toolbar.
	import { registerMenuCommands } from '#lib/menu-commands.svelte.ts';
	const ready = () => !!status?.running && !globalBusy;
	registerMenuCommands({
		'docker.import': { run: () => open('import'), enabled: () => !!status?.running },
		'docker.startAll': {
			run: startAll,
			enabled: () => ready() && runCount < containers.length,
		},
		'docker.stopAll': {
			run: () => (confirmStopAll = true),
			enabled: () => ready() && runCount > 0,
		},
		'docker.checkUpdates': { run: checkUpdates, enabled: () => !!status?.running && !globalBusy },
		'docker.updateAll': { run: askUpdates, enabled: () => !!updatable && !globalBusy },
		...Object.fromEntries(
			(['all', 'running', 'stopped', 'updates'] as const).map((f) => [
				'docker.filter.' + f,
				{ run: () => (filter = f), checked: () => filter === f },
			]),
		),
	});
</script>

{#snippet searchBox()}
	<span class="search-box"
		><Icon name="search" size={15} /><input
			class="field search"
			type="search"
			bind:value={search}
			placeholder={t('docker.search')}
			aria-label={t('docker.search')}
		/></span
	>
{/snippet}

{#snippet filters()}
	<div class="segmented" role="group" aria-label={t('docker.filter')}>
		{#each FILTERS as [id, label] (id)}
			<button type="button" aria-pressed={filter === id} onclick={() => (filter = id)}
				>{t(label)} <b class="mono">{counts[id]}</b></button
			>
		{/each}
	</div>
{/snippet}

<PageHead title={t('docker.title')}>
	{#snippet actions()}
		{#if status?.running && containers.length > 0 && !phone}
			{@render searchBox()}
			{#if roomy}{@render filters()}{/if}
		{/if}
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
		<button
			type="button"
			class="btn icon-btn"
			aria-label={t('docker.table.more')}
			title={t('docker.table.more')}
			aria-haspopup="menu"
			aria-expanded={pageMenu === 'more'}
			onpointerdown={() => (pageMenuWasOpen = pageMenu === 'more')}
			onclick={(event) => openPageMenu('more', event.currentTarget)}
			><Icon name="more" size={17} /></button
		>
	{/snippet}
</PageHead>

{#if phone && status?.running && containers.length > 0}
	<!-- PLAN 64: on the phone search and filters get rows of their own under the tool bar. -->
	<div class="phone-tools">{@render searchBox()}{@render filters()}</div>
{:else if !roomy && status?.running && containers.length > 0}
	<div class="phone-tools">{@render filters()}</div>
{/if}

{#if pageMenu && pageMenuAt}
	<ContextMenu
		label={t(pageMenu === 'more' ? 'docker.table.more' : 'docker.installMenu')}
		x={pageMenuAt.x}
		y={pageMenuAt.y}
		onclose={() => (pageMenu = null)}
	>
		{#if pageMenu === 'more'}
			{#if status?.running && containers.length > 0}
				<button
					type="button"
					role="menuitem"
					disabled={globalBusy || runCount === containers.length}
					onclick={() => pick(startAll)}
					><Icon name="play" size={16} /><span>{t('docker.startAll')}</span></button
				>
				<button
					type="button"
					role="menuitem"
					disabled={globalBusy || runCount === 0}
					onclick={() => pick(() => (confirmStopAll = true))}
					><Icon name="stop" size={16} /><span>{t('docker.stopAll')} …</span></button
				>
				<button
					type="button"
					role="menuitem"
					disabled={globalBusy}
					onclick={() => pick(checkUpdates)}
					><Icon name="refresh" size={16} /><span>{t('docker.checkUpdates')}</span></button
				>
				<button
					type="button"
					role="menuitem"
					disabled={globalBusy || !updatable}
					onclick={() => pick(askUpdates)}
					><Icon name="download" size={16} /><span
						>{t('docker.updateAllCount', { count: updateApps.length })} …</span
					></button
				>
				<hr />
			{/if}
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
		<div class="stats">
			<section class="tile ui-card stat">
				<span class="stat-label">{t('docker.table.statRunning')}</span>
				<b class="stat-value"
					>{t('docker.table.statRunningValue', { running: counts.running, total: counts.all })}</b
				>
			</section>
			<section class="tile ui-card stat">
				<span class="stat-label">{t('docker.cpu')}</span>
				<b class="stat-value">{formatPercent(totalCpu)}</b>
			</section>
			<section class="tile ui-card stat">
				<span class="stat-label">{t('docker.table.statRam')}</span>
				<b class="stat-value">{formatBytes(totalRam)}</b>
			</section>
			<section class="tile ui-card stat">
				<span class="stat-label">{t('docker.table.statUpdates')}</span>
				<b class="stat-value" class:warn={counts.updates > 0}
					>{counts.updates
						? t('docker.table.statUpdatesValue', { count: counts.updates })
						: t('docker.table.statUpdatesNone')}</b
				>
			</section>
		</div>

		{#if shownGroups.length > 0}
			<section class="tile ui-card apps" aria-label={t('docker.table.title')}>
				<div class="table-wrap">
					<table class="data-table app-table">
						<thead>
							<tr>
								<th scope="col">{t('docker.table.colName')}</th>
								<th scope="col">{t('docker.table.colState')}</th>
								<th scope="col" class="wide">{t('docker.cpu')}</th>
								<th scope="col" class="wide num">{t('docker.ram')}</th>
								<th scope="col" class="wide">{t('docker.column.ports')}</th>
								<th scope="col" class="upd"><span class="sr-only">{t('docker.update')}</span></th>
								<th scope="col"><span class="sr-only">{t('docker.table.colActions')}</span></th>
							</tr>
						</thead>
						<tbody>
							{#each shownGroups as group (group.id)}
								{@const state = stateOf.get(group.id) ?? 'stopped'}
								{@const on = isOn(group) || state === 'restarting'}
								{@const app = groupApp(group)}
								{@const icon = group.members.find((c) => c.icon)?.icon ?? ''}
								{@const cpu = groupCpu(group)}
								{@const ports = groupPorts(group)}
								{@const busy = groupBusy(group)}
								<tr
									class:clickable={!!app}
									data-app={group.name}
									onclick={(event) => rowClick(event, group)}
									oncontextmenu={(event) => {
										event.preventDefault();
										rowMenu = { group, x: event.clientX, y: event.clientY };
									}}
								>
									<td class="name">
										<span class="who">
											<span
												class="app-icon"
												style:--hue={hue(group.name)}
												class:letter={!icon}
												aria-hidden="true"><AppIcon src={icon} name={group.name} /></span
											>
											<span class="lines">
												<span class="title-line"
													>{#if app}<a
															class="app-link"
															href="/docker/apps/{encodeURIComponent(app)}">{group.name}</a
														>{:else}<strong>{group.name}</strong>{/if}

													{#if groupUpdate(group)}<span class="chip warn narrow-chip"
															>{t('docker.update')}</span
														>{/if}</span
												>
												<span class="sub">{subLine(group)}</span>
												<!-- Narrow screens: the figures in the sub-line instead of columns. -->
												<span class="sub narrow"
													>{on
														? `${t('docker.cpu')} ${formatPercent(cpu)} · ${t('docker.ram')} ${formatBytes(groupRam(group))}`
														: ''}{on && ports.length ? ' · ' : ''}{ports.join(', ')}</span
												>
											</span>
										</span>
									</td>
									<td class="state {state}">
										<!-- The mark has a shape of its own per state (dot, ring, triangle). -->
										<span class="mark" aria-hidden="true"></span>{state === 'partial'
											? t('docker.stackSummary', {
													running: running(group).length,
													total: group.members.length,
												})
											: t(`docker.table.state.${state}`)}
									</td>
									<td class="wide cpu">
										{#if on}<span class="bar" aria-hidden="true"
												><span style:width="{Math.min(100, Math.max(2, cpu))}%"></span></span
											>{formatPercent(cpu)}{:else}<span class="muted">–</span>{/if}
									</td>
									<td class="wide num"
										>{#if on}{formatBytes(groupRam(group))}{:else}<span class="muted">–</span
											>{/if}</td
									>
									<td class="wide ports mono">{ports.length ? ports.join(', ') : '–'}</td>
									<td class="upd"
										>{#if groupUpdate(group)}<span class="chip warn"
												><Icon name="download" size={12} />{t('docker.update')}</span
											>{/if}</td
									>
									<td class="actions">
										<button
											type="button"
											class="btn icon-btn small"
											disabled={busy || globalBusy}
											aria-label={t(on ? 'docker.table.stopNamed' : 'docker.table.startNamed', {
												name: group.name,
											})}
											title={t(on ? 'docker.stop' : 'docker.start')}
											onclick={() => groupAction(group, on ? 'stop' : 'start')}
											><Icon name={on ? 'stop' : 'play'} size={14} /></button
										>
										<button
											type="button"
											class="btn icon-btn small"
											aria-label={t('docker.actionsFor', { name: group.name })}
											title={t('docker.actionsFor', { name: group.name })}
											aria-haspopup="menu"
											aria-expanded={rowMenu?.group.id === group.id}
											onpointerdown={() =>
												(rowMenuWasOpen = rowMenu?.group.id === group.id ? group.id : '')}
											onclick={(event) => openRowMenu(group, event.currentTarget)}
											><Icon name="more" size={15} /></button
										>
									</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			</section>
		{:else}
			<EmptyState title={t('docker.noMatch')} />
		{/if}
		<p class="foot muted">
			<span>{t('docker.onVolume', { volume: status.settings.volume })}</span>
			<span aria-hidden="true">·</span>
			<span>{checked}</span><button
				type="button"
				class="icon-link"
				aria-label={t('docker.checkUpdates')}
				title={t('docker.checkUpdates')}
				onclick={checkUpdates}
				disabled={globalBusy}><Icon name="refresh" size={14} /></button
			>
			<span aria-hidden="true">·</span>
			<span>{t('docker.version', { version: status.version })}</span>
		</p>
	{:else if jobs.length === 0 && idleApps.length === 0}
		<EmptyState title={t('docker.empty')} />
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

{#if rowMenu}
	{@const group = rowMenu.group}
	{@const app = groupApp(group)}
	{@const primary = stackPrimary(group, layout.primaryServices) ?? group.members[0]}
	{@const links = primary ? webLinks(primary, host, tailscaleHost) : []}
	{@const busy = groupBusy(group)}
	{@const stack = group.members.length > 1}
	<ContextMenu
		label={t('docker.actionsFor', { name: group.name })}
		x={rowMenu.x}
		y={rowMenu.y}
		onclose={() => (rowMenu = null)}
	>
		<div class="menu-title">{group.name}</div>
		{#if layout.primaryServices[group.id] && !stackPrimary(group, layout.primaryServices)}<p
				class="menu-title"
			>
				{t('docker.primaryMissing', { service: layout.primaryServices[group.id] })}
			</p>{/if}
		<button
			type="button"
			role="menuitem"
			disabled={busy}
			onclick={() => pick(() => groupAction(group, 'restart'))}
			><Icon name="restart" size={16} /><span>{t('docker.restart')}</span></button
		>
		{#if links.length}<p class="menu-title">{t('docker.table.open')}</p>{/if}
		{#each links as link, index (`${index}:${link.href}`)}<a
				role="menuitem"
				href={link.href}
				target="_blank"
				rel="noreferrer"
				title={link.label}
				onclick={() => (rowMenu = null)}
				><Icon name={link.icon} size={16} /><span>{link.text}</span></a
			>{/each}
		{#if inWindow && app}
			<button type="button" role="menuitem" onclick={() => pick(() => openRemote(app))}
				><Icon name="monitor" size={16} /><span>{t('docker.table.openRemote')}</span></button
			>
		{/if}
		<hr />
		{#each group.members as c (c.id)}
			{@const label = c.service || dockerName(c, 'distinct')}
			<button
				type="button"
				role="menuitem"
				onclick={() => pick(() => (dialog = { kind: 'logs', container: c }))}
				><Icon name="fileText" size={16} /><span
					>{stack ? t('docker.table.logsOf', { name: label }) : t('docker.logs')}</span
				></button
			>
			<button
				type="button"
				role="menuitem"
				disabled={!isRunning(c)}
				onclick={() => pick(() => addContainerConsole(c.id, dockerName(c, 'distinct')))}
				><Icon name="terminal" size={16} /><span
					>{stack ? t('docker.table.consoleOf', { name: label }) : t('docker.console')}</span
				></button
			>
		{/each}
		<hr />
		{#if app}
			<a
				role="menuitem"
				href="/docker/apps/{encodeURIComponent(app)}"
				onclick={() => (rowMenu = null)}
				><Icon name="edit" size={16} /><span>{t('docker.edit')}</span></a
			>
		{/if}
		{#if app && groupUpdate(group)}
			<button
				type="button"
				role="menuitem"
				disabled={busy}
				onclick={() => pick(() => updateApp(app))}
				><Icon name="download" size={16} /><span>{t('docker.updateNow')}</span></button
			>
		{/if}
		{#if stack}
			<button
				type="button"
				role="menuitem"
				disabled={savingLayout}
				onclick={() => pick(() => editLinks(group))}
				><Icon name="globe" size={16} /><span>{t('docker.stackLinks')}</span></button
			>
		{/if}
		{#if proxy && app}
			<button type="button" role="menuitem" onclick={() => pick(() => openTailnet(app))}
				><Icon name="tailnet" size={16} /><span
					>{t(appInTailnet(app) ? 'docker.tailnetUnpublish' : 'docker.tailnetPublish')}</span
				></button
			>
		{/if}
		<hr />
		<button
			type="button"
			role="menuitem"
			class="danger"
			onclick={() => pick(() => removeGroup(group))}
			><Icon name="trash" size={16} /><span>{t('docker.remove')}</span></button
		>
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
	<LogsDialog container={dialog.container} onclose={() => (dialog = null)} />
{:else if dialog?.kind === 'updates'}
	{@const target = dialog}
	<UpdatesDialog
		apps={updateApps}
		selected={target.selected}
		failed={target.failed}
		busy={dialogBusy}
		onselect={(selected) => (target.selected = selected)}
		onclose={() => (dialog = null)}
		onsubmit={applyUpdates}
	/>
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
	<RemoveAppDialog
		{app}
		volume={status?.settings.volume ?? ''}
		busy={dialogBusy}
		error={dialogError?.text ?? ''}
		onclose={() => (dialog = null)}
		onremove={(purge) => removeApp(app, purge)}
	/>
{:else if dialog}
	<ComposeSettingsDialog
		kind={dialog.kind}
		bind:draft
		{volumes}
		appdata={`/mnt/${status?.settings.volume}/appdata`}
		busy={dialogBusy}
		error={dialogError}
		onclose={() => (dialog = null)}
		onsubmit={submit}
	/>
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
	/* The filters: one segmented control with the number of apps in each. */
	.segmented {
		display: flex;
		flex: none;
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
		gap: 6px;
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
	.segmented button b {
		font-weight: 500;
	}
	.segmented button:hover {
		color: var(--text);
	}
	.segmented button[aria-pressed='true'] {
		background: var(--chip);
		color: var(--text);
		font-weight: 600;
	}
	/* Four figures above the table. */
	.stats {
		display: grid;
		grid-template-columns: repeat(4, minmax(0, 1fr));
		gap: var(--gap, 16px);
	}
	.stat {
		gap: 2px;
		padding: 12px 16px;
	}
	.stat-label {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.stat-value {
		font-size: var(--text-lg);
		font-weight: 600;
		font-variant-numeric: tabular-nums;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.stat-value.warn {
		color: var(--warn-text);
	}
	/* The table of apps. */
	.app-table tbody tr {
		transition: background 0.12s ease;
	}
	.app-table tbody tr:hover {
		background: color-mix(in srgb, var(--text) 4%, transparent);
	}
	.app-table tr.clickable {
		cursor: pointer;
	}
	.who {
		display: flex;
		align-items: center;
		gap: 12px;
		min-width: 0;
	}
	.app-icon {
		flex: none;
		display: grid;
		place-items: center;
		width: 30px;
		height: 30px;
		overflow: hidden;
		border-radius: 8px;
		background: var(--chip);
		font-size: var(--text-sm);
		font-weight: 700;
		text-transform: uppercase;
	}
	.app-icon.letter {
		background: hsl(var(--hue) 50% 36%);
		color: #fff;
	}
	.lines {
		display: flex;
		flex-direction: column;
		min-width: 0;
	}
	.lines strong,
	.app-link {
		color: var(--text);
		font-weight: 600;
		text-decoration: none;
		overflow-wrap: anywhere;
	}
	.app-link:hover,
	.app-link:focus-visible {
		text-decoration: underline;
	}
	.title-line {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 2px 8px;
		min-width: 0;
	}
	.lines .sub {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
		max-width: 22rem;
	}
	.narrow,
	.narrow-chip {
		display: none !important;
	}
	.state {
		white-space: nowrap;
	}
	.state .mark {
		display: inline-block;
		width: 8px;
		height: 8px;
		margin-right: 7px;
		border-radius: 50%;
		background: currentColor;
		vertical-align: 1px;
	}
	.state.stopped .mark {
		background: none;
		border: 1.5px solid currentColor;
	}
	.state.restarting .mark,
	.state.error .mark {
		width: 10px;
		height: 9px;
		border-radius: 0;
		clip-path: polygon(50% 0, 100% 100%, 0 100%);
	}
	.state.running,
	.state.partial {
		color: var(--ok-text);
	}
	.state.partial .mark,
	.state.restarting {
		color: var(--warn-text);
	}
	.state.error {
		color: var(--error-text);
	}
	.state.stopped {
		color: var(--muted);
	}
	.cpu {
		white-space: nowrap;
		font-variant-numeric: tabular-nums;
	}
	.bar {
		display: inline-block;
		width: 56px;
		height: 4px;
		margin-right: 8px;
		vertical-align: middle;
		border-radius: 2px;
		background: var(--track);
		overflow: hidden;
	}
	.bar > span {
		display: block;
		height: 100%;
		background: var(--accent);
	}
	.num {
		font-variant-numeric: tabular-nums;
	}
	.ports {
		max-width: 10rem;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
		color: var(--muted);
	}
	.upd {
		width: 1%;
		white-space: nowrap;
	}
	.chip :global(svg) {
		vertical-align: -1px;
		margin-right: 3px;
	}
	.actions {
		width: 1%;
		white-space: nowrap;
		text-align: right;
	}
	.actions .btn + .btn {
		margin-left: 6px;
	}
	.small {
		min-height: 30px;
		height: 30px;
		width: 30px;
	}
	.foot {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 4px 8px;
		margin: 0;
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
	:global(.dropdown-panel) [role^='menuitem'] small {
		display: block;
		color: var(--muted);
		font-size: var(--text-sm);
		white-space: normal;
	}
	small {
		font-size: var(--text-sm);
		color: var(--muted);
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
	.error-line {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-2);
	}
	/* PLAN 64: in the window's tool bar the search sits left and gives way first. */
	:global(.win-toolbar) .search-box {
		flex: 0 1 15rem;
		min-width: 7rem;
		width: auto;
		margin-right: auto;
	}
	.phone-tools {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}
	.phone-tools .search-box {
		width: 100%;
	}
	@media (prefers-reduced-motion: reduce) {
		.app-table tbody tr {
			transition: none;
		}
	}
	/* Fewer columns as the window narrows; the figures move into the sub-line. */
	@media (max-width: 899px) {
		.app-table .ports {
			max-width: 6rem;
		}
		.lines .sub {
			max-width: 13rem;
		}
		.app-table td.upd,
		.app-table th.upd {
			display: none;
		}
		.lines .narrow-chip {
			display: inline-flex !important;
			font-size: var(--text-xs);
		}
		.bar {
			width: 36px;
		}
	}
	@media (max-width: 699px) {
		.stats {
			grid-template-columns: repeat(2, minmax(0, 1fr));
		}
		.app-table .wide {
			display: none;
		}
		.lines .narrow {
			display: block !important;
		}
	}
	/* Phones: each app a stacked row – name with its figures, state, actions. */
	@media (max-width: 639px) {
		.app-table thead {
			display: none;
		}
		.app-table,
		.app-table tbody {
			display: block;
		}
		.app-table tr {
			display: grid;
			grid-template-columns: minmax(0, 1fr) auto;
			grid-template-areas:
				'name actions'
				'state actions';
			align-items: center;
			column-gap: 8px;
			padding: 10px 16px;
			border-bottom: 1px solid var(--border);
		}
		.app-table tr:last-child {
			border-bottom: 0;
		}
		.app-table td {
			display: block;
			padding: 0 !important;
			border: 0 !important;
		}
		.app-table td.name {
			grid-area: name;
		}
		.app-table td.state {
			grid-area: state;
			padding-left: 42px !important;
			white-space: normal;
		}
		.app-table td.upd {
			display: none;
		}
		.app-table td.actions {
			grid-area: actions;
			width: auto;
		}
		.lines .sub {
			max-width: none;
			white-space: normal;
		}
		.actions .small {
			width: 2.75rem;
			height: 2.75rem;
		}
		.search-box {
			flex: 1 1 10rem;
			width: auto;
		}
		.segmented button {
			height: 2.5rem;
		}
	}
</style>
