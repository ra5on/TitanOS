// The file manager's page: where it is, what the list shows, what is selected,
// which dialogs are open, and loading and moving between folders. The actions
// on top of this are in files-page.svelte.ts.

import { untrack, tick, onMount } from 'svelte';
import { page } from '$app/state';
import { poll } from '#lib/poll.ts';
import { api, type FileEntry, type FileRoot, type UploadStatus } from '#lib/api.ts';
import { folderPath, rangeSelection, selectionType } from '#lib/file-actions.ts';
import type { FileDrag } from '#lib/file-drag.ts';
import { fileList } from '#lib/file-list.ts';
import { preferredFileRoot } from '#lib/file-root.ts';
import { apiErrorText, i18n, t } from '#lib/i18n/index.svelte.ts';
import { session } from '#lib/session.svelte.ts';
import { isEditableText } from '#lib/text-files.ts';
import { uploads } from '#lib/uploads.svelte.ts';
import { loadWebApps } from '#lib/webapps/webapps.svelte.ts';
import { FileClipboard } from './clipboard.svelte.ts';
import type { PickedFile } from './drop-files.ts';
import { FileHistory, type Location } from './history.svelte.ts';
import { FilePreferenceStore } from './preferences.svelte.ts';
import { UsbDrives } from './usb.svelte.ts';
import type NavBar from './NavBar.svelte';

export const join = (...parts: string[]) => parts.filter(Boolean).join('/');

export class FilesState {
	canAdminister = $derived(
		session.data?.user?.role === 'owner' || session.data?.user?.role === 'admin',
	);

	// --- Where we are -------------------------------------------------------

	roots = $state<FileRoot[] | null>(null);
	rootId = $state('');
	dir = $state(''); // folder inside the root, "" for its top
	entries = $state<FileEntry[] | null>(null);
	// Why the open folder could not be listed; shown in the list instead of "empty".
	listError = $state('');
	listLoading = $state(false);
	selected = $state<string[]>([]);
	anchor = $state('');
	error = $state('');
	trash = $state(false);
	/** A volume shows the recycle bin of one of its shares: that share's folder. */
	trashBin = $state('');
	/** On narrow screens the list of places is folded away behind a button. */
	placesOpen = $state(false);

	root = $derived(this.roots?.find((candidate) => candidate.id === this.rootId));
	bins = $derived(this.root?.kind === 'volume' ? (this.root.bins ?? []) : []);
	openBin = $derived(this.trashBin || (this.bins.length === 1 ? this.bins[0] : ''));
	binRoot = $derived(
		this.root && this.openBin
			? { ...this.root, name: this.openBin, kind: 'share' as const, recycle: true }
			: undefined,
	);
	appliedLocation = '';

	// --- Dialogs and panels -------------------------------------------------

	copying = $state<{
		move: boolean;
		restore?: boolean;
		from?: FileRoot;
		paths?: string[];
		targetRoot?: string;
		targetDir?: string;
		pasted?: boolean;
	} | null>(null);
	editing = $state<string | null>(null);
	jobsTick = $state(0);
	windowsPath = $state(false);
	favoriteManager = $state(false);
	creatingFile = $state(false);
	shortcuts = $state(false);
	downloadingLink = $state(false);
	selectionFilter = $state('images');
	downloadPaths = $state<string[] | null>(null);
	usage = $state(false);
	duplicates = $state(false);
	photos = $state(false);
	deepSearch = $state(false);
	comparison = $state(false);
	batchRename = $state(false);
	archive = $state<string | null>(null);
	properties = $state<{ root: FileRoot; path: string } | null>(null);
	preview = $state<FileEntry | null>(null);
	otherPreview = $state<{ root: string; path: string; size: number } | null>(null);
	uploadReview = $state<{ root: FileRoot; dir: string; files: PickedFile[] } | null>(null);
	dropReview = $state<{ source: FileDrag; target: string; path: string } | null>(null);
	dialog = $state<{ kind: 'mkdir' } | { kind: 'rename'; name: string } | { kind: 'delete' } | null>(
		null,
	);
	dialogBusy = $state(false);
	/** The floating menus: one for entries, one for the empty part of the folder. */
	menu = $state<{ entry: FileEntry; x: number; y: number } | null>(null);
	backgroundMenu = $state<{ x: number; y: number } | null>(null);

	fileInput = $state<HTMLInputElement>();
	folderInput = $state<HTMLInputElement>();
	dragging = $state(false);

	// --- Stores -------------------------------------------------------------

	prefs = new FilePreferenceStore(() => ({ root: this.rootId, path: this.dir }));
	clipboard = new FileClipboard();
	usb = new UsbDrives({
		admin: () => this.canAdminister,
		rootId: () => this.rootId,
		roots: () => this.roots,
		loadRoots: () => this.loadRoots(),
		open: (id, path) => this.open(id, path),
		restore: (from, path) => (this.copying = { move: false, restore: true, from, paths: [path] }),
		setError: (message) => (this.error = message),
	});

	// --- Two panes ----------------------------------------------------------

	right = $state<{ root: string; path: string; selected: string[] }>({
		root: '',
		path: '',
		selected: [],
	});

	// --- Unfinished uploads ---------------------------------------------------

	// Partial uploads of the folder; UnfinishedUploads offers to finish them.
	pending = $state<UploadStatus[]>([]);
	recovering = $state(false);

	// --- List position, history and search ---------------------------------

	virtualList = $state<{
		getScroll: () => number;
		setScroll: (top: number) => void;
		reveal: (index: number) => void;
		getColumns: () => number;
	}>();
	steps = new FileHistory();
	search = $state('');
	visibleFavorites = $derived(
		this.prefs.favorites.filter((f) => this.roots?.some((r) => r.id === f.root)),
	);

	// --- The list -----------------------------------------------------------

	visibleEntries = $derived(
		fileList(
			(this.entries ?? []).filter((entry) => this.prefs.showHidden || !entry.name.startsWith('.')),
			this.search,
			this.prefs.sort,
			this.prefs.descending,
			i18n.locale,
		),
	);
	// PLAN 70, quick look: the arrows go through every file of the folder in its order.
	gallery = $derived(this.visibleEntries.filter((e) => !e.dir));
	galleryIndex = $derived(
		this.preview ? this.gallery.findIndex((e) => e.name === this.preview?.name) : -1,
	);
	selectedNames = $derived(new Set(this.selected));
	allSelected = $derived(
		this.visibleEntries.length > 0 &&
			this.visibleEntries.every((entry) => this.selectedNames.has(entry.name)),
	);
	selectedEntries = $derived(
		(this.entries ?? []).filter((entry) => this.selectedNames.has(entry.name)),
	);
	selectedBytes = $derived(
		this.selectedEntries.filter((entry) => !entry.dir).reduce((sum, entry) => sum + entry.size, 0),
	);
	selectedFolders = $derived(this.selectedEntries.filter((entry) => entry.dir).length);
	selectionKey = $derived(JSON.stringify([this.rootId, this.dir, this.selected]));
	measured = $state<{ key: string; bytes: number } | null>(null);
	measuring = $state(false);
	selectionBytes = $derived(
		this.measured?.key === this.selectionKey ? this.measured.bytes : this.selectedBytes,
	);
	/** Selected folders whose size is not known yet. */
	uncounted = $derived(
		this.selectedFolders && this.measured?.key !== this.selectionKey ? this.selectedFolders : 0,
	);
	measureSelection = async () => {
		const key = this.selectionKey;
		this.measuring = true;
		try {
			const result = await api.post<{ bytes: number }>('/files/inspect', {
				root: this.rootId,
				paths: this.selected.map((name) => join(this.dir, name)),
			});
			if (key === this.selectionKey) this.measured = { key, bytes: result.bytes };
		} catch (err) {
			if (key === this.selectionKey) this.error = apiErrorText(err);
		} finally {
			this.measuring = false;
		}
	};

	// --- USB drives and their backups ---------------------------------------

	formattingDrive = $state(false);
	backupLocked = $derived(!!this.root && this.usb.isLocked(this.root.id, this.root.kind));
	writable = $derived(!!this.root?.writable && !this.backupLocked);
	safeRoots = $derived(
		(this.roots ?? []).map((place) =>
			this.usb.isLocked(place.id, place.kind) ? { ...place, writable: false } : place,
		),
	);
	rightRoot = $derived(this.safeRoots.find((r) => r.id === this.right.root));
	currentUSBBackup = $derived(this.usb.backupAt(this.rootId, this.dir));

	/** The text editor may save here (for "Öffnen mit › Texteditor"). */
	writableText = $derived(!this.backupLocked && !!(this.root?.editable || this.writable));
	editable = $derived.by(() => {
		if (this.backupLocked || !(this.root?.editable || this.writable) || this.selected.length !== 1)
			return false;
		const entry = this.entries?.find((candidate) => candidate.name === this.selected[0]);
		return !!entry && !entry.dir && isEditableText(entry.name, entry.size);
	});

	// --- Loading ------------------------------------------------------------

	loadRoots = async () => {
		try {
			this.roots = await api.get<FileRoot[]>('/files/roots');
			if (!this.roots.some((candidate) => candidate.id === this.rootId))
				this.open(preferredFileRoot(this.roots, this.prefs.rememberedRoot), '');
		} catch (err) {
			this.error = apiErrorText(err);
		}
	};
	load = async () => {
		if (!this.rootId) {
			this.entries = null;
			this.pending = [];
			return;
		}
		const requestedRoot = this.rootId,
			requestedDir = this.dir;
		this.listLoading = true;
		try {
			const query = `root=${encodeURIComponent(requestedRoot)}&path=${encodeURIComponent(requestedDir)}`;
			const [listing, parts] = await Promise.all([
				api.get<FileEntry[]>(`/files/list?${query}`),
				this.writable ? api.get<UploadStatus[]>(`/files/uploads?${query}`) : Promise.resolve([]),
			]);
			if (this.rootId !== requestedRoot || this.dir !== requestedDir) return;
			this.entries = listing;
			this.pending = parts;
			const names = new Set(this.entries.map((entry) => entry.name));
			this.selected = this.selected.filter((name) => names.has(name));
			this.listError = '';
			this.error = '';
		} catch (err) {
			if (this.rootId !== requestedRoot || this.dir !== requestedDir) return;
			this.entries = null;
			this.listError = apiErrorText(err);
		} finally {
			if (this.rootId === requestedRoot && this.dir === requestedDir) this.listLoading = false;
		}
	};

	refreshError = $state(false);

	// --- Navigation -----------------------------------------------------------

	pathEditing = $state(false);
	pathInput = $state('');
	pathMessage = $state('');
	mobileSearch = $state(false);
	navBar = $state<ReturnType<typeof NavBar>>();

	captureLocation = (): Location => {
		return {
			root: this.rootId,
			path: this.dir,
			search: this.search,
			selected: [...this.selected],
			scroll: window.scrollY,
			listScroll: this.virtualList?.getScroll() ?? 0,
		};
	};
	historyGo = (direction: 'back' | 'forward') => {
		const target = this.steps.go(direction, this.captureLocation);
		if (target) this.navigate(target.root, target.path, target);
	};
	open = (id: string, path: string) => {
		this.steps.leave(
			this.rootId && (this.rootId !== id || this.dir !== path) ? this.captureLocation() : null,
		);
		this.placesOpen = false;
		this.navigate(id, path);
	};
	navigate = async (id: string, path: string, restore?: Location) => {
		const chosen = this.roots?.find((place) => place.id === id);
		if (
			(chosen?.kind === 'share' || chosen?.kind === 'volume') &&
			this.prefs.rememberedRoot !== id
		) {
			// Per browser, never a write to /config by walking around.
			this.prefs.rememberRoot(id);
		}
		if (this.rootId !== id) this.usb.unlockedRoot = '';
		this.rootId = id;
		this.dir = path;
		this.prefs.applyFolderView();
		this.search = restore?.search ?? '';
		this.anchor = '';
		this.menu = null;
		this.backgroundMenu = null;
		this.preview = null;
		this.pathEditing = false;
		this.pathInput = path;
		this.pathMessage = '';
		this.trash = false;
		this.selected = [];
		this.entries = null;
		this.listError = '';
		this.pending = [];
		await this.load();
		if (this.rootId !== id || this.dir !== path) return;
		if (restore) {
			const names = new Set((this.entries ?? ([] as FileEntry[])).map((e) => e.name));
			this.selected = restore.selected.filter((name) => names.has(name));
		}
		await tick();
		this.virtualList?.setScroll(restore?.listScroll ?? 0);
		window.scrollTo({ top: restore?.scroll ?? 0, behavior: 'instant' });
	};

	// --- Selection and mouse ------------------------------------------------

	toggle = (name: string, checked: boolean) => {
		this.anchor = name;
		this.selected = checked
			? [...this.selected, name]
			: this.selected.filter((other) => other !== name);
	};
	selectRange = (name: string, add = false) => {
		const range = rangeSelection(
			this.visibleEntries.map((e) => e.name),
			this.anchor,
			name,
		);
		this.selected = add ? [...new Set([...this.selected, ...range])] : range;
	};
	selectType = () => {
		this.selected = this.visibleEntries
			.filter((e) =>
				this.selectionFilter === 'folders'
					? e.dir
					: !e.dir && selectionType(e.name) === this.selectionFilter,
			)
			.map((e) => e.name);
	};

	// --- Menus ----------------------------------------------------------------

	entryMenu = (entry: FileEntry, x: number, y: number) => {
		if (!this.selectedNames.has(entry.name)) this.selected = [entry.name];
		this.backgroundMenu = null;
		this.menu = { entry, x, y };
	};
	menuFromButton = (entry: FileEntry, button: HTMLElement) => {
		const rect = button.getBoundingClientRect();
		this.entryMenu(entry, rect.right - 232, rect.bottom + 4);
	};
	blankMenu = (event: MouseEvent) => {
		if (
			this.trash ||
			!this.root ||
			(event.target instanceof Element &&
				event.target.closest('.row:not(.header),button,input,select,a,[role="menu"]'))
		)
			return;
		event.preventDefault();
		this.menu = null;
		this.backgroundMenu = { x: event.clientX, y: event.clientY };
	};
	/** Runs a menu action, then closes the menu. The action may still read the entry of the menu. */
	choose = (action: () => void) => {
		action();
		this.menu = null;
		this.backgroundMenu = null;
	};

	constructor() {
		$effect(() => {
			const requested = page.url.searchParams.get('root') ?? '';
			const requestedPath = folderPath(page.url.searchParams.get('path') ?? '');
			const location = JSON.stringify([requested, requestedPath]);
			const available = this.roots;
			if (!requested) {
				this.appliedLocation = '';
				return;
			}
			if (available && location !== this.appliedLocation) {
				this.appliedLocation = location;
				if (requestedPath === null) this.error = t('files.invalidPath');
				else if (available.some((r) => r.id === requested))
					untrack(() => this.open(requested, requestedPath));
				else this.error = t('files.requestedRootUnavailable');
			}
		});
		onMount(() => {
			void this.prefs.load();
			void loadWebApps();
		});
		$effect(() => this.usb.watch());
		// Looks again every ten seconds, unless the user is in the middle of something.
		onMount(() => {
			const blocked = () =>
				document.hidden ||
				document.querySelector('dialog[open]') !== null ||
				this.menu !== null ||
				this.backgroundMenu !== null ||
				this.dragging ||
				this.trash ||
				!this.rootId ||
				this.root?.kind === 'stick' ||
				this.entries === null ||
				['INPUT', 'TEXTAREA', 'SELECT'].includes(document.activeElement?.tagName ?? '');
			const refresh = async (signal: AbortSignal) => {
				const id = this.rootId,
					path = this.dir,
					before = this.entries;
				try {
					const listing = await api.get<FileEntry[]>(
						'/files/list?' + new URLSearchParams({ root: id, path }),
						signal,
					);
					if (
						signal.aborted ||
						id !== this.rootId ||
						path !== this.dir ||
						before !== this.entries ||
						blocked()
					)
						return;
					this.refreshError = false;
					if (JSON.stringify(listing) === JSON.stringify(this.entries)) return;
					const first = Array.from(document.querySelectorAll<HTMLElement>('[data-file-name]')).find(
						(e) => e.getBoundingClientRect().top >= 0,
					);
					const name = first?.dataset.fileName,
						offset = first?.getBoundingClientRect().top,
						scroll = window.scrollY;
					this.entries = listing;
					const names = new Set(listing.map((e) => e.name));
					this.selected = this.selected.filter((n) => names.has(n));
					await tick();
					if (signal.aborted || id !== this.rootId || path !== this.dir) return;
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
					if (!signal.aborted && id === this.rootId && path === this.dir) this.refreshError = true;
				}
			};
			return poll(refresh, { interval: 10000, immediate: false, skip: blocked });
		});

		// At the start, and whenever an upload finishes: it may belong to the
		// folder on screen, and it changes the free space.
		$effect(() => {
			uploads.finished;
			if (!this.prefs.ready) return;
			untrack(() => {
				this.loadRoots();
				this.load();
			});
		});
	}
}
