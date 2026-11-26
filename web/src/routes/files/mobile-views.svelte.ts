import { api } from '#lib/api.ts';
import { activeNode } from '#lib/cluster-node.ts';
import { t } from '#lib/i18n/index.svelte.ts';

import { layouts, type Layout } from '#lib/file-layouts.ts';
export { layouts, type Layout } from '#lib/file-layouts.ts';

export type ViewPlace = { root: string; path: string; node?: string };
export type FileLayouts = { default: Layout; folders: (ViewPlace & { view: Layout })[] };
export function layoutLabel(layout: Layout) {
	return t(layouts.find((item) => item.value === layout)!.label);
}
export function currentPlace(root: string, path: string): ViewPlace {
	const node = activeNode();
	return node ? { node, root, path } : { root, path };
}
export function samePlace(a: ViewPlace, b: ViewPlace) {
	return (a.node ?? '') === (b.node ?? '') && a.root === b.root && a.path === b.path;
}

/** Sends one change at a time. The response includes changes made by other clients. */
export class MobileViews {
	data = $state<FileLayouts>();
	saving = $state(false);
	error = $state('');
	success = $state('');
	folder(place: ViewPlace) {
		return this.data?.folders.find((folder) => samePlace(folder, place));
	}
	async load() {
		try {
			this.data = (await api.get<{ fileviews: FileLayouts }>('/preferences')).fileviews;
			this.error = '';
		} catch {
			this.error = t('files.preferencesLoadFailed');
		}
	}
	async change(view: Layout | 'inherit', folder?: ViewPlace) {
		if (!this.data || this.saving) return;
		this.saving = true;
		this.error = this.success = '';
		try {
			this.data = await api.put<FileLayouts>('/preferences/fileviews', { view, folder });
			if (view === 'inherit' && folder) {
				const name = folder.path.split('/').pop() || folder.root.split('/').at(-1) || folder.root;
				this.success = t('files.mobile.resetDone', { name, view: layoutLabel(this.data.default) });
			}
		} catch {
			this.error = t('files.preferencesSaveFailed');
		} finally {
			this.saving = false;
		}
	}
}
