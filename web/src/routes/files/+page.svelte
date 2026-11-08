<script lang="ts">
	import { openAgent } from '#lib/agent-app.ts';
	import { goto } from '$app/navigation';
	import { poll } from '#lib/poll.ts';
	import { apiPath } from '#lib/cluster-node.ts';
	import { untrack, tick, onMount } from 'svelte';
	import { page } from '$app/state';
	import {
		api,
		ApiError,
		type CopyJob,
		type FileEntry,
		type FileRoot,
		type UploadStatus,
	} from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import { folderPath, rangeSelection, previewKind, selectionType } from '#lib/file-actions.ts';
	import { beginFileDrag, hasFileDrag, takeFileDrag, type FileDrag } from '#lib/file-drag.ts';
	import { fileList } from '#lib/file-list.ts';
	import { preferredFileRoot } from '#lib/file-root.ts';
	import { apiErrorText, i18n, t } from '#lib/i18n/index.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import { isEditableText } from '#lib/text-files.ts';
	import { enqueue, uploads } from '#lib/uploads.svelte.ts';
	import ActionBar from './ActionBar.svelte';
	import ArchiveDialog from './ArchiveDialog.svelte';
	import BackupLock from './BackupLock.svelte';
	import CompareDialog from './CompareDialog.svelte';
	import CopyDialog from './CopyDialog.svelte';
	import CreateFileDialog from './CreateFileDialog.svelte';
	import DownloadDialog from './DownloadDialog.svelte';
	import DropHint from './DropHint.svelte';
	import DuplicatesDialog from './DuplicatesDialog.svelte';
	import EntryDialog from './EntryDialog.svelte';
	import FavoritesDialog from './FavoritesDialog.svelte';
	import FileList from './FileList.svelte';
	import FileMenus from './FileMenus.svelte';
	import FilePreview from './FilePreview.svelte';
	import FileProperties from './FileProperties.svelte';
	import FolderPane from './FolderPane.svelte';
	import JobsPanel from './JobsPanel.svelte';
	import MobileActions from './MobileActions.svelte';
	import MobileLocation from './MobileLocation.svelte';
	import NavBar from './NavBar.svelte';
	import Places from './Places.svelte';
	import RenameBatchDialog from './RenameBatchDialog.svelte';
	import SearchDialog from './SearchDialog.svelte';
	import SelectionBar from './SelectionBar.svelte';
	import ShortcutsDialog from './ShortcutsDialog.svelte';
	import PhotoView from './PhotoView.svelte';
	import TextEditor from './TextEditor.svelte';
	import TrashArea from './TrashArea.svelte';
	import UndoPanel from './UndoPanel.svelte';
	import UnfinishedUploads from './UnfinishedUploads.svelte';
	import UploadPanel from './UploadPanel.svelte';
	import UploadReview from './UploadReview.svelte';
	import UrlDownloadDialog from './UrlDownloadDialog.svelte';
	import DriveFormatDialog from './DriveFormatDialog.svelte';
	import StorageAnalysis from './StorageAnalysis.svelte';
	import WindowsPath from './WindowsPath.svelte';
	import { FileClipboard } from './clipboard.svelte.ts';
	import type { FileCommand } from './commands.ts';
	import { droppedFiles, pickedFiles, type PickedFile } from './drop-files.ts';
	import { frameMessage, inWindow } from '#lib/desktop/frame.ts';
	import Icon from '#lib/components/Icon.svelte';
	import { FileHistory, type Location } from './history.svelte.ts';
	import { fileKeys, type KeyboardHost } from './keyboard.ts';
	import { FilePreferenceStore } from './preferences.svelte.ts';
	import { UsbDrives } from './usb.svelte.ts';
	// PLAN 66: files open in their standard app (a web app running in the browser).
	import { appForFile, loadWebApps, setFileApp } from '#lib/webapps/webapps.svelte.ts';
	import { EDITOR, extOf } from '#lib/webapps/protocol.ts';
	import DockerMountDialog from './DockerMountDialog.svelte';
	import {
		dockerAppForExt,
		fileOpeners,
		mountShare,
		openInDockerApp,
		setDockerFileApp,
		waitMounted,
		type FileOpener,
	} from '#lib/docker-opens.ts';
	import { openInWebApp } from '#lib/webapps/open.ts';

	const canAdminister = $derived(
		session.data?.user?.role === 'owner' || session.data?.user?.role === 'admin',
	);
	const join = (...parts: string[]) => parts.filter(Boolean).join('/');

	// --- Where we are -------------------------------------------------------

	let roots = $state<FileRoot[] | null>(null);
	let rootId = $state('');
	let dir = $state(''); // folder inside the root, "" for its top
	let entries = $state<FileEntry[] | null>(null);
	// Why the open folder could not be listed; shown in the list instead of "empty".
	let listError = $state('');
	let listLoading = $state(false);
	let selected = $state<string[]>([]);
	let anchor = $state('');
	let error = $state('');
	let trash = $state(false);
	/** A volume shows the recycle bin of one of its shares: that share's folder. */
	let trashBin = $state('');
	/** On narrow screens the list of places is folded away behind a button. */
	let placesOpen = $state(false);

	const root = $derived(roots?.find((candidate) => candidate.id === rootId));
	const bins = $derived(root?.kind === 'volume' ? (root.bins ?? []) : []);
	const openBin = $derived(trashBin || (bins.length === 1 ? bins[0] : ''));
	const binRoot = $derived(
		root && openBin ? { ...root, name: openBin, kind: 'share' as const, recycle: true } : undefined,
	);
	let appliedLocation = '';
	$effect(() => {
		const requested = page.url.searchParams.get('root') ?? '';
		const requestedPath = folderPath(page.url.searchParams.get('path') ?? '');
		const location = JSON.stringify([requested, requestedPath]);
		const available = roots;
		if (!requested) {
			appliedLocation = '';
			return;
		}
		if (available && location !== appliedLocation) {
			appliedLocation = location;
			if (requestedPath === null) error = t('files.invalidPath');
			else if (available.some((r) => r.id === requested))
				untrack(() => open(requested, requestedPath));
			else error = t('files.requestedRootUnavailable');
		}
	});

	// --- Dialogs and panels -------------------------------------------------

	let copying = $state<{
		move: boolean;
		restore?: boolean;
		from?: FileRoot;
		paths?: string[];
		targetRoot?: string;
		targetDir?: string;
		pasted?: boolean;
	} | null>(null);
	let editing = $state<string | null>(null);
	let jobsTick = $state(0);
	let windowsPath = $state(false);
	let favoriteManager = $state(false),
		creatingFile = $state(false),
		shortcuts = $state(false),
		downloadingLink = $state(false);
	let selectionFilter = $state('images');
	let downloadPaths = $state<string[] | null>(null),
		usage = $state(false),
		duplicates = $state(false);
	let photos = $state(false);
	let deepSearch = $state(false),
		comparison = $state(false),
		batchRename = $state(false),
		archive = $state<string | null>(null);
	let properties = $state<{ root: FileRoot; path: string } | null>(null);
	let preview = $state<FileEntry | null>(null),
		otherPreview = $state<{ root: string; path: string; size: number } | null>(null);
	let uploadReview = $state<{ root: FileRoot; dir: string; files: PickedFile[] } | null>(null);
	let dropReview = $state<{ source: FileDrag; target: string; path: string } | null>(null);
	let dialog = $state<
		{ kind: 'mkdir' } | { kind: 'rename'; name: string } | { kind: 'delete' } | null
	>(null);
	let dialogBusy = $state(false);
	/** The floating menus: one for entries, one for the empty part of the folder. */
	let menu = $state<{ entry: FileEntry; x: number; y: number } | null>(null);
	let backgroundMenu = $state<{ x: number; y: number } | null>(null);

	let fileInput = $state<HTMLInputElement>();
	let folderInput = $state<HTMLInputElement>();
	let dragging = $state(false);

	// --- Stores -------------------------------------------------------------

	const prefs = new FilePreferenceStore(() => ({ root: rootId, path: dir }));
	const clipboard = new FileClipboard();
	const usb = new UsbDrives({
		admin: () => canAdminister,
		rootId: () => rootId,
		roots: () => roots,
		loadRoots,
		open,
		restore: (from, path) => (copying = { move: false, restore: true, from, paths: [path] }),
		setError: (message) => (error = message),
	});

	// --- Two panes ----------------------------------------------------------

	let right = $state<{ root: string; path: string; selected: string[] }>({
		root: '',
		path: '',
		selected: [],
	});

	function reviewDrop(event: DragEvent, target: string, path: string) {
		if (!hasFileDrag(event)) return;
		event.preventDefault();
		event.stopPropagation();
		dragging = false;
		const source = takeFileDrag(event),
			destination = safeRoots.find((r) => r.id === target),
			origin = safeRoots.find((r) => r.id === source?.root);
		if (!source || !origin || origin.kind === 'stick' || !destination?.writable) return;
		if (source.root === target && source.paths.some((p) => path === p || path.startsWith(p + '/')))
			return;
		dropReview = { source, target, path };
	}
	function acceptDrop(move: boolean) {
		const review = dropReview;
		if (!review) return;
		const from = safeRoots.find((r) => r.id === review.source.root),
			target = safeRoots.find((r) => r.id === review.target);
		if (!from || !target?.writable || (move && !from.writable)) return;
		copying = {
			from,
			paths: review.source.paths,
			move,
			targetRoot: target.id,
			targetDir: review.path,
		};
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
		const sourceDir = direction === 'right' ? dir : right.path,
			targetDir = direction === 'right' ? right.path : dir;
		if (!source || !target?.writable || !names.length || (move && !source.writable)) return;
		copying = {
			from: source,
			paths: names.map((n) => join(sourceDir, n)),
			move,
			targetRoot: target.id,
			targetDir,
		};
	}
	function canRetry(job: CopyJob) {
		return (
			!!safeRoots.find((r) => r.id === job.to)?.writable &&
			(!job.move || !!safeRoots.find((r) => r.id === job.from)?.writable)
		);
	}

	// --- Unfinished uploads ---------------------------------------------------

	// Partial uploads of the folder; UnfinishedUploads offers to finish them.
	let pending = $state<UploadStatus[]>([]);
	let recovering = $state(false);

	// --- List position, history and search ---------------------------------

	let virtualList = $state<{
		getScroll: () => number;
		setScroll: (top: number) => void;
		reveal: (index: number) => void;
		getColumns: () => number;
	}>();
	const steps = new FileHistory();
	let search = $state('');

	onMount(() => {
		void prefs.load();
		void loadWebApps();
	});
	const visibleFavorites = $derived(
		prefs.favorites.filter((f) => roots?.some((r) => r.id === f.root)),
	);

	// --- The list -----------------------------------------------------------

	const visibleEntries = $derived(
		fileList(
			(entries ?? []).filter((entry) => prefs.showHidden || !entry.name.startsWith('.')),
			search,
			prefs.sort,
			prefs.descending,
			i18n.locale,
		),
	);
	// PLAN 70, quick look: the arrows go through every file of the folder in its order.
	const gallery = $derived(visibleEntries.filter((e) => !e.dir));
	const galleryIndex = $derived(preview ? gallery.findIndex((e) => e.name === preview?.name) : -1);
	const selectedNames = $derived(new Set(selected));
	const allSelected = $derived(
		visibleEntries.length > 0 && visibleEntries.every((entry) => selectedNames.has(entry.name)),
	);
	const selectedEntries = $derived(
		(entries ?? []).filter((entry) => selectedNames.has(entry.name)),
	);
	const selectedBytes = $derived(
		selectedEntries.filter((entry) => !entry.dir).reduce((sum, entry) => sum + entry.size, 0),
	);
	const selectedFolders = $derived(selectedEntries.filter((entry) => entry.dir).length);
	const selectionKey = $derived(JSON.stringify([rootId, dir, selected]));
	let measured = $state<{ key: string; bytes: number } | null>(null),
		measuring = $state(false);
	const selectionBytes = $derived(measured?.key === selectionKey ? measured.bytes : selectedBytes);
	/** Selected folders whose size is not known yet. */
	const uncounted = $derived(
		selectedFolders && measured?.key !== selectionKey ? selectedFolders : 0,
	);
	async function measureSelection() {
		const key = selectionKey;
		measuring = true;
		try {
			const result = await api.post<{ bytes: number }>('/files/inspect', {
				root: rootId,
				paths: selected.map((name) => join(dir, name)),
			});
			if (key === selectionKey) measured = { key, bytes: result.bytes };
		} catch (err) {
			if (key === selectionKey) error = apiErrorText(err);
		} finally {
			measuring = false;
		}
	}

	// --- USB drives and their backups ---------------------------------------

	let formattingDrive = $state(false);
	const backupLocked = $derived(!!root && usb.isLocked(root.id, root.kind));
	const writable = $derived(!!root?.writable && !backupLocked);
	const safeRoots = $derived(
		(roots ?? []).map((place) =>
			usb.isLocked(place.id, place.kind) ? { ...place, writable: false } : place,
		),
	);
	const rightRoot = $derived(safeRoots.find((r) => r.id === right.root));
	const currentUSBBackup = $derived(usb.backupAt(rootId, dir));
	$effect(() => usb.watch());

	/** The text editor may save here (for "Öffnen mit › Texteditor"). */
	const writableText = $derived(!backupLocked && !!(root?.editable || writable));
	const editable = $derived.by(() => {
		if (backupLocked || !(root?.editable || writable) || selected.length !== 1) return false;
		const entry = entries?.find((candidate) => candidate.name === selected[0]);
		return !!entry && !entry.dir && isEditableText(entry.name, entry.size);
	});

	// --- Loading ------------------------------------------------------------

	async function loadRoots() {
		try {
			roots = await api.get<FileRoot[]>('/files/roots');
			if (!roots.some((candidate) => candidate.id === rootId))
				open(preferredFileRoot(roots, prefs.rememberedRoot), '');
		} catch (err) {
			error = apiErrorText(err);
		}
	}
	async function load() {
		if (!rootId) {
			entries = null;
			pending = [];
			return;
		}
		const requestedRoot = rootId,
			requestedDir = dir;
		listLoading = true;
		try {
			const query = `root=${encodeURIComponent(requestedRoot)}&path=${encodeURIComponent(requestedDir)}`;
			const [listing, parts] = await Promise.all([
				api.get<FileEntry[]>(`/files/list?${query}`),
				writable ? api.get<UploadStatus[]>(`/files/uploads?${query}`) : Promise.resolve([]),
			]);
			if (rootId !== requestedRoot || dir !== requestedDir) return;
			entries = listing;
			pending = parts;
			const names = new Set(entries.map((entry) => entry.name));
			selected = selected.filter((name) => names.has(name));
			listError = '';
			error = '';
		} catch (err) {
			if (rootId !== requestedRoot || dir !== requestedDir) return;
			entries = null;
			listError = apiErrorText(err);
		} finally {
			if (rootId === requestedRoot && dir === requestedDir) listLoading = false;
		}
	}

	// Looks again every ten seconds, unless the user is in the middle of something.
	let refreshError = $state(false);
	onMount(() => {
		const blocked = () =>
			document.hidden ||
			document.querySelector('dialog[open]') !== null ||
			menu !== null ||
			backgroundMenu !== null ||
			dragging ||
			trash ||
			!rootId ||
			root?.kind === 'stick' ||
			entries === null ||
			['INPUT', 'TEXTAREA', 'SELECT'].includes(document.activeElement?.tagName ?? '');
		async function refresh(signal: AbortSignal) {
			const id = rootId,
				path = dir,
				before = entries;
			try {
				const listing = await api.get<FileEntry[]>(
					'/files/list?' + new URLSearchParams({ root: id, path }),
					signal,
				);
				if (signal.aborted || id !== rootId || path !== dir || before !== entries || blocked())
					return;
				refreshError = false;
				if (JSON.stringify(listing) === JSON.stringify(entries)) return;
				const first = Array.from(document.querySelectorAll<HTMLElement>('[data-file-name]')).find(
					(e) => e.getBoundingClientRect().top >= 0,
				);
				const name = first?.dataset.fileName,
					offset = first?.getBoundingClientRect().top,
					scroll = window.scrollY;
				entries = listing;
				const names = new Set(listing.map((e) => e.name));
				selected = selected.filter((n) => names.has(n));
				await tick();
				if (signal.aborted || id !== rootId || path !== dir) return;
				const next = Array.from(document.querySelectorAll<HTMLElement>('[data-file-name]')).find(
					(e) => e.dataset.fileName === name,
				);
				window.scrollTo({
					top:
						next && offset !== undefined
							? window.scrollY + next.getBoundingClientRect().top - offset
							: scroll,
					behavior: 'instant',
				});
			} catch {
				if (!signal.aborted && id === rootId && path === dir) refreshError = true;
			}
		}
		return poll(refresh, { interval: 10000, immediate: false, skip: blocked });
	});

	// At the start, and whenever an upload finishes: it may belong to the
	// folder on screen, and it changes the free space.
	$effect(() => {
		uploads.finished;
		if (!prefs.ready) return;
		untrack(() => {
			loadRoots();
			load();
		});
	});

	// --- Navigation -----------------------------------------------------------

	let pathEditing = $state(false);
	let pathInput = $state('');
	let pathMessage = $state('');
	let mobileSearch = $state(false);
	let navBar = $state<ReturnType<typeof NavBar>>();

	function captureLocation(): Location {
		return {
			root: rootId,
			path: dir,
			search,
			selected: [...selected],
			scroll: window.scrollY,
			listScroll: virtualList?.getScroll() ?? 0,
		};
	}
	function historyGo(direction: 'back' | 'forward') {
		const target = steps.go(direction, captureLocation);
		if (target) navigate(target.root, target.path, target);
	}
	function open(id: string, path: string) {
		steps.leave(rootId && (rootId !== id || dir !== path) ? captureLocation() : null);
		placesOpen = false;
		navigate(id, path);
	}
	async function navigate(id: string, path: string, restore?: Location) {
		const chosen = roots?.find((place) => place.id === id);
		if ((chosen?.kind === 'share' || chosen?.kind === 'volume') && prefs.rememberedRoot !== id) {
			prefs.rememberedRoot = id;
			if (rootId) prefs.save();
		}
		if (rootId !== id) usb.unlockedRoot = '';
		rootId = id;
		dir = path;
		prefs.applyFolderView();
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
		listError = '';
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

	// --- Selection and mouse ------------------------------------------------

	function toggle(name: string, checked: boolean) {
		anchor = name;
		selected = checked ? [...selected, name] : selected.filter((other) => other !== name);
	}
	function selectRange(name: string, add = false) {
		const range = rangeSelection(
			visibleEntries.map((e) => e.name),
			anchor,
			name,
		);
		selected = add ? [...new Set([...selected, ...range])] : range;
	}
	function selectType() {
		selected = visibleEntries
			.filter((e) =>
				selectionFilter === 'folders' ? e.dir : !e.dir && selectionType(e.name) === selectionFilter,
			)
			.map((e) => e.name);
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
		if (
			(event.target instanceof Element && event.target.closest('input')) ||
			event.shiftKey ||
			event.ctrlKey ||
			event.metaKey
		)
			return;
		selected = [entry.name];
		anchor = entry.name;
		if (openWithStandard(entry)) return;
		if (previewKind(entry.name, entry.size)) preview = entry;
		else if (editable) editing = join(dir, entry.name);
	}
	/** PLAN 66: the standard app of the file type (Dateien › Einstellungen › Standard-Apps). */
	/** Quick look of the next file: it becomes the selection, like in the list. */
	function quickLook(entry: FileEntry) {
		preview = entry;
		selected = [entry.name];
		anchor = entry.name;
	}
	// PLAN 74 (owner 8.11.2026: hint only): "._name" is the Finder data of a
	// Mac (AppleDouble), not the file itself; opening it explains that.
	let companion = $state<{ file: FileEntry; real: FileEntry | null } | null>(null);
	function macCompanion(entry: FileEntry): boolean {
		if (entry.dir || !entry.name.startsWith('._') || entry.name.length < 3) return false;
		const name = entry.name.slice(2);
		companion = { file: entry, real: entries?.find((e) => !e.dir && e.name === name) ?? null };
		return true;
	}
	function openWithStandard(entry: FileEntry): boolean {
		if (macCompanion(entry)) return true;
		// PLAN 72: a standard Docker app comes first (choosing one clears the other).
		const docker = dockerAppForExt(extOf(entry.name));
		if (docker) {
			void openDocker(entry, docker, false);
			return true;
		}
		const choice = appForFile(entry.name);
		if (
			choice === EDITOR &&
			isEditableText(entry.name, entry.size) &&
			(root?.editable || writable)
		) {
			editing = join(dir, entry.name);
			return true;
		}
		if (!choice?.startsWith('webapp:')) return false;
		openInWebApp(choice, rootId, join(dir, entry.name));
		return true;
	}
	/** "Öffnen mit": an app chosen for this file, and with "always" as its standard app. */
	async function openWith(entry: FileEntry, app: string, always: boolean) {
		if (macCompanion(entry)) return;
		if (app.startsWith('docker:')) return openDocker(entry, app.slice(7), always);
		if (always) {
			try {
				await setFileApp(extOf(entry.name), app);
				if (dockerAppForExt(extOf(entry.name))) await setDockerFileApp(extOf(entry.name), null);
			} catch (e) {
				error = apiErrorText(e);
			}
		}
		if (app === EDITOR) editing = join(dir, entry.name);
		else openInWebApp(app, rootId, join(dir, entry.name));
	}

	// --- PLAN 72: „Öffnen mit“ für Docker-Apps --------------------------------

	let dockerMount = $state<{
		opener: FileOpener;
		root: string;
		path: string;
		file: string;
		busy: boolean;
		error: string;
	} | null>(null);

	async function openDocker(entry: FileEntry, app: string, always: boolean) {
		const ext = extOf(entry.name);
		if (always) {
			try {
				await setDockerFileApp(ext, app);
				if (appForFile(entry.name)?.startsWith('webapp:')) await setFileApp(ext, null);
			} catch (e) {
				error = apiErrorText(e);
			}
		}
		const root = rootId;
		const path = join(dir, entry.name);
		const opener = (await fileOpeners(root, path)).find((o) => o.app === app);
		if (!opener) {
			error = t('files.openWith.dockerGone');
			return;
		}
		if (opener.mounted) return startDocker(app, root, path);
		if (opener.canMount && opener.share) {
			dockerMount = { opener, root, path, file: entry.name, busy: false, error: '' };
			return;
		}
		error = opener.share
			? t('files.openWith.askAdmin', { share: opener.share, app: opener.name })
			: t('files.openWith.outside');
	}

	async function startDocker(app: string, root: string, path: string) {
		try {
			await openInDockerApp(app, root, path);
		} catch (e) {
			error = apiErrorText(e);
		}
	}

	async function confirmMount(readOnly: boolean) {
		const m = dockerMount;
		if (!m?.opener.share) return;
		m.busy = true;
		m.error = '';
		try {
			await mountShare(m.opener.app, m.opener.share, readOnly);
			if (!(await waitMounted(m.opener.app, m.root, m.path))) {
				m.busy = false;
				m.error = t('files.openWith.mountSlow', { app: m.opener.name });
				return;
			}
		} catch (e) {
			m.busy = false;
			m.error = apiErrorText(e);
			return;
		}
		dockerMount = null;
		await startDocker(m.opener.app, m.root, m.path);
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
		if (
			trash ||
			!root ||
			(event.target instanceof Element &&
				event.target.closest('.row:not(.header),button,input,select,a,[role="menu"]'))
		)
			return;
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

	function rememberSelection(move: boolean) {
		if (!root || !selected.length || root.kind === 'stick' || (move && !writable)) return;
		clipboard.remember(root, dir, selected, move);
		menu = null;
	}
	const canPaste = $derived(clipboard.canPaste(writable, safeRoots));
	function paste() {
		if (!clipboard.content || !canPaste) return;
		copying = { ...clipboard.content, targetRoot: rootId, targetDir: dir, pasted: true };
		backgroundMenu = null;
	}
	const keys: KeyboardHost = {
		ignored: () => trash || !root || !!menu || !!backgroundMenu,
		entries: () => visibleEntries,
		selected: () => selected,
		anchor: () => anchor,
		setAnchor: (name) => (anchor = name),
		pick: (name) => {
			selected = [name];
			anchor = name;
		},
		selectRange: (name) => selectRange(name),
		list: () => virtualList,
		writable: () => writable,
		stick: () => root?.kind === 'stick',
		editable: () => editable,
		hasClipboard: () => !!clipboard.content,
		menuAt: entryMenu,
		run: command,
	};

	// --- Actions --------------------------------------------------------------

	function download() {
		if (!entries || selected.length === 0) return;
		const single =
			selected.length === 1 ? entries.find((entry) => entry.name === selected[0]) : undefined;
		// Folders and several entries are packed; the dialog asks for the name of the ZIP.
		if (!single || single.dir) {
			downloadPaths = selected.map((name) => join(dir, name));
			return;
		}
		// The browser saves the answer; nothing passes through this page's memory.
		location.href = apiPath(
			'/files/download?' + new URLSearchParams({ root: rootId, path: join(dir, single.name) }),
		);
	}
	function show(kind: 'mkdir' | 'rename' | 'delete') {
		if (!writable || dialogBusy) return;
		dialog = kind === 'rename' ? { kind, name: selected[0] } : { kind };
	}
	async function dialogDone() {
		dialog = null;
		jobsTick++;
		selected = [];
		await load();
		loadRoots();
	}

	/** Carries out a command of a bar or a menu; `entry` is the one a menu was opened on. */
	function command(action: FileCommand, entry?: FileEntry) {
		switch (action) {
			case 'up':
				return open(rootId, dir.split('/').slice(0, -1).join('/'));
			case 'back':
			case 'forward':
				return historyGo(action);
			case 'editPath':
				return void navBar?.editPath();
			case 'favorite':
				return prefs.toggleFavorite();
			case 'mkdir':
			case 'rename':
			case 'delete':
				return show(action);
			case 'newFile':
				creatingFile = true;
				return;
			case 'upload':
				return fileInput?.click();
			case 'uploadFolder':
				return folderInput?.click();
			case 'urlDownload':
				downloadingLink = true;
				return;
			case 'selectAll':
				selected = visibleEntries.map((e) => e.name);
				return;
			case 'clearSelection':
				selected = [];
				return;
			case 'invertSelection':
				selected = visibleEntries.filter((e) => !selectedNames.has(e.name)).map((e) => e.name);
				return;
			case 'selectType':
				return selectType();
			case 'openFolder':
				if (entry) open(rootId, join(dir, entry.name));
				return;
			case 'preview':
				if (entry) preview = entry;
				return;
			case 'edit':
				editing = join(dir, selected[0]);
				return;
			case 'download':
				return download();
			case 'archive':
				if (entry) archive = join(dir, entry.name);
				return;
			case 'properties':
				if (entry && root) properties = { root, path: join(dir, entry.name) };
				return;
			case 'agent': {
				// Opens the KI-Agent with the chosen files attached (PLAN 44).
				const names = selected.length ? selected : entry ? [entry.name] : [];
				const attach = names.map((n) => `${rootId}:${join(dir, n)}`).join('|');
				openAgent('/agent?' + new URLSearchParams({ attach }).toString(), goto);
				return;
			}
			case 'clipboardCopy':
			case 'cut':
				return rememberSelection(action === 'cut');
			case 'paste':
				return paste();
			case 'copy':
			case 'move':
				copying = { move: action === 'move' };
				return;
			case 'restore':
				copying = { move: false, restore: true };
				return;
			case 'renameBatch':
				batchRename = true;
				return;
			case 'deepSearch':
				deepSearch = true;
				return;
			case 'usage':
				usage = true;
				return;
			case 'duplicates':
				duplicates = true;
				return;
			case 'compare':
				comparison = true;
				return;
			case 'photos':
				photos = true;
				return;
			case 'shortcuts':
				shortcuts = true;
				return;
			case 'trash':
				trash = true;
				trashBin = '';
				selected = [];
				return;
			case 'twoPanes':
				prefs.split = !prefs.split;
				prefs.save();
				return;
			case 'showHidden':
				prefs.showHidden = !prefs.showHidden;
				selected = [];
				prefs.save();
				return;
			case 'folderView':
				return prefs.toggleFolderView();
			default:
				return prefs.changeView(action);
		}
	}

	// --- Uploads --------------------------------------------------------------

	function upload(files: PickedFile[]) {
		if (root && writable && files.length) uploadReview = { root, dir, files };
	}
	// PLAN 61: files dropped on the desktop arrive here (Dateien in a window);
	// they wait until a writable folder is open, then the upload review asks.
	let fromDesktop = $state<PickedFile[]>([]);
	onMount(() => {
		const take = (event: MessageEvent) => {
			const msg = frameMessage(event);
			if (msg?.type !== 'titanos-upload' || event.source !== window.parent) return;
			const files = (msg as unknown as { files: unknown[] }).files.filter(
				(f): f is File => f instanceof File,
			);
			window.parent.postMessage({ type: 'titanos-upload-ok' }, location.origin);
			if (!fromDesktop.length) fromDesktop = files.map((file) => ({ file, path: file.name }));
		};
		window.addEventListener('message', take);
		return () => window.removeEventListener('message', take);
	});
	// PLAN 69: in a window the places are the window's one side bar (the
	// window draws none of its own here); folding it stays the desktop's setting.
	let sideFolded = $state(false);
	onMount(() => {
		if (!inWindow) return;
		const side = (event: MessageEvent) => {
			const msg = frameMessage(event);
			if (msg?.type === 'titanos-side' && event.source === window.parent) sideFolded = msg.folded;
		};
		window.addEventListener('message', side);
		window.parent.postMessage({ type: 'titanos-side-get' }, location.origin);
		return () => window.removeEventListener('message', side);
	});
	function foldSide() {
		window.parent.postMessage({ type: 'titanos-side-fold' }, location.origin);
	}
	$effect(() => {
		if (!fromDesktop.length || !root || !writable) return;
		upload(fromDesktop);
		fromDesktop = [];
	});
	/** Creates the folders of an uploaded tree, parents first, then queues the files. */
	async function startUpload(
		files: { file: File; path: string; conflict: '' | 'rename' | 'replace' }[],
	) {
		const review = uploadReview;
		if (!review || !safeRoots.find((r) => r.id === review.root.id)?.writable)
			throw new ApiError(403, 'read_only_share', '');
		const target = review.dir;
		const folders = new Set<string>();
		for (const { path } of files) {
			const parts = path.split('/').slice(0, -1);
			for (let depth = 1; depth <= parts.length; depth++)
				folders.add(parts.slice(0, depth).join('/'));
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
			files.map(({ file, path, conflict }) => ({
				file,
				conflict,
				dir: join(target, path.split('/').slice(0, -1).join('/')),
			})),
			review.root,
		);
		uploadReview = null;
		load();
	}
	async function dropped(event: DragEvent) {
		if (hasFileDrag(event)) {
			reviewDrop(event, rootId, dir);
			return;
		}
		event.preventDefault();
		dragging = false;
		if (!writable || !event.dataTransfer) return;
		const { files, failed } = await droppedFiles(event.dataTransfer);
		// The rest is still offered for upload; the unreadable ones are named.
		if (failed.length)
			error = t('files.dropUnreadable', {
				count: failed.length,
				names: failed.slice(0, 5).join(', ') + (failed.length > 5 ? ' …' : ''),
			});
		upload(files);
	}

	// PLAN 67-1: the menu bar of the desktop runs the same commands as the bars.
	import { registerMenuCommands } from '#lib/menu-commands.svelte.ts';
	import { hasTrash } from './commands.ts';
	const canWrite = () => !!root && writable && !trash;
	const some = () => selected.length > 0 && !trash;
	registerMenuCommands({
		'files.mkdir': { run: () => command('mkdir'), enabled: canWrite },
		'files.newFile': { run: () => command('newFile'), enabled: canWrite },
		'files.upload': { run: () => command('upload'), enabled: canWrite },
		'files.uploadFolder': { run: () => command('uploadFolder'), enabled: canWrite },
		'files.download': { run: () => command('download'), enabled: some },
		'files.zip': {
			run: () => (downloadPaths = selected.map((name) => join(dir, name))),
			enabled: some,
		},
		'files.rename': {
			run: () => command('rename'),
			enabled: () => canWrite() && selected.length === 1,
		},
		'files.delete': { run: () => command('delete'), enabled: () => canWrite() && some() },
		'files.trash': { run: () => command('trash'), enabled: () => hasTrash(root) },
		'files.cut': { run: () => command('cut'), enabled: () => canWrite() && some() },
		'files.copy': { run: () => command('clipboardCopy'), enabled: some },
		'files.paste': {
			run: () => command('paste'),
			enabled: () => canWrite() && !!clipboard.content,
		},
		'files.selectAll': { run: () => command('selectAll'), enabled: () => !!root && !trash },
		'files.search': { run: () => command('deepSearch'), enabled: () => !!root },
		...Object.fromEntries(
			(['list', 'small', 'large'] as const).map((v) => [
				'files.view.' + v,
				{ run: () => command(v), checked: () => prefs.view === v },
			]),
		),
		...Object.fromEntries(
			(['name', 'size', 'modified'] as const).map((s) => [
				'files.sort.' + s,
				{ run: () => prefs.sort !== s && prefs.sortBy(s), checked: () => prefs.sort === s },
			]),
		),
		'files.hidden': { run: () => command('showHidden'), checked: () => prefs.showHidden },
		'files.twoPanes': { run: () => command('twoPanes'), checked: () => prefs.split },
		'files.back': { run: () => command('back'), enabled: () => !!steps.back.length },
		'files.forward': { run: () => command('forward'), enabled: () => !!steps.forward.length },
		'files.up': { run: () => command('up'), enabled: () => !!root && dir !== '' },
		'files.goto': { run: () => command('editPath'), enabled: () => !!root },
		'files.favorites': () => (favoriteManager = true),
		'files.preview': { run: () => one() && command('preview', one()), enabled: () => !!one() },
		'files.properties': {
			run: () => one() && command('properties', one()),
			enabled: () => !!one(),
		},
		'files.folderView': {
			run: () => command('folderView'),
			enabled: () => !!root,
			checked: () => prefs.folderViewSaved,
		},
		'files.undo': { run: () => undoPanel?.undoLast(), enabled: () => !!undoPanel?.canUndo() },
		// "Öffnen mit ›": the apps for the type of the one file chosen (as in its context menu).
		'files.openWith': {
			items: () => {
				const entry = one();
				if (!entry || entry.dir) return [];
				const standard = appForFile(entry.name);
				const list = appsFor(extOf(entry.name), webapps.apps).map((a) => ({
					id: webAppKey(a.id),
					text: a.name,
					checked: webAppKey(a.id) === standard,
				}));
				if (writableText && isEditableText(entry.name, entry.size))
					list.push({
						id: EDITOR,
						text: t('files.openWith.editor'),
						checked: standard === EDITOR,
					});
				return list;
			},
			run: (app) => {
				const entry = one();
				if (entry && app) void openWith(entry, app, false);
			},
		},
		// "Gehe zu › Freigaben": the places of the side bar.
		'files.shares': {
			items: () => (roots ?? []).map((r) => ({ id: r.id, text: r.name, checked: r.id === rootId })),
			run: (id) => id && open(id, ''),
		},
		// "Gehe zu › Zuletzt": the folders visited before, newest first.
		'files.recent': {
			items: () => {
				const seen = new Set<string>();
				const out: { id: string; text: string }[] = [];
				for (const step of [...steps.back].reverse()) {
					const key = step.root + '\n' + step.path;
					if (seen.has(key) || (step.root === rootId && step.path === dir)) continue;
					seen.add(key);
					const name = roots?.find((r) => r.id === step.root)?.name ?? step.root;
					out.push({ id: key, text: step.path ? `${name} / ${step.path}` : name });
				}
				return out.slice(0, 15);
			},
			run: (key) => {
				const [id, ...path] = (key ?? '').split('\n');
				if (id) open(id, path.join('\n'));
			},
		},
	});
	import { appsFor, webAppKey } from '#lib/webapps/protocol.ts';
	import { webapps } from '#lib/webapps/webapps.svelte.ts';
	let undoPanel = $state<ReturnType<typeof UndoPanel>>();
	/** The one entry chosen, if exactly one is. */
	const one = () =>
		selected.length === 1 && !trash ? entries?.find((e) => e.name === selected[0]) : undefined;
</script>

<svelte:window onkeydown={(event) => fileKeys(event, keys)} />
<div class="files-pagehead"><PageHead title={t('files.title')} /></div>
{#if prefs.error}<p class="notice-error" role="alert">
		{prefs.error}
		<button class="btn" onclick={() => (prefs.ready ? prefs.save() : prefs.load())}
			>{t('files.retryPreferences')}</button
		>
	</p>{/if}
{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
<!-- Fixed, so that saving never moves the page. -->
{#if prefs.saving}<p class="saving" class:windowed={inWindow} role="status">
		{t('files.savingPreferences')}
	</p>{/if}

{#if roots && roots.length === 0}
	<EmptyState
		title={t('files.noRoots')}
		text={canAdminister ? t('files.noRootsAdmin') : undefined}
	/>
{:else if roots}
	<div class="layout" class:windowed={inWindow} class:folded={inWindow && sideFolded}>
		<MobileLocation
			{root}
			{dir}
			bind:placesOpen
			canBack={!!steps.back.length}
			onback={() => historyGo('back')}
		/>
		<nav
			id="file-places"
			class="places"
			class:tile={!inWindow}
			class:windowed={inWindow}
			class:folded={inWindow && sideFolded}
			class:open={placesOpen}
			aria-label={t('files.places')}
		>
			<div class="side-tree">
				<Places
					windowed={inWindow}
					{roots}
					current={trash ? '' : rootId}
					{dir}
					showHidden={prefs.showHidden}
					admin={canAdminister}
					usbDevices={usb.devices}
					usbBusy={usb.busy}
					usbLoadError={usb.loadError}
					backups={usb.backups}
					backupLoadError={usb.backupLoadError}
					backupProgressError={usb.progressError}
					openingBackup={usb.opening}
					favorites={visibleFavorites}
					onopen={open}
					onusb={(device, action) => usb.action(device, action)}
					onbackup={(backup, restore) => usb.openBackup(backup, restore)}
					onrefreshbackups={() => {
						usb.loadBackups();
						usb.load();
					}}
					onmanagefavorites={() => (favoriteManager = true)}
					onremovefavorite={(favorite) =>
						prefs.saveFavorites(prefs.favorites.filter((f) => f !== favorite))}
					onformat={() => (formattingDrive = true)}
				/>
			</div>
			{#if inWindow}
				<!-- PLAN 69: the program's other entries and folding, as in every window's side bar. -->
				<div class="side-foot">
					<span class="side-group">{t('desktop.side.settings')}</span>
					<a
						class="side-item"
						href="/settings/files"
						title={sideFolded ? t('desktop.side.filesSettings') : undefined}
						aria-label={sideFolded ? t('desktop.side.filesSettings') : undefined}
						><Icon name="gear" size={17} /><span>{t('desktop.side.filesSettings')}</span></a
					>
					<button
						type="button"
						class="side-item side-fold"
						onclick={foldSide}
						title={sideFolded ? t('desktop.window.unfold') : undefined}
						aria-label={sideFolded ? t('desktop.window.unfold') : undefined}
						><Icon name="sidebar" size={17} /><span>{t('desktop.window.fold')}</span></button
					>
				</div>
			{/if}
		</nav>
		{#if !trash}
			<MobileActions
				{root}
				{dir}
				{writable}
				canForward={!!steps.forward.length}
				isFavorite={prefs.isFavorite}
				clipboard={!!clipboard.content}
				{canPaste}
				split={prefs.split}
				canCompare={!!rightRoot}
				preferencesReady={prefs.ready}
				folderViewSaved={prefs.folderViewSaved}
				folderViewsFull={prefs.folderViews.length >= 200}
				selected={selected.length}
				bind:search
				bind:searchOpen={mobileSearch}
				oncommand={command}
				onsearch={() => {
					selected = [];
					anchor = '';
				}}
				onmore={(button) => {
					const entry = selectedEntries.at(-1);
					if (entry) menuFromButton(entry, button);
				}}
			/>
		{/if}

		<div class="workspace" class:split={prefs.split}>
			{#if prefs.split && !trash && (selected.length || right.selected.length) && root?.kind !== 'stick'}
				<div class="pane-actions">
					{#if selected.length}
						<button
							class="btn"
							disabled={!rightRoot?.writable}
							onclick={() => transferPane('right', false)}>{t('files.copyRight')} →</button
						>
						<button
							class="btn"
							disabled={!writable || !rightRoot?.writable}
							onclick={() => transferPane('right', true)}>{t('files.moveRight')} →</button
						>
					{/if}
					{#if right.selected.length}
						<button class="btn" disabled={!writable} onclick={() => transferPane('left', false)}
							>← {t('files.copyLeft')}</button
						>
						<button
							class="btn"
							disabled={!rightRoot?.writable || !writable}
							onclick={() => transferPane('left', true)}>← {t('files.moveLeft')}</button
						>
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
					<TrashArea
						{root}
						{binRoot}
						{bins}
						{openBin}
						bind:trashBin
						user={session.data?.user?.name ?? ''}
						admin={canAdminister}
						onback={() => {
							trash = false;
							load();
						}}
						onchanged={() => {
							load();
							loadRoots();
						}}
					/>
				{:else if usage && root}
					<StorageAnalysis
						root={safeRoots.find((r) => r.id === rootId) ?? root}
						{dir}
						showHidden={prefs.showHidden}
						onback={() => (usage = false)}
						onopen={(path) => {
							usage = false;
							open(rootId, path);
						}}
						onduplicates={() => (duplicates = true)}
						onchanged={() => {
							load();
							loadRoots();
							jobsTick++;
						}}
					/>
				{:else}
					<NavBar
						bind:this={navBar}
						{root}
						{rootId}
						{dir}
						canBack={!!steps.back.length}
						canForward={!!steps.forward.length}
						isFavorite={prefs.isFavorite}
						favoritesFull={prefs.favorites.length >= 50}
						bind:search
						bind:editing={pathEditing}
						bind:message={pathMessage}
						bind:input={pathInput}
						onopen={open}
						oncommand={command}
						onwindowspath={() => (windowsPath = true)}
						onreload={() => {
							load();
							loadRoots();
						}}
						onsearch={() => {
							selected = [];
							anchor = '';
						}}
					/>
					<ActionBar
						{root}
						{writable}
						clipboard={!!clipboard.content}
						{canPaste}
						selected={selected.length}
						split={prefs.split}
						canCompare={!!rightRoot}
						view={prefs.view}
						preferencesReady={prefs.ready}
						showHidden={prefs.showHidden}
						folderViewSaved={prefs.folderViewSaved}
						folderViewsFull={prefs.folderViews.length >= 200}
						bind:selectionFilter
						bind:fileInput
						bind:folderInput
						onpicked={(input) => upload(pickedFiles(input))}
						oncommand={command}
					/>

					<UnfinishedUploads
						{pending}
						{root}
						{dir}
						bind:recovering
						onchanged={load}
						onerror={(message) => (error = message)}
					/>
					{#if root?.kind === 'usb' && usb.mayHoldBackup(rootId)}
						<BackupLock {usb} {rootId} locked={backupLocked} onlock={() => clipboard.dropCut()} />
					{/if}
					{#if root && !writable}<p class="notice-warn">
							{t(
								backupLocked
									? 'files.rightReason.usb'
									: root.kind === 'share'
										? 'files.rightReason.share'
										: root.kind === 'stick'
											? 'files.rightReason.stick'
											: 'files.rightReason.readOnly',
							)}
						</p>{/if}
					{#if refreshError}<p class="notice-warn" role="status">
							{t('files.autoRefreshFailed')}
						</p>{/if}

					<FileList
						bind:virtualList
						items={visibleEntries}
						{entries}
						{root}
						{rootId}
						{dir}
						{search}
						{listError}
						{listLoading}
						view={prefs.view}
						sort={prefs.sort}
						descending={prefs.descending}
						preferencesReady={prefs.ready}
						{selectedNames}
						{allSelected}
						{writable}
						menuName={menu?.entry.name}
						onload={load}
						onselectall={(checked) =>
							(selected = checked ? visibleEntries.map((entry) => entry.name) : [])}
						onsort={(column) => prefs.sortBy(column)}
						ontoggle={toggle}
						onrange={selectRange}
						onrowclick={rowClicked}
						onrowdblclick={rowDoubleClicked}
						onfolderclick={folderClick}
						onmenu={entryMenu}
						onmenubutton={menuFromButton}
						ondragentry={dragEntry}
						ondropon={(event, path) => reviewDrop(event, rootId, path)}
					/>

					<p class="list-footer muted" role="status">
						{t('files.resultCount', {
							shown: visibleEntries.length,
							total: entries?.length ?? 0,
						})}{#if clipboard.content}
							· {t(clipboard.content.move ? 'files.cutCount' : 'files.copiedCount', {
								count: clipboard.content.paths.length,
							})}{/if}
					</p>
					{#if root?.kind === 'stick'}<p class="muted none">{t('files.stickNote')}</p>{/if}
					{#if writable}<DropHint {dragging} path={join(root?.name ?? '', dir)} />{/if}

					{#if selected.length > 0}
						<SelectionBar
							count={selected.length}
							bytes={selectionBytes}
							folders={selectedFolders}
							{uncounted}
							{measuring}
							backup={currentUSBBackup}
							stick={root?.kind === 'stick'}
							{editable}
							{writable}
							onmeasure={measureSelection}
							oncommand={command}
							onmore={(button) => {
								const entry = selectedEntries.at(-1);
								if (entry) menuFromButton(entry, button);
							}}
						/>
					{/if}
				{/if}
			</section>
			{#if prefs.split}<FolderPane
					onpreview={(id, path, entry) => (otherPreview = { root: id, path, size: entry.size })}
					ondropfiles={reviewDrop}
					roots={safeRoots}
					refresh={jobsTick + uploads.finished}
					showHidden={prefs.showHidden}
					onchange={(id, path, names) => (right = { root: id, path, selected: names })}
				/>{/if}
		</div>
	</div>
{/if}

<FileMenus
	{menu}
	background={backgroundMenu}
	{root}
	{selected}
	{editable}
	{writable}
	{canPaste}
	view={prefs.view}
	{writableText}
	{dir}
	onopenwith={(entry, app, always) => choose(() => void openWith(entry, app, always))}
	oncommand={(action, entry) => choose(() => command(action, entry))}
	onclose={() => {
		menu = null;
		backgroundMenu = null;
	}}
/>

{#if dockerMount}
	<DockerMountDialog
		app={dockerMount.opener.name}
		share={dockerMount.opener.share ?? ''}
		file={dockerMount.file}
		busy={dockerMount.busy}
		error={dockerMount.error}
		onconfirm={(readOnly) => void confirmMount(readOnly)}
		onclose={() => {
			if (!dockerMount?.busy) dockerMount = null;
		}}
	/>
{/if}
{#if dropReview}
	<Dialog title={t('files.dropTransfer')} onclose={() => (dropReview = null)}>
		<p>{t('files.dropTransferHint', { count: dropReview.source.paths.length })}</p>
		<p class="mono">
			{safeRoots.find((r) => r.id === dropReview?.source.root)?.name}: {dropReview.source.paths.join(
				', ',
			)}
		</p>
		<p class="mono">
			→ {safeRoots.find((r) => r.id === dropReview?.target)?.name}/{dropReview.path}
		</p>
		<div class="dialog-actions">
			<button class="btn" onclick={() => (dropReview = null)}>{t('common.cancel')}</button>
			<button class="btn" onclick={() => acceptDrop(false)}>{t('files.copy')}</button>
			<button
				class="btn btn-warn"
				disabled={!safeRoots.find((r) => r.id === dropReview?.source.root)?.writable}
				onclick={() => acceptDrop(true)}>{t('files.move')}</button
			>
		</div>
	</Dialog>
{/if}
{#if companion}
	<Dialog title={t('files.macCompanion.title')} onclose={() => (companion = null)}>
		<p>{t('files.macCompanion.text', { name: companion.file.name })}</p>
		<p>
			{companion.real
				? t('files.macCompanion.real', { name: companion.real.name })
				: t('files.macCompanion.noReal', { name: companion.file.name.slice(2) })}
		</p>
		<div class="dialog-actions">
			<button class="btn" onclick={() => (companion = null)}>{t('common.close')}</button>
			{#if companion.real}<button
					class="btn btn-primary"
					onclick={() => {
						const real = companion?.real;
						companion = null;
						if (!real) return;
						selected = [real.name];
						anchor = real.name;
						if (openWithStandard(real)) return;
						if (previewKind(real.name, real.size)) preview = real;
						else if (editable) editing = join(dir, real.name);
					}}>{t('files.macCompanion.open', { name: companion.real.name })}</button
				>{/if}
		</div>
	</Dialog>
{/if}
<JobsPanel
	{canRetry}
	onopen={(id, path) => open(id, path)}
	refresh={jobsTick}
	onfinished={() => {
		load();
		loadRoots();
		jobsTick++;
	}}
/>
<UploadPanel />
<UndoPanel
	bind:this={undoPanel}
	roots={safeRoots}
	refresh={jobsTick}
	onchanged={() => {
		load();
		loadRoots();
		jobsTick++;
	}}
/>

{#if copying && root && roots}
	{#key copying}<CopyDialog
			roots={safeRoots}
			from={copying.from ?? root}
			paths={copying.paths ?? selected.map((name) => join(dir, name))}
			move={copying.move}
			restore={copying.restore}
			targetRoot={copying.targetRoot}
			targetDir={copying.targetDir}
			startDir={dir}
			onclose={() => (copying = null)}
			ondone={() => {
				if (copying?.pasted && copying.move) clipboard.content = null;
				copying = null;
				selected = [];
				jobsTick++;
			}}
		/>{/key}
{/if}
{#if editing && root}
	<TextEditor
		root={root.id}
		stick={root.kind === 'stick'}
		path={editing}
		onclose={() => {
			editing = null;
			load();
		}}
	/>
{/if}
{#if windowsPath && root}<WindowsPath {root} {dir} onclose={() => (windowsPath = false)} />{/if}

{#if dialog}
	{#key dialog}
		<EntryDialog
			{dialog}
			{root}
			{rootId}
			{dir}
			{writable}
			{selected}
			files={selectedEntries.length - selectedFolders}
			folders={selectedFolders}
			bytes={selectionBytes}
			{uncounted}
			busy={dialogBusy}
			onbusy={(busy) => (dialogBusy = busy)}
			onclose={() => (dialog = null)}
			ondone={dialogDone}
		/>
	{/key}
{/if}

{#if downloadPaths && root}<DownloadDialog
		{root}
		paths={downloadPaths}
		onclose={() => (downloadPaths = null)}
	/>{/if}
{#if duplicates && root}<DuplicatesDialog
		root={safeRoots.find((r) => r.id === rootId) ?? root}
		{dir}
		showHidden={prefs.showHidden}
		onclose={() => (duplicates = false)}
		onopen={(path) => {
			duplicates = false;
			open(rootId, path);
		}}
		onchanged={() => {
			load();
			loadRoots();
			jobsTick++;
		}}
	/>{/if}
{#if photos && root}<PhotoView root={rootId} {dir} onclose={() => (photos = false)} />{/if}
{#if deepSearch && root}<SearchDialog
		{root}
		{dir}
		showHidden={prefs.showHidden}
		onclose={() => (deepSearch = false)}
		onopen={(path) => {
			deepSearch = false;
			open(rootId, path);
		}}
	/>{/if}
{#if comparison && root && rightRoot}<CompareDialog
		left={safeRoots.find((r) => r.id === rootId) ?? root}
		leftDir={dir}
		right={rightRoot}
		rightDir={right.path}
		showHidden={prefs.showHidden}
		ondone={() => {
			comparison = false;
			jobsTick++;
		}}
		onclose={() => (comparison = false)}
		onopen={(id, path) => {
			comparison = false;
			open(id, path);
		}}
	/>{/if}
{#if batchRename && root}<RenameBatchDialog
		{root}
		{dir}
		names={selected}
		onclose={() => {
			batchRename = false;
			load();
			jobsTick++;
		}}
		ondone={() => {
			batchRename = false;
			selected = [];
			load();
			jobsTick++;
		}}
	/>{/if}
{#if archive && root}<ArchiveDialog
		{root}
		path={archive}
		{dir}
		roots={safeRoots}
		onclose={() => (archive = null)}
		ondone={() => {
			archive = null;
			jobsTick++;
		}}
	/>{/if}
{#if properties}<FileProperties
		root={properties.root}
		path={properties.path}
		locked={properties.root.kind === 'usb' &&
			!safeRoots.find((r) => r.id === properties?.root.id)?.writable &&
			properties.root.writable}
		onclose={() => (properties = null)}
	/>{/if}
{#if uploadReview}<UploadReview
		root={uploadReview.root}
		dir={uploadReview.dir}
		files={uploadReview.files}
		onclose={() => (uploadReview = null)}
		onconfirm={startUpload}
	/>{/if}
{#if favoriteManager}<FavoritesDialog
		favorites={prefs.favorites}
		onsave={(next) => prefs.saveFavorites(next)}
		onclose={() => (favoriteManager = false)}
	/>{/if}
{#if formattingDrive && canAdminister}<DriveFormatDialog
		onclose={() => (formattingDrive = false)}
		onformatted={() => {
			usb.load();
		}}
	/>{/if}
{#if downloadingLink && root && writable}<UrlDownloadDialog
		root={rootId}
		{dir}
		confined={!canAdminister}
		onclose={() => (downloadingLink = false)}
		onstarted={() => {
			downloadingLink = false;
			jobsTick++;
		}}
	/>{/if}
{#if creatingFile && root && writable}<CreateFileDialog
		root={rootId}
		{dir}
		onclose={() => (creatingFile = false)}
		oncreated={(path) => {
			creatingFile = false;
			editing = path;
			load();
		}}
	/>{/if}
{#if otherPreview}<FilePreview {...otherPreview} onclose={() => (otherPreview = null)} />{/if}
{#if shortcuts}<ShortcutsDialog onclose={() => (shortcuts = false)} />{/if}
{#if preview && root}<FilePreview
		root={root.id}
		path={join(dir, preview.name)}
		size={preview.size}
		modified={preview.modified}
		onprevious={galleryIndex > 0 ? () => quickLook(gallery[galleryIndex - 1]) : undefined}
		onnext={galleryIndex >= 0 && galleryIndex < gallery.length - 1
			? () => quickLook(gallery[galleryIndex + 1])
			: undefined}
		onopen={appForFile(preview.name)?.startsWith('webapp:') ||
		(appForFile(preview.name) === EDITOR && isEditableText(preview.name, preview.size))
			? () => {
					const entry = preview;
					preview = null;
					if (entry) openWithStandard(entry);
				}
			: undefined}
		onclose={() => (preview = null)}
	/>{/if}

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
		padding: var(--space-2);
		position: sticky;
		top: calc(var(--header-height, 0px) + var(--gap));
		max-height: calc(100vh - var(--header-height, 0px) - 2 * var(--gap) - 40px);
		overflow: auto;
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
		gap: var(--space-2);
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
	.list-footer {
		margin: 0;
		padding: 10px 14px 0;
		font-size: var(--text-base);
	}
	.saving {
		position: fixed;
		left: 16px;
		bottom: 44px;
		z-index: 50;
		margin: 0;
		padding: 6px var(--space-3);
		background: var(--surface);
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		box-shadow: var(--shadow-menu);
		color: var(--muted);
		font-size: var(--text-base);
	}
	.none {
		padding: 20px 14px;
		font-size: var(--text-md);
	}

	/* PLAN 69: in a window of the desktop the places are the window's one side
	   bar (the window draws none on this page): same width, surface, group
	   headings and highlighted entry as the side bar of every other window. */
	.side-foot {
		display: none;
	}
	.side-group {
		margin: 12px 10px 4px;
		font-size: var(--text-xs);
		color: var(--muted);
		text-transform: uppercase;
		letter-spacing: 0.05em;
	}
	.side-item {
		display: flex;
		align-items: center;
		gap: 10px;
		min-height: 36px;
		padding: 6px 10px;
		border: 0;
		border-radius: calc(9px * var(--radius-scale, 1));
		background: none;
		color: var(--text);
		font: inherit;
		text-align: left;
		text-decoration: none;
		cursor: pointer;
		width: 100%;
	}
	.side-item :global(svg) {
		flex: none;
		color: var(--muted);
	}
	.side-item:hover {
		background: color-mix(in srgb, var(--text) 7%, transparent);
	}
	.side-item:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
	.side-fold {
		color: var(--muted);
		margin-top: 6px;
	}
	/* The page head is a heading for screen readers only: it takes no room
	   (in a window the flex gap above the content came from it, PLAN 73). */
	.files-pagehead {
		display: contents;
	}
	@media (min-width: 761px) {
		/* PLAN 73: edge to edge in the window – the places are a column of the
		   page's own grid (no fixed bar laid over the list), the list reaches
		   the window's edges, the bars keep their inner spacing. */
		.layout.windowed {
			grid-template-columns: 220px minmax(0, 1fr);
			gap: 0;
			align-items: stretch;
			margin: calc(-1 * var(--page-pad));
			min-height: 100dvh;
		}
		.layout.windowed.folded {
			grid-template-columns: 60px minmax(0, 1fr);
		}
		.layout.windowed > .workspace {
			align-content: start;
		}
		.layout.windowed .browser {
			border: 0;
			border-radius: 0;
			background: none;
			box-shadow: none;
		}
		.places.windowed {
			position: sticky;
			z-index: 25;
			top: 0;
			height: 100dvh;
			width: auto;
			max-height: none;
			gap: 0;
			padding: 12px 10px;
			overflow: hidden;
			/* The same opaque pane as the window's side bar (PLAN 71). */
			background:
				linear-gradient(
					180deg,
					color-mix(in srgb, var(--text) 3%, transparent),
					color-mix(in srgb, var(--accent) 3%, transparent)
				),
				color-mix(in srgb, var(--surface) 88%, var(--bg));
			border-right: 1px solid var(--border);
		}
		.places.windowed.folded {
			padding: 12px 8px;
			align-items: center;
		}
		.side-tree {
			flex: 1;
			min-height: 0;
			overflow-x: hidden;
			overflow-y: auto;
		}
		.folded .side-tree,
		.folded .side-group,
		.folded .side-item span {
			display: none;
		}
		.side-foot {
			display: flex;
			flex-direction: column;
			gap: 2px;
			flex: none;
			width: 100%;
		}
		.folded .side-item {
			justify-content: center;
			padding: 0;
			width: 44px;
		}
		.folded .side-foot {
			align-items: center;
		}
		/* The places look like the entries of a window's side bar. */
		.places.windowed :global(.group) {
			margin: 12px 10px 4px;
			padding: 0;
			font-size: var(--text-xs);
			text-transform: uppercase;
			letter-spacing: 0.05em;
		}
		.places.windowed :global(:is(.name, .place)) {
			min-height: 36px;
			border-radius: calc(9px * var(--radius-scale, 1));
			color: var(--text);
		}
		.places.windowed :global(:is(.name, .place):hover) {
			background: color-mix(in srgb, var(--text) 7%, transparent) !important;
		}
		.places.windowed :global(:is(.name, .place).current) {
			background: color-mix(in srgb, var(--accent) 18%, transparent) !important;
			/* Toward the text color: 4.5:1 on the tinted glass in every theme (PLAN 65-5). */
			color: color-mix(in srgb, var(--accent) 65%, var(--text));
			font-weight: 600;
		}
		.places.windowed :global(.name.current svg) {
			color: var(--accent);
		}
		.saving.windowed {
			left: calc(220px + 16px);
		}
	}
	@media (max-width: 760px) {
		/* A narrow window: the places unfold under the location like on the phone. */
		.places.windowed {
			background: var(--panel-bg);
			border: 1px solid var(--panel-border);
			border-radius: var(--radius-tile);
		}
		.places.windowed.open {
			flex-direction: column;
		}
		.places.windowed .side-foot {
			display: flex;
			flex-direction: column;
			gap: 2px;
		}
		.places.windowed .side-fold {
			display: none;
		}
	}
	@media (max-width: 1100px) {
		.workspace.split {
			grid-template-columns: minmax(0, 1fr);
		}
	}
	@media (max-width: 760px) {
		.layout {
			grid-template-columns: minmax(0, 1fr);
			gap: var(--space-2);
		}
		.places {
			display: none;
			position: static;
			max-height: none;
		}
		.places.open {
			display: flex;
		}
	}
</style>
