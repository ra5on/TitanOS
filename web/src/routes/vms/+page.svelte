<script lang="ts">
	import AdoptDialog from './AdoptDialog.svelte';
	import StatusBar from '#lib/components/explorer/StatusBar.svelte';
	import CloneDialog from './CloneDialog.svelte';
	let cloning = $state<VM | null>(null);
	// VMs: the list on the left, the chosen VM on the right with the tabs
	// Overview, Screen and Snapshots.
	import DangerZone from '#lib/components/DangerZone.svelte';
	import Dropdown from '#lib/components/Dropdown.svelte';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import { phoneView } from '#lib/desktop/mode.svelte.ts';
	import TasksPanel from '#lib/components/TasksPanel.svelte';
	import { api, type VM, type VMStatus } from '#lib/api.ts';
	import { poll, type Poller } from '#lib/poll.ts';
	import { i18n, t } from '#lib/i18n/index.svelte.ts';
	import { storage } from '#lib/storage.svelte.ts';
	import { addVmConsole } from '#lib/terminal.svelte.ts';
	import { vmLabel } from '#lib/vm-name.ts';
	import { nextScheduled } from '#lib/vm-schedule.ts';
	import { formatBytes, formatDateTime } from '#lib/format.ts';
	import { VNC_HEIGHT } from '#lib/vnc-size.ts';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { inWindow } from '#lib/desktop/frame.ts';
	import { SvelteSet } from 'svelte/reactivity';
	import Overview from './Overview.svelte';
	import Snapshots from './Snapshots.svelte';
	import VncScreen, { type ScaleMode, type ConnectionPhase } from './VncScreen.svelte';
	import VmActionDialog, { type VmDialogDraft, type VmDialogKind } from './VmActionDialog.svelte';
	import VmRowMenu, { type RowChoice } from './VmRowMenu.svelte';
	import VmXmlDialog from './VmXmlDialog.svelte';
	import { ConsoleSize } from './console-size.svelte.ts';
	import {
		KEYS,
		keyLabel,
		lowSpaceVolumes,
		MODES,
		noticeClass,
		problem,
		vmNotices,
		type Failure,
		type Problem,
	} from './vm-page.ts';

	// PLAN 67: Übersicht · Bildschirm · Hardware · Snapshots · Zeitplan.
	type Tab = 'overview' | 'screen' | 'hardware' | 'snapshots' | 'schedule';
	const TABS: Tab[] = ['overview', 'screen', 'hardware', 'snapshots', 'schedule'];
	type Filter = 'all' | 'running' | 'stopped';

	let status = $state<VMStatus | null>(null);
	// The first load failed: nothing to show but the error.
	let error = $state<Problem | null>(null);
	// A later refresh failed: the last list stays, marked as possibly stale.
	let refreshError = $state(''),
		lastRefresh = $state(''),
		refreshing = $state(false);
	// VMs with a request on its way; the others stay usable meanwhile.
	const busyNames = new SvelteSet<string>();
	const isBusy = (vm: VM | undefined) => !!vm && busyNames.has(vm.name);
	let pending = $state<Record<string, string>>({});
	let feedback = $state('');
	// The failed action of each VM, shown only with that VM.
	let failures = $state<Record<string, Failure>>({});
	let search = $state('');
	let filter = $state<Filter>('all');
	// On a phone the list folds away behind the name of the chosen VM.
	let listOpen = $state(false);
	let allNotices = $state(false);
	let rowMenu = $state<{ vm: VM; x: number; y: number } | null>(null);
	// The last CPU loads of every running VM, only in this page.
	let history = $state<Record<string, number[]>>({});

	// Adjusting the guest's resolution is the default; guests that cannot do it scale.
	let mode = $state<ScaleMode>('remote');
	let sidebar = $state(true);
	let dialog = $state<
		| { kind: 'remove' | 'poweroff'; vm: string }
		| { kind: 'paste' }
		| { kind: 'xml'; text: string }
		| null
	>(null);
	// "VM übernehmen": copies of VM settings on the volumes; read only on request.
	let adopting = $state(false);
	// In a desktop window the window's side bar lists the VMs (PLAN 62-3); "?adopt=1" opens the take-over.
	$effect(() => {
		if (page.url.searchParams.get('adopt') === '1') adopting = true;
	});
	let screen = $state<VncScreen>();
	let connectionPhase = $state<ConnectionPhase>('connecting');
	const size = new ConsoleSize();

	// Opening the screen of a VM brings all of it into view: if its bottom
	// would be cut off, the page scrolls once so that the toolbar sits below
	// the header. Scrolling by hand afterwards is left alone.
	let shownFor = '';
	$effect(() => {
		const key = tab === 'screen' && current ? current.name : '';
		if (!key || key === shownFor || !size.frame) return;
		shownFor = key;
		size.bringIntoView();
	});

	$effect(() => size.observe());

	const vms = $derived(status?.vms ?? []);
	const counts = $derived({
		all: vms.length,
		running: vms.filter((vm) => vm.state === 'running').length,
		stopped: vms.filter((vm) => vm.state !== 'running').length,
	});
	const shown = $derived(
		vms.filter(
			(vm) =>
				(filter === 'all' || (filter === 'running') === (vm.state === 'running')) &&
				`${vmLabel(vm)} ${vm.name}`.toLocaleLowerCase().includes(search.toLocaleLowerCase()),
		),
	);
	const current = $derived(vms.find((vm) => vm.name === page.url.searchParams.get('vm')) ?? vms[0]);
	const tab = $derived.by<Tab>(() => {
		const asked = page.url.searchParams.get('tab');
		if (TABS.includes(asked as Tab)) return asked as Tab;
		return current?.state === 'running' ? 'screen' : 'overview';
	});
	const link = (vm: VM, next?: Tab) =>
		`/vms?vm=${encodeURIComponent(vm.name)}${next ? `&tab=${next}` : ''}`;
	const canStart = (vm: VM) => vm.available && !!status?.kvm && !vm.job;

	const lowSpace = $derived(lowSpaceVolumes(storage.status?.volumes ?? [], current));

	// Every refresh goes through the poller: runs never overlap, and one asked
	// for during a run follows it, so an older answer never wins.
	let poller: Poller | undefined;
	const load = () => poller?.now() ?? Promise.resolve();
	async function fetchStatus() {
		refreshing = true;
		try {
			status = await api.get<VMStatus>('/vms');
			error = null;
			refreshError = '';
			lastRefresh = new Date().toISOString();
			const next: Record<string, number[]> = {};
			for (const vm of status.vms)
				if (vm.state === 'running')
					next[vm.name] = [...(history[vm.name] ?? []), vm.cpuPercent].slice(-40);
			history = next;
		} catch (err) {
			if (!status) error = problem(err);
			else refreshError = problem(err).text;
		} finally {
			refreshing = false;
		}
	}
	$effect(() => {
		const current = (poller = poll(fetchStatus, { interval: 3000 }));
		return () => {
			current();
			if (poller === current) poller = undefined;
		};
	});

	let feedbackTimer: ReturnType<typeof setTimeout> | undefined;
	async function act(vm: VM, action: string) {
		if (busyNames.has(vm.name)) return;
		busyNames.add(vm.name);
		delete failures[vm.name];
		feedback = '';
		pending[vm.name] = action;
		try {
			await api.post(`/vms/${encodeURIComponent(vm.name)}/${action}`);
			feedback = t('ui.vmAccepted', { name: vmLabel(vm) });
			clearTimeout(feedbackTimer);
			feedbackTimer = setTimeout(() => (feedback = ''), 5000);
		} catch (err) {
			failures[vm.name] = { problem: problem(err), action };
		} finally {
			await load();
			delete pending[vm.name];
			busyNames.delete(vm.name);
		}
	}
	const notices = $derived(
		vmNotices({ current, status, vms, failures, pending, lowSpace, feedback }),
	);

	function open(kind: VmDialogKind, vm = current) {
		if (!vm) return;
		dialog = kind === 'paste' ? { kind } : { kind, vm: vm.name };
	}
	const dialogVM = $derived.by(() => {
		const target = dialog && 'vm' in dialog ? dialog.vm : '';
		return target ? vms.find((vm) => vm.name === target) : current;
	});

	async function showXML(vm = current) {
		if (!vm) return;
		try {
			dialog = {
				kind: 'xml',
				text: (await api.get<{ xml: string }>(`/vms/${encodeURIComponent(vm.name)}/xml`)).xml,
			};
		} catch (err) {
			failures[vm.name] = { problem: problem(err), action: '' };
		}
	}

	// Errors are shown by the dialog.
	async function submit(draft: VmDialogDraft) {
		const vm = dialogVM;
		if (!vm || !dialog || dialog.kind === 'xml') return;
		const marks = dialog.kind !== 'paste';
		if (marks) busyNames.add(vm.name);
		try {
			if (dialog.kind === 'remove') {
				await api.delete(`/vms/${encodeURIComponent(vm.name)}`, {
					deleteDisks: draft.deleteDisks,
					// The API wants the VM name as confirmation for deleting disks.
					confirm: draft.deleteDisks ? vm.name : '',
				});
				await goto('/vms', { replaceState: true });
			} else if (dialog.kind === 'poweroff') {
				await api.post(`/vms/${encodeURIComponent(vm.name)}/poweroff`);
			} else if (dialog.kind === 'paste') {
				screen?.paste(draft.text);
			}
			dialog = null;
			await load();
		} finally {
			if (marks) busyNames.delete(vm.name);
		}
	}

	function saveScreenshot() {
		const data = screen?.screenshot();
		if (!data || !current) return;
		const anchor = document.createElement('a');
		anchor.href = data;
		anchor.download = `${current.name}.png`;
		anchor.click();
	}

	function ownWindow(event: MouseEvent) {
		if (!current) return;
		event.preventDefault();
		window.open(
			`/vms/screen?vm=${encodeURIComponent(current.name)}`,
			`vm-${current.name}`,
			'popup,width=1296,height=860',
		);
	}

	// Only the virtio graphics of the Linux template can take a resolution
	// from the browser.
	const modeHint = (entry: ScaleMode) =>
		t(
			entry === 'remote' && current?.template !== 'linux'
				? 'vms.mode.remote.unsupported'
				: `vms.mode.${entry}.hint`,
		);
	// A VM that cannot change its resolution shows scaling; the choice stays
	// for the next VM that can.
	const shownMode = $derived<ScaleMode>(
		mode === 'remote' && current && current.template !== 'linux' ? 'scale' : mode,
	);
	// Only a different VM closes the list; a refreshed copy of the same one
	// (a new object after every poll) must not.
	const currentName = $derived(current?.name);
	$effect(() => {
		void currentName;
		listOpen = false;
		allNotices = false;
	});

	// Snapshot before closing: the template's reactive @const becomes undefined
	// immediately when rowMenu is cleared.
	function rowAction(action: (vm: VM) => void) {
		const vm = rowMenu?.vm;
		rowMenu = null;
		if (vm) action(vm);
	}

	function rowChoice(vm: VM, choice: RowChoice) {
		if (choice === 'shutdown' || choice === 'reboot' || choice === 'start') act(vm, choice);
		else if (choice === 'screen' || choice === 'snapshots') goto(link(vm, choice));
		else if (choice === 'edit') goto(`/vms/edit?vm=${encodeURIComponent(vm.name)}`);
		else if (choice === 'clone') cloning = vm;
		else open(choice, vm);
	}

	function rowContext(vm: VM, event: MouseEvent) {
		event.preventDefault();
		rowMenu = { vm, x: event.clientX, y: event.clientY };
	}
	function rowButton(vm: VM, event: MouseEvent) {
		const rect = (event.currentTarget as HTMLElement).getBoundingClientRect();
		rowMenu = { vm, x: rect.left, y: rect.bottom + 4 };
	}
	// PLAN 64: the phone's full-screen view keeps only the buttons in the bar.
	const phone = $derived(phoneView());

	// PLAN 67-1: the menu bar of the desktop acts on the VM shown.
	import { registerMenuCommands } from '#lib/menu-commands.svelte.ts';
	const vmOn = () => current?.state === 'running';
	const vmFree = () => !!current && !isBusy(current);
	const screenOn = () => tab === 'screen' && vmOn() && !!screen;
	let overview = $state<ReturnType<typeof Overview>>();
	/** The CD drive lives in the hardware part: shown first if another tab is open. */
	async function withOverview(act: (o: NonNullable<typeof overview>) => void) {
		if (!current) return;
		if (tab !== 'hardware' && tab !== 'overview') {
			await goto(link(current, 'hardware'));
			await tick();
		}
		if (overview) act(overview);
	}
	import { tick } from 'svelte';
	registerMenuCommands({
		'vms.start': {
			run: () => current && act(current, 'start'),
			enabled: () => vmFree() && !vmOn() && !!current && canStart(current),
		},
		'vms.shutdown': {
			run: () => current && act(current, 'shutdown'),
			enabled: () => vmFree() && vmOn(),
		},
		'vms.reboot': {
			run: () => current && act(current, 'reboot'),
			enabled: () => vmFree() && vmOn(),
		},
		'vms.poweroff': { run: () => open('poweroff'), enabled: () => vmFree() && vmOn() },
		'vms.clone': { run: () => (cloning = current ?? null), enabled: () => vmFree() && !vmOn() },
		'vms.remove': { run: () => open('remove'), enabled: vmFree },
		'vms.edit': {
			run: () => current && goto(`/vms/edit?vm=${encodeURIComponent(current.name)}`),
			enabled: () => !!current,
		},
		...Object.fromEntries(
			TABS.map((next) => [
				'vms.tab.' + next,
				{
					run: () => current && goto(link(current, next)),
					enabled: () => !!current,
					checked: () => !!current && tab === next,
				},
			]),
		),
		'vms.cad': { run: () => screen?.ctrlAltDel(), enabled: screenOn },
		// "Tasten senden ›": the key combinations of the screen's own key menu.
		'vms.keys': {
			items: () =>
				screenOn()
					? KEYS.map((k, i) => ({ id: String(i), text: keyLabel(k.label, i18n.locale) }))
					: [],
			run: (i) => {
				const entry = KEYS[Number(i)];
				if (entry) screen?.sendKeys(entry.keys);
			},
		},
		'vms.isoInsert': { run: () => void withOverview((o) => o.insertMedium()), enabled: vmFree },
		'vms.isoEject': {
			run: () => void withOverview((o) => o.ejectMedium()),
			enabled: () => vmFree() && !!current?.iso && !!current?.disks.length,
		},
		...Object.fromEntries(
			MODES.map((entry) => [
				'vms.mode.' + entry,
				{
					run: () => (mode = entry),
					enabled: () => entry !== 'remote' || current?.template === 'linux',
					checked: () => shownMode === entry,
				},
			]),
		),
		'vms.fullscreen': { run: () => void size.frame?.requestFullscreen(), enabled: screenOn },
		'vms.screenshot': { run: saveScreenshot, enabled: screenOn },
		'vms.paste': { run: () => open('paste'), enabled: screenOn },
		'vms.serial': {
			run: () => current && addVmConsole(current.name, vmLabel(current)),
			enabled: vmOn,
		},
	});
</script>

<svelte:document
	onfullscreenchange={() => (size.fullscreen = document.fullscreenElement === size.frame)}
/>
<!-- The corner belongs to the frame itself; clicks into the picture land on noVNC's canvas. -->
<svelte:window
	onpointerdown={(event) => {
		if (event.target === size.frame) size.dragStart();
	}}
	onpointerup={size.dragEnd}
/>

{#if !status && error}
	<div class="notice-error" role="alert">
		<div class="notice-line">
			<span class="notice-text">{error.text}</span>
			<button type="button" class="btn small" disabled={refreshing} onclick={load}
				>{t('common.retry')}</button
			>
		</div>
		{#if error.detail}<pre>{error.detail}</pre>{/if}
	</div>
{:else if !status}
	<p class="muted loading" role="status">{t('common.loading')}</p>
{/if}
{#if status && refreshError}
	<div class="notice-warn" role="alert">
		<div class="notice-line">
			<span class="notice-text"
				>{refreshError}
				{t('vms.stale', { time: new Date(lastRefresh).toLocaleTimeString() })}</span
			>
			<button type="button" class="btn small" disabled={refreshing} onclick={load}
				>{t('common.retry')}</button
			>
		</div>
	</div>
{/if}

{#snippet vmState(current: VM)}
	<span class="chip" class:ok={current.state === 'running'}>{t(`vms.state.${current.state}`)}</span>
	{#if current.autostart}<span class="chip">{t('vms.autostart')}</span>{/if}
{/snippet}

{#snippet vmButtons(current: VM, tasks = true)}
	<!-- PLAN 67: the main actions, the screen as the main one while the VM
	     runs; the rest in "⋯" (what does not fit moves there too). -->
	{#if tasks}<TasksPanel compact />{/if}
	{#if current.state === 'running'}
		<button
			type="button"
			class="btn"
			onclick={() => act(current, 'shutdown')}
			disabled={isBusy(current)}><Icon name="power" size={15} />{t('vms.shutdown')}</button
		>
		<button
			type="button"
			class="btn"
			onclick={() => act(current, 'reboot')}
			disabled={isBusy(current)}><Icon name="restart" size={15} />{t('vms.reboot')}</button
		>
		{#if tab !== 'screen'}<a
				class="btn btn-primary"
				href={link(current, 'screen')}
				data-sveltekit-replacestate
				data-sveltekit-noscroll>{t('vms.openScreen')}</a
			>{/if}
	{:else}
		<button
			type="button"
			class="btn btn-primary"
			onclick={() => act(current, 'start')}
			disabled={isBusy(current) || !canStart(current)}>{t('vms.start')}</button
		>
	{/if}
	<Dropdown label={t('ui.more')} icon="more" align="right">
		{#snippet children(close)}
			<a role="menuitem" href="/vms/edit?vm={encodeURIComponent(current.name)}" onclick={close}
				><Icon name="edit" size={16} /><span>{t('vms.edit')}</span></a
			>
			<hr />
			{#if current.state === 'running'}
				<button
					type="button"
					role="menuitem"
					disabled={isBusy(current)}
					onclick={() => {
						close();
						open('poweroff');
					}}><Icon name="power" size={16} /><span>{t('vms.poweroff')}</span></button
				>
				<button
					type="button"
					role="menuitem"
					onclick={() => {
						close();
						addVmConsole(current.name, vmLabel(current));
					}}><Icon name="terminal" size={16} /><span>{t('vms.serial')}</span></button
				>
				<hr />
			{/if}
			<button
				type="button"
				role="menuitem"
				onclick={() => {
					close();
					showXML();
				}}><Icon name="fileText" size={16} /><span>{t('vms.showXml')}</span></button
			>
			{#if current.state !== 'running'}
				<button
					type="button"
					role="menuitem"
					disabled={isBusy(current) || !!current.job}
					onclick={() => {
						close();
						cloning = current;
					}}><Icon name="copy" size={16} /><span>{t('ops.clone')}</span></button
				>
			{/if}
		{/snippet}
	</Dropdown>
{/snippet}

{#if status && vms.length === 0}
	{#if !status.kvm}<p class="notice-warn" role="status">{t('vms.noKvm')}</p>{/if}
	<PageHead title={t('vms.title')}>
		{#snippet actions()}
			<a class="btn" href="/vms/media">{t('media.title')}</a>
			<button type="button" class="btn" onclick={() => (adopting = true)}
				>{t('vms.adopt.open')}</button
			>
			<TasksPanel compact />
			<a class="btn btn-primary" href="/vms/new">{t('vms.create')}</a>
		{/snippet}
	</PageHead>
	<EmptyState title={t('vms.empty')} />
{:else if status && current}
	<div class="vm-page">
		<!-- PLAN 64: in a window the title bar names the VM; state and buttons form the tool bar. -->
		<PageHead title={vmLabel(current)} id="vm-title">
			{#snippet actions()}
				{#if !phone}<strong class="bar-name" aria-hidden="true">{vmLabel(current)}</strong
					>{@render vmState(current)}<span class="spacer"></span>{/if}
				{@render vmButtons(current, false)}
			{/snippet}
		</PageHead>
		<!-- On the phone the bar keeps only the buttons; the state gets a row of its own. -->
		{#if phone}<div class="phone-state">
				{@render vmState(current)}<TasksPanel compact />
			</div>{/if}
		{#if current.description || current.ioError}<div class="title">
				{#if current.description}<span class="description muted">{current.description}</span>{/if}
				{#if current.ioError}<p class="notice-error io-error" role="alert">
						{t('vms.ioError')}
						<button type="button" class="btn small" onclick={() => act(current, 'resume')}
							>{t('vms.resume')}</button
						>
					</p>{/if}
			</div>{/if}

		{#if notices.length}
			<div class="notices">
				{#each allNotices ? notices : notices.slice(0, 1) as notice, index (index)}
					<div
						class={noticeClass[notice.level]}
						role={notice.level === 'error' ? 'alert' : 'status'}
					>
						<div class="notice-line">
							<span class="notice-text">{notice.text}</span>
							{#if notice.retry && failures[current.name]?.action}<button
									type="button"
									class="btn small"
									disabled={isBusy(current)}
									onclick={() => act(current, failures[current.name].action)}
									>{t('ui.retry')}</button
								>{/if}
							{#if index === 0 && notices.length > 1}
								<button
									type="button"
									class="more-notices"
									aria-expanded={allNotices}
									onclick={() => (allNotices = !allNotices)}
									>{allNotices
										? t('vms.noticesLess')
										: t('vms.noticesMore', { count: notices.length - 1 })}</button
								>
							{/if}
						</div>
						{#if notice.detail}<pre>{notice.detail}</pre>{/if}
					</div>
				{/each}
			</div>
		{/if}

		<div class="command-bar">
			<nav class="tabs" aria-label={t('vms.tabs')}>
				{#each TABS as entry (entry)}
					<a
						href={link(current, entry)}
						aria-current={tab === entry ? 'page' : undefined}
						data-sveltekit-replacestate
						data-sveltekit-noscroll>{t(`vms.tab.${entry}`)}</a
					>
				{/each}
			</nav>

			{#if tab === 'screen'}
				<div class="toolbar" role="toolbar" aria-label={t('vms.screen')}>
					{#if current.state === 'running'}
						<Dropdown label={t('vms.send')} text={t('vms.send')} icon="keyboard" compact>
							{#snippet children(close)}
								{#each KEYS as entry (entry.label)}
									<button
										type="button"
										role="menuitem"
										onclick={() => {
											close();
											screen?.sendKeys(entry.keys);
										}}><span>{keyLabel(entry.label, i18n.locale)}</span></button
									>
								{/each}
								<hr />
								<button
									type="button"
									role="menuitem"
									onclick={() => {
										close();
										open('paste');
									}}><Icon name="paste" size={16} /><span>{t('vms.clipboard')} …</span></button
								>
							{/snippet}
						</Dropdown>
						<Dropdown
							label={t('vms.display')}
							text={t(`vms.mode.${shownMode}`)}
							icon="monitor"
							compact
						>
							{#snippet children(close)}
								{#each MODES as entry (entry)}
									<button
										type="button"
										role="menuitemradio"
										aria-checked={shownMode === entry}
										disabled={entry === 'remote' && current.template !== 'linux'}
										title={modeHint(entry)}
										onclick={() => {
											close();
											mode = entry;
										}}><span>{t(`vms.mode.${entry}`)}</span></button
									>
								{/each}
							{/snippet}
						</Dropdown>
						<span class="grow"></span>
						{#if !inWindow}<button
								type="button"
								class="tool icon-tool"
								aria-pressed={sidebar}
								aria-label={t('vms.sidebar')}
								title={t('vms.sidebar')}
								onclick={() => (sidebar = !sidebar)}><Icon name="sidebar" size={16} /></button
							>{/if}
						<button
							type="button"
							class="tool icon-tool"
							aria-label={t('vms.screenshot')}
							title={t('vms.screenshot')}
							onclick={saveScreenshot}><Icon name="camera" size={16} /></button
						>
						<button
							type="button"
							class="tool icon-tool"
							aria-label={t('vms.serial')}
							title={t('vms.serial')}
							onclick={() => addVmConsole(current.name, vmLabel(current))}
							><Icon name="terminal" size={16} /></button
						>
						<a
							class="tool icon-tool"
							href="/vms/screen?vm={encodeURIComponent(current.name)}"
							target="_blank"
							rel="noopener"
							aria-label={t('vms.ownWindow')}
							title={t('vms.ownWindow')}
							onclick={ownWindow}><Icon name="external" size={16} /></a
						>
						<button
							type="button"
							class="tool icon-tool strong"
							aria-label={t('vms.fullscreen')}
							title={t('vms.fullscreen')}
							onclick={() => size.frame?.requestFullscreen()}
							><Icon name="maximize" size={16} /></button
						>
					{:else}<span class="grow"></span>
						{#if !inWindow}<button
								type="button"
								class="tool icon-tool"
								aria-pressed={sidebar}
								aria-label={t('vms.sidebar')}
								title={t('vms.sidebar')}
								onclick={() => (sidebar = !sidebar)}><Icon name="sidebar" size={16} /></button
							>{/if}
					{/if}
				</div>
			{/if}
		</div>
		<div class="layout">
			{#if !inWindow && (sidebar || tab !== 'screen')}
				<aside class="tile" class:open={listOpen} aria-labelledby="vms-title">
					<div class="aside-head">
						<h2 class="tile-title" id="vms-title">{t('vms.title')}</h2>
						<button
							type="button"
							class="btn picker"
							aria-expanded={listOpen}
							onclick={() => (listOpen = !listOpen)}
						>
							<span>{vmLabel(current)}</span><Icon name="chevronDown" size={14} />
						</button>
						<button
							type="button"
							class="btn add"
							aria-label={t('vms.adopt.open')}
							title={t('vms.adopt.open')}
							onclick={() => (adopting = true)}><Icon name="download" size={16} /></button
						>
						<a class="btn add" href="/vms/new" aria-label={t('vms.create')} title={t('vms.create')}
							><Icon name="plus" size={16} /></a
						>
					</div>
					<div class="list-body">
						<label class="vm-search"
							><span class="sr-only">{t('ui.vmSearch')}</span><input
								class="field"
								type="search"
								bind:value={search}
								placeholder={t('ui.vmSearch')}
							/></label
						>
						<div class="filters" role="group" aria-label={t('vms.filter')}>
							{#each ['all', 'running', 'stopped'] as const as entry (entry)}
								<button
									type="button"
									class="filter"
									aria-pressed={filter === entry}
									onclick={() => (filter = entry)}
									>{t(`vms.filter.${entry}`)} <span class="count">{counts[entry]}</span></button
								>
							{/each}
						</div>
						{#if shown.length === 0}<p class="vm-empty muted">{t('ui.vmNoResults')}</p>{:else}
							<!-- PLAN 75-19/75-20, style W4: one row per VM, columns instead of cards. -->
							<div class="vm-cols muted" aria-hidden="true">
								<span>{t('vms.col.name')}</span><span>{t('vms.col.state')}</span><span
									class="num">{t('vms.col.memory')}</span
								>
							</div>
						{/if}
						{#each shown as vm (vm.name)}
							{@const selected = vm.name === current.name}
							<div
								class="vm"
								class:selected
								oncontextmenu={(event) => rowContext(vm, event)}
								role="presentation"
							>
								<a class="vm-link" href={link(vm)} aria-current={selected ? 'page' : undefined}>
									<span class="vm-name">{vmLabel(vm)}</span>
									<span class="vm-mem num">{formatBytes(vm.memoryMiB * 1024 * 1024)}</span>
									<span class="vm-state">
										<span class="dot" class:ok={vm.state === 'running'} aria-hidden="true"
										></span>{t(`vms.state.${vm.state}`)}
										{#if vm.autostart}<span class="autostart" title={t('vms.autostart')}
												><Icon name="power" size={12} /><span class="sr-only"
													>{t('vms.autostart')}</span
												></span
											>{/if}
										{#if nextScheduled(vm.schedule)}
											{@const next = nextScheduled(vm.schedule)!}
											{@const label = t(
												next.action === 'start' ? 'vms.sched.nextStartAt' : 'vms.sched.nextStopAt',
												{ at: formatDateTime(new Date(next.at)) },
											)}
											<span class="autostart" title={label}
												><Icon name="clock" size={12} /><span class="sr-only">{label}</span></span
											>
										{/if}
									</span>
								</a>
								<div class="vm-actions">
									<button
										type="button"
										class="icon"
										aria-label={t('vms.actionsFor', { name: vmLabel(vm) })}
										title={t('ui.more')}
										aria-haspopup="menu"
										onclick={(event) => rowButton(vm, event)}><Icon name="more" size={15} /></button
									>
								</div>
							</div>
						{/each}
					</div>
				</aside>
			{/if}

			<section class="main" aria-labelledby="vm-title">
				{#if tab === 'overview' || tab === 'hardware' || tab === 'schedule'}
					<Overview
						bind:this={overview}
						part={tab}
						vm={current}
						history={history[current.name] ?? []}
						volumes={storage.status?.volumes ?? []}
						canStart={canStart(current)}
						busy={isBusy(current)}
						onstart={() => act(current, 'start')}
						onchanged={load}
						onsnapshots={() => goto(link(current, 'snapshots'), { replaceState: true })}
					/>
					<!-- Removing stands apart, at the end of the hardware. -->
					{#if tab === 'hardware'}<DangerZone
							tile
							hint={t(current.state === 'running' ? 'vms.removeStopFirst' : 'vms.removeHint')}
						>
							<button
								type="button"
								class="btn btn-danger"
								disabled={isBusy(current) || current.state === 'running'}
								onclick={() => open('remove')}>{t('vms.remove')}</button
							>
						</DangerZone>{/if}
				{:else if tab === 'snapshots'}
					<Snapshots vm={current} />
				{:else}
					{#if size.error}<p class="notice-error" role="alert">
							{t('vms.size.failed')}
							{size.error}
						</p>{/if}
					<div
						class="frame"
						style:height={size.fullscreen ? null : `${size.fitted}px`}
						style:width={size.fullscreen || size.width === null ? null : `${size.width}px`}
						style:max-height={size.fullscreen || size.room <= 0
							? null
							: `${Math.max(VNC_HEIGHT.min, Math.floor(size.room))}px`}
						class:fullscreen={size.fullscreen}
						bind:this={size.frame}
					>
						{#if current.state === 'running'}
							{#key current.name}
								<VncScreen
									bind:this={screen}
									vm={current.name}
									{mode}
									resizable={current.template === 'linux'}
									onstatus={(phase) => (connectionPhase = phase)}
								/>
							{/key}
						{/if}
					</div>
					<div class="screen-footer">
						{#if current.state !== 'running'}
							<div class="screen-state">
								<span class="muted">{t('vms.off', { name: vmLabel(current) })}</span><button
									type="button"
									class="btn btn-primary"
									onclick={() => act(current, 'start')}
									disabled={isBusy(current) || !canStart(current)}>{t('vms.startVm')}</button
								>
							</div>
						{:else if connectionPhase !== 'connected'}
							<p class="muted hint" role="status">
								{t(
									connectionPhase === 'disabled'
										? 'vms.screenDisabled'
										: connectionPhase === 'lost'
											? 'vms.screenLost'
											: 'vms.screenConnecting',
								)}
							</p>
						{/if}
						<p class="muted hint">{t('vms.hint')}</p>
					</div>
				{/if}
			</section>
		</div>
	</div>
{/if}

{#if status && vms.length > 0}
	{@const memory = vms
		.filter((vm) => vm.state === 'running')
		.reduce((sum, vm) => sum + vm.memoryMiB, 0)}
	<!-- PLAN 75-19, style W4: the figures in one line at the bottom. -->
	<StatusBar label={t('vms.title')}>
		<span>{t('vms.title')} <b>{counts.all}</b></span>
		<span>{t('vms.filter.running')} <b>{counts.running}</b></span>
		<span>{t('vms.memoryRunning')} <b>{formatBytes(memory * 1024 * 1024)}</b></span>
	</StatusBar>
{/if}
{#if rowMenu}
	{@const vm = rowMenu.vm}
	<VmRowMenu
		{vm}
		x={rowMenu.x}
		y={rowMenu.y}
		busy={isBusy(vm)}
		canStart={canStart(vm)}
		onclose={() => (rowMenu = null)}
		onchoose={(choice) => rowAction((vm) => rowChoice(vm, choice))}
	/>
{/if}

{#if dialog?.kind === 'xml'}
	<VmXmlDialog text={dialog.text} onclose={() => (dialog = null)} />
{:else if dialog && dialogVM}
	<VmActionDialog
		kind={dialog.kind}
		vm={dialogVM}
		onclose={() => (dialog = null)}
		onsubmit={submit}
	/>
{/if}

{#if adopting}<AdoptDialog
		onclose={() => (adopting = false)}
		onadopted={async (name) => {
			adopting = false;
			await load();
			goto(`/vms?vm=${encodeURIComponent(name)}`);
		}}
	/>{/if}

{#if cloning}<CloneDialog
		source={cloning}
		onclose={() => (cloning = null)}
		onstarted={() => {
			cloning = null;
			load();
		}}
	/>{/if}

<style>
	/* PLAN 67: the VM's name leads the tool bar (the heading itself stays for screen readers). */
	.bar-name {
		min-width: 0;
		max-width: 40%;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
		font-size: var(--text-lg);
		font-weight: 600;
	}
	.vm-page {
		width: 100%;
		max-width: 1480px;
		margin: 0 auto;
		display: flex;
		flex-direction: column;
		gap: 14px;
	}
	.command-bar {
		display: flex;
		align-items: center;
		gap: var(--space-5);
		min-height: 48px;
		border-bottom: 1px solid var(--border);
	}
	.layout {
		display: flex;
		flex-wrap: wrap;
		align-items: flex-start;
		gap: var(--space-5);
	}
	aside {
		flex: 0 0 260px;
		min-width: 220px;
		display: flex;
		flex-direction: column;
		overflow: hidden;
	}
	.aside-head {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		min-height: 50px;
		padding: 14px;
		border-bottom: 1px solid var(--panel-border);
	}
	.aside-head h2 {
		flex: 1;
	}
	.picker {
		display: none;
	}
	.add {
		width: 2.25rem;
		height: 2.25rem;
		padding: 0;
		justify-content: center;
	}
	.vm-search {
		display: block;
		margin: var(--space-3) var(--space-3) var(--space-2);
	}
	.filters {
		display: flex;
		gap: var(--space-1);
		padding: 0 var(--space-3) var(--space-2-5);
	}
	.filter {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1-5);
		height: 1.875rem;
		padding: 0 var(--space-2-5);
		background: transparent;
		border: 1px solid var(--border);
		border-radius: 999px;
		color: var(--muted);
		font: inherit;
		font-size: var(--text-sm);
		cursor: pointer;
	}
	.filter[aria-pressed='true'] {
		background: var(--chip);
		border-color: var(--accent);
		color: var(--text);
	}
	.count {
		font-family: var(--font-mono);
	}
	.vm-empty {
		margin: 0;
		padding: var(--space-1) 14px var(--space-3);
		font-size: var(--text-base);
	}
	.vm-cols {
		display: grid;
		grid-template-columns: minmax(0, 1fr) auto;
		gap: var(--space-2);
		padding: var(--space-1) calc(2rem + var(--space-3)) var(--space-1) 17px;
		border-bottom: 1px solid var(--panel-border);
		font-size: var(--text-xs);
	}
	.vm-cols span:nth-child(2) {
		display: none;
	}
	.num {
		text-align: end;
		font-variant-numeric: tabular-nums;
	}
	.vm {
		display: flex;
		align-items: center;
		gap: var(--space-1);
		padding-right: var(--space-2);
		border-left: 3px solid transparent;
		border-bottom: 1px solid var(--panel-border);
		min-height: 44px;
	}
	.vm:hover {
		background: var(--chip);
	}
	.vm.selected {
		border-left-color: var(--accent);
		background: var(--chip);
	}
	.vm-link {
		display: grid;
		grid-template-columns: minmax(0, 1fr) auto;
		align-items: center;
		gap: 2px var(--space-2);
		flex: 1;
		min-width: 0;
		padding: var(--space-2) var(--space-1-5) var(--space-2) 14px;
		color: var(--text);
		text-decoration: none;
	}
	.vm-name {
		overflow: hidden;
		font-size: var(--text-md);
		font-weight: 500;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.vm-mem {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.vm-state {
		grid-column: 1 / -1;
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-1) var(--space-1-5);
		font-size: var(--text-sm);
	}
	.autostart {
		display: inline-flex;
		color: var(--muted);
	}
	.vm-actions {
		display: flex;
		flex: none;
	}
	.icon,
	.icon-tool {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		width: 2rem;
		height: 2rem;
		padding: 0;
		background: transparent;
		border: 0;
		border-radius: var(--radius-ctl);
		color: var(--muted);
		cursor: pointer;
	}
	.icon:hover:not(:disabled),
	.icon-tool:hover,
	.icon-tool[aria-pressed='true'] {
		background: var(--chip);
		color: var(--text);
	}
	.icon:disabled {
		opacity: 0.4;
		cursor: default;
	}
	.main {
		flex: 999 1 560px;
		min-width: 0;
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
	}
	.title {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
		min-width: 0;
	}
	.description {
		font-size: var(--text-md);
	}
	.icon-btn {
		width: 2.375rem;
		padding: 0;
		justify-content: center;
	}
	.notices {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}
	.notice-line {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-1-5) var(--space-3);
	}
	.notice-text {
		flex: 1 1 16rem;
	}
	.more-notices {
		padding: 0;
		background: none;
		border: 0;
		color: inherit;
		font: inherit;
		font-size: var(--text-base);
		text-decoration: underline;
		cursor: pointer;
	}
	.btn.small {
		height: 2rem;
		padding: 0 var(--space-2-5);
		font-size: var(--text-base);
	}
	.tabs {
		display: flex;
		gap: 26px;
		align-self: stretch;
		align-items: center;
		flex: none;
	}
	.tabs a {
		display: flex;
		align-items: center;
		align-self: stretch;
		padding: 0 2px;
		border-bottom: 2px solid transparent;
		color: var(--muted);
		font-size: var(--text-md);
		text-decoration: none;
		white-space: nowrap;
	}
	.tabs a:hover {
		color: var(--text);
	}
	.tabs a[aria-current='page'] {
		border-bottom-color: var(--accent);
		color: var(--text);
		font-weight: 600;
	}
	.toolbar {
		display: flex;
		flex: 1;
		min-width: 0;
		align-items: center;
		gap: var(--space-1-5);
		font-size: var(--text-base);
	}
	.toolbar::before {
		content: '';
		height: 22px;
		width: 1px;
		background: var(--border);
		margin-right: var(--space-2-5);
	}
	.grow {
		flex: 1 1 auto;
	}
	.icon-tool {
		width: 34px;
		height: 34px;
		color: var(--text);
		text-decoration: none;
	}
	.icon-tool.strong {
		background: var(--chip);
		color: var(--accent);
	}
	/* Width and height are dragged at the bottom right corner; it is never
	   wider than its column and never taller than the rest of the window
	   (max-height is set from the page). */
	.frame {
		position: relative;
		width: 100%;
		max-width: 100%;
		min-width: min(320px, 100%);
		min-height: 240px;
		background: var(--screen-bg);
		border: 1px solid var(--border);
		border-radius: var(--radius-tile);
		overflow: hidden;
		resize: both;
	}
	.frame.fullscreen {
		width: 100%;
		height: 100%;
		max-height: none;
		border: 0;
		border-radius: 0;
		resize: none;
	}
	.hint {
		font-size: var(--text-sm);
	}
	.screen-footer {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}
	.screen-state {
		display: flex;
		align-items: center;
		flex-wrap: wrap;
		gap: var(--space-3);
	}
	label {
		display: flex;
		flex-direction: column;
		gap: var(--space-1-5);
		font-size: var(--text-base);
	}
	label > span {
		color: var(--muted);
	}
	pre {
		margin: var(--space-2) 0 0;
		font-family: var(--font-mono);
		font-size: var(--text-sm);
		white-space: pre-wrap;
		word-break: break-word;
	}
	@media (max-width: 1100px) {
		.command-bar {
			flex-wrap: wrap;
			gap: var(--space-2);
		}
		.tabs {
			min-height: 44px;
			/* Five tabs (PLAN 67): they scroll sideways in a narrow window, the page does not. */
			flex: 1 1 100%;
			min-width: 0;
			gap: 20px;
			overflow-x: auto;
			scrollbar-width: none;
		}
		.toolbar {
			flex: 1 1 100%;
			padding-bottom: var(--space-2);
			flex-wrap: wrap;
		}
		.toolbar::before {
			display: none;
		}
	}
	/* On a phone the list folds away behind the name of the chosen VM. */
	@media (max-width: 760px) {
		aside {
			flex: 1 1 100%;
		}
		.aside-head h2 {
			display: none;
		}
		.picker {
			display: inline-flex;
			flex: 1;
			justify-content: space-between;
			min-width: 0;
		}
		.picker span {
			overflow: hidden;
			text-overflow: ellipsis;
			white-space: nowrap;
		}
		aside:not(.open) .list-body {
			display: none;
		}
		aside:not(.open) .aside-head {
			border-bottom: 0;
		}
		.vm-actions .icon {
			width: 2.75rem;
			height: 2.75rem;
		}
	}
	/* PLAN 64: the state at the left of the window's tool bar, buttons right. */
	.spacer {
		flex: 1;
	}
	.phone-state {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2);
	}
</style>
