// Commands of the file manager's bars and menus. The page carries them out,
// so the toolbars, the mobile bar and the menus all do the same thing.

import type { FileView } from '#lib/file-actions.ts';

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
