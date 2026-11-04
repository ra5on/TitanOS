// Commands of the file manager's bars and menus. The page carries them out,
// so the toolbars, the mobile bar and the menus all do the same thing.

import { api, type FileRoot } from '#lib/api.ts';
import type { FileView } from '#lib/file-actions.ts';

/** Whether deleting a path (relative to the root) goes to a recycle bin: in a share with its bin
 * on, or in a volume inside the folder of such a share (not the folder itself). */
export const inTrash = (root: FileRoot | undefined, path: string) =>
	!!root?.recycle || !!root?.bins?.some((bin) => path.startsWith(bin + '/'));

/** Deletes paths: those with a recycle bin go there unless `permanent`, the others for good. */
export async function deletePaths(
	root: FileRoot,
	paths: string[],
	permanent: boolean,
	extra: Record<string, unknown> = {},
) {
	const binned = permanent ? [] : paths.filter((p) => inTrash(root, p));
	const rest = paths.filter((p) => !binned.includes(p));
	if (binned.length)
		await api.post('/files/delete', { root: root.id, paths: binned, permanent: false, ...extra });
	if (rest.length)
		await api.post('/files/delete', { root: root.id, paths: rest, permanent: true, ...extra });
}

/** A share has its recycle bin; a volume shows the bins of its shares. USB and the stick have none. */
export const hasTrash = (root: FileRoot | undefined) =>
	root?.kind === 'share' || (root?.kind === 'volume' && !!root.bins?.length);

export const views = [
	{ mode: 'list', icon: 'list', label: 'files.view.list' },
	{ mode: 'small', icon: 'gridSmall', label: 'files.view.small' },
	{ mode: 'large', icon: 'grid', label: 'files.view.large' },
] as const;

export type FileCommand =
	// Navigation
	| 'up'
	| 'back'
	| 'forward'
	| 'editPath'
	| 'favorite'
	// Creating and uploading
	| 'mkdir'
	| 'newFile'
	| 'upload'
	| 'uploadFolder'
	| 'urlDownload'
	// The selection
	| 'selectAll'
	| 'clearSelection'
	| 'invertSelection'
	| 'selectType'
	| 'openFolder'
	| 'preview'
	| 'edit'
	| 'download'
	| 'archive'
	| 'properties'
	| 'clipboardCopy'
	| 'cut'
	| 'paste'
	| 'copy'
	| 'move'
	| 'restore'
	| 'rename'
	| 'renameBatch'
	| 'delete'
	// Tools and views
	| 'deepSearch'
	| 'usage'
	| 'duplicates'
	| 'compare'
	| 'photos'
	| 'trash'
	| 'twoPanes'
	| 'showHidden'
	| 'folderView'
	| 'shortcuts'
	| FileView;
