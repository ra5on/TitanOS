<script lang="ts">
	import CloneDialog from './CloneDialog.svelte';
	let cloning = $state<VM | null>(null);
	// VMs: the list on the left, the chosen VM on the right with the tabs
	// Overview, Screen and Snapshots.
	import ContextMenu from '#lib/components/ContextMenu.svelte';
	import Dialog from '#lib/components/Dialog.svelte';
	import Dropdown from '#lib/components/Dropdown.svelte';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import TasksPanel from '#lib/components/TasksPanel.svelte';
	import { api, ApiError, type VM, type VMStatus } from '#lib/api.ts';
	import { errorText, i18n, t } from '#lib/i18n/index.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import { storage } from '#lib/storage.svelte.ts';
	import { addVmConsole } from '#lib/terminal.svelte.ts';
	import { vmLabel } from '#lib/vm-name.ts';
	import { fitHeight, VNC_HEIGHT, vncHeight, vncWidth } from '#lib/vnc-size.ts';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import Overview from './Overview.svelte';
	import Snapshots from './Snapshots.svelte';
	import VncScreen, { type ScaleMode, type ConnectionPhase } from './VncScreen.svelte';

	type Tab = 'overview' | 'screen' | 'snapshots';
	type Filter = 'all' | 'running' | 'stopped';
	type Problem = { text: string; detail: string };

	let status = $state<VMStatus | null>(null);
	let error = $state<Problem | null>(null);
	let busy = $state(false);
	let pending = $state<{ name: string; action: string } | null>(null);
	let feedback = $state('');
	let retry = $state<{ vm: VM; action: string } | null>(null);
	let search = $state('');
	let filter = $state<Filter>('all');
	// On a phone the list folds away behind the name of the chosen VM.
	let listOpen = $state(false);
	let allNotices = $state(false);
	let rowMenu = $state<{ vm: VM; x: number; y: number } | null>(null);
	// The last CPU loads of every running VM, only in this page.
	let history = $state<Record<string, number[]>>({});

	let mode = $state<ScaleMode>('scale');
	let sidebar = $state(true);
	let fullscreen = $state(false);
	let dialog = $state<
		| { kind: 'remove' | 'poweroff'; vm: string }
		| { kind: 'paste' }
		| { kind: 'xml'; text: string }
		| null
	>(null);
	let draft = $state({ deleteDisks: false, confirm: '', text: '' });
	let dialogError = $state<Problem | null>(null);
	let screen = $state<VncScreen>();
	let connectionPhase = $state<ConnectionPhase>('connecting');
	let frame = $state<HTMLElement>();
	let height = $state(vncHeight(session.vnc));
	let width = $state(vncWidth(session.vnc));
	let sizeError = $state('');
	// Room for the console when its toolbar sits right below the header,
	// with the line below it: then all of the picture is in view at once.
	let room = $state(0);
	const fitted = $derived(fitHeight(height, room));

	// What lies between the top of the window and the console when the page
	// is scrolled to its toolbar: the sticky header and the toolbar itself.
	function above(element: HTMLElement): number {
		const header = document.querySelector('header')?.getBoundingClientRect().height ?? 60;
		const toolbar = element.closest('.vm-page')?.querySelector<HTMLElement>('.command-bar');
		const fromToolbar = toolbar
			? element.getBoundingClientRect().top - toolbar.getBoundingClientRect().top
			: 0;
		return header + 8 + fromToolbar;
	}

	function measureRoom() {
		if (!frame || fullscreen) return;
		const below =
			frame.nextElementSibling instanceof HTMLElement
				? frame.nextElementSibling.offsetHeight + 24
				: 16;
		// Fit where the frame actually starts, keeping the heading and tabs visible.
		const top = Math.max(above(frame), frame.getBoundingClientRect().top);
		room = Math.max(1, window.innerHeight - top - below);
	}

	// Opening the screen of a VM brings all of it into view: if its bottom
	// would be cut off, the page scrolls once so that the toolbar sits below
	// the header. Scrolling by hand afterwards is left alone.
	let shownFor = '';
	$effect(() => {
		const key = tab === 'screen' && current ? current.name : '';
		if (!key || key === shownFor || !frame) return;
		shownFor = key;
		const element = frame;
		requestAnimationFrame(() => {
			const box = element.getBoundingClientRect();
			if (box.bottom <= window.innerHeight) return;
			const toolbar =
				element.closest('.vm-page')?.querySelector<HTMLElement>('.command-bar') ?? element;
			const header = document.querySelector('header')?.getBoundingClientRect().height ?? 60;
			window.scrollTo({
				top: toolbar.getBoundingClientRect().top + window.scrollY - header - 8,
				behavior: 'smooth',
			});
		});
	});

	$effect(() => {
		if (!frame) return;
		const element = frame;
		measureRoom();
		// Notices above the console move it down; the window may change too.
		const sizes = new ResizeObserver(measureRoom);
		if (element.parentElement) sizes.observe(element.parentElement);
		const workspace = element.closest('.vm-page');
		if (workspace) sizes.observe(workspace);
		window.addEventListener('resize', measureRoom);
		return () => {
			sizes.disconnect();
			window.removeEventListener('resize', measureRoom);
		};
	});

	// Width and height are dragged at the corner. They are stored when the
	// mouse lets go, not while dragging, so the stick is written once per change.
	let dragFrom: { width: number; height: number } | null = null;
	function dragStart() {
		const box = frame?.getBoundingClientRect();
		dragFrom = box ? { width: box.width, height: box.height } : null;
	}
	async function dragEnd() {
		if (!dragFrom || !frame || fullscreen) return;
		const box = frame.getBoundingClientRect();
		const moved =
			Math.abs(box.width - dragFrom.width) >= 2 || Math.abs(box.height - dragFrom.height) >= 2;
		dragFrom = null;
		if (!moved) return;
		// Dragged to the full column again: follow the column from now on.
		const column = frame.parentElement?.clientWidth ?? 0;
		const nextWidth = box.width >= column - 2 ? null : vncWidth({ width: box.width });
		// A height at the limit of the window keeps what was stored, so a
		// larger window later shows more again.
		const nextHeight =
			Math.abs(box.height - fitted) < 2 && fitted < height
				? height
				: vncHeight({ height: box.height });
		if (nextWidth === width && nextHeight === height) return;
		height = nextHeight;
		width = nextWidth;
		const value = width === null ? { height } : { height, width };
		const previous = session.vnc;
		session.vnc = value;
		sizeError = '';
		try {
			await api.put('/preferences/vnc', value);
		} catch (err) {
			session.vnc = previous;
			sizeError = errorText(err instanceof ApiError ? err.code : 'internal');
		}
	}

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
		if (asked === 'overview' || asked === 'screen' || asked === 'snapshots') return asked;
		return current?.state === 'running' ? 'screen' : 'overview';
	});
	const link = (vm: VM, next?: Tab) =>
		`/vms?vm=${encodeURIComponent(vm.name)}${next ? `&tab=${next}` : ''}`;
	const canStart = (vm: VM) => vm.available && !!status?.kvm && !vm.job;

	const lowSpace = $derived(
		(storage.status?.volumes ?? [])
			.filter(
				(volume) =>
					volume.mounted &&
					volume.sizeBytes > 0 &&
					volume.usedBytes / volume.sizeBytes >= 0.9 &&
					current?.disks.some(
						(disk) => disk.volume === volume.name || disk.path?.startsWith(`/mnt/${volume.name}/`),
					),
			)
			.map((volume) => volume.name),
	);

	function problem(err: unknown): Problem {
		const code = err instanceof ApiError ? err.code : 'internal';
		return {
			text: errorText(code),
			detail: code === 'vm_failed' && err instanceof ApiError ? err.message : '',
		};
	}

	async function load() {
		if (document.hidden) return;
		try {
			status = await api.get<VMStatus>('/vms');
			const next: Record<string, number[]> = {};
			for (const vm of status.vms)
				if (vm.state === 'running')
					next[vm.name] = [...(history[vm.name] ?? []), vm.cpuPercent].slice(-40);
			history = next;
		} catch (err) {
			if (!status) error = problem(err);
		}
	}
	$effect(() => {
		load();
		const timer = setInterval(load, 3000);
		return () => clearInterval(timer);
	});

	let feedbackTimer: ReturnType<typeof setTimeout> | undefined;
	async function act(vm: VM, action: string) {
		if (busy) return;
		busy = true;
		error = null;
		feedback = '';
		retry = null;
		pending = { name: vm.name, action };
		try {
			await api.post(`/vms/${encodeURIComponent(vm.name)}/${action}`);
			feedback = t('ui.vmAccepted', { name: vmLabel(vm) });
			clearTimeout(feedbackTimer);
			feedbackTimer = setTimeout(() => (feedback = ''), 5000);
		} catch (err) {
			error = problem(err);
			retry = { vm, action };
		} finally {
			await load();
			pending = null;
			busy = false;
		}
	}
	const pendingText = (action: string) =>
		t(action === 'start' ? 'ui.starting' : action === 'shutdown' ? 'ui.stopping' : 'ui.rebooting');

	// One line of notices above the tabs; the most serious first, the rest
	// behind "+N more".
	type Notice = {
		level: 'error' | 'warn' | 'info' | 'ok';
		text: string;
		detail?: string;
		retry?: boolean;
	};
	const notices = $derived.by<Notice[]>(() => {
		if (!current) return [];
		const list: Notice[] = [];
		if (error)
			list.push({ level: 'error', text: error.text, detail: error.detail, retry: !!retry });
		if (current.job?.state === 'failed')
			list.push({ level: 'error', text: t('vms.jobFailed'), detail: current.job.text });
		if (status && !status.kvm) list.push({ level: 'warn', text: t('vms.noKvm') });
		if (!current.available) list.push({ level: 'warn', text: t('vms.unavailable') });
		if (current.job?.state === 'running')
			list.push({ level: 'warn', text: t('vms.jobRunning', { file: current.job.text }) });
		if (lowSpace.length)
			list.push({ level: 'warn', text: t('vms.lowSpace', { volumes: lowSpace.join(', ') }) });
		if (pending)
			list.push({
				level: 'info',
				text: `${vmLabel(vms.find((vm) => vm.name === pending?.name) ?? current)}: ${pendingText(pending.action)}`,
			});
		if (feedback) list.push({ level: 'ok', text: feedback });
		return list;
	});
	const noticeClass = {
		error: 'notice-error',
		warn: 'notice-warn',
		info: 'notice-info',
		ok: 'notice-ok',
	} as const;

	function open(kind: 'remove' | 'poweroff' | 'paste', vm = current) {
		if (!vm) return;
		draft = { deleteDisks: false, confirm: '', text: '' };
		dialogError = null;
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
			error = problem(err);
		}
	}

	async function submit(event: SubmitEvent) {
		event.preventDefault();
		const vm = dialogVM;
		if (!vm || !dialog || busy) return;
		busy = true;
		dialogError = null;
		try {
			if (dialog.kind === 'remove') {
				await api.delete(`/vms/${encodeURIComponent(vm.name)}`, {
					deleteDisks: draft.deleteDisks,
					confirm: draft.confirm,
				});
				await goto('/vms', { replaceState: true });
			} else if (dialog.kind === 'poweroff') {
				await api.post(`/vms/${encodeURIComponent(vm.name)}/poweroff`);
			} else if (dialog.kind === 'paste') {
				screen?.paste(draft.text);
			}
			dialog = null;
			await load();
		} catch (err) {
			dialogError = problem(err);
		} finally {
			busy = false;
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

	// Key combinations the browser or the NAS's own desktop would keep for
	// themselves; as X keysyms with the codes of the keys.
	const CTRL: [number, string] = [0xffe3, 'ControlLeft'];
	const ALT: [number, string] = [0xffe9, 'AltLeft'];
	const KEYS: { label: string; keys: [number, string][] }[] = [
		{ label: 'Strg+Alt+Entf', keys: [CTRL, ALT, [0xffff, 'Delete']] },
		{ label: 'Strg+Alt+⌫', keys: [CTRL, ALT, [0xff08, 'Backspace']] },
		...[1, 2, 3, 7].map((n) => ({
			label: `Strg+Alt+F${n}`,
			keys: [CTRL, ALT, [0xffbd + n, `F${n}`]] as [number, string][],
		})),
		{ label: 'Alt+Tab', keys: [ALT, [0xff09, 'Tab']] },
		{ label: 'Win', keys: [[0xffeb, 'MetaLeft']] },
		{ label: 'Druck', keys: [[0xff61, 'PrintScreen']] },
	];
	const keyLabel = (label: string) =>
		i18n.locale === 'en'
			? label.replace('Strg', 'Ctrl').replace('Entf', 'Del').replace('Druck', 'Print')
			: label;

	const MODES: ScaleMode[] = ['scale', 'remote', 'full'];
	// Only the virtio graphics of the Linux template can take a resolution
	// from the browser.
	const modeHint = (entry: ScaleMode) =>
		t(
			entry === 'remote' && current?.template !== 'linux'
				? 'vms.mode.remote.unsupported'
				: `vms.mode.${entry}.hint`,
		);
	// A VM that cannot change its resolution falls back to scaling.
	$effect(() => {
		if (mode === 'remote' && current && current.template !== 'linux') mode = 'scale';
	});
	$effect(() => {
		current?.name;
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

	function rowContext(vm: VM, event: MouseEvent) {
		event.preventDefault();
		rowMenu = { vm, x: event.clientX, y: event.clientY };
	}
	function rowButton(vm: VM, event: MouseEvent) {
		const rect = (event.currentTarget as HTMLElement).getBoundingClientRect();
		rowMenu = { vm, x: rect.left, y: rect.bottom + 4 };
	}
</script>

<svelte:document onfullscreenchange={() => (fullscreen = document.fullscreenElement === frame)} />
<!-- The corner belongs to the frame itself; clicks into the picture land on noVNC's canvas. -->
<svelte:window
	onpointerdown={(event) => {
		if (event.target === frame) dragStart();
	}}
	onpointerup={dragEnd}
/>

{#if !status && error}
	<div class="notice-error" role="alert">
		{error.text}{#if error.detail}<pre>{error.detail}</pre>{/if}
	</div>
{/if}

{#if status && vms.length === 0}
	{#if !status.kvm}<p class="notice-warn" role="status">{t('vms.noKvm')}</p>{/if}
	<PageHead title={t('vms.title')}>
		{#snippet actions()}
<a class="btn" href="/vms/media">{t('media.title')}</a>
			<TasksPanel compact />
			<a class="btn btn-primary" href="/vms/new">{t('vms.create')}</a>
		{/snippet}
	</PageHead>
	<EmptyState title={t('vms.empty')} />
{:else if status && current}
	<div class="vm-page">
		<div class="head">
			<div class="title">
				<div class="name-row">
					<h1 id="vm-title">{vmLabel(current)}</h1>
					<span class="chip" class:ok={current.state === 'running'}
						>{t(`vms.state.${current.state}`)}</span
					>
					{#if current.autostart}<span class="chip">{t('vms.autostart')}</span>{/if}
				</div>
				{#if current.description}<span class="description muted">{current.description}</span>{/if}
			</div>
			<div class="buttons">
				<TasksPanel compact />
				{#if current.state === 'running'}
					<button
						type="button"
						class="btn btn-primary"
						onclick={() => act(current, 'shutdown')}
						disabled={busy}>{t('vms.shutdown')}</button
					>
				{:else}
					<button
						type="button"
						class="btn btn-primary"
						onclick={() => act(current, 'start')}
						disabled={busy || !canStart(current)}>{t('vms.start')}</button
					>
				{/if}
				<a
					class="btn icon-btn"
					href="/vms/edit?vm={encodeURIComponent(current.name)}"
					aria-label={t('vms.edit')}
					title={t('vms.edit')}><Icon name="edit" size={16} /></a
				>
				<Dropdown label={t('ui.more')} icon="more" align="right">
					{#snippet children(close)}
						{#if current.state === 'running'}
							<button
								type="button"
								role="menuitem"
								disabled={busy}
								onclick={() => {
									close();
									act(current, 'reboot');
								}}><Icon name="restart" size={16} /><span>{t('vms.reboot')}</span></button
							>
							<button
								type="button"
								role="menuitem"
								disabled={busy}
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
								disabled={busy || !!current.job}
								onclick={() => {
									close();
									cloning = current;
								}}>{t('ops.clone')}</button
							>
							<hr />
							<button
								type="button"
								role="menuitem"
								class="danger"
								disabled={busy}
								onclick={() => {
									close();
									open('remove');
								}}><Icon name="trash" size={16} /><span>{t('vms.remove')}</span></button
							>
						{/if}
					{/snippet}
				</Dropdown>
			</div>
		</div>

		{#if notices.length}
			<div class="notices">
				{#each allNotices ? notices : notices.slice(0, 1) as notice, index (index)}
					<div
						class={noticeClass[notice.level]}
						role={notice.level === 'error' ? 'alert' : 'status'}
					>
						<div class="notice-line">
							<span class="notice-text">{notice.text}</span>
							{#if notice.retry && retry}<button
									type="button"
									class="btn small"
									disabled={busy}
									onclick={() => retry && act(retry.vm, retry.action)}>{t('ui.retry')}</button
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
				{#each ['overview', 'screen', 'snapshots'] as const as entry (entry)}
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
										}}><span>{keyLabel(entry.label)}</span></button
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
						<Dropdown label={t('vms.display')} text={t(`vms.mode.${mode}`)} icon="monitor" compact>
							{#snippet children(close)}
								{#each MODES as entry (entry)}
									<button
										type="button"
										role="menuitemradio"
										aria-checked={mode === entry}
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
						<button
							type="button"
							class="tool icon-tool"
							aria-pressed={sidebar}
							aria-label={t('vms.sidebar')}
							title={t('vms.sidebar')}
							onclick={() => (sidebar = !sidebar)}><Icon name="sidebar" size={16} /></button
						>
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
							onclick={() => frame?.requestFullscreen()}><Icon name="maximize" size={16} /></button
						>
					{:else}<span class="grow"></span>
						<button
							type="button"
							class="tool icon-tool"
							aria-pressed={sidebar}
							aria-label={t('vms.sidebar')}
							title={t('vms.sidebar')}
							onclick={() => (sidebar = !sidebar)}><Icon name="sidebar" size={16} /></button
						>
					{/if}
				</div>
			{/if}
		</div>
		<div class="layout">
			{#if sidebar || tab !== 'screen'}
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
						{#if shown.length === 0}<p class="vm-empty muted">{t('ui.vmNoResults')}</p>{/if}
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
									<span class="vm-state">
										<span class="dot" class:ok={vm.state === 'running'} aria-hidden="true"
										></span>{t(`vms.state.${vm.state}`)}
										{#if vm.autostart}<span class="autostart" title={t('vms.autostart')}
												><Icon name="power" size={12} /><span class="sr-only"
													>{t('vms.autostart')}</span
												></span
											>{/if}
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
				{#if tab === 'overview'}
					<Overview
						vm={current}
						history={history[current.name] ?? []}
						volumes={storage.status?.volumes ?? []}
						canStart={canStart(current)}
						{busy}
						onstart={() => act(current, 'start')}
						onchanged={load}
						onsnapshots={() => goto(link(current, 'snapshots'), { replaceState: true })}
					/>
				{:else if tab === 'snapshots'}
					<Snapshots vm={current} />
				{:else}
					{#if sizeError}<p class="notice-error" role="alert">
							{t('vms.size.failed')}
							{sizeError}
						</p>{/if}
					<div
						class="frame"
						style:height={fullscreen ? null : `${fitted}px`}
						style:width={fullscreen || width === null ? null : `${width}px`}
						style:max-height={fullscreen || room <= 0
							? null
							: `${Math.max(VNC_HEIGHT.min, Math.floor(room))}px`}
						class:fullscreen
						bind:this={frame}
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
									disabled={busy || !canStart(current)}>{t('vms.startVm')}</button
								>
							</div>
						{:else if connectionPhase !== 'connected'}
							<p class="muted hint" role="status">
								{t(connectionPhase === 'lost' ? 'vms.screenLost' : 'vms.screenConnecting')}
							</p>
						{/if}
						<p class="muted hint">{t('vms.hint')}</p>
					</div>
				{/if}
			</section>
		</div>
	</div>
{/if}

{#if rowMenu}
	{@const vm = rowMenu.vm}
	<ContextMenu
		label={t('vms.actionsFor', { name: vmLabel(vm) })}
		x={rowMenu.x}
		y={rowMenu.y}
		onclose={() => (rowMenu = null)}
	>
		{#if vm.state === 'running'}
			<button
				type="button"
				role="menuitem"
				disabled={busy}
				onclick={() =>
					rowAction((vm) => {
						act(vm, 'shutdown');
					})}><Icon name="stop" size={16} /><span>{t('vms.shutdown')}</span></button
			>
			<button
				type="button"
				role="menuitem"
				disabled={busy}
				onclick={() =>
					rowAction((vm) => {
						act(vm, 'reboot');
					})}><Icon name="restart" size={16} /><span>{t('vms.reboot')}</span></button
			>
			<button
				type="button"
				role="menuitem"
				onclick={() =>
					rowAction((vm) => {
						goto(link(vm, 'screen'));
					})}><Icon name="monitor" size={16} /><span>{t('vms.tab.screen')}</span></button
			>
		{:else}
			<button
				type="button"
				role="menuitem"
				disabled={busy || !canStart(vm)}
				onclick={() =>
					rowAction((vm) => {
						act(vm, 'start');
					})}><Icon name="play" size={16} /><span>{t('vms.start')}</span></button
			>
		{/if}
		<button
			type="button"
			role="menuitem"
			onclick={() =>
				rowAction((vm) => {
					goto(`/vms/edit?vm=${encodeURIComponent(vm.name)}`);
				})}><Icon name="edit" size={16} /><span>{t('vms.edit')}</span></button
		>
		{#if vm.state === 'stopped'}<button
				type="button"
				role="menuitem"
				disabled={busy || !!vm.job}
				onclick={() =>
					rowAction((vm) => {
						cloning = vm;
					})}>{t('ops.clone')}</button
			>{/if}
		<button
			type="button"
			role="menuitem"
			onclick={() =>
				rowAction((vm) => {
					goto(link(vm, 'snapshots'));
				})}><Icon name="camera" size={16} /><span>{t('snapshots.title')}</span></button
		>
		<hr />
		{#if vm.state === 'running'}
			<button
				type="button"
				role="menuitem"
				class="danger"
				disabled={busy}
				onclick={() =>
					rowAction((vm) => {
						open('poweroff', vm);
					})}><Icon name="power" size={16} /><span>{t('vms.poweroff')}</span></button
			>
		{:else}
			<button
				type="button"
				role="menuitem"
				class="danger"
				disabled={busy}
				onclick={() =>
					rowAction((vm) => {
						open('remove', vm);
					})}><Icon name="trash" size={16} /><span>{t('vms.remove')}</span></button
			>
		{/if}
	</ContextMenu>
{/if}

{#if dialog?.kind === 'xml'}
	<Dialog title={t('vms.showXml')} wide onclose={() => (dialog = null)}>
		<pre class="xml">{dialog.text}</pre>
		<div class="dialog-actions">
			<button type="button" class="btn btn-primary" onclick={() => (dialog = null)}
				>{t('common.done')}</button
			>
		</div>
	</Dialog>
{:else if dialog && dialogVM}
	{@const kind = dialog.kind}
	{@const vm = dialogVM}
	<Dialog title={t(`vms.dialog.${kind}`, { name: vmLabel(vm) })} onclose={() => (dialog = null)}>
		<form onsubmit={submit}>
			{#if kind === 'remove'}
				<p class="text">{t('vms.removeText')}</p>
				<label class="check"
					><input type="checkbox" bind:checked={draft.deleteDisks} /><span
						>{t('vms.deleteDisks')}</span
					></label
				>
				{#if draft.deleteDisks}
					<p class="notice-error">
						{t('vms.deleteDisksWarning', {
							disks:
								vm.disks
									.filter((disk) => !disk.path)
									.map((disk) => disk.file)
									.join(', ') || '–',
						})}
					</p>
					<label
						><span>{t('vms.confirmName', { name: vmLabel(vm) })}</span><input
							class="field mono"
							type="text"
							bind:value={draft.confirm}
							autocapitalize="none"
							spellcheck="false"
						/></label
					>
				{/if}
			{:else if kind === 'poweroff'}
				<p class="text">{t('vms.poweroffText')}</p>
			{:else if kind === 'paste'}
				<p class="text">{t('vms.pasteText')}</p>
				<!-- svelte-ignore a11y_autofocus -->
				<textarea class="field mono" rows="5" bind:value={draft.text} spellcheck="false" autofocus
				></textarea>
			{/if}
			{#if dialogError}
				<div class="notice-error" role="alert">
					{dialogError.text}{#if dialogError.detail}<pre>{dialogError.detail}</pre>{/if}
				</div>
			{/if}
			<div class="dialog-actions">
				<button type="button" class="btn" onclick={() => (dialog = null)}
					>{t('common.cancel')}</button
				>
				<button
					type="submit"
					class="btn {kind === 'paste' ? 'btn-primary' : 'btn-warn'}"
					disabled={busy ||
						(kind === 'remove' && draft.deleteDisks && draft.confirm !== vmLabel(vm))}
					>{t(`vms.submit.${kind}`)}</button
				>
			</div>
		</form>
	</Dialog>
{/if}

{#if cloning}<CloneDialog
		source={cloning}
		onclose={() => (cloning = null)}
		onstarted={() => {
			cloning = null;
			load();
		}}
	/>{/if}

<style>
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
		gap: 24px;
		min-height: 48px;
		border-bottom: 1px solid var(--border);
	}
	.layout {
		display: flex;
		flex-wrap: wrap;
		align-items: flex-start;
		gap: 24px;
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
		gap: 8px;
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
		margin: 12px 12px 8px;
	}
	.filters {
		display: flex;
		gap: 4px;
		padding: 0 12px 10px;
	}
	.filter {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		height: 1.875rem;
		padding: 0 10px;
		background: transparent;
		border: 1px solid var(--border);
		border-radius: 999px;
		color: var(--muted);
		font: inherit;
		font-size: 0.75rem;
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
		padding: 4px 14px 12px;
		font-size: 0.8125rem;
	}
	.vm {
		display: flex;
		align-items: center;
		gap: 4px;
		padding-right: 8px;
		border-left: 3px solid transparent;
		min-height: 74px;
	}
	.vm:hover {
		background: var(--chip);
	}
	.vm.selected {
		border-left-color: var(--accent);
		background: var(--chip);
	}
	.vm-link {
		display: flex;
		flex: 1;
		flex-direction: column;
		gap: 8px;
		min-width: 0;
		padding: 14px 6px 14px 14px;
		color: var(--text);
		text-decoration: none;
	}
	.vm-name {
		overflow: hidden;
		font-size: 0.875rem;
		font-weight: 500;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.vm-state {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 4px 6px;
		font-size: 0.75rem;
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
		gap: 12px;
	}
	.head {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		min-height: 58px;
		justify-content: space-between;
		gap: 12px;
	}
	h1 {
		font-size: var(--text-page);
		font-weight: 600;
	}
	.title {
		display: flex;
		flex-direction: column;
		gap: 4px;
		min-width: 0;
	}
	.name-row {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 10px;
	}
	.description {
		font-size: 0.875rem;
	}
	.buttons {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 8px;
	}
	.icon-btn {
		width: 2.375rem;
		padding: 0;
		justify-content: center;
	}
	.notices {
		display: flex;
		flex-direction: column;
		gap: 8px;
	}
	.notice-line {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 6px 12px;
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
		font-size: 0.8125rem;
		text-decoration: underline;
		cursor: pointer;
	}
	.btn.small {
		height: 2rem;
		padding: 0 10px;
		font-size: 0.8125rem;
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
		font-size: 0.875rem;
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
		gap: 6px;
		font-size: 0.8125rem;
	}
	.toolbar::before {
		content: '';
		height: 22px;
		width: 1px;
		background: var(--border);
		margin-right: 10px;
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
		font-size: 0.75rem;
	}
	.screen-footer {
		display: flex;
		flex-direction: column;
		gap: 8px;
	}
	.screen-state {
		display: flex;
		align-items: center;
		flex-wrap: wrap;
		gap: 12px;
	}
	form {
		display: flex;
		flex-direction: column;
		gap: 14px;
	}
	label {
		display: flex;
		flex-direction: column;
		gap: 6px;
		font-size: 0.8125rem;
	}
	label > span {
		color: var(--muted);
	}
	.check {
		flex-direction: row;
		align-items: center;
		gap: 10px;
		font-size: 0.875rem;
	}
	.check span {
		color: var(--text);
	}
	.check input {
		width: 18px;
		height: 18px;
		margin: 0;
	}
	.text {
		font-size: 0.875rem;
	}
	textarea {
		height: auto;
		padding: 10px 12px;
		font-size: 0.8125rem;
	}
	pre {
		margin: 8px 0 0;
		font-family: var(--font-mono);
		font-size: 0.75rem;
		white-space: pre-wrap;
		word-break: break-word;
	}
	.xml {
		max-height: 60vh;
		margin: 0;
		padding: 12px;
		overflow: auto;
		background: var(--bg);
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
	}
	@media (max-width: 1100px) {
		.command-bar {
			flex-wrap: wrap;
			gap: 8px;
		}
		.tabs {
			min-height: 44px;
		}
		.toolbar {
			flex: 1 1 100%;
			padding-bottom: 8px;
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
</style>
