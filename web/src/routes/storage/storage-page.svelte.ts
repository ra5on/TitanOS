// Storage: the page's state and actions – volumes and disks with search,
// filter and sorting, the chosen volume or disk, what lives on a volume,
// the hint about checks, the dialogs. The page and its parts (StorageTables,
// StorageDetail) get this one object.
import { onMount, untrack } from 'svelte';
import { page } from '$app/state';
import { beforeNavigate } from '$app/navigation';
import { storageNavigation } from '#lib/storage-view.ts';
import {
	api,
	ApiError,
	type Disk,
	type DockerStatus,
	type Share,
	type StorageStatus,
	type Volume,
	type VMStatus,
} from '#lib/api.ts';
import { apiErrorText, errorText, i18n, t } from '#lib/i18n/index.svelte.ts';
import { session } from '#lib/session.svelte.ts';
import { refreshStorage, storage } from '#lib/storage.svelte.ts';
import { takesDisk } from './btrfs.ts';
import type { MaintenanceStatus } from '#lib/operations.ts';
import { checksHintVolumes, sanitizeHints, uncheckedVolumes } from './checks-hint.ts';

// What lives on the volume shown in the details: shares, VMs and Docker.
// Read when a volume is opened, from the lists the other pages use.
export type Usage = { shares: string[]; vms: string[]; docker: string[] | null; failed: boolean };

export class StoragePage {
	canAdminister = $derived(
		session.data?.user?.role === 'owner' || session.data?.user?.role === 'admin',
	);
	disks = $derived(storage.status?.disks ?? []);
	volumes = $derived(storage.status?.volumes ?? []);
	free = $derived(this.disks.filter((disk) => disk.role === 'free' && !disk.testing));

	dialog = $state<
		| { kind: 'create' }
		| { kind: 'add'; volume: string; disk?: Disk }
		| { kind: 'delete'; volume: string }
		| { kind: 'remove'; volume: string; disk: string }
		| { kind: 'profile'; volume: string }
		| null
	>(null);
	busy = $state(false);
	error = $state<{ text: string; detail: string } | null>(null);
	diskSleep = $derived(storage.status?.diskSleep);

	navigation = storageNavigation(session.data?.user?.name ?? '');
	tab = $state(this.navigation.tab);
	query = $state(this.navigation.query);
	filter = $state(this.navigation.filter);
	volumeFilter = $state(this.navigation.volume);
	sort = $state(this.navigation.sort);
	pendingDisk = $state('');
	selected = $state(this.navigation.selected);
	selectedDisk = $state(this.navigation.disk);
	sleepOpen = $state(false);
	sleepFocus = $state<{ kind: 'volume' | 'disk'; key: string } | undefined>();
	detail = $derived(this.volumes.find((v) => v.name === this.selected));
	diskDetail = $derived(
		this.disks.find((d) => (d.identity || d.serial || d.name) === this.selectedDisk),
	);
	visibleVolumes = $derived(
		this.volumes
			.filter(
				(v) =>
					`${v.name} ${v.level} ${v.filesystem}`
						.toLocaleLowerCase()
						.includes(this.query.toLocaleLowerCase()) &&
					(this.filter !== 'problems' || !['ok', 'syncing'].includes(v.state)),
			)
			.toSorted((a, b) =>
				this.sort === 'size'
					? b.sizeBytes - a.sizeBytes
					: a.name.localeCompare(b.name, i18n.locale),
			),
	);
	visibleDisks = $derived(
		this.disks
			.filter(
				(d) =>
					`${d.name} ${d.model} ${d.serial}`
						.toLocaleLowerCase()
						.includes(this.query.toLocaleLowerCase()) &&
					(!this.volumeFilter || d.volume === this.volumeFilter) &&
					(this.filter === 'all' ||
						(this.filter === 'free' && d.role === 'free') ||
						(this.filter === 'assigned' && !!d.volume) ||
						(this.filter === 'problems' &&
							(d.memberState === 'faulty' || d.smart?.healthy === false)) ||
						d.kind === this.filter),
			)
			.toSorted((a, b) =>
				this.sort === 'size'
					? b.sizeBytes - a.sizeBytes
					: this.sort === 'temperature'
						? (b.smart?.temperatureC ?? -1) - (a.smart?.temperatureC ?? -1)
						: a.name.localeCompare(b.name, i18n.locale, { numeric: true }),
			),
	);

	// One-time hint for volumes without regular checks (plan 7a). Shown only
	// once storage and maintenance are both known, so it does not flash.
	maintenance = $state<MaintenanceStatus | null>(null);
	hintBusy = $state(false);
	hintError = $state('');
	loadMaintenance = async () => {
		try {
			this.maintenance = await api.get<MaintenanceStatus>('/maintenance');
		} catch {
			// Without the plan the hint stays away; the maintenance page reports problems.
		}
	};
	checksHint = $derived(
		this.canAdminister && storage.status && this.maintenance
			? checksHintVolumes(
					uncheckedVolumes(this.volumes, this.disks, this.maintenance.settings),
					sanitizeHints(session.hints),
				)
			: [],
	);
	dismissChecksHint = async () => {
		this.hintBusy = true;
		this.hintError = '';
		const hints = sanitizeHints(session.hints);
		const value = {
			...hints,
			checksDismissed: [...new Set([...(hints.checksDismissed ?? []), ...this.checksHint])],
		};
		try {
			await api.put('/preferences/hints', value);
			session.hints = value;
		} catch (err) {
			this.hintError = apiErrorText(err);
		}
		this.hintBusy = false;
	};

	// The shared poller stays quiet about failures; the first load here shows
	// them, so an empty list never stands for "no volumes".
	statusError = $state('');
	statusLoading = $state(false);
	loadStatus = async () => {
		this.statusLoading = true;
		this.statusError = '';
		try {
			storage.status = await api.get<StorageStatus>('/storage');
		} catch (err) {
			this.statusError = apiErrorText(err);
		}
		this.statusLoading = false;
	};

	usage = $state<{ volume: string; data: Usage | null }>({ volume: '', data: null });
	loadUsage = async (volume: string) => {
		this.usage = { volume, data: null };
		const [shares, vms, docker] = await Promise.allSettled([
			api.get<Share[]>('/shares'),
			api.get<VMStatus>('/vms'),
			api.get<DockerStatus>('/docker'),
		]);
		if (this.usage.volume !== volume) return;
		// Answers of an unexpected shape count as failed reads, not as "nothing".
		const shareList =
			shares.status === 'fulfilled' && Array.isArray(shares.value) ? shares.value : null;
		const vmList =
			vms.status === 'fulfilled' && Array.isArray(vms.value?.vms) ? vms.value.vms : null;
		const engine = docker.status === 'fulfilled' ? docker.value : null;
		this.usage = {
			volume,
			data: {
				shares: (shareList ?? [])
					.filter((share) => share.volume === volume)
					.map((share) => share.name),
				vms: (vmList ?? [])
					.filter(
						(vm) => vm.volume === volume || (vm.disks ?? []).some((disk) => disk.volume === volume),
					)
					.map((vm) => vm.displayName || vm.name),
				docker:
					engine?.settings?.volume === volume && Array.isArray(engine.apps) ? engine.apps : null,
				failed: !shareList || !vmList || !engine,
			},
		};
	};
	// A string, so the 4-second storage poll does not read the lists again.
	detailName = $derived(this.detail?.name ?? '');
	usageFor = (volume: string) => (this.usage.volume === volume ? this.usage.data : null);

	switchTab = (value: 'volumes' | 'disks') => {
		this.tab = value;
		this.filter = 'all';
	};

	// Dialogs work on the live volume, so that they follow its state.
	dialogVolume = $derived.by(() => {
		const current = this.dialog;
		if (!current || !('volume' in current)) return undefined;
		return this.volumes.find((volume) => volume.name === current.volume);
	});

	summary = $derived.by(() => {
		const parts = [
			this.volumes.length === 1
				? t('storage.summaryVolume')
				: t('storage.summaryVolumes', { count: this.volumes.length }),
			this.disks.length === 1
				? t('storage.summaryDisk')
				: t('storage.summaryDisks', { count: this.disks.length }),
		];
		if (this.free.length > 0) parts.push(t('storage.summaryFree', { count: this.free.length }));
		return parts.join(' · ');
	});

	hours = $derived(new Intl.NumberFormat(i18n.locale));
	// Volumes that take another disk; the same rule as the button in VolumeCard.
	addTargets = $derived(this.volumes.filter(takesDisk));

	/** Runs an action that needs no dialog and shows what went wrong. */
	run = async (action: () => Promise<unknown>) => {
		this.busy = true;
		this.error = null;
		try {
			await action();
		} catch (err) {
			const code = err instanceof ApiError ? err.code : 'internal';
			this.error = {
				text: errorText(code),
				detail: code === 'storage_failed' && err instanceof ApiError ? err.message : '',
			};
		}
		await refreshStorage();
		this.busy = false;
	};

	rescan = () =>
		this.run(async () => (storage.status = await api.post<StorageStatus>('/storage/rescan')));
	check = (volume: Volume, start: boolean) =>
		this.run(() =>
			api.post(`/storage/volumes/${volume.name}/check`, { action: start ? 'start' : 'stop' }),
		);
	// The dialog stays open and locked while the disk is removed and erased.
	removeDisk = async () => {
		if (this.dialog?.kind !== 'remove' || this.busy) return;
		const { volume, disk } = this.dialog;
		await this.run(() => api.delete(`/storage/volumes/${volume}/disks/${disk}`));
		if (!this.error && this.dialog?.kind === 'remove' && this.dialog.disk === disk)
			this.dialog = null;
	};

	done = () => {
		this.dialog = null;
		refreshStorage();
	};

	constructor() {
		beforeNavigate(() =>
			Object.assign(this.navigation, {
				tab: this.tab,
				query: this.query,
				filter: this.filter,
				volume: this.volumeFilter,
				sort: this.sort,
				selected: this.selected,
				disk: this.selectedDisk,
				scroll: window.scrollY,
			}),
		);
		$effect(() => {
			const url = page.url;
			untrack(() => {
				const requested = url.searchParams.get('tab');
				const volume = url.searchParams.get('volume');
				let hash = '';
				try {
					hash = decodeURIComponent(url.hash.slice(1));
				} catch {
					return;
				}
				if (requested === 'disks' || requested === 'volumes') {
					this.tab = requested;
					this.query = '';
					this.filter = 'all';
				}
				if (volume !== null) {
					this.volumeFilter = volume;
					this.query = '';
					this.filter = 'all';
					this.selectedDisk = '';
				}
				if (hash.startsWith('volume-') && requested !== 'disks') {
					this.tab = 'volumes';
					this.selected = hash.slice(7);
					this.query = '';
					this.filter = 'all';
				}
				if (hash.startsWith('disk-')) {
					this.tab = 'disks';
					this.pendingDisk = hash.slice(5);
					this.query = '';
					this.filter = 'all';
					this.volumeFilter = '';
				}
			});
		});
		onMount(() => {
			if (!location.hash && !location.search)
				requestAnimationFrame(() => window.scrollTo({ top: this.navigation.scroll }));
			if (!storage.status) void this.loadStatus();
			if (this.canAdminister) void this.loadMaintenance();
		});

		$effect(() => {
			const name = this.detailName;
			if (this.canAdminister && name && this.tab === 'volumes') untrack(() => this.loadUsage(name));
		});

		$effect(() => {
			if (!this.pendingDisk) return;
			const disk = this.disks.find((d) => d.name === this.pendingDisk);
			if (disk) {
				this.selectedDisk = disk.identity || disk.serial || disk.name;
				this.pendingDisk = '';
			}
		});
	}
}
