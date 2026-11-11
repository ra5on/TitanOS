// The file manager's actions: moving between the two panes, mouse and keyboard,
// "Öffnen mit", the clipboard, the commands of bars and menus, and uploads.
// The state they work on is in files-state.svelte.ts.

import { openAgent } from '#lib/agent-app.ts';
import { goto } from '$app/navigation';
import { apiPath } from '#lib/cluster-node.ts';
import { onMount } from 'svelte';
import { api, ApiError, type CopyJob, type FileEntry } from '#lib/api.ts';
import { previewKind } from '#lib/file-actions.ts';
import { beginFileDrag, hasFileDrag, takeFileDrag } from '#lib/file-drag.ts';
import { apiErrorText, errorText, t } from '#lib/i18n/index.svelte.ts';
import { isEditableText } from '#lib/text-files.ts';
import { saveFile } from '#lib/save-file.ts';
import { enqueue } from '#lib/uploads.svelte.ts';
import type { FileCommand } from './commands.ts';
import { droppedFiles, type PickedFile } from './drop-files.ts';
import { frameMessage, inWindow } from '#lib/desktop/frame.ts';
import type { KeyboardHost } from './keyboard.ts';
// PLAN 66: files open in their standard app (a web app running in the browser).
import { appForFile, setFileApp } from '#lib/webapps/webapps.svelte.ts';
import { EDITOR, extOf } from '#lib/webapps/protocol.ts';
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
import { FilesState, join } from './files-state.svelte.ts';
import type UndoPanel from './UndoPanel.svelte';

export class FilesPage extends FilesState {
	// --- Two panes ----------------------------------------------------------

	reviewDrop = (event: DragEvent, target: string, path: string) => {
		if (!hasFileDrag(event)) return;
		event.preventDefault();
		event.stopPropagation();
		this.dragging = false;
		const source = takeFileDrag(event),
			destination = this.safeRoots.find((r) => r.id === target),
			origin = this.safeRoots.find((r) => r.id === source?.root);
		if (!source || !origin || origin.kind === 'stick' || !destination?.writable) return;
		if (source.root === target && source.paths.some((p) => path === p || path.startsWith(p + '/')))
			return;
		this.dropReview = { source, target, path };
	};
	acceptDrop = (move: boolean) => {
		const review = this.dropReview;
		if (!review) return;
		const from = this.safeRoots.find((r) => r.id === review.source.root),
			target = this.safeRoots.find((r) => r.id === review.target);
		if (!from || !target?.writable || (move && !from.writable)) return;
		this.copying = {
			from,
			paths: review.source.paths,
			move,
			targetRoot: target.id,
			targetDir: review.path,
		};
		this.dropReview = null;
	};
	dragEntry = (event: DragEvent, entry: FileEntry) => {
		if (this.root?.kind === 'stick' || this.trash) {
			event.preventDefault();
			return;
		}
		const names = this.selectedNames.has(entry.name) ? this.selected : [entry.name];
		this.selected = names;
		beginFileDrag(event, { root: this.rootId, paths: names.map((name) => join(this.dir, name)) });
	};
	transferPane = (direction: 'right' | 'left', move: boolean) => {
		const source =
			direction === 'right' ? this.safeRoots.find((r) => r.id === this.rootId) : this.rightRoot;
		const target =
			direction === 'right' ? this.rightRoot : this.safeRoots.find((r) => r.id === this.rootId);
		const names = direction === 'right' ? this.selected : this.right.selected;
		const sourceDir = direction === 'right' ? this.dir : this.right.path,
			targetDir = direction === 'right' ? this.right.path : this.dir;
		if (!source || !target?.writable || !names.length || (move && !source.writable)) return;
		this.copying = {
			from: source,
			paths: names.map((n) => join(sourceDir, n)),
			move,
			targetRoot: target.id,
			targetDir,
		};
	};
	canRetry = (job: CopyJob) => {
		return (
			!!this.safeRoots.find((r) => r.id === job.to)?.writable &&
			(!job.move || !!this.safeRoots.find((r) => r.id === job.from)?.writable)
		);
	};

	// --- Mouse ----------------------------------------------------------------

	// The entry hit by the last click. A double click only opens what both
	// clicks hit: the first click on a folder already opened it, and the
	// second one lands on whatever is in its place now.
	lastClick = '';
	/** The second click of a double click that opened a folder lands in the new folder; it does nothing there. */
	secondClickElsewhere = (entry: FileEntry, event: MouseEvent) => {
		return event.detail > 1 && this.lastClick !== `${this.rootId}/${this.dir}/${entry.name}`;
	};
	folderClick = (entry: FileEntry, event: MouseEvent) => {
		if (this.secondClickElsewhere(entry, event)) return;
		this.lastClick = `${this.rootId}/${this.dir}/${entry.name}`;
		if (event.shiftKey) this.selectRange(entry.name, event.ctrlKey || event.metaKey);
		else if (event.ctrlKey || event.metaKey)
			this.toggle(entry.name, !this.selectedNames.has(entry.name));
		else this.open(this.rootId, join(this.dir, entry.name));
	};
	// A click on the row opens a folder or toggles the selection of a file.
	// Clicks on the checkbox or the buttons inside the row are left to them.
	rowClicked = (entry: FileEntry, event: MouseEvent) => {
		if (event.target instanceof Element && event.target.closest('input, button')) return;
		if (this.secondClickElsewhere(entry, event)) return;
		this.lastClick = `${this.rootId}/${this.dir}/${entry.name}`;
		if (event.shiftKey) this.selectRange(entry.name, event.ctrlKey || event.metaKey);
		else if (event.ctrlKey || event.metaKey)
			this.toggle(entry.name, !this.selectedNames.has(entry.name));
		else if (entry.dir) this.open(this.rootId, join(this.dir, entry.name));
		else if (inWindow) {
			// PLAN 73: like Explorer – a click selects this one entry only.
			this.selected = [entry.name];
			this.anchor = entry.name;
		} else this.toggle(entry.name, !this.selectedNames.has(entry.name));
	};
	/** Double click on a file: preview, or the editor for text the preview does not show. */
	rowDoubleClicked = (entry: FileEntry, event: MouseEvent) => {
		if (entry.dir || this.lastClick !== `${this.rootId}/${this.dir}/${entry.name}`) return;
		if (
			(event.target instanceof Element && event.target.closest('input')) ||
			event.shiftKey ||
			event.ctrlKey ||
			event.metaKey
		)
			return;
		this.selected = [entry.name];
		this.anchor = entry.name;
		if (this.openWithStandard(entry)) return;
		if (previewKind(entry.name, entry.size)) this.preview = entry;
		else if (this.editable) this.editing = join(this.dir, entry.name);
	};
	/** PLAN 66: the standard app of the file type (Dateien › Einstellungen › Standard-Apps). */
	/** Quick look of the next file: it becomes the selection, like in the list. */
	quickLook = (entry: FileEntry) => {
		this.preview = entry;
		this.selected = [entry.name];
		this.anchor = entry.name;
	};
	// PLAN 74 (owner 8.11.2026: hint only): "._name" is the Finder data of a
	// Mac (AppleDouble), not the file itself; opening it explains that.
	companion = $state<{ file: FileEntry; real: FileEntry | null } | null>(null);
	macCompanion = (entry: FileEntry): boolean => {
		if (entry.dir || !entry.name.startsWith('._') || entry.name.length < 3) return false;
		const name = entry.name.slice(2);
		this.companion = {
			file: entry,
			real: this.entries?.find((e) => !e.dir && e.name === name) ?? null,
		};
		return true;
	};
	openWithStandard = (entry: FileEntry): boolean => {
		if (this.macCompanion(entry)) return true;
		// PLAN 72: a standard Docker app comes first (choosing one clears the other).
		const docker = dockerAppForExt(extOf(entry.name));
		if (docker) {
			void this.openDocker(entry, docker, false);
			return true;
		}
		const choice = appForFile(entry.name);
		if (
			choice === EDITOR &&
			isEditableText(entry.name, entry.size) &&
			(this.root?.editable || this.writable)
		) {
			this.editing = join(this.dir, entry.name);
			return true;
		}
		if (!choice?.startsWith('webapp:')) return false;
		openInWebApp(choice, this.rootId, join(this.dir, entry.name));
		return true;
	};
	/** "Öffnen mit": an app chosen for this file, and with "always" as its standard app. */
	openWith = async (entry: FileEntry, app: string, always: boolean) => {
		if (this.macCompanion(entry)) return;
		if (app.startsWith('docker:')) return this.openDocker(entry, app.slice(7), always);
		if (always) {
			try {
				await setFileApp(extOf(entry.name), app);
				if (dockerAppForExt(extOf(entry.name))) await setDockerFileApp(extOf(entry.name), null);
			} catch (e) {
				this.error = apiErrorText(e);
			}
		}
		if (app === EDITOR) this.editing = join(this.dir, entry.name);
		else openInWebApp(app, this.rootId, join(this.dir, entry.name));
	};

	// --- PLAN 72: „Öffnen mit“ für Docker-Apps --------------------------------

	dockerMount = $state<{
		opener: FileOpener;
		root: string;
		path: string;
		file: string;
		busy: boolean;
		error: string;
	} | null>(null);

	openDocker = async (entry: FileEntry, app: string, always: boolean) => {
		const ext = extOf(entry.name);
		if (always) {
			try {
				await setDockerFileApp(ext, app);
				if (appForFile(entry.name)?.startsWith('webapp:')) await setFileApp(ext, null);
			} catch (e) {
				this.error = apiErrorText(e);
			}
		}
		const root = this.rootId;
		const path = join(this.dir, entry.name);
		const opener = (await fileOpeners(root, path)).find((o) => o.app === app);
		if (!opener) {
			this.error = t('files.openWith.dockerGone');
			return;
		}
		if (opener.mounted) return this.startDocker(app, root, path);
		if (opener.canMount && opener.share) {
			this.dockerMount = { opener, root, path, file: entry.name, busy: false, error: '' };
			return;
		}
		this.error = opener.share
			? t('files.openWith.askAdmin', { share: opener.share, app: opener.name })
			: t('files.openWith.outside');
	};

	startDocker = async (app: string, root: string, path: string) => {
		try {
			await openInDockerApp(app, root, path);
		} catch (e) {
			this.error = apiErrorText(e);
		}
	};

	confirmMount = async (readOnly: boolean) => {
		const m = this.dockerMount;
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
		this.dockerMount = null;
		await this.startDocker(m.opener.app, m.root, m.path);
	};

	// --- Actions --------------------------------------------------------------

	download = () => {
		if (!this.entries || this.selected.length === 0) return;
		const single =
			this.selected.length === 1
				? this.entries.find((entry) => entry.name === this.selected[0])
				: undefined;
		// Folders and several entries are packed; the dialog asks for the name of the ZIP.
		if (!single || single.dir) {
			this.downloadPaths = this.selected.map((name) => join(this.dir, name));
			return;
		}
		// The browser saves the answer; nothing passes through this page's memory.
		saveFile(
			apiPath(
				'/files/download?' +
					new URLSearchParams({ root: this.rootId, path: join(this.dir, single.name) }),
			),
			(code) => (this.error = errorText(code)),
		);
	};
	show = (kind: 'mkdir' | 'rename' | 'delete') => {
		if (!this.writable || this.dialogBusy) return;
		this.dialog = kind === 'rename' ? { kind, name: this.selected[0] } : { kind };
	};
	dialogDone = async () => {
		this.dialog = null;
		this.jobsTick++;
		this.selected = [];
		await this.load();
		this.loadRoots();
	};

	/** Carries out a command of a bar or a menu; `entry` is the one a menu was opened on. */
	command = (action: FileCommand, entry?: FileEntry) => {
		switch (action) {
			case 'up':
				return this.open(this.rootId, this.dir.split('/').slice(0, -1).join('/'));
			case 'back':
			case 'forward':
				return this.historyGo(action);
			case 'editPath':
				return void this.navBar?.editPath();
			case 'favorite':
				return this.prefs.toggleFavorite();
			case 'mkdir':
			case 'rename':
			case 'delete':
				return this.show(action);
			case 'newFile':
				this.creatingFile = true;
				return;
			case 'upload':
				return this.fileInput?.click();
			case 'uploadFolder':
				return this.folderInput?.click();
			case 'urlDownload':
				this.downloadingLink = true;
				return;
			case 'selectAll':
				this.selected = this.visibleEntries.map((e) => e.name);
				return;
			case 'clearSelection':
				this.selected = [];
				return;
			case 'invertSelection':
				this.selected = this.visibleEntries
					.filter((e) => !this.selectedNames.has(e.name))
					.map((e) => e.name);
				return;
			case 'selectType':
				return this.selectType();
			case 'openFolder':
				if (entry) this.open(this.rootId, join(this.dir, entry.name));
				return;
			case 'preview':
				if (entry) this.preview = entry;
				return;
			case 'edit':
				this.editing = join(this.dir, this.selected[0]);
				return;
			case 'download':
				return this.download();
			case 'archive':
				if (entry) this.archive = join(this.dir, entry.name);
				return;
			case 'properties':
				if (entry && this.root)
					this.properties = { root: this.root, path: join(this.dir, entry.name) };
				return;
			case 'agent': {
				// Opens the KI-Agent with the chosen files attached (PLAN 44).
				const names = this.selected.length ? this.selected : entry ? [entry.name] : [];
				const attach = names.map((n) => `${this.rootId}:${join(this.dir, n)}`).join('|');
				openAgent('/agent?' + new URLSearchParams({ attach }).toString(), goto);
				return;
			}
			case 'clipboardCopy':
			case 'cut':
				return this.rememberSelection(action === 'cut');
			case 'paste':
				return this.paste();
			case 'copy':
			case 'move':
				this.copying = { move: action === 'move' };
				return;
			case 'restore':
				this.copying = { move: false, restore: true };
				return;
			case 'renameBatch':
				this.batchRename = true;
				return;
			case 'deepSearch':
				this.deepSearch = true;
				return;
			case 'usage':
				this.usage = true;
				return;
			case 'duplicates':
				this.duplicates = true;
				return;
			case 'compare':
				this.comparison = true;
				return;
			case 'photos':
				this.photos = true;
				return;
			case 'shortcuts':
				this.shortcuts = true;
				return;
			case 'trash':
				this.trash = true;
				this.trashBin = '';
				this.selected = [];
				return;
			case 'twoPanes':
				this.prefs.split = !this.prefs.split;
				this.prefs.save();
				return;
			case 'showHidden':
				this.prefs.showHidden = !this.prefs.showHidden;
				this.selected = [];
				this.prefs.save();
				return;
			case 'folderView':
				return this.prefs.toggleFolderView();
			default:
				return this.prefs.changeView(action);
		}
	};

	// --- Clipboard and keyboard ---------------------------------------------

	rememberSelection = (move: boolean) => {
		if (
			!this.root ||
			!this.selected.length ||
			this.root.kind === 'stick' ||
			(move && !this.writable)
		)
			return;
		this.clipboard.remember(this.root, this.dir, this.selected, move);
		this.menu = null;
	};
	canPaste = $derived(this.clipboard.canPaste(this.writable, this.safeRoots));
	paste = () => {
		if (!this.clipboard.content || !this.canPaste) return;
		this.copying = {
			...this.clipboard.content,
			targetRoot: this.rootId,
			targetDir: this.dir,
			pasted: true,
		};
		this.backgroundMenu = null;
	};
	keys: KeyboardHost = {
		ignored: () => this.trash || !this.root || !!this.menu || !!this.backgroundMenu,
		entries: () => this.visibleEntries,
		selected: () => this.selected,
		anchor: () => this.anchor,
		setAnchor: (name) => (this.anchor = name),
		pick: (name) => {
			this.selected = [name];
			this.anchor = name;
		},
		selectRange: (name) => this.selectRange(name),
		list: () => this.virtualList,
		writable: () => this.writable,
		stick: () => this.root?.kind === 'stick',
		editable: () => this.editable,
		hasClipboard: () => !!this.clipboard.content,
		menuAt: this.entryMenu,
		run: this.command,
	};

	// --- Uploads --------------------------------------------------------------

	upload = (files: PickedFile[]) => {
		if (this.root && this.writable && files.length)
			this.uploadReview = { root: this.root, dir: this.dir, files };
	};
	// PLAN 61: files dropped on the desktop arrive here (Dateien in a window);
	// they wait until a writable folder is open, then the upload review asks.
	fromDesktop = $state<PickedFile[]>([]);
	// PLAN 69: in a window the places are the window's one side bar (the
	// window draws none of its own here); folding it stays the desktop's setting.
	sideFolded = $state(false);
	foldSide = () => {
		window.parent.postMessage({ type: 'titanos-side-fold' }, location.origin);
	};
	/** Creates the folders of an uploaded tree, parents first, then queues the files. */
	startUpload = async (
		files: { file: File; path: string; conflict: '' | 'rename' | 'replace' }[],
	) => {
		const review = this.uploadReview;
		if (!review || !this.safeRoots.find((r) => r.id === review.root.id)?.writable)
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
		this.uploadReview = null;
		this.load();
	};
	dropped = async (event: DragEvent) => {
		if (hasFileDrag(event)) {
			this.reviewDrop(event, this.rootId, this.dir);
			return;
		}
		event.preventDefault();
		this.dragging = false;
		if (!this.writable || !event.dataTransfer) return;
		const { files, failed } = await droppedFiles(event.dataTransfer);
		// The rest is still offered for upload; the unreadable ones are named.
		if (failed.length)
			this.error = t('files.dropUnreadable', {
				count: failed.length,
				names: failed.slice(0, 5).join(', ') + (failed.length > 5 ? ' …' : ''),
			});
		this.upload(files);
	};

	undoPanel = $state<ReturnType<typeof UndoPanel>>();

	constructor() {
		super();
		onMount(() => {
			const take = (event: MessageEvent) => {
				const msg = frameMessage(event);
				if (msg?.type !== 'titanos-upload' || event.source !== window.parent) return;
				const files = (msg as unknown as { files: unknown[] }).files.filter(
					(f): f is File => f instanceof File,
				);
				window.parent.postMessage({ type: 'titanos-upload-ok' }, location.origin);
				if (!this.fromDesktop.length)
					this.fromDesktop = files.map((file) => ({ file, path: file.name }));
			};
			window.addEventListener('message', take);
			return () => window.removeEventListener('message', take);
		});
		onMount(() => {
			if (!inWindow) return;
			const side = (event: MessageEvent) => {
				const msg = frameMessage(event);
				if (msg?.type === 'titanos-side' && event.source === window.parent)
					this.sideFolded = msg.folded;
			};
			window.addEventListener('message', side);
			window.parent.postMessage({ type: 'titanos-side-get' }, location.origin);
			return () => window.removeEventListener('message', side);
		});
		$effect(() => {
			if (!this.fromDesktop.length || !this.root || !this.writable) return;
			this.upload(this.fromDesktop);
			this.fromDesktop = [];
		});
	}
}
