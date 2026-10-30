// USB drives of the file manager and the USB backups written to them.
// A drive that holds a backup is locked against changes until the user
// unlocks it, so a stray move does not damage the backup.

import { untrack } from 'svelte';
import { api, ApiError, type FileEntry, type FileRoot, type UsbDevice } from '#lib/api.ts';
import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
import { poll } from '#lib/poll.ts';
import { backupFolder, type BackupState, type USBBackup } from './usb-backup.ts';

/** What the store needs from the page. */
export type UsbHost = {
	admin: () => boolean;
	rootId: () => string;
	roots: () => FileRoot[] | null;
	loadRoots: () => Promise<void>;
	open: (root: string, path: string) => void;
	/** Opens the restore dialog for a backup folder. */
	restore: (from: FileRoot, path: string) => void;
	/** Shows an error above the page; "" clears it. */
	setError: (message: string) => void;
};

export class UsbDrives {
	devices = $state<UsbDevice[]>([]);
	busy = $state('');
	loadError = $state(false);
	backups = $state<USBBackup[]>([]);
	backupLoadError = $state(false);
	catalogReady = $state(false);
	progressError = $state(false);
	opening = $state('');
	unlockedRoot = $state('');
	#host: UsbHost;

	constructor(host: UsbHost) {
		this.#host = host;
	}

	/** Roots of drives that hold a backup. */
	readonly backupDrives = $derived(
		this.devices
			.filter((device) =>
				this.backups.some((backup) => backup.job.destination.deviceId === device.id),
			)
			.map((device) => device.root),
	);

	/** Whether a place is a backup drive (or might be one) the user has not unlocked. */
	isLocked(id: string, kind: string) {
		return (
			kind === 'usb' &&
			(!this.catalogReady || this.loadError || this.backupDrives.includes(id)) &&
			this.unlockedRoot !== id
		);
	}

	/** Whether the lock bar belongs on screen for this place. */
	mayHoldBackup(id: string) {
		return !this.catalogReady || this.loadError || this.backupDrives.includes(id);
	}

	/** The backup whose folder contains `dir` of the place `rootId`. */
	backupAt(rootId: string, dir: string) {
		return this.backups.find(
			(backup) =>
				this.devices.some(
					(device) => device.id === backup.job.destination.deviceId && device.root === rootId,
				) &&
				(dir === backupFolder(backup) || dir.startsWith(backupFolder(backup) + '/')),
		);
	}

	/** Starts asking for drives and backup progress; the result stops it. Call it from an effect. */
	watch() {
		if (!this.#host.admin()) return;
		void untrack(() => this.loadBackups());
		const progress = poll(() => this.loadBackupProgress(), { interval: 5000, immediate: false });
		const drives = poll(() => this.load(), { interval: 5000 });
		return () => {
			progress();
			drives();
		};
	}

	async loadBackupProgress() {
		try {
			const states = await api.get<Record<string, BackupState>>('/backups/progress');
			this.backups = this.backups.map((backup) => ({
				...backup,
				state: states[backup.job.id] ?? backup.state,
			}));
			this.progressError = false;
		} catch {
			this.progressError = true;
		}
	}

	async loadBackups() {
		try {
			const overview = await api.get<{ jobs: USBBackup[] }>('/backups');
			this.backups = overview.jobs.filter((job) => job.job.destination.type === 'usb-copy');
			this.backupLoadError = false;
			this.catalogReady = true;
		} catch {
			this.backupLoadError = true;
		}
	}

	async load() {
		if (!this.#host.admin()) return;
		try {
			const list = await api.get<UsbDevice[]>('/usb');
			this.loadError = false;
			const before = this.devices
				.filter((d) => d.mounted)
				.map((d) => d.id)
				.join();
			this.devices = list;
			// A drive that was mounted or taken away changes the places on the left.
			if (
				before !==
				list
					.filter((d) => d.mounted)
					.map((d) => d.id)
					.join()
			)
				this.#host.loadRoots();
		} catch {
			this.loadError = true;
			// The next look may work.
		}
	}

	async openBackup(backup: USBBackup, restore = false) {
		const host = this.#host;
		const id = backup.job.destination.deviceId;
		const drive = this.devices.find((device) => device.id === id);
		if (!drive || !drive.supported) return;
		this.opening = backup.job.id;
		host.setError('');
		try {
			let devices = this.devices;
			if (!drive.mounted)
				devices = await api.post<UsbDevice[]>(`/usb/${encodeURIComponent(drive.id)}/mount`);
			this.devices = devices;
			const mounted = devices.find((device) => device.id === id);
			if (!mounted?.root) throw new Error('unavailable');
			await host.loadRoots();
			const path = backupFolder(backup);
			// Do not create a backup directory just to browse it.
			await api.get<FileEntry[]>(
				'/files/list?' + new URLSearchParams({ root: mounted.root, path }),
			);
			if (restore) {
				const source = host.roots()?.find((place) => place.id === mounted.root);
				if (source) host.restore(source, path);
			} else host.open(mounted.root, path);
		} catch (err) {
			host.setError(
				err instanceof ApiError && err.code === 'file_not_found'
					? t('files.usbBackupMissing')
					: apiErrorText(err),
			);
		} finally {
			this.opening = '';
		}
	}

	async action(device: UsbDevice, action: 'mount' | 'eject') {
		const host = this.#host;
		this.busy = device.id;
		host.setError('');
		try {
			this.devices = await api.post<UsbDevice[]>(`/usb/${encodeURIComponent(device.id)}/${action}`);
			if (action === 'eject' && host.rootId() === device.root) host.open('', '');
			await host.loadRoots();
			if (action === 'mount') {
				const mounted = this.devices.find((d) => d.id === device.id);
				if (mounted?.root) host.open(mounted.root, '');
			}
		} catch (err) {
			host.setError(apiErrorText(err));
		}
		this.busy = '';
	}
}
