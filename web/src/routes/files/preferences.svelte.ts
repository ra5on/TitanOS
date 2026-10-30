// The file manager's settings of the account: view, sort order, panes,
// hidden files, favorites and the views remembered for single folders.

import { api } from '#lib/api.ts';
import {
	filePreferences,
	type Favorite,
	type FileView,
	type FolderView,
} from '#lib/file-actions.ts';
import type { FileSort } from '#lib/file-list.ts';
import { t } from '#lib/i18n/index.svelte.ts';

/** The folder on screen; folder views belong to it. */
type Place = () => { root: string; path: string };

export class FilePreferenceStore {
	split = $state(false);
	showHidden = $state(false);
	rememberedRoot = $state('');
	view = $state<FileView>('list');
	sort = $state<FileSort>('name');
	descending = $state(false);
	favorites = $state<Favorite[]>([]);
	folderViews = $state<FolderView[]>([]);
	defaults = $state<{ view: FileView; sort: FileSort; descending: boolean }>({
		view: 'list',
		sort: 'name',
		descending: false,
	});
	ready = $state(false);
	error = $state('');
	saving = $state(false);
	#queue = Promise.resolve();
	#place: Place;

	constructor(place: Place) {
		this.#place = place;
	}

	#here(f: { root: string; path: string }) {
		const { root, path } = this.#place();
		return f.root === root && f.path === path;
	}

	readonly folderViewSaved = $derived(this.folderViews.some((f) => this.#here(f)));
	readonly isFavorite = $derived(this.favorites.some((f) => this.#here(f)));

	async load() {
		try {
			const result = await api.get<{ files: unknown }>('/preferences');
			const prefs = filePreferences(result.files);
			this.split = prefs.split;
			this.showHidden = prefs.showHidden;
			this.rememberedRoot = prefs.root;
			this.view = prefs.view;
			this.sort = prefs.sort;
			this.descending = prefs.descending;
			this.favorites = prefs.favorites;
			this.folderViews = prefs.folders;
			this.defaults = { view: prefs.view, sort: prefs.sort, descending: prefs.descending };
			this.applyFolderView();
			this.ready = true;
			this.error = '';
		} catch {
			this.error = t('files.preferencesLoadFailed');
		}
	}

	save() {
		if (!this.ready) return;
		const { view, sort, descending } = this;
		if (this.folderViews.some((f) => this.#here(f)))
			this.folderViews = this.folderViews.map((f) =>
				this.#here(f) ? { ...f, view, sort, descending } : f,
			);
		else this.defaults = { view, sort, descending };
		const value = {
			split: this.split,
			showHidden: this.showHidden,
			root: this.rememberedRoot,
			...this.defaults,
			folders: $state.snapshot(this.folderViews),
			favorites: $state.snapshot(this.favorites),
		};
		this.saving = true;
		this.#queue = this.#queue.then(async () => {
			try {
				await api.put('/preferences/files', value);
				this.error = '';
			} catch {
				this.error = t('files.preferencesSaveFailed');
			}
		});
		const queued = this.#queue;
		void queued.then(() => {
			if (queued === this.#queue) this.saving = false;
		});
	}

	/** Takes the view of the folder on screen, or the default one. */
	applyFolderView() {
		const settings = this.folderViews.find((f) => this.#here(f)) ?? this.defaults;
		this.view = settings.view;
		this.sort = settings.sort;
		this.descending = settings.descending;
	}

	toggleFolderView() {
		if (this.folderViewSaved) {
			this.folderViews = this.folderViews.filter((f) => !this.#here(f));
			this.applyFolderView();
		} else if (this.folderViews.length < 200) {
			const { root, path } = this.#place();
			this.folderViews = [
				...this.folderViews,
				{ root, path, view: this.view, sort: this.sort, descending: this.descending },
			];
		}
		this.save();
	}

	changeView(next: FileView) {
		this.view = next;
		this.save();
	}

	/** A click on a column heading sorts by it; a second click turns the order around. */
	sortBy(column: FileSort) {
		if (!this.ready) return;
		if (this.sort === column) this.descending = !this.descending;
		else {
			this.sort = column;
			this.descending = false;
		}
		this.save();
	}

	saveFavorites(next: Favorite[]) {
		this.favorites = next;
		this.save();
	}

	toggleFavorite() {
		const { root, path } = this.#place();
		if (this.isFavorite)
			this.saveFavorites(this.favorites.filter((f) => f.root !== root || f.path !== path));
		else if (this.favorites.length < 50) this.saveFavorites([...this.favorites, { root, path }]);
	}
}
