// VMs: the page's state and actions – the list with search and filter, the
// chosen VM and its tab, requests on their way and their failures, the
// screen's size and mode, the dialogs. The page and its parts (VmList,
// VmButtons, ScreenToolbar) get this one object.
import { tick } from 'svelte';
import { SvelteSet } from 'svelte/reactivity';
import { goto } from '$app/navigation';
import { page } from '$app/state';
import { api, type VM, type VMStatus } from '#lib/api.ts';
import { phoneView } from '#lib/desktop/mode.svelte.ts';
import { t } from '#lib/i18n/index.svelte.ts';
import { poll, type Poller } from '#lib/poll.ts';
import { storage } from '#lib/storage.svelte.ts';
import { vmLabel } from '#lib/vm-name.ts';
import { ConsoleSize } from './console-size.svelte.ts';
import type Overview from './Overview.svelte';
import type { VmDialogDraft, VmDialogKind } from './VmActionDialog.svelte';
import type { RowChoice } from './VmRowMenu.svelte';
import type VncScreen from './VncScreen.svelte';
import type { ScaleMode, ConnectionPhase } from './VncScreen.svelte';
import {
	lowSpaceVolumes,
	problem,
	takeStartError,
	vmNotices,
	type Failure,
	type Problem,
} from './vm-page.ts';

// PLAN 67: Übersicht · Bildschirm · Hardware · Snapshots · Zeitplan.
export type Tab = 'overview' | 'screen' | 'hardware' | 'snapshots' | 'schedule';
export const TABS: Tab[] = ['overview', 'screen', 'hardware', 'snapshots', 'schedule'];
export type Filter = 'all' | 'running' | 'stopped';

export class VmsPage {
	cloning = $state<VM | null>(null);
	status = $state<VMStatus | null>(null);
	// The first load failed: nothing to show but the error.
	error = $state<Problem | null>(null);
	// A later refresh failed: the last list stays, marked as possibly stale.
	refreshError = $state('');
	lastRefresh = $state('');
	refreshing = $state(false);
	// VMs with a request on its way; the others stay usable meanwhile.
	busyNames = new SvelteSet<string>();
	isBusy = (vm: VM | undefined) => !!vm && this.busyNames.has(vm.name);
	pending = $state<Record<string, string>>({});
	feedback = $state('');
	// The failed action of each VM, shown only with that VM.
	failures = $state<Record<string, Failure>>({});
	search = $state('');
	filter = $state<Filter>('all');
	// On a phone the list folds away behind the name of the chosen VM.
	listOpen = $state(false);
	allNotices = $state(false);
	rowMenu = $state<{ vm: VM; x: number; y: number } | null>(null);
	// The last CPU loads of every running VM, only in this page.
	history = $state<Record<string, number[]>>({});

	// Adjusting the guest's resolution is the default; guests that cannot do it scale.
	mode = $state<ScaleMode>('remote');
	sidebar = $state(true);
	dialog = $state<
		| { kind: 'remove' | 'poweroff'; vm: string }
		| { kind: 'paste' }
		| { kind: 'xml'; text: string }
		| null
	>(null);
	// "VM übernehmen": copies of VM settings on the volumes; read only on request.
	adopting = $state(false);
	screen = $state<VncScreen>();
	connectionPhase = $state<ConnectionPhase>('connecting');
	size = new ConsoleSize();
	shownFor = '';

	vms = $derived(this.status?.vms ?? []);
	counts = $derived({
		all: this.vms.length,
		running: this.vms.filter((vm) => vm.state === 'running').length,
		stopped: this.vms.filter((vm) => vm.state !== 'running').length,
	});
	shown = $derived(
		this.vms.filter(
			(vm) =>
				(this.filter === 'all' || (this.filter === 'running') === (vm.state === 'running')) &&
				`${vmLabel(vm)} ${vm.name}`.toLocaleLowerCase().includes(this.search.toLocaleLowerCase()),
		),
	);
	current = $derived(
		this.vms.find((vm) => vm.name === page.url.searchParams.get('vm')) ?? this.vms[0],
	);
	tab = $derived.by<Tab>(() => {
		const asked = page.url.searchParams.get('tab');
		if (TABS.includes(asked as Tab)) return asked as Tab;
		return this.current?.state === 'running' ? 'screen' : 'overview';
	});
	link = (vm: VM, next?: Tab) =>
		`/vms?vm=${encodeURIComponent(vm.name)}${next ? `&tab=${next}` : ''}`;
	canStart = (vm: VM) => vm.available && !!this.status?.kvm && !vm.job;

	lowSpace = $derived(lowSpaceVolumes(storage.status?.volumes ?? [], this.current));

	// Every refresh goes through the poller: runs never overlap, and one asked
	// for during a run follows it, so an older answer never wins.
	poller: Poller | undefined;
	load = () => this.poller?.now() ?? Promise.resolve();
	fetchStatus = async () => {
		this.refreshing = true;
		try {
			this.status = await api.get<VMStatus>('/vms');
			this.error = null;
			this.refreshError = '';
			this.lastRefresh = new Date().toISOString();
			const next: Record<string, number[]> = {};
			for (const vm of this.status.vms)
				if (vm.state === 'running')
					next[vm.name] = [...(this.history[vm.name] ?? []), vm.cpuPercent].slice(-40);
			this.history = next;
		} catch (err) {
			if (!this.status) this.error = problem(err);
			else this.refreshError = problem(err).text;
		} finally {
			this.refreshing = false;
		}
	};

	feedbackTimer: ReturnType<typeof setTimeout> | undefined;
	act = async (vm: VM, action: string) => {
		if (this.busyNames.has(vm.name)) return;
		this.busyNames.add(vm.name);
		delete this.failures[vm.name];
		this.feedback = '';
		this.pending[vm.name] = action;
		try {
			await api.post(`/vms/${encodeURIComponent(vm.name)}/${action}`);
			this.feedback = t('ui.vmAccepted', { name: vmLabel(vm) });
			clearTimeout(this.feedbackTimer);
			this.feedbackTimer = setTimeout(() => (this.feedback = ''), 5000);
		} catch (err) {
			this.failures[vm.name] = { problem: problem(err), action };
		} finally {
			await this.load();
			delete this.pending[vm.name];
			this.busyNames.delete(vm.name);
		}
	};
	notices = $derived(
		vmNotices({
			current: this.current,
			status: this.status,
			vms: this.vms,
			failures: this.failures,
			pending: this.pending,
			lowSpace: this.lowSpace,
			feedback: this.feedback,
		}),
	);

	open = (kind: VmDialogKind, vm = this.current) => {
		if (!vm) return;
		this.dialog = kind === 'paste' ? { kind } : { kind, vm: vm.name };
	};
	dialogVM = $derived.by(() => {
		const target = this.dialog && 'vm' in this.dialog ? this.dialog.vm : '';
		return target ? this.vms.find((vm) => vm.name === target) : this.current;
	});

	showXML = async (vm = this.current) => {
		if (!vm) return;
		try {
			this.dialog = {
				kind: 'xml',
				text: (await api.get<{ xml: string }>(`/vms/${encodeURIComponent(vm.name)}/xml`)).xml,
			};
		} catch (err) {
			this.failures[vm.name] = { problem: problem(err), action: '' };
		}
	};

	// Errors are shown by the dialog.
	submit = async (draft: VmDialogDraft) => {
		const vm = this.dialogVM;
		if (!vm || !this.dialog || this.dialog.kind === 'xml') return;
		const marks = this.dialog.kind !== 'paste';
		if (marks) this.busyNames.add(vm.name);
		try {
			if (this.dialog.kind === 'remove') {
				await api.delete(`/vms/${encodeURIComponent(vm.name)}`, {
					deleteDisks: draft.deleteDisks,
					// The API wants the VM name as confirmation for deleting disks.
					confirm: draft.deleteDisks ? vm.name : '',
				});
				await goto('/vms', { replaceState: true });
			} else if (this.dialog.kind === 'poweroff') {
				await api.post(`/vms/${encodeURIComponent(vm.name)}/poweroff`);
			} else if (this.dialog.kind === 'paste') {
				this.screen?.paste(draft.text);
			}
			this.dialog = null;
			await this.load();
		} finally {
			if (marks) this.busyNames.delete(vm.name);
		}
	};

	saveScreenshot = () => {
		const data = this.screen?.screenshot();
		if (!data || !this.current) return;
		const anchor = document.createElement('a');
		anchor.href = data;
		anchor.download = `${this.current.name}.png`;
		anchor.click();
	};

	ownWindow = (event: MouseEvent) => {
		if (!this.current) return;
		event.preventDefault();
		window.open(
			`/vms/screen?vm=${encodeURIComponent(this.current.name)}`,
			`vm-${this.current.name}`,
			'popup,width=1296,height=860',
		);
	};

	// Only the virtio graphics of the Linux template can take a resolution
	// from the browser.
	modeHint = (entry: ScaleMode) =>
		t(
			entry === 'remote' && this.current?.template !== 'linux'
				? 'vms.mode.remote.unsupported'
				: `vms.mode.${entry}.hint`,
		);
	// A VM that cannot change its resolution shows scaling; the choice stays
	// for the next VM that can.
	shownMode = $derived<ScaleMode>(
		this.mode === 'remote' && this.current && this.current.template !== 'linux'
			? 'scale'
			: this.mode,
	);
	// Only a different VM closes the list; a refreshed copy of the same one
	// (a new object after every poll) must not.
	currentName = $derived(this.current?.name);

	// Snapshot before closing: the template's reactive @const becomes undefined
	// immediately when rowMenu is cleared.
	rowAction = (action: (vm: VM) => void) => {
		const vm = this.rowMenu?.vm;
		this.rowMenu = null;
		if (vm) action(vm);
	};

	rowChoice = (vm: VM, choice: RowChoice) => {
		if (choice === 'shutdown' || choice === 'reboot' || choice === 'start') this.act(vm, choice);
		else if (choice === 'screen' || choice === 'snapshots') goto(this.link(vm, choice));
		else if (choice === 'edit') goto(`/vms/edit?vm=${encodeURIComponent(vm.name)}`);
		else if (choice === 'clone') this.cloning = vm;
		else this.open(choice, vm);
	};

	rowContext = (vm: VM, event: MouseEvent) => {
		event.preventDefault();
		this.rowMenu = { vm, x: event.clientX, y: event.clientY };
	};
	rowButton = (vm: VM, event: MouseEvent) => {
		const rect = (event.currentTarget as HTMLElement).getBoundingClientRect();
		this.rowMenu = { vm, x: rect.left, y: rect.bottom + 4 };
	};
	// PLAN 64: the phone's full-screen view keeps only the buttons in the bar.
	phone = $derived(phoneView());

	overview = $state<ReturnType<typeof Overview>>();
	/** The CD drive lives in the hardware part: shown first if another tab is open. */
	withOverview = async (act: (o: NonNullable<VmsPage['overview']>) => void) => {
		if (!this.current) return;
		if (this.tab !== 'hardware' && this.tab !== 'overview') {
			await goto(this.link(this.current, 'hardware'));
			await tick();
		}
		if (this.overview) act(this.overview);
	};

	constructor() {
		// A VM that was just created but did not start says why (VmForm.svelte).
		const notStarted = takeStartError();
		if (notStarted)
			this.failures[notStarted.name] = { problem: notStarted.problem, action: 'start' };
		// In a desktop window the window's side bar lists the VMs (PLAN 62-3); "?adopt=1" opens the take-over.
		$effect(() => {
			if (page.url.searchParams.get('adopt') === '1') this.adopting = true;
		});
		// Opening the screen of a VM brings all of it into view: if its bottom
		// would be cut off, the page scrolls once so that the toolbar sits below
		// the header. Scrolling by hand afterwards is left alone.
		$effect(() => {
			const key = this.tab === 'screen' && this.current ? this.current.name : '';
			if (!key || key === this.shownFor || !this.size.frame) return;
			this.shownFor = key;
			this.size.bringIntoView();
		});

		$effect(() => this.size.observe());
		$effect(() => {
			const current = (this.poller = poll(this.fetchStatus, { interval: 3000 }));
			return () => {
				current();
				if (this.poller === current) this.poller = undefined;
			};
		});
		$effect(() => {
			void this.currentName;
			this.listOpen = false;
			this.allNotices = false;
		});
	}
}
