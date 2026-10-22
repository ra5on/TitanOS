<script lang="ts">
	import { untrack, tick, onMount } from 'svelte';
	import { api, ApiError, type CopyJob, type FileEntry, type FileRoot, type UploadStatus, type UsbDevice } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import { filePreferences, folderPath, rangeSelection, previewKind, selectionType, type FolderView, type Favorite, type FileView } from '#lib/file-actions.ts';
	import { beginFileDrag, endFileDrag, hasFileDrag, takeFileDrag, type FileDrag } from '#lib/file-drag.ts';
	import { fileList, type FileSort } from '#lib/file-list.ts';
	import { preferredFileRoot } from '#lib/file-root.ts';
	import { formatBytes } from '#lib/format.ts';
	import { errorText, i18n, t } from '#lib/i18n/index.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import { isEditableText } from '#lib/text-files.ts';
	import { enqueue, recover, uploads } from '#lib/uploads.svelte.ts';
	import ArchiveDialog from './ArchiveDialog.svelte';
	import CompareDialog from './CompareDialog.svelte';
	import ContextMenu from '#lib/components/ContextMenu.svelte';
	import CopyDialog from './CopyDialog.svelte';
	import CreateFileDialog from './CreateFileDialog.svelte';
	import DownloadDialog from './DownloadDialog.svelte';
	import Dropdown from '#lib/components/Dropdown.svelte';
	import DuplicatesDialog from './DuplicatesDialog.svelte';
	import FavoritesDialog from './FavoritesDialog.svelte';
	import FileIcon from './FileIcon.svelte';
	import FilePreview from './FilePreview.svelte';
	import FileProperties from './FileProperties.svelte';
	import FileThumbnail from './FileThumbnail.svelte';
	import FolderPane from './FolderPane.svelte';
	import JobsPanel from './JobsPanel.svelte';
	import Places from './Places.svelte';
	import RenameBatchDialog from './RenameBatchDialog.svelte';
	import SearchDialog from './SearchDialog.svelte';
	import TextEditor from './TextEditor.svelte';
	import TrashView from './TrashView.svelte';
	import UndoPanel from './UndoPanel.svelte';
	import UploadPanel from './UploadPanel.svelte';
	import UploadReview from './UploadReview.svelte';
	import UsageDialog from './UsageDialog.svelte';
	import VirtualEntries from './VirtualEntries.svelte';
	import WindowsPath from './WindowsPath.svelte';
	import { backupFolder, type BackupState, type USBBackup } from './usb-backup.ts';
	import { askAbout, assistant, readAttachment } from '#lib/assistant.svelte.ts';

	const canAdminister = $derived(session.data?.user?.role === 'owner' || session.data?.user?.role === 'admin');
	const join = (...parts: string[]) => parts.filter(Boolean).join('/');
	const fail = (err: unknown) => errorText(err instanceof ApiError ? err.code : 'internal');

	// --- Where we are -------------------------------------------------------

	let roots = $state<FileRoot[] | null>(null);
	let rootId = $state('');
	let dir = $state(''); // folder inside the root, "" for its top
	let entries = $state<FileEntry[] | null>(null);
	let selected = $state<string[]>([]);
	let anchor = $state('');
	let error = $state('');
	let trash = $state(false);
	/** On narrow screens the list of places is folded away behind a button. */
	let placesOpen = $state(false);

	const root = $derived(roots?.find((candidate) => candidate.id === rootId));
	const crumbs = $derived(dir ? dir.split('/') : []);
	const date = $derived(new Intl.DateTimeFormat(i18n.locale, { dateStyle: 'medium', timeStyle: 'short' }));

	// --- Dialogs and panels -------------------------------------------------

	let copying = $state<{ move: boolean; restore?: boolean; from?: FileRoot; paths?: string[]; targetRoot?: string; targetDir?: string; pasted?: boolean } | null>(null);
	let editing = $state<string | null>(null);
	let jobsTick = $state(0);
	let windowsPath = $state(false);
	let favoriteManager = $state(false), creatingFile = $state(false), shortcuts = $state(false);
	let selectionFilter = $state('images');
	let downloadPaths = $state<string[] | null>(null), usage = $state(false), duplicates = $state(false);
	let deepSearch = $state(false), comparison = $state(false), batchRename = $state(false), archive = $state<string | null>(null);
	let properties = $state<{ root: FileRoot; path: string } | null>(null);
	let preview = $state<FileEntry | null>(null), otherPreview = $state<{ root: string; path: string; size: number } | null>(null);
	let uploadReview = $state<{ root: FileRoot; dir: string; files: { file: File; path: string }[] } | null>(null);
	let dropReview = $state<{ source: FileDrag; target: string; path: string } | null>(null);
	let dialog = $state<{ kind: 'mkdir' } | { kind: 'rename'; name: string } | { kind: 'delete' } | null>(null);
	let typed = $state('');
	let confirmed = $state(false);
	let permanent = $state(false);
	let dialogError = $state('');
	let unlockDialog = $state(false);
	/** The floating menus: one for entries, one for the empty part of the folder. */
	let menu = $state<{ entry: FileEntry; x: number; y: number } | null>(null);
	let backgroundMenu = $state<{ x: number; y: number } | null>(null);

	let fileInput = $state<HTMLInputElement>();
	let folderInput = $state<HTMLInputElement>();
	let dragging = $state(false);

	// --- Two panes ----------------------------------------------------------

	let split = $state(false);
	let right = $state<{ root: string; path: string; selected: string[] }>({ root: '', path: '', selected: [] });

	function reviewDrop(event: DragEvent, target: string, path: string) {
		if (!hasFileDrag(event)) return;
		event.preventDefault();
		event.stopPropagation();
		dragging = false;
		const source = takeFileDrag(event), destination = safeRoots.find((r) => r.id === target), origin = safeRoots.find((r) => r.id === source?.root);
		if (!source || !origin || origin.kind === 'stick' || !destination?.writable) return;
		if (source.root === target && source.paths.some((p) => path === p || path.startsWith(p + '/'))) return;
		dropReview = { source, target, path };
	}
	function acceptDrop(move: boolean) {
		const review = dropReview;
		if (!review) return;
		const from = safeRoots.find((r) => r.id === review.source.root), target = safeRoots.find((r) => r.id === review.target);
		if (!from || !target?.writable || (move && !from.writable)) return;
		copying = { from, paths: review.source.paths, move, targetRoot: target.id, targetDir: review.path };
		dropReview = null;
	}
	function dragEntry(event: DragEvent, entry: FileEntry) {
		if (root?.kind === 'stick' || trash) {
			event.preventDefault();
			return;
		}
		const names = selectedNames.has(entry.name) ? selected : [entry.name];
		selected = names;
		beginFileDrag(event, { root: rootId, paths: names.map((name) => join(dir, name)) });
	}
	function transferPane(direction: 'right' | 'left', move: boolean) {
		const source = direction === 'right' ? safeRoots.find((r) => r.id === rootId) : rightRoot;
		const target = direction === 'right' ? rightRoot : safeRoots.find((r) => r.id === rootId);
		const names = direction === 'right' ? selected : right.selected;
		const sourceDir = direction === 'right' ? dir : right.path, targetDir = direction === 'right' ? right.path : dir;
		if (!source || !target?.writable || !names.length || (move && !source.writable)) return;
		copying = { from: source, paths: names.map((n) => join(sourceDir, n)), move, targetRoot: target.id, targetDir };
	}
	function canRetry(job: CopyJob) {
		return !!safeRoots.find((r) => r.id === job.to)?.writable && (!job.move || !!safeRoots.find((r) => r.id === job.from)?.writable);
	}

	// --- Unfinished uploads ---------------------------------------------------

	let pending = $state<UploadStatus[]>([]);
	let recoveryInput = $state<HTMLInputElement>();
	let recovery = $state<{ status: UploadStatus; root: FileRoot; dir: string } | null>(null);
	let recovering = $state(false);
	const unfinished = $derived(pending.filter((part) => !uploads.items.some((item) => item.id === part.id)));
	async function recoverPicked(input: HTMLInputElement) {
		const file = input.files?.[0], chosen = recovery;
		input.value = '';
		if (!file || !chosen) return;
		recovering = true;
		try {
			await recover(file, chosen.root, chosen.dir, chosen.status);
			await load();
		} catch (err) {
			error = fail(err);
		} finally {
			recovering = false;
			recovery = null;
		}
	}
	async function removePending(part: UploadStatus) {
		try {
			await api.delete(`/files/uploads/${part.id}`);
			await load();
		} catch (err) {
			error = fail(err);
		}
	}

	// --- Preferences of the account -----------------------------------------

	type Location = { root: string; path: string; search: string; selected: string[]; scroll: number; listScroll: number };
	let virtualList = $state<{ getScroll: () => number; setScroll: (top: number) => void; reveal: (index: number) => void; getColumns: () => number }>();
	let back = $state<Location[]>([]), forward = $state<Location[]>([]);
	let favorites = $state<Favorite[]>([]);
	let folderViews = $state<FolderView[]>([]);
	let defaults = $state<{ view: FileView; sort: FileSort; descending: boolean }>({ view: 'list', sort: 'name', descending: false });
	let preferencesReady = $state(false);
	let rememberedRoot = $state('');
	let preferenceError = $state('');
	let savingPreferences = $state(false);
	let view = $state<FileView>('list');
	let showHidden = $state(false);
	let search = $state('');
	let sort = $state<FileSort>('name');
	let descending = $state(false);
	const views = [
		{ mode: 'list', icon: 'list', label: 'files.view.list' },
		{ mode: 'small', icon: 'gridSmall', label: 'files.view.small' },
		{ mode: 'large', icon: 'grid', label: 'files.view.large' }
	] as const;
	let saveQueue = Promise.resolve();

	onMount(() => {
		void loadPreferences();
	});
	async function loadPreferences() {
		try {
			const result = await api.get<{ files: unknown }>('/preferences');
			const prefs = filePreferences(result.files);
			split = prefs.split;
			showHidden = prefs.showHidden;
			rememberedRoot = prefs.root;
			view = prefs.view;
			sort = prefs.sort;
			descending = prefs.descending;
			favorites = prefs.favorites;
			folderViews = prefs.folders;
			defaults = { view: prefs.view, sort: prefs.sort, descending: prefs.descending };
			applyFolderView();
			preferencesReady = true;
			preferenceError = '';
		} catch {
			preferenceError = t('files.preferencesLoadFailed');
		}
	}
	function savePreferences() {
		if (!preferencesReady) return;
		if (folderViews.some((f) => f.root === rootId && f.path === dir)) folderViews = folderViews.map((f) => (f.root === rootId && f.path === dir ? { ...f, view, sort, descending } : f));
		else defaults = { view, sort, descending };
		const value = { split, showHidden, root: rememberedRoot, ...defaults, folders: $state.snapshot(folderViews), favorites: $state.snapshot(favorites) };
		savingPreferences = true;
		saveQueue = saveQueue.then(async () => {
			try {
				await api.put('/preferences/files', value);
				preferenceError = '';
			} catch {
				preferenceError = t('files.preferencesSaveFailed');
			}
		});
		const queued = saveQueue;
		void queued.then(() => {
			if (queued === saveQueue) savingPreferences = false;
		});
	}
	const folderViewSaved = $derived(folderViews.some((f) => f.root === rootId && f.path === dir));
	function applyFolderView() {
		const settings = folderViews.find((f) => f.root === rootId && f.path === dir) ?? defaults;
		view = settings.view;
		sort = settings.sort;
		descending = settings.descending;
	}
	function toggleFolderView() {
		if (folderViewSaved) {
			folderViews = folderViews.filter((f) => f.root !== rootId || f.path !== dir);
			applyFolderView();
		} else if (folderViews.length < 200) folderViews = [...folderViews, { root: rootId, path: dir, view, sort, descending }];
		savePreferences();
	}
	function changeView(next: FileView) {
		view = next;
		savePreferences();
	}
	/** A click on a column heading sorts by it; a second click turns the order around. */
	function sortBy(column: FileSort) {
		if (!preferencesReady) return;
		if (sort === column) descending = !descending;
		else {
			sort = column;
			descending = false;
		}
		savePreferences();
	}
	function sortLabel(column: FileSort) {
		const name = t(column === 'name' ? 'files.name' : column === 'size' ? 'files.size' : 'files.modified');
		return sort === column ? `${name}, ${t(descending ? 'files.sortedDescending' : 'files.sortedAscending')}` : name;
	}
	const visibleFavorites = $derived(favorites.filter((f) => roots?.some((r) => r.id === f.root)));
	const isFavorite = $derived(favorites.some((f) => f.root === rootId && f.path === dir));
	function saveFavorites(next: Favorite[]) {
		favorites = next;
		savePreferences();
	}
	function toggleFavorite() {
		if (isFavorite) saveFavorites(favorites.filter((f) => f.root !== rootId || f.path !== dir));
		else if (favorites.length < 50) saveFavorites([...favorites, { root: rootId, path: dir }]);
	}

	// --- The list -----------------------------------------------------------

	const visibleEntries = $derived(fileList((entries ?? []).filter((entry) => showHidden || !entry.name.startsWith('.')), search, sort, descending, i18n.locale));
	const gallery = $derived(visibleEntries.filter((e) => !e.dir && previewKind(e.name, e.size) === 'image'));
	const galleryIndex = $derived(preview ? gallery.findIndex((e) => e.name === preview?.name) : -1);
	const selectedNames = $derived(new Set(selected));
	const allSelected = $derived(visibleEntries.length > 0 && visibleEntries.every((entry) => selectedNames.has(entry.name)));
	const selectedEntries = $derived((entries ?? []).filter((entry) => selectedNames.has(entry.name)));
	const selectedBytes = $derived(selectedEntries.filter((entry) => !entry.dir).reduce((sum, entry) => sum + entry.size, 0));
	const selectedFolders = $derived(selectedEntries.filter((entry) => entry.dir).length);
	const selectionKey = $derived(JSON.stringify([rootId, dir, selected]));
	let measured = $state<{ key: string; bytes: number } | null>(null), measuring = $state(false);
	async function measureSelection() {
		const key = selectionKey;
		measuring = true;
		try {
			const result = await api.post<{ bytes: number }>('/files/inspect', { root: rootId, paths: selected.map((name) => join(dir, name)) });
			if (key === selectionKey) measured = { key, bytes: result.bytes };
		} catch (err) {
			if (key === selectionKey) error = fail(err);
		} finally {
			measuring = false;
		}
	}

	// --- USB drives and their backups ---------------------------------------

	let usbDevices = $state<UsbDevice[]>([]);
	let usbBusy = $state('');
	let usbLoadError = $state(false);
	let usbBackups = $state<USBBackup[]>([]);
	let backupLoadError = $state(false);
	let backupCatalogReady = $state(false);
	let backupProgressError = $state(false);
	let openingBackup = $state('');
	let unlockedRoot = $state('');
	const backupDrives = $derived(usbDevices.filter((device) => usbBackups.some((backup) => backup.job.destination.deviceId === device.id)).map((device) => device.root));
	const isLockedBackup = (id: string, kind: string) => kind === 'usb' && (!backupCatalogReady || usbLoadError || backupDrives.includes(id)) && unlockedRoot !== id;
	const backupLocked = $derived(!!root && isLockedBackup(root.id, root.kind));
	const writable = $derived(!!root?.writable && !backupLocked);
	const safeRoots = $derived((roots ?? []).map((place) => (isLockedBackup(place.id, place.kind) ? { ...place, writable: false } : place)));
	const rightRoot = $derived(safeRoots.find((r) => r.id === right.root));
	const currentUSBBackup = $derived(usbBackups.find((backup) => usbDevices.some((device) => device.id === backup.job.destination.deviceId && device.root === rootId) && (dir === backupFolder(backup) || dir.startsWith(backupFolder(backup) + '/'))));

	async function loadBackupProgress() {
		try {
			const states = await api.get<Record<string, BackupState>>('/backups/progress');
			usbBackups = usbBackups.map((backup) => ({ ...backup, state: states[backup.job.id] ?? backup.state }));
			backupProgressError = false;
		} catch {
			backupProgressError = true;
		}
	}
	$effect(() => {
		if (!canAdminister) return;
		void loadUSBBackups();
		const timer = setInterval(() => {
			if (!document.hidden) void loadBackupProgress();
		}, 5000);
		return () => clearInterval(timer);
	});
	async function loadUSBBackups() {
		try {
			const overview = await api.get<{ jobs: USBBackup[] }>('/backups');
			usbBackups = overview.jobs.filter((job) => job.job.destination.type === 'usb-copy');
			backupLoadError = false;
			backupCatalogReady = true;
		} catch {
			backupLoadError = true;
		}
	}
	async function openUSBBackup(backup: USBBackup, restore = false) {
		const id = backup.job.destination.deviceId;
		const drive = usbDevices.find((device) => device.id === id);
		if (!drive || !drive.supported) return;
		openingBackup = backup.job.id;
		error = '';
		try {
			let devices = usbDevices;
			if (!drive.mounted) devices = await api.post<UsbDevice[]>(`/usb/${encodeURIComponent(drive.id)}/mount`);
			usbDevices = devices;
			const mounted = devices.find((device) => device.id === id);
			if (!mounted?.root) throw new Error('unavailable');
			await loadRoots();
			const path = backupFolder(backup);
			// Do not create a backup directory just to browse it.
			await api.get<FileEntry[]>('/files/list?' + new URLSearchParams({ root: mounted.root, path }));
			if (restore) {
				const source = roots?.find((place) => place.id === mounted.root);
				if (source) copying = { move: false, restore: true, from: source, paths: [path] };
			} else open(mounted.root, path);
		} catch (err) {
			error = err instanceof ApiError && err.code === 'file_not_found' ? t('files.usbBackupMissing') : fail(err);
		} finally {
			openingBackup = '';
		}
	}
	async function loadUsb() {
		if (!canAdminister) return;
		try {
			const list = await api.get<UsbDevice[]>('/usb');
			usbLoadError = false;
			const before = usbDevices.filter((d) => d.mounted).map((d) => d.id).join();
			usbDevices = list;
			// A drive that was mounted or taken away changes the places on the left.
			if (before !== list.filter((d) => d.mounted).map((d) => d.id).join()) loadRoots();
		} catch {
			usbLoadError = true;
			// The next look may work.
		}
	}
	async function usbAction(device: UsbDevice, action: 'mount' | 'eject') {
		usbBusy = device.id;
		error = '';
		try {
			usbDevices = await api.post<UsbDevice[]>(`/usb/${encodeURIComponent(device.id)}/${action}`);
			if (action === 'eject' && rootId === device.root) open('', '');
			await loadRoots();
			if (action === 'mount') {
				const mounted = usbDevices.find((d) => d.id === device.id);
				if (mounted?.root) open(mounted.root, '');
			}
		} catch (err) {
			error = fail(err);
		}
		usbBusy = '';
	}
	$effect(() => {
		if (!canAdminister) return;
		loadUsb();
		const timer = setInterval(() => !document.hidden && loadUsb(), 5000);
		return () => clearInterval(timer);
	});

	const editable = $derived.by(() => {
		if (backupLocked || !(root?.editable || writable) || selected.length !== 1) return false;
		const entry = entries?.find((candidate) => candidate.name === selected[0]);
		return !!entry && !entry.dir && isEditableText(entry.name, entry.size);
	});

	// --- Loading ------------------------------------------------------------

	async function loadRoots() {
		try {
			roots = await api.get<FileRoot[]>('/files/roots');
			if (!roots.some((candidate) => candidate.id === rootId)) open(preferredFileRoot(roots, rememberedRoot), '');
		} catch (err) {
			error = fail(err);
		}
	}
	async function load() {
		if (!rootId) {
			entries = null;
			pending = [];
			return;
		}
		const requestedRoot = rootId, requestedDir = dir;
		try {
			const query = `root=${encodeURIComponent(requestedRoot)}&path=${encodeURIComponent(requestedDir)}`;
			const [listing, parts] = await Promise.all([
				api.get<FileEntry[]>(`/files/list?${query}`),
				writable ? api.get<UploadStatus[]>(`/files/uploads?${query}`) : Promise.resolve([])
			]);
			if (rootId !== requestedRoot || dir !== requestedDir) return;
			entries = listing;
			pending = parts;
			const names = new Set(entries.map((entry) => entry.name));
			selected = selected.filter((name) => names.has(name));
			error = '';
		} catch (err) {
			if (rootId !== requestedRoot || dir !== requestedDir) return;
			entries = [];
			error = fail(err);
		}
	}

	// Looks again every ten seconds, unless the user is in the middle of something.
	let refreshError = $state(false);
	onMount(() => {
		let active = true, inFlight = false;
		let controller: AbortController | undefined;
		const blocked = () =>
			document.hidden || document.querySelector('dialog[open]') !== null || menu !== null || backgroundMenu !== null || dragging || trash || !rootId ||
			root?.kind === 'stick' || entries === null || ['INPUT', 'TEXTAREA', 'SELECT'].includes(document.activeElement?.tagName ?? '');
		async function refresh() {
			if (inFlight || blocked()) return;
			const id = rootId, path = dir, before = entries;
			inFlight = true;
			controller = new AbortController();
			try {
				const listing = await api.get<FileEntry[]>('/files/list?' + new URLSearchParams({ root: id, path }), controller.signal);
				if (!active || id !== rootId || path !== dir || before !== entries || blocked()) return;
				refreshError = false;
				if (JSON.stringify(listing) === JSON.stringify(entries)) return;
				const first = Array.from(document.querySelectorAll<HTMLElement>('[data-file-name]')).find((e) => e.getBoundingClientRect().top >= 0);
				const name = first?.dataset.fileName, offset = first?.getBoundingClientRect().top, scroll = window.scrollY;
				entries = listing;
				const names = new Set(listing.map((e) => e.name));
				selected = selected.filter((n) => names.has(n));
				await tick();
				if (!active || id !== rootId || path !== dir) return;
				const next = Array.from(document.querySelectorAll<HTMLElement>('[data-file-name]')).find((e) => e.dataset.fileName === name);
				window.scrollTo({ top: next && offset !== undefined ? window.scrollY + next.getBoundingClientRect().top - offset : scroll, behavior: 'instant' });
			} catch {
				if (active && id === rootId && path === dir) refreshError = true;
			} finally {
				inFlight = false;
			}
		}
		const timer = setInterval(refresh, 10000);
		document.addEventListener('visibilitychange', refresh);
		return () => {
			active = false;
			controller?.abort();
			clearInterval(timer);
			document.removeEventListener('visibilitychange', refresh);
		};
	});

	// At the start, and whenever an upload finishes: it may belong to the
	// folder on screen, and it changes the free space.
	$effect(() => {
		uploads.finished;
		if (!preferencesReady) return;
		untrack(() => {
			loadRoots();
			load();
		});
	});

	// --- Navigation -----------------------------------------------------------

	let pathEditing = $state(false);
	let pathInput = $state('');
	let pathMessage = $state('');
	let pathField = $state<HTMLInputElement>();

	function captureLocation(): Location {
		return { root: rootId, path: dir, search, selected: [...selected], scroll: window.scrollY, listScroll: virtualList?.getScroll() ?? 0 };
	}
	function historyGo(direction: 'back' | 'forward') {
		const list = direction === 'back' ? back : forward;
		const target = list.at(-1);
		if (!target) return;
		if (direction === 'back') {
			back = back.slice(0, -1);
			forward = [...forward, captureLocation()];
		} else {
			forward = forward.slice(0, -1);
			back = [...back, captureLocation()];
		}
		navigate(target.root, target.path, target);
	}
	function open(id: string, path: string) {
		if (rootId && (rootId !== id || dir !== path)) back = [...back, captureLocation()].slice(-50);
		forward = [];
		placesOpen = false;
		navigate(id, path);
	}
	async function navigate(id: string, path: string, restore?: Location) {
		const chosen = roots?.find((place) => place.id === id);
		if ((chosen?.kind === 'share' || chosen?.kind === 'volume') && rememberedRoot !== id) {
			rememberedRoot = id;
			if (rootId) savePreferences();
		}
		if (rootId !== id) unlockedRoot = '';
		rootId = id;
		dir = path;
		applyFolderView();
		search = restore?.search ?? '';
		anchor = '';
		menu = null;
		backgroundMenu = null;
		preview = null;
		pathEditing = false;
		pathInput = path;
		pathMessage = '';
		trash = false;
		selected = [];
		entries = null;
		pending = [];
		await load();
		if (rootId !== id || dir !== path) return;
		if (restore) {
			const names = new Set((entries ?? ([] as FileEntry[])).map((e) => e.name));
			selected = restore.selected.filter((name) => names.has(name));
		}
		await tick();
		virtualList?.setScroll(restore?.listScroll ?? 0);
		window.scrollTo({ top: restore?.scroll ?? 0, behavior: 'instant' });
	}
	async function editPath() {
		pathInput = '/' + dir;
		pathMessage = '';
		pathEditing = true;
		await tick();
		pathField?.select();
	}
	async function goToPath(event: SubmitEvent) {
		event.preventDefault();
		const path = folderPath(pathInput);
		if (path === null) {
			pathMessage = t('files.invalidPath');
			return;
		}
		const id = rootId;
		try {
			await api.get<FileEntry[]>('/files/list?' + new URLSearchParams({ root: id, path }));
			if (rootId === id) {
				pathMessage = '';
				open(id, path);
			}
		} catch (err) {
			pathMessage = fail(err);
		}
	}
	async function copyPath() {
		try {
			await navigator.clipboard.writeText('/' + dir);
			pathMessage = t('files.pathCopied');
		} catch {
			pathMessage = t('files.pathCopyFailed');
		}
	}

	// --- Selection and mouse ------------------------------------------------

	function toggle(name: string, checked: boolean) {
		anchor = name;
		selected = checked ? [...selected, name] : selected.filter((other) => other !== name);
	}
	function selectRange(name: string, add = false) {
		const range = rangeSelection(visibleEntries.map((e) => e.name), anchor, name);
		selected = add ? [...new Set([...selected, ...range])] : range;
	}
	function selectType() {
		selected = visibleEntries.filter((e) => (selectionFilter === 'folders' ? e.dir : !e.dir && selectionType(e.name) === selectionFilter)).map((e) => e.name);
	}
	// The entry hit by the last click. A double click only opens what both
	// clicks hit: the first click on a folder already opened it, and the
	// second one lands on whatever is in its place now.
	let lastClick = '';
	/** The second click of a double click that opened a folder lands in the new folder; it does nothing there. */
	function secondClickElsewhere(entry: FileEntry, event: MouseEvent) {
		return event.detail > 1 && lastClick !== `${rootId}/${dir}/${entry.name}`;
	}
	function folderClick(entry: FileEntry, event: MouseEvent) {
		if (secondClickElsewhere(entry, event)) return;
		lastClick = `${rootId}/${dir}/${entry.name}`;
		if (event.shiftKey) selectRange(entry.name, event.ctrlKey || event.metaKey);
		else if (event.ctrlKey || event.metaKey) toggle(entry.name, !selectedNames.has(entry.name));
		else open(rootId, join(dir, entry.name));
	}
	// A click on the row opens a folder or toggles the selection of a file.
	// Clicks on the checkbox or the buttons inside the row are left to them.
	function rowClicked(entry: FileEntry, event: MouseEvent) {
		if (event.target instanceof Element && event.target.closest('input, button')) return;
		if (secondClickElsewhere(entry, event)) return;
		lastClick = `${rootId}/${dir}/${entry.name}`;
		if (event.shiftKey) selectRange(entry.name, event.ctrlKey || event.metaKey);
		else if (event.ctrlKey || event.metaKey) toggle(entry.name, !selectedNames.has(entry.name));
		else if (entry.dir) open(rootId, join(dir, entry.name));
		else toggle(entry.name, !selectedNames.has(entry.name));
	}
	/** Double click on a file: preview, or the editor for text the preview does not show. */
	function rowDoubleClicked(entry: FileEntry, event: MouseEvent) {
		if (entry.dir || lastClick !== `${rootId}/${dir}/${entry.name}`) return;
		if (event.target instanceof Element && event.target.closest('input') || event.shiftKey || event.ctrlKey || event.metaKey) return;
		selected = [entry.name];
		anchor = entry.name;
		if (previewKind(entry.name, entry.size)) preview = entry;
		else if (editable) editing = join(dir, entry.name);
	}
	function entryMenu(entry: FileEntry, x: number, y: number) {
		if (!selectedNames.has(entry.name)) selected = [entry.name];
		backgroundMenu = null;
		menu = { entry, x, y };
	}
	function menuFromButton(entry: FileEntry, button: HTMLElement) {
		const rect = button.getBoundingClientRect();
		entryMenu(entry, rect.right - 232, rect.bottom + 4);
	}
	function blankMenu(event: MouseEvent) {
		if (trash || !root || (event.target instanceof Element && event.target.closest('.row:not(.header),button,input,select,a,[role="menu"]'))) return;
		event.preventDefault();
		menu = null;
		backgroundMenu = { x: event.clientX, y: event.clientY };
	}
	/** Runs a menu action, then closes the menu. The action may still read the entry of the menu. */
	function choose(action: () => void) {
		action();
		menu = null;
		backgroundMenu = null;
	}

	// --- Clipboard and keyboard ---------------------------------------------

	let clipboard = $state<{ from: FileRoot; paths: string[]; move: boolean } | null>(null);
	function rememberSelection(move: boolean) {
		if (!root || !selected.length || root.kind === 'stick' || (move && !writable)) return;
		clipboard = { from: root, paths: selected.map((name) => join(dir, name)), move };
		menu = null;
	}
	const canPaste = $derived(!!clipboard && writable && (!clipboard.move || !!safeRoots.find((place) => place.id === clipboard?.from.id)?.writable));
	function paste() {
		if (!clipboard || !canPaste) return;
		copying = { ...clipboard, targetRoot: rootId, targetDir: dir, pasted: true };
		backgroundMenu = null;
	}
	async function focusEntry(index: number) {
		virtualList?.reveal(index);
		await tick();
		const name = visibleEntries[index]?.name;
		if (name) document.querySelector<HTMLElement>(`.browser [data-file-name="${CSS.escape(name)}"]`)?.focus({ preventScroll: true });
	}
	function keyboard(event: KeyboardEvent) {
		if (trash || !root || menu || backgroundMenu || document.querySelector('dialog') || event.altKey || event.defaultPrevented) return;
		if (event.target instanceof Element && event.target.closest('input:not([type=checkbox]):not([type=radio]),textarea,select,[contenteditable="true"],[role="menu"],.dropdown-panel')) return;
		const key = event.key.toLowerCase(), command = event.ctrlKey || event.metaKey;
		if (key === 'f1') {
			event.preventDefault();
			shortcuts = true;
			return;
		}
		if (event.target instanceof Element && event.target.closest('.pane')) return;
		const listTarget = !(event.target instanceof Element) || event.target === document.body || !!event.target.closest('.file-entries');
		if (!command && listTarget && ['arrowup', 'arrowdown', 'arrowleft', 'arrowright', 'home', 'end'].includes(key) && visibleEntries.length) {
			event.preventDefault();
			const focused = event.target instanceof Element ? event.target.closest<HTMLElement>('[data-file-name]')?.dataset.fileName : undefined;
			const old = visibleEntries.findIndex((e) => e.name === (focused ?? selected.at(-1)));
			const columns = virtualList?.getColumns() ?? 1;
			const step = key === 'arrowup' ? -columns : key === 'arrowdown' ? columns : key === 'arrowleft' ? -1 : 1;
			const index = key === 'home' ? 0 : key === 'end' ? visibleEntries.length - 1 : Math.max(0, Math.min(visibleEntries.length - 1, old < 0 ? 0 : old + step));
			const name = visibleEntries[index].name;
			if (event.shiftKey) {
				if (!anchor) anchor = visibleEntries[Math.max(0, old)].name;
				selectRange(name);
			} else {
				selected = [name];
				anchor = name;
			}
			void focusEntry(index);
			return;
		}
		if (!command && listTarget && (key === 'enter' || key === ' ') && selected.length === 1) {
			if (event.target instanceof Element && event.target.closest('button,input,a')) return;
			const entry = visibleEntries.find((e) => e.name === selected[0]);
			if (!entry) return;
			event.preventDefault();
			if (entry.dir && key === 'enter') open(rootId, join(dir, entry.name));
			else if (!entry.dir && previewKind(entry.name, entry.size)) preview = entry;
			return;
		}
		// Shift+F10 and the menu key open the menu of the focused entry.
		if ((key === 'contextmenu' || (key === 'f10' && event.shiftKey)) && selected.length) {
			const row = document.querySelector<HTMLElement>(`.browser [data-file-name="${CSS.escape(selected.at(-1) ?? '')}"]`);
			const entry = visibleEntries.find((e) => e.name === selected.at(-1));
			if (row && entry) {
				event.preventDefault();
				const rect = row.getBoundingClientRect();
				entryMenu(entry, rect.left + 40, rect.bottom);
			}
			return;
		}
		if (command && key === 'a') {
			event.preventDefault();
			selected = visibleEntries.map((entry) => entry.name);
		} else if (command && (key === 'c' || key === 'x') && selected.length && root.kind !== 'stick' && (key !== 'x' || writable)) {
			event.preventDefault();
			rememberSelection(key === 'x');
		} else if (command && key === 'v' && clipboard && writable) {
			event.preventDefault();
			paste();
		} else if (!command && key === 'f2' && selected.length === 1 && writable) {
			event.preventDefault();
			show('rename');
		} else if (!command && key === 'delete' && selected.length && writable) {
			event.preventDefault();
			show('delete');
		} else if (!command && key === 'escape' && selected.length) {
			selected = [];
		}
	}

	// --- Actions --------------------------------------------------------------

	/** Gives a text file or an image to the assistant with a question about it. */
	async function showAssistant(name: string) {
		try {
			const res = await fetch('/api/v1/files/download?' + new URLSearchParams({ root: rootId, path: join(dir, name) }), { credentials: 'same-origin' });
			if (!res.ok) throw new Error(String(res.status));
			const blob = await res.blob();
			const file = new File([blob], name, { type: previewKind(name, blob.size) === 'image' ? blob.type || 'image/png' : 'text/plain' });
			await askAbout(t('assistant.askFile', { name }), [await readAttachment(file)]);
		} catch {
			error = t('assistant.askFileFailed');
		}
	}

	function download() {
		if (!entries || selected.length === 0) return;
		const single = selected.length === 1 ? entries.find((entry) => entry.name === selected[0]) : undefined;
		// Folders and several entries are packed; the dialog asks for the name of the ZIP.
		if (!single || single.dir) {
			downloadPaths = selected.map((name) => join(dir, name));
			return;
		}
		// The browser saves the answer; nothing passes through this page's memory.
		location.href = '/api/v1/files/download?' + new URLSearchParams({ root: rootId, path: join(dir, single.name) });
	}
	function show(kind: 'mkdir' | 'rename' | 'delete') {
		if (!writable) return;
		dialogError = '';
		confirmed = false;
		permanent = !root?.recycle;
		if (kind === 'rename') {
			typed = selected[0];
			dialog = { kind, name: selected[0] };
		} else {
			typed = '';
			dialog = { kind };
		}
	}
	async function submitDialog(event: SubmitEvent) {
		event.preventDefault();
		if (!dialog || !writable) return;
		dialogError = '';
		try {
			if (dialog.kind === 'mkdir') {
				await api.post('/files/mkdir', { root: rootId, path: join(dir, typed.trim()) });
			} else if (dialog.kind === 'rename') {
				await api.post('/files/rename', { root: rootId, from: join(dir, dialog.name), to: join(dir, typed.trim()) });
			} else {
				if (permanent && !confirmed) return;
				await api.post('/files/delete', { root: rootId, paths: selected.map((name) => join(dir, name)), permanent });
			}
			dialog = null;
			jobsTick++;
			selected = [];
			await load();
			loadRoots();
		} catch (err) {
			dialogError = fail(err);
		}
	}

	// --- Uploads --------------------------------------------------------------

	function upload(files: { file: File; path: string }[]) {
		if (root && writable && files.length) uploadReview = { root, dir, files };
	}
	/** Creates the folders of an uploaded tree, parents first, then queues the files. */
	async function startUpload(files: { file: File; path: string; conflict: '' | 'rename' | 'replace' }[]) {
		const review = uploadReview;
		if (!review || !safeRoots.find((r) => r.id === review.root.id)?.writable) throw new ApiError(403, 'read_only_share', '');
		const target = review.dir;
		const folders = new Set<string>();
		for (const { path } of files) {
			const parts = path.split('/').slice(0, -1);
			for (let depth = 1; depth <= parts.length; depth++) folders.add(parts.slice(0, depth).join('/'));
		}
		for (const folder of [...folders].sort()) {
			try {
				await api.post('/files/mkdir', { root: review.root.id, path: join(target, folder) });
			} catch (err) {
				// The folder being there already is what we want.
				if (!(err instanceof ApiError && err.code === 'file_exists')) throw err;
			}
		}
		enqueue(
			files.map(({ file, path, conflict }) => ({ file, conflict, dir: join(target, path.split('/').slice(0, -1).join('/')) })),
			review.root
		);
		uploadReview = null;
		load();
	}
	function picked(input: HTMLInputElement) {
		const files = [...(input.files ?? [])].map((file) => ({ file, path: file.webkitRelativePath || file.name }));
		input.value = '';
		upload(files);
	}
	/** Reads a dropped folder with everything below it. */
	async function readEntry(entry: FileSystemEntry, prefix: string, out: { file: File; path: string }[]) {
		if (entry.isFile) {
			const file = await new Promise<File>((resolve, reject) => (entry as FileSystemFileEntry).file(resolve, reject));
			out.push({ file, path: prefix + entry.name });
		} else if (entry.isDirectory) {
			const reader = (entry as FileSystemDirectoryEntry).createReader();
			for (;;) {
				const batch = await new Promise<FileSystemEntry[]>((resolve, reject) => reader.readEntries(resolve, reject));
				if (batch.length === 0) break;
				for (const child of batch) await readEntry(child, prefix + entry.name + '/', out);
			}
		}
	}
	async function dropped(event: DragEvent) {
		if (hasFileDrag(event)) {
			reviewDrop(event, rootId, dir);
			return;
		}
		event.preventDefault();
		dragging = false;
		if (!writable || !event.dataTransfer) return;
		// The entries have to be taken before the first await; the list is gone afterwards.
		const items = [...event.dataTransfer.items].map((item) => item.webkitGetAsEntry?.()).filter((entry): entry is FileSystemEntry => !!entry);
		const plain = [...event.dataTransfer.files];
		const files: { file: File; path: string }[] = [];
		if (items.length > 0) for (const entry of items) await readEntry(entry, '', files);
		else files.push(...plain.map((file) => ({ file, path: file.name })));
		upload(files);
	}
</script>

<svelte:window onkeydown={keyboard} />
<PageHead title={t('files.title')} />
{#if preferenceError}<p class="notice-error" role="alert">{preferenceError} <button class="btn" onclick={() => (preferencesReady ? savePreferences() : loadPreferences())}>{t('files.retryPreferences')}</button></p>{/if}
{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
<!-- Fixed, so that saving never moves the page. -->
{#if savingPreferences}<p class="saving" role="status">{t('files.savingPreferences')}</p>{/if}

{#if roots && roots.length === 0}
	<EmptyState title={t('files.noRoots')} text={canAdminister ? t('files.noRootsAdmin') : undefined} />
{:else if roots}
	<div class="layout">
		<button type="button" class="btn places-toggle" aria-expanded={placesOpen} aria-controls="file-places" onclick={() => (placesOpen = !placesOpen)}>
			<Icon name="folder" size={16} /><span>{t('files.placeCurrent', { name: root?.name ?? '–' })}</span><Icon name="chevronDown" size={14} />
		</button>
		<nav id="file-places" class="tile places" class:open={placesOpen} aria-label={t('files.places')}>
			<Places
				{roots}
				current={trash ? '' : rootId}
				{dir}
				{showHidden}
				admin={canAdminister}
				{usbDevices}
				{usbBusy}
				{usbLoadError}
				backups={usbBackups}
				{backupLoadError}
				{backupProgressError}
				{openingBackup}
				favorites={visibleFavorites}
				onopen={open}
				onusb={usbAction}
				onbackup={openUSBBackup}
				onrefreshbackups={() => {
					loadUSBBackups();
					loadUsb();
				}}
				onmanagefavorites={() => (favoriteManager = true)}
				onremovefavorite={(favorite) => saveFavorites(favorites.filter((f) => f !== favorite))}
			/>
		</nav>

		<div class="workspace" class:split>
			{#if split && !trash && (selected.length || right.selected.length) && root?.kind !== 'stick'}
				<div class="pane-actions">
					{#if selected.length}
						<button class="btn" disabled={!rightRoot?.writable} onclick={() => transferPane('right', false)}>{t('files.copyRight')} →</button>
						<button class="btn" disabled={!writable || !rightRoot?.writable} onclick={() => transferPane('right', true)}>{t('files.moveRight')} →</button>
					{/if}
					{#if right.selected.length}
						<button class="btn" disabled={!writable} onclick={() => transferPane('left', false)}>← {t('files.copyLeft')}</button>
						<button class="btn" disabled={!rightRoot?.writable || !writable} onclick={() => transferPane('left', true)}>← {t('files.moveLeft')}</button>
					{/if}
				</div>
			{/if}
			<section
				oncontextmenu={blankMenu}
				class="tile browser"
				class:dragging
				aria-label={root?.name}
				ondragover={(event) => {
					if (writable) {
						event.preventDefault();
						dragging = true;
					}
				}}
				ondragleave={(event) => {
					if (!event.currentTarget.contains(event.relatedTarget as Node)) dragging = false;
				}}
				ondrop={dropped}
			>
				{#if trash && root}
					{#if root.recycle}
						<TrashView {root} user={session.data?.user?.name ?? ''} admin={canAdminister} onclose={() => { trash = false; load(); }} onchanged={() => { load(); loadRoots(); }} />
					{:else}
						<div class="toolbar"><button type="button" class="btn" onclick={() => { trash = false; load(); }}>{t('files.trashBack')}</button></div>
						<EmptyState title={t('files.trashOf', { name: root.name })} text={t('files.trashDisabled')} />
						{#if canAdminister}<a class="btn trash-configure" href="/shares">{t('files.trashConfigure')}</a>{/if}
					{/if}
				{:else}
					<!-- Where: history, path and search. -->
					<div class="toolbar nav-bar">
						<div class="nav-buttons">
							<button type="button" class="btn icon" aria-label={t('files.back')} title={t('files.back')} disabled={!back.length} onclick={() => historyGo('back')}><Icon name="arrowLeft" size={16} /></button>
							<button type="button" class="btn icon" aria-label={t('files.forward')} title={t('files.forward')} disabled={!forward.length} onclick={() => historyGo('forward')}><Icon name="arrowRight" size={16} /></button>
							<button type="button" class="btn icon" aria-label={t('files.up')} title={t('files.up')} disabled={!dir} onclick={() => open(rootId, dir.split('/').slice(0, -1).join('/'))}><Icon name="arrowUp" size={16} /></button>
						</div>
						{#if pathEditing && root}
							<form class="path-form" onsubmit={goToPath}>
								<label class="sr-only" for="file-path">{t('files.folderPath')}</label>
								<input
									id="file-path"
									class="field mono"
									bind:this={pathField}
									bind:value={pathInput}
									placeholder="/"
									title={t('files.pathHint', { name: root.name })}
									autocomplete="off"
									spellcheck="false"
									onkeydown={(event) => {
										if (event.key === 'Escape') {
											event.preventDefault();
											pathEditing = false;
											pathMessage = '';
										}
									}}
								/>
								<button class="btn">{t('files.goToPath')}</button>
								<button type="button" class="btn" onclick={() => { pathEditing = false; pathMessage = ''; }}>{t('common.cancel')}</button>
							</form>
						{:else}
							<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
							<div class="crumbs mono" title={t('files.editPathHint')} onclick={(event) => { if (event.target === event.currentTarget) editPath(); }}>
								{#if root}<button type="button" onclick={() => open(rootId, '')}>{root.name}</button>{/if}
								{#each crumbs as crumb, index (index)}
									<span class="muted" aria-hidden="true">/</span>
									<button type="button" onclick={() => open(rootId, crumbs.slice(0, index + 1).join('/'))}>{crumb}</button>
								{/each}
								{#if root}<button type="button" class="edit-path" aria-label={t('files.editPath')} title={t('files.editPath')} onclick={editPath}><Icon name="edit" size={14} /></button>{/if}
							</div>
						{/if}
						{#if root && root.kind !== 'stick'}
							<button type="button" class="btn icon favorite" aria-label={t(isFavorite ? 'files.unfavorite' : 'files.favorite')} title={`${t(isFavorite ? 'files.unfavorite' : 'files.favorite')} – ${t('files.favoriteAccount')}`} aria-pressed={isFavorite} disabled={!isFavorite && favorites.length >= 50} onclick={toggleFavorite}><Icon name="star" size={16} /></button>
						{/if}
						{#if root}
							<Dropdown label={t('files.moreFolderActions')} icon="more" align="right">
								{#snippet children(close)}
									<button type="button" role="menuitem" onclick={() => { close(); editPath(); }}><span>{t('files.editPath')}</span></button>
									<button type="button" role="menuitem" onclick={() => { close(); copyPath(); }}><span>{t('files.copyPath')}</span></button>
									{#if root?.kind === 'share' || root?.kind === 'volume'}<button type="button" role="menuitem" onclick={() => { close(); windowsPath = true; }}><span>{t('files.windowsPath')}</span></button>{/if}
									<hr />
									<button type="button" role="menuitem" onclick={() => { close(); load(); loadRoots(); }}><span>{t('files.reload')}</span></button>
								{/snippet}
							</Dropdown>
						{/if}
						<label class="search">
							<Icon name="search" size={16} />
							<input class="field" type="search" aria-label={t('files.searchFolder')} placeholder={t('files.searchFolder')} bind:value={search} oninput={() => { selected = []; anchor = ''; }} />
						</label>
					</div>
					{#if pathMessage}<p class="path-message" role="status">{pathMessage}</p>{/if}

					<!-- What: creating, choosing, viewing. -->
					<div class="toolbar action-bar">
						{#if writable}
							<button type="button" class="btn btn-primary" onclick={() => fileInput?.click()}><Icon name="upload" size={16} />{t('files.upload')}</button>
							<Dropdown label={t('files.new')} text={t('files.new')} icon="plus">
								{#snippet children(close)}
									<button type="button" role="menuitem" onclick={() => { close(); show('mkdir'); }}><Icon name="folder" size={16} /><span>{t('files.newFolder')}</span></button>
									<button type="button" role="menuitem" onclick={() => { close(); creatingFile = true; }}><Icon name="fileText" size={16} /><span>{t('files.newFile')}</span></button>
									<hr />
									<button type="button" role="menuitem" onclick={() => { close(); folderInput?.click(); }}><Icon name="upload" size={16} /><span>{t('files.uploadFolder')}</span></button>
								{/snippet}
							</Dropdown>
							<input bind:this={fileInput} type="file" multiple hidden onchange={(event) => picked(event.currentTarget)} />
							<input bind:this={folderInput} type="file" webkitdirectory hidden onchange={(event) => picked(event.currentTarget)} />
						{/if}
						{#if clipboard}
							<button type="button" class="btn" disabled={!canPaste} onclick={paste}><Icon name="paste" size={16} />{t('files.paste')}</button>
						{/if}
						<span class="spacer"></span>
						{#if root}
							<Dropdown label={t('files.selection')} text={t('files.selection')} icon="select" kind="panel" align="right" compact>
								{#snippet children()}
									<button class="btn" onclick={() => (selected = visibleEntries.map((e) => e.name))}>{t('files.selectAll')}</button>
									<button class="btn" disabled={!selected.length} onclick={() => (selected = [])}>{t('files.clearSelection')}</button>
									<button class="btn" onclick={() => (selected = visibleEntries.filter((e) => !selectedNames.has(e.name)).map((e) => e.name))}>{t('files.invertSelection')}</button>
									<hr />
									<label class="stack">{t('files.selectType')}
										<select class="field" bind:value={selectionFilter}>
											{#each ['images', 'video', 'audio', 'documents', 'folders', 'other'] as type (type)}<option value={type}>{t(`files.selectionType.${type}` as 'files.selectionType.images')}</option>{/each}
										</select>
									</label>
									<button class="btn" onclick={selectType}>{t('files.selectMatches')}</button>
								{/snippet}
							</Dropdown>
							<Dropdown label={t('files.tools')} text={t('files.tools')} icon="tool" align="right" compact>
								{#snippet children(close)}
									<button type="button" role="menuitem" onclick={() => { close(); deepSearch = true; }}><span>{t('files.deepSearch')}</span></button>
									{#if root?.kind !== 'stick'}
										<button type="button" role="menuitem" onclick={() => { close(); usage = true; }}><span>{t('files.usageTitle')}</span></button>
										<button type="button" role="menuitem" onclick={() => { close(); duplicates = true; }}><span>{t('files.duplicatesTitle')}</span></button>
									{/if}
									{#if split}<button type="button" role="menuitem" disabled={!rightRoot} onclick={() => { close(); comparison = true; }}><span>{t('files.compareFolders')}</span></button>{/if}
								{/snippet}
							</Dropdown>
							{#if root.kind === 'share'}
								<button type="button" class="btn" aria-label={t('files.trash')} title={t('files.trash')} onclick={() => { trash = true; selected = []; }}><Icon name="trash" size={16} /><span class="label">{t('files.trash')}</span></button>
							{/if}
						{/if}
						<div class="segmented" role="group" aria-label={t('files.view')}>
							{#each views as option (option.mode)}
								<button type="button" class="btn icon" disabled={!preferencesReady} aria-label={t(option.label)} title={option.mode === 'large' ? `${t(option.label)} – ${t('files.thumbnailHint')}` : t(option.label)} aria-pressed={view === option.mode} onclick={() => changeView(option.mode)}><Icon name={option.icon} size={16} /></button>
							{/each}
						</div>
						<button type="button" class="btn icon" aria-label={t('files.twoPanes')} title={t('files.twoPanes')} aria-pressed={split} onclick={() => { split = !split; savePreferences(); }}><Icon name="columns" size={16} /></button>
						<Dropdown label={t('files.view')} text={t('files.view')} icon="eye" kind="panel" align="right" compact>
							{#snippet children(close)}
								<label><input type="checkbox" checked={showHidden} onchange={(event) => { showHidden = event.currentTarget.checked; selected = []; savePreferences(); }} />{t('files.showHidden')}</label>
								{#if root?.kind !== 'stick'}<label><input type="checkbox" checked={folderViewSaved} disabled={!preferencesReady || (!folderViewSaved && folderViews.length >= 200)} onchange={toggleFolderView} />{t('files.rememberFolderView')}</label>{/if}
								<hr />
								<button class="btn" onclick={() => { close(); shortcuts = true; }}>{t('files.shortcuts')} <kbd aria-hidden="true">F1</kbd></button>
							{/snippet}
						</Dropdown>
					</div>

					{#if unfinished.length > 0}
						<section class="unfinished" aria-label={t('uploads.unfinished')}>
							<strong>{t('uploads.unfinished')}</strong>
							<p class="muted">{recovering ? t('uploads.verifying') : t('uploads.recoverHint')}</p>
							<input bind:this={recoveryInput} type="file" hidden onchange={(event) => recoverPicked(event.currentTarget)} />
							{#each unfinished as part (part.id)}
								<div class="unfinished-row">
									<span>{part.name} · <span class="mono">{formatBytes(part.offset)} / {formatBytes(part.size)}</span></span>
									<button class="btn" disabled={recovering || part.busy} onclick={() => { if (root) { recovery = { status: part, root, dir }; recoveryInput?.click(); } }}>{t('uploads.resume')}</button>
									<button class="btn" disabled={recovering || part.busy} onclick={() => removePending(part)}>{t('uploads.removePartial')}</button>
								</div>
							{/each}
						</section>
					{/if}
					{#if root?.kind === 'usb' && (!backupCatalogReady || usbLoadError || backupDrives.includes(rootId))}
						<div class="toolbar">
							<span>{t(backupLocked ? 'files.backupLocked' : 'files.backupUnlocked')}</span>
							<button class="btn" onclick={() => { if (backupLocked) unlockDialog = true; else { unlockedRoot = ''; if (clipboard?.move) clipboard = null; } }}>{t(backupLocked ? 'files.unlockBackup' : 'files.lockBackup')}</button>
						</div>
					{/if}
					{#if root && !writable}<p class="notice-warn">{t(backupLocked ? 'files.rightReason.usb' : root.kind === 'share' ? 'files.rightReason.share' : root.kind === 'stick' ? 'files.rightReason.stick' : 'files.rightReason.readOnly')}</p>{/if}
					{#if refreshError}<p class="notice-warn" role="status">{t('files.autoRefreshFailed')}</p>{/if}

					<div class="file-entries" class:small={view === 'small'} class:large={view === 'large'}>
						<div class="row header">
							<input type="checkbox" aria-label={t('files.selectAll')} checked={allSelected} onchange={(event) => (selected = event.currentTarget.checked ? visibleEntries.map((entry) => entry.name) : [])} />
							{#each ['name', 'size', 'modified'] as const as column (column)}
								<button type="button" class="sort" class:num={column !== 'name'} class:active={sort === column} disabled={!preferencesReady} aria-label={sortLabel(column)} title={t('files.sort')} onclick={() => sortBy(column)}>
									<span>{t(column === 'name' ? 'files.name' : column === 'size' ? 'files.size' : 'files.modified')}</span>
									{#if sort === column}<span aria-hidden="true">{descending ? '↓' : '↑'}</span>{/if}
								</button>
							{/each}
						</div>
						<VirtualEntries bind:this={virtualList} items={visibleEntries} key={(e) => e.name} minWidth={view === 'list' ? 0 : view === 'small' ? 220 : 160} rowHeight={view === 'large' ? 260 : 72} label={t('files.title')} reset={`${rootId}/${dir}/${search}/${view}/${sort}/${descending}`}>
							{#snippet children(entry)}
								<!-- The whole row is a larger target for the mouse. Keyboards and screen
								     readers use the checkbox and the buttons inside it, so the row
								     itself is no control: one nested in another is announced badly. -->
								<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
								<div
									class="row"
									tabindex="-1"
									draggable={root?.kind !== 'stick'}
									ondragstart={(event) => dragEntry(event, entry)}
									ondragend={endFileDrag}
									ondragover={(event) => { if (entry.dir && writable && hasFileDrag(event)) { event.preventDefault(); event.stopPropagation(); } }}
									ondrop={(event) => { if (entry.dir) reviewDrop(event, rootId, join(dir, entry.name)); }}
									data-file-name={entry.name}
									class:picked={selectedNames.has(entry.name)}
									onclick={(event) => rowClicked(entry, event)}
									ondblclick={(event) => rowDoubleClicked(entry, event)}
									oncontextmenu={(event) => { event.preventDefault(); event.stopPropagation(); entryMenu(entry, event.clientX, event.clientY); }}
								>
									<input type="checkbox" aria-label={t('files.select', { name: entry.name })} checked={selectedNames.has(entry.name)} onclick={(event) => { if (event.shiftKey) selectRange(entry.name, event.ctrlKey || event.metaKey); else toggle(entry.name, event.currentTarget.checked); }} />
									{#if entry.dir}
										<button type="button" class="entry" onclick={(event) => folderClick(entry, event)}><FileIcon name={entry.name} dir size={view === 'large' ? 48 : view === 'small' ? 24 : 18} /><span>{entry.name}</span></button>
									{:else}
										<span class="entry">{#if view === 'large' && previewKind(entry.name, entry.size) === 'image'}<FileThumbnail root={rootId} path={join(dir, entry.name)} size={entry.size} />{:else}<FileIcon name={entry.name} dir={false} size={view === 'large' ? 48 : view === 'small' ? 24 : 18} />{/if}<span>{entry.name}</span></span>
									{/if}
									<span class="mono num muted">{entry.dir ? '–' : formatBytes(entry.size)}</span>
									<span class="num muted">{date.format(new Date(entry.modified))}</span>
									<button type="button" class="entry-more" aria-label={t('files.actionsFor', { name: entry.name })} title={t('files.actionsFor', { name: entry.name })} aria-haspopup="menu" aria-expanded={menu?.entry.name === entry.name} onclick={(event) => menuFromButton(entry, event.currentTarget)}><Icon name="more" size={16} /></button>
								</div>
							{/snippet}
						</VirtualEntries>
						{#if !visibleEntries.length}
							{#if !root}<EmptyState inline title={t('files.chooseRoot')} />{:else if entries}<EmptyState inline title={t(search && entries.length > 0 ? 'files.noMatches' : 'files.empty')} />{/if}
						{/if}
					</div>

					<p class="list-footer muted" role="status">{t('files.resultCount', { shown: visibleEntries.length, total: entries?.length ?? 0 })}{#if clipboard} · {t(clipboard.move ? 'files.cutCount' : 'files.copiedCount', { count: clipboard.paths.length })}{/if}</p>
					{#if root?.kind === 'stick'}<p class="muted none">{t('files.stickNote')}</p>{/if}
					{#if writable}
						<p class="drop" class:active={dragging}>
							<Icon name="upload" size={18} />{dragging ? t('files.dropNow', { path: join(root?.name ?? '', dir) }) : t('files.dropHint')}
						</p>
					{/if}

					<!-- Stays at the bottom of the screen while the list scrolls. -->
					{#if selected.length > 0}
						<div class="actions" role="region" aria-label={t('files.selectionActions')}>
							<span class="count">{t('files.selected', { count: selected.length })} · {formatBytes(measured?.key === selectionKey ? measured.bytes : selectedBytes)}{#if selectedFolders && measured?.key !== selectionKey} · <span class="muted">{t('files.foldersNotCounted', { count: selectedFolders })}</span>{/if}</span>
							{#if selectedFolders}<button class="btn secondary" disabled={measuring} onclick={measureSelection}>{t(measuring ? 'files.measuring' : 'files.measureFolders')}</button>{/if}
							{#if currentUSBBackup}<button type="button" class="btn" disabled={currentUSBBackup.state?.running} onclick={() => (copying = { move: false, restore: true })}>{t('files.restoreSelected')}</button>{/if}
							<button type="button" class="btn" onclick={download}><Icon name="download" size={16} />{t('files.download')}</button>
							{#if root?.kind !== 'stick'}<button type="button" class="btn secondary" onclick={() => (copying = { move: false })}>{t('files.copy')}</button>{/if}
							{#if editable}<button type="button" class="btn secondary" onclick={() => (editing = join(dir, selected[0]))}>{t('files.edit')}</button>{/if}
							{#if writable}
								<button type="button" class="btn secondary" onclick={() => (copying = { move: true })}>{t('files.move')}</button>
								{#if selected.length === 1}<button type="button" class="btn secondary" onclick={() => show('rename')}>{t('files.rename')}</button>{:else}<button class="btn secondary" onclick={() => (batchRename = true)}>{t('files.renameBatch')}</button>{/if}
								<button type="button" class="btn btn-danger" onclick={() => show('delete')}><Icon name="trash" size={16} />{t('files.delete')}</button>
							{/if}
							<!-- On phones the other actions are in the menu, so the bar keeps one line. -->
							<button type="button" class="btn icon more-actions" aria-label={t('files.moreActions')} title={t('files.moreActions')} aria-haspopup="menu" onclick={(event) => { const entry = selectedEntries.at(-1); if (entry) menuFromButton(entry, event.currentTarget); }}><Icon name="more" size={16} /></button>
							<button type="button" class="btn icon clear" aria-label={t('files.clearSelection')} title={t('files.clearSelection')} onclick={() => (selected = [])}><Icon name="close" size={16} /></button>
						</div>
					{/if}
				{/if}
			</section>
			{#if split}<FolderPane onpreview={(id, path, entry) => (otherPreview = { root: id, path, size: entry.size })} ondropfiles={reviewDrop} roots={safeRoots} refresh={jobsTick + uploads.finished} {showHidden} onchange={(id, path, names) => (right = { root: id, path, selected: names })} />{/if}
		</div>
	</div>
{/if}

{#if menu && root}
	{@const entry = menu.entry}
	{@const one = selected.length === 1}
	<ContextMenu label={one ? t('files.actionsFor', { name: entry.name }) : t('files.selected', { count: selected.length })} x={menu.x} y={menu.y} onclose={() => (menu = null)}>
		<div class="menu-title" role="presentation">{one ? entry.name : t('files.selected', { count: selected.length })}</div>
		{#if one && entry.dir}<button type="button" role="menuitem" onclick={() => choose(() => open(rootId, join(dir, entry.name)))}><Icon name="folder" size={16} /><span>{t('files.openFolder')}</span><kbd aria-hidden="true">Enter</kbd></button>
		{:else if one && previewKind(entry.name, entry.size)}<button type="button" role="menuitem" onclick={() => choose(() => (preview = entry))}><Icon name="search" size={16} /><span>{t('files.preview')}</span><kbd aria-hidden="true">Space</kbd></button>{/if}
		{#if editable}<button type="button" role="menuitem" onclick={() => choose(() => (editing = join(dir, selected[0])))}><Icon name="edit" size={16} /><span>{t('files.edit')}</span></button>{/if}
		{#if one && !entry.dir && root.kind !== 'stick' && assistant.status?.available && ((previewKind(entry.name, entry.size) === 'text' && entry.size <= 256 << 10) || (previewKind(entry.name, entry.size) === 'image' && entry.size <= 4 << 20))}
			<button type="button" role="menuitem" onclick={() => choose(() => showAssistant(entry.name))}><Icon name="chat" size={16} /><span>{t('assistant.askFileMenu')}</span></button>
		{/if}
		<button type="button" role="menuitem" onclick={() => choose(download)}><Icon name="download" size={16} /><span>{t('files.download')}</span></button>
		{#if one && !entry.dir && /\.zip$/i.test(entry.name)}<button type="button" role="menuitem" onclick={() => choose(() => (archive = join(dir, entry.name)))}><Icon name="archive" size={16} /><span>{t('files.archiveTitle')}</span></button>{/if}
		{#if root.kind !== 'stick'}
			<hr />
			<button type="button" role="menuitem" onclick={() => choose(() => rememberSelection(false))}><Icon name="copy" size={16} /><span>{t('files.clipboardCopy')}</span><kbd aria-hidden="true">Ctrl+C</kbd></button>
			{#if writable}<button type="button" role="menuitem" onclick={() => choose(() => rememberSelection(true))}><i class="no-icon" aria-hidden="true"></i><span>{t('files.cut')}</span><kbd aria-hidden="true">Ctrl+X</kbd></button>{/if}
			<button type="button" role="menuitem" onclick={() => choose(() => (copying = { move: false }))}><i class="no-icon" aria-hidden="true"></i><span>{t('files.copy')}</span></button>
			{#if writable}<button type="button" role="menuitem" onclick={() => choose(() => (copying = { move: true }))}><i class="no-icon" aria-hidden="true"></i><span>{t('files.move')}</span></button>{/if}
		{/if}
		{#if writable}
			{#if one}<button type="button" role="menuitem" onclick={() => choose(() => show('rename'))}><i class="no-icon" aria-hidden="true"></i><span>{t('files.rename')}</span><kbd aria-hidden="true">F2</kbd></button>
			{:else}<button type="button" role="menuitem" onclick={() => choose(() => (batchRename = true))}><i class="no-icon" aria-hidden="true"></i><span>{t('files.renameBatch')}</span></button>{/if}
		{/if}
		{#if one}<hr /><button type="button" role="menuitem" onclick={() => choose(() => (properties = { root: root!, path: join(dir, entry.name) }))}><i class="no-icon" aria-hidden="true"></i><span>{t('files.properties')}</span></button>{/if}
		{#if writable}<hr /><button type="button" role="menuitem" class="danger" onclick={() => choose(() => show('delete'))}><Icon name="trash" size={16} /><span>{t('files.delete')}</span><kbd aria-hidden="true">Entf</kbd></button>{/if}
	</ContextMenu>
{/if}

{#if backgroundMenu && root}
	<ContextMenu label={t('files.folderActions')} x={backgroundMenu.x} y={backgroundMenu.y} onclose={() => (backgroundMenu = null)}>
		{#if writable}
			<button type="button" role="menuitem" onclick={() => choose(() => show('mkdir'))}><Icon name="folder" size={16} /><span>{t('files.newFolder')}</span></button>
			<button type="button" role="menuitem" onclick={() => choose(() => (creatingFile = true))}><Icon name="fileText" size={16} /><span>{t('files.newFile')}</span></button>
			<button type="button" role="menuitem" onclick={() => choose(() => fileInput?.click())}><Icon name="upload" size={16} /><span>{t('files.upload')}</span></button>
			<button type="button" role="menuitem" disabled={!canPaste} onclick={() => choose(paste)}><Icon name="paste" size={16} /><span>{t('files.paste')}</span><kbd aria-hidden="true">Ctrl+V</kbd></button>
			<hr />
		{/if}
		{#each views as option (option.mode)}
			<button type="button" role="menuitemradio" aria-checked={view === option.mode} onclick={() => choose(() => changeView(option.mode))}><Icon name={option.icon} size={16} /><span>{t(option.label)}</span></button>
		{/each}
	</ContextMenu>
{/if}

{#if dropReview}
	<Dialog title={t('files.dropTransfer')} onclose={() => (dropReview = null)}>
		<p>{t('files.dropTransferHint', { count: dropReview.source.paths.length })}</p>
		<p class="mono">{safeRoots.find((r) => r.id === dropReview?.source.root)?.name}: {dropReview.source.paths.join(', ')}</p>
		<p class="mono">→ {safeRoots.find((r) => r.id === dropReview?.target)?.name}/{dropReview.path}</p>
		<div class="dialog-actions">
			<button class="btn" onclick={() => (dropReview = null)}>{t('common.cancel')}</button>
			<button class="btn" onclick={() => acceptDrop(false)}>{t('files.copy')}</button>
			<button class="btn btn-warn" disabled={!safeRoots.find((r) => r.id === dropReview?.source.root)?.writable} onclick={() => acceptDrop(true)}>{t('files.move')}</button>
		</div>
	</Dialog>
{/if}
<JobsPanel {canRetry} onopen={(id, path) => open(id, path)} refresh={jobsTick} onfinished={() => { load(); loadRoots(); jobsTick++; }} />
<UploadPanel />
<UndoPanel roots={safeRoots} refresh={jobsTick} onchanged={() => { load(); loadRoots(); jobsTick++; }} />

{#if copying && root && roots}
	<CopyDialog roots={safeRoots} from={copying.from ?? root} paths={copying.paths ?? selected.map((name) => join(dir, name))} move={copying.move} restore={copying.restore} targetRoot={copying.targetRoot} targetDir={copying.targetDir} startDir={dir} onclose={() => (copying = null)} ondone={() => { if (copying?.pasted && copying.move) clipboard = null; copying = null; selected = []; jobsTick++; }} />
{/if}
{#if editing && root}
	<TextEditor root={root.id} stick={root.kind === 'stick'} path={editing} onclose={() => { editing = null; load(); }} />
{/if}
{#if windowsPath && root}<WindowsPath {root} {dir} onclose={() => (windowsPath = false)} />{/if}

{#if dialog}
	{@const kind = dialog.kind}
	<Dialog
		title={kind === 'mkdir'
			? t('files.newFolderTitle')
			: kind === 'rename'
				? t('files.renameTitle', { name: dialog.name })
				: selected.length === 1
					? t('files.deleteTitleOne', { name: selected[0] })
					: t('files.deleteTitleMany', { count: selected.length })}
		onclose={() => (dialog = null)}
	>
		<form onsubmit={submitDialog}>
			{#if kind === 'delete'}
				{#if root?.recycle}
					<label class="field-row"><span>{t('files.deleteAction')}</span>
						<select class="field" bind:value={permanent} onchange={() => { confirmed = false; }}>
							<option value={false}>{t('files.moveToTrash')}</option>
							<option value={true}>{t('files.trashDeleteForever')}</option>
						</select>
					</label>
				{/if}
				<p class={permanent ? 'notice-error' : 'text'}>{t(permanent ? (root?.recycle ? 'files.trashForeverText' : 'files.deletePermanent') : 'files.deleteRecycle')}</p>
				{#if permanent}
					<label class="confirm"><input type="checkbox" bind:checked={confirmed} /><span>{t('files.deleteConfirm')}</span></label>
				{/if}
			{:else}
				<label class="field-row">
					<span>{t(kind === 'mkdir' ? 'files.name' : 'files.newName')}</span>
					<!-- svelte-ignore a11y_autofocus -->
					<input class="field" type="text" bind:value={typed} maxlength="255" autocomplete="off" spellcheck="false" required autofocus />
				</label>
			{/if}
			{#if dialogError}<p class="notice-warn" role="alert">{dialogError}</p>{/if}
			<div class="dialog-actions">
				<button type="button" class="btn" onclick={() => (dialog = null)}>{t('common.cancel')}</button>
				{#if kind === 'delete'}
					<button type="submit" class="btn {permanent ? 'btn-danger' : 'btn-warn'}" disabled={permanent && !confirmed}>{t(permanent ? 'files.trashDeleteForever' : 'files.moveToTrash')}</button>
				{:else}
					<button type="submit" class="btn btn-primary">{t(kind === 'mkdir' ? 'files.create' : 'files.rename')}</button>
				{/if}
			</div>
		</form>
	</Dialog>
{/if}

{#if downloadPaths && root}<DownloadDialog {root} paths={downloadPaths} onclose={() => (downloadPaths = null)} />{/if}
{#if duplicates && root}<DuplicatesDialog root={safeRoots.find((r) => r.id === rootId) ?? root} {dir} {showHidden} onclose={() => (duplicates = false)} onopen={(path) => { duplicates = false; open(rootId, path); }} onchanged={() => { load(); loadRoots(); jobsTick++; }} />{/if}
{#if usage && root}<UsageDialog {root} {dir} {showHidden} onclose={() => (usage = false)} onopen={(path) => { usage = false; open(rootId, path); }} />{/if}
{#if deepSearch && root}<SearchDialog {root} {dir} {showHidden} onclose={() => (deepSearch = false)} onopen={(path) => { deepSearch = false; open(rootId, path); }} />{/if}
{#if comparison && root && rightRoot}<CompareDialog left={safeRoots.find((r) => r.id === rootId) ?? root} leftDir={dir} right={rightRoot} rightDir={right.path} {showHidden} ondone={() => { comparison = false; jobsTick++; }} onclose={() => (comparison = false)} onopen={(id, path) => { comparison = false; open(id, path); }} />{/if}
{#if batchRename && root}<RenameBatchDialog {root} {dir} names={selected} onclose={() => { batchRename = false; load(); jobsTick++; }} ondone={() => { batchRename = false; selected = []; load(); jobsTick++; }} />{/if}
{#if archive && root}<ArchiveDialog {root} path={archive} {dir} roots={safeRoots} onclose={() => (archive = null)} ondone={() => { archive = null; jobsTick++; }} />{/if}
{#if properties}<FileProperties root={properties.root} path={properties.path} locked={properties.root.kind === 'usb' && !safeRoots.find((r) => r.id === properties?.root.id)?.writable && properties.root.writable} onclose={() => (properties = null)} />{/if}
{#if uploadReview}<UploadReview root={uploadReview.root} dir={uploadReview.dir} files={uploadReview.files} onclose={() => (uploadReview = null)} onconfirm={startUpload} />{/if}
{#if favoriteManager}<FavoritesDialog {favorites} onsave={saveFavorites} onclose={() => (favoriteManager = false)} />{/if}
{#if creatingFile && root && writable}<CreateFileDialog root={rootId} {dir} onclose={() => (creatingFile = false)} oncreated={(path) => { creatingFile = false; editing = path; load(); }} />{/if}
{#if otherPreview}<FilePreview {...otherPreview} onclose={() => (otherPreview = null)} />{/if}
{#if shortcuts}
	<Dialog title={t('files.shortcuts')} onclose={() => (shortcuts = false)}>
		<p>{t('files.shortcutsHint')}</p>
		<dl class="file-properties">
			{#each [['↑ ↓ ← →', 'navigate'], ['Enter', 'open'], ['Space', 'preview'], ['Shift + ↑ / ↓', 'range'], ['Ctrl / ⌘ + A', 'all'], ['Ctrl / ⌘ + C / X / V', 'clipboard'], ['F2', 'rename'], ['Delete', 'delete'], ['Shift + F10', 'menu'], ['Esc', 'clear'], ['F1', 'help']] as const as [keys, action] (keys)}<dt class="mono">{keys}</dt><dd>{t(`files.key.${action}`)}</dd>{/each}
		</dl>
		<p class="muted">{t('files.key.doubleClick')}</p>
		<div class="dialog-actions"><button class="btn" onclick={() => (shortcuts = false)}>{t('files.editClose')}</button></div>
	</Dialog>
{/if}
{#if preview && root}<FilePreview root={root.id} path={join(dir, preview.name)} size={preview.size} onprevious={galleryIndex > 0 ? () => (preview = gallery[galleryIndex - 1]) : undefined} onnext={galleryIndex >= 0 && galleryIndex < gallery.length - 1 ? () => (preview = gallery[galleryIndex + 1]) : undefined} onclose={() => (preview = null)} />{/if}
{#if unlockDialog}
	<Dialog title={t('files.unlockBackup')} onclose={() => (unlockDialog = false)}>
		<p>{t('files.unlockBackupHelp')}</p>
		<div class="dialog-actions">
			<button class="btn" onclick={() => (unlockDialog = false)}>{t('common.cancel')}</button>
			<button class="btn btn-warn" onclick={() => { unlockedRoot = rootId; unlockDialog = false; }}>{t('files.unlockBackup')}</button>
		</div>
	</Dialog>
{/if}

<style>
	.layout {
		display: grid;
		grid-template-columns: 240px minmax(0, 1fr);
		gap: var(--gap);
		align-items: start;
	}
	.places {
		display: flex;
		flex-direction: column;
		gap: 2px;
		padding: 8px;
		position: sticky;
		top: calc(var(--header-height, 60px) + var(--gap));
		max-height: calc(100vh - var(--header-height, 60px) - 2 * var(--gap) - 40px);
		overflow: auto;
	}
	.places-toggle {
		display: none;
	}
	.workspace {
		min-width: 0;
		display: grid;
		grid-template-columns: minmax(0, 1fr);
		gap: var(--gap);
		align-items: start;
	}
	.workspace.split {
		grid-template-columns: repeat(2, minmax(0, 1fr));
	}
	.pane-actions {
		grid-column: 1 / -1;
		display: flex;
		gap: 8px;
		flex-wrap: wrap;
	}
	.browser {
		display: flex;
		flex-direction: column;
		min-width: 0;
	}
	.browser.dragging {
		border-color: var(--accent);
	}
	.toolbar {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 8px;
		padding: 10px 12px;
		border-bottom: 1px solid var(--panel-border);
	}
	.toolbar .btn {
		height: 2.375rem;
	}
	.btn.icon {
		width: 2.375rem;
		padding: 0;
		justify-content: center;
	}
	.btn[aria-pressed='true'] {
		border-color: var(--accent);
		color: var(--accent);
	}
	.nav-buttons,
	.segmented {
		display: flex;
		gap: 4px;
	}
	.spacer {
		flex: 1 1 auto;
	}
	.crumbs {
		flex: 1 1 240px;
		min-width: 0;
		min-height: 2.375rem;
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 2px;
		padding: 0 4px;
		border: 1px solid transparent;
		border-radius: var(--radius-ctl);
		font-size: 0.875rem;
		cursor: text;
	}
	.crumbs:hover {
		border-color: var(--border);
	}
	.crumbs button {
		padding: 4px 6px;
		background: transparent;
		border: 0;
		border-radius: var(--radius-small);
		color: var(--text);
		font: inherit;
		cursor: pointer;
	}
	.crumbs button:hover {
		background: var(--chip);
	}
	.crumbs .edit-path {
		display: inline-flex;
		margin-left: auto;
		color: var(--muted);
	}
	.path-form {
		flex: 1 1 320px;
		min-width: 0;
		display: flex;
		gap: 6px;
	}
	.path-form input {
		flex: 1 1 160px;
		min-width: 0;
	}
	.path-message {
		margin: 0;
		padding: 6px 14px;
		border-bottom: 1px solid var(--panel-border);
		font-size: 0.8125rem;
	}
	.search {
		flex: 0 1 260px;
		min-width: 160px;
		position: relative;
		display: flex;
		align-items: center;
	}
	.search :global(svg) {
		position: absolute;
		left: 10px;
		color: var(--muted);
		pointer-events: none;
	}
	.search input {
		width: 100%;
		padding-left: 34px;
	}
	.list-footer {
		margin: 0;
		padding: 10px 14px 0;
		font-size: 0.8125rem;
	}
	.saving {
		position: fixed;
		left: 16px;
		bottom: 44px;
		z-index: 50;
		margin: 0;
		padding: 6px 12px;
		background: var(--surface);
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		box-shadow: var(--shadow-menu);
		color: var(--muted);
		font-size: 0.8125rem;
	}
	.unfinished {
		padding: 12px var(--tile-pad);
		border-bottom: 1px solid var(--border);
	}
	.unfinished-row {
		display: flex;
		align-items: center;
		flex-wrap: wrap;
		gap: 10px;
		margin-top: 10px;
	}
	.unfinished-row > span {
		flex: 1;
		overflow-wrap: anywhere;
	}
	.trash-configure {
		align-self: center;
		margin-bottom: 16px;
	}

	/* The list. */
	.file-entries {
		min-height: 120px;
	}
	.row {
		display: grid;
		grid-template-columns: 22px minmax(0, 1fr) 96px 170px 36px;
		align-items: center;
		gap: 12px;
		cursor: pointer;
		padding: 7px 14px;
		border-bottom: 1px solid var(--panel-border);
		font-size: 0.875rem;
		user-select: none;
	}
	.row:not(.header):hover {
		background: color-mix(in srgb, var(--chip) 60%, transparent);
	}
	.row:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
	.row.header {
		position: sticky;
		top: var(--header-height, 60px);
		z-index: 2;
		padding-top: 4px;
		padding-bottom: 4px;
		background: var(--surface);
		font-size: 0.75rem;
		color: var(--muted);
		cursor: default;
	}
	.row.picked {
		background: color-mix(in srgb, var(--accent) 14%, transparent);
	}
	.row input {
		width: 18px;
		height: 18px;
		margin: 0;
	}
	.sort {
		display: inline-flex;
		align-items: center;
		gap: 4px;
		min-width: 0;
		padding: 4px 6px;
		margin: 0 -6px;
		background: transparent;
		border: 0;
		border-radius: var(--radius-small);
		color: inherit;
		font: inherit;
		cursor: pointer;
	}
	.sort.num {
		justify-content: flex-end;
	}
	.sort:hover:not(:disabled) {
		background: var(--chip);
		color: var(--text);
	}
	.sort.active {
		color: var(--text);
		font-weight: 500;
	}
	.num {
		text-align: right;
		font-size: 0.8125rem;
		white-space: nowrap;
	}
	.entry {
		display: flex;
		align-items: center;
		gap: 10px;
		min-width: 0;
		padding: 0;
		background: transparent;
		border: 0;
		color: var(--text);
		font: inherit;
		text-align: left;
	}
	button.entry {
		cursor: pointer;
	}
	button.entry:hover span {
		text-decoration: underline;
	}
	.entry span {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.entry-more {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		width: 32px;
		height: 32px;
		padding: 0;
		background: transparent;
		border: 1px solid transparent;
		border-radius: var(--radius-ctl);
		color: var(--muted);
		cursor: pointer;
	}
	.entry-more:hover,
	.entry-more[aria-expanded='true'] {
		background: var(--chip);
		border-color: var(--border);
		color: var(--text);
	}

	/* Symbol views: the header keeps the checkbox and the sort buttons in one line. */
	.file-entries.small,
	.file-entries.large {
		padding: 0 12px 12px;
	}
	.small .row.header,
	.large .row.header {
		display: flex;
		gap: 16px;
		margin: 0 -12px 8px;
		padding-left: 26px;
	}
	.small .row:not(.header),
	.large .row:not(.header) {
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		padding: 10px;
		grid-template-columns: 18px minmax(0, 1fr) 32px;
		gap: 8px;
	}
	.small .row:not(.header) > .num,
	.large .row:not(.header) > .num {
		display: none;
	}
	.large .row:not(.header) {
		align-content: start;
	}
	.large .entry {
		grid-column: 1 / -1;
		grid-row: 2;
		flex-direction: column;
		text-align: center;
		min-height: 130px;
		justify-content: center;
	}
	.large .entry span {
		white-space: normal;
		overflow-wrap: anywhere;
		width: 100%;
	}
	.large .entry-more {
		grid-column: 3;
		grid-row: 1;
	}

	/* The bar for the selection. */
	.actions {
		position: sticky;
		bottom: 12px;
		z-index: 5;
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 8px;
		margin: 0 12px 12px;
		padding: 8px 10px 8px 14px;
		background: var(--surface);
		border: 1px solid var(--accent);
		border-radius: var(--radius-ctl);
		box-shadow: var(--shadow-menu);
	}
	.actions .btn {
		height: 2.25rem;
	}
	.actions .more-actions {
		display: none;
	}
	.count {
		margin-right: auto;
		font-size: 0.8125rem;
	}
	.none {
		padding: 20px 14px;
		font-size: 0.875rem;
	}
	.drop {
		display: flex;
		align-items: center;
		justify-content: center;
		gap: 8px;
		margin: 12px;
		padding: 12px;
		border: 1px dashed var(--outline-border);
		border-radius: var(--radius-ctl);
		font-size: 0.8125rem;
		color: var(--muted);
	}
	.drop.active {
		border-color: var(--accent);
		background: color-mix(in srgb, var(--accent) 8%, transparent);
		color: var(--text);
	}
	form {
		display: flex;
		flex-direction: column;
		gap: 16px;
	}
	.path-form {
		flex-direction: row;
		gap: 6px;
	}
	.field-row {
		display: flex;
		flex-direction: column;
		gap: 6px;
		font-size: 0.875rem;
	}
	.text {
		font-size: 0.875rem;
	}
	.file-properties dd {
		margin: 0 0 8px;
		overflow-wrap: anywhere;
	}
	.file-properties dt {
		color: var(--muted);
		font-size: 0.8rem;
	}
	.no-icon {
		width: 16px;
		flex: none;
	}

	@media (max-width: 1100px) {
		.workspace.split {
			grid-template-columns: minmax(0, 1fr);
		}
	}
	@media (max-width: 1180px) {
		.action-bar .label {
			display: none;
		}
	}
	@media (max-width: 760px) {
		.layout {
			grid-template-columns: minmax(0, 1fr);
		}
		.places-toggle {
			display: flex;
			justify-content: flex-start;
			width: 100%;
		}
		.places-toggle span {
			flex: 1;
			text-align: left;
			overflow: hidden;
			text-overflow: ellipsis;
			white-space: nowrap;
		}
		.places {
			display: none;
			position: static;
			max-height: none;
		}
		.places.open {
			display: flex;
		}
		.toolbar .btn,
		.btn.icon {
			height: 2.75rem;
		}
		.btn.icon {
			width: 2.75rem;
		}
		.crumbs {
			order: 5;
			flex-basis: 100%;
		}
		.path-form {
			order: 5;
			flex-basis: 100%;
		}
		.search {
			flex: 1 1 100%;
			order: 6;
		}
		.spacer {
			display: none;
		}
		.row {
			grid-template-columns: 22px minmax(0, 1fr) 76px 36px;
		}
		.row > :nth-child(4) {
			display: none;
		}
		.row.header {
			top: 0;
		}
		.entry-more {
			width: 2.75rem;
			height: 2.75rem;
		}
		.actions .secondary {
			display: none;
		}
		.actions .more-actions {
			display: inline-flex;
		}
		/* Count and "clear" on the first line, the actions below. */
		.actions > * {
			order: 2;
		}
		.actions .count {
			order: 0;
			flex: 1 1 calc(100% - 3.5rem);
		}
		.actions .clear {
			order: 1;
		}
		.actions .btn {
			height: 2.75rem;
		}
	}
</style>
