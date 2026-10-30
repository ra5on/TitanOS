import { apiPath } from '#lib/cluster-node.ts';
import { isEditableText } from './text-files.ts';
export type Favorite = { root: string; path: string; label?: string };
export function readFavorites(value: string | null): Favorite[] {
	try {
		const parsed: unknown = JSON.parse(value ?? '[]');
		if (!Array.isArray(parsed)) return [];
		return parsed
			.filter(
				(x): x is Favorite =>
					!!x &&
					typeof x.root === 'string' &&
					/^(share|volume|usb)\/.+/.test(x.root) &&
					typeof x.path === 'string' &&
					x.path.length < 4096 &&
					!x.path.startsWith('/') &&
					!x.path.split('/').some((p: string) => p === '..' || p === '.') &&
					!/[\x00-\x1f]/.test(x.path),
			)
			.slice(0, 50)
			.map((x) => ({
				root: x.root,
				path: x.path,
				...(typeof x.label === 'string' && x.label.trim()
					? { label: x.label.trim().slice(0, 100) }
					: {}),
			}));
	} catch {
		return [];
	}
}
export function rangeSelection(names: string[], anchor: string, target: string): string[] {
	const a = names.indexOf(anchor),
		b = names.indexOf(target);
	if (b < 0) return [];
	return a < 0 ? [target] : names.slice(Math.min(a, b), Math.max(a, b) + 1);
}
export function previewKind(
	name: string,
	size: number,
): 'image' | 'text' | 'pdf' | 'audio' | 'video' | null {
	if (/\.(mp3|wav|ogg|m4a|flac)$/i.test(name)) return 'audio';
	if (/\.(mp4|webm|ogv)$/i.test(name)) return 'video';
	if (size <= 20 * 1024 * 1024 && /\.pdf$/i.test(name)) return 'pdf';
	if (size <= 20 * 1024 * 1024 && /\.(png|jpe?g|gif|webp)$/i.test(name)) return 'image';
	return isEditableText(name, size) ? 'text' : null;
}

export type FileView = 'list' | 'small' | 'large';
export type FolderView = {
	root: string;
	path: string;
	view: FileView;
	sort: 'name' | 'size' | 'modified';
	descending: boolean;
};
export type FilePreferences = {
	root: string;
	split: boolean;
	showHidden: boolean;
	view: FileView;
	sort: 'name' | 'size' | 'modified';
	descending: boolean;
	favorites: Favorite[];
	folders: FolderView[];
};
export function filePreferences(value: unknown): FilePreferences {
	const p = value && typeof value === 'object' ? (value as Record<string, unknown>) : {};
	return {
		split: p.split === true,
		showHidden: p.showHidden === true,
		root: typeof p.root === 'string' && /^(share|volume)\/.+/.test(p.root) ? p.root : '',
		view: p.view === 'small' || p.view === 'large' ? p.view : 'list',
		sort: p.sort === 'size' || p.sort === 'modified' ? p.sort : 'name',
		descending: p.descending === true,
		folders: Array.isArray(p.folders)
			? p.folders
					.filter(
						(x): x is FolderView =>
							!!x &&
							typeof x === 'object' &&
							typeof x.root === 'string' &&
							/^(share|volume|usb)\/.+/.test(x.root) &&
							typeof x.path === 'string' &&
							x.path.length < 4096 &&
							folderPath(x.path) === x.path &&
							['list', 'small', 'large'].includes(x.view) &&
							['name', 'size', 'modified'].includes(x.sort) &&
							typeof x.descending === 'boolean',
					)
					.slice(0, 200)
			: [],
		favorites: readFavorites(JSON.stringify(p.favorites ?? [])),
	};
}
/** Paths entered here are relative to the selected share/volume, never host paths. */
export function folderPath(value: string): string | null {
	const path = value.replace(/^\/+|\/+$/g, '');
	if (
		/[\x00-\x1f]/.test(path) ||
		path.split('/').some((p) => p === '..' || p === '.') ||
		path.includes('//')
	)
		return null;
	return path;
}

export function selectionType(name: string): string {
	const ext = name.split('.').at(-1)?.toLowerCase() ?? '';
	if (/^(png|jpe?g|gif|webp|svg|heic|avif)$/.test(ext)) return 'images';
	if (/^(mp4|webm|ogv|mkv|mov)$/.test(ext)) return 'video';
	if (/^(mp3|wav|ogg|m4a|flac)$/.test(ext)) return 'audio';
	if (/^(pdf|txt|md|json|docx?|xlsx?|odt|csv)$/.test(ext)) return 'documents';
	return 'other';
}

export type FileKind =
	'folder' | 'image' | 'video' | 'audio' | 'archive' | 'disc' | 'text' | 'file';
/** Picks the symbol of a list entry from its name; the contents are never read for this. */
export function fileKind(name: string, dir: boolean): FileKind {
	if (dir) return 'folder';
	const ext = name.includes('.') ? name.split('.').at(-1)!.toLowerCase() : '';
	if (/^(zip|tar|gz|tgz|bz2|xz|zst|7z|rar)$/.test(ext)) return 'archive';
	if (/^(iso|img|qcow2|vmdk|vhdx|vdi|raw)$/.test(ext)) return 'disc';
	const type = selectionType(name);
	if (type === 'images') return 'image';
	if (type === 'video') return 'video';
	if (type === 'audio') return 'audio';
	if (
		type === 'documents' ||
		/^(log|conf|ini|ya?ml|toml|xml|html?|css|js|ts|sh|py|go|env)$/.test(ext)
	)
		return 'text';
	return 'file';
}

/** Files titand makes thumbnails of (same list as backend/internal/thumbs). */
export type ThumbnailKind = 'image' | 'pdf' | 'video';
export function thumbnailKind(name: string): ThumbnailKind | null {
	if (/\.(jpe?g|png|gif|webp)$/i.test(name)) return 'image';
	if (/\.pdf$/i.test(name)) return 'pdf';
	if (/\.(mp4|m4v|mov|mkv|webm|avi|ogv|mts|m2ts|wmv)$/i.test(name)) return 'video';
	return null;
}
/** The modification time is part of the address, so the browser cache never shows an old version. */
export function thumbnailURL(root: string, path: string, size: number, modified?: string): string {
	return apiPath(
		'/files/thumb?' +
			new URLSearchParams({ root, path, size: String(size), ...(modified ? { v: modified } : {}) }),
	);
}
export type Photo = {
	name: string;
	size: number;
	modified: string;
	taken?: string;
	kind: 'image' | 'video';
};
/** Pictures by date taken (EXIF, else modification time), newest first, or by name. */
export function sortPhotos(list: Photo[], by: 'taken' | 'name', locale = 'de'): Photo[] {
	const when = (p: Photo) => Date.parse(p.taken || p.modified) || 0;
	const byName = (a: Photo, b: Photo) => a.name.localeCompare(b.name, locale, { numeric: true });
	return list.toSorted((a, b) =>
		by === 'name' ? byName(a, b) : when(b) - when(a) || byName(a, b),
	);
}
/** A horizontal swipe: -1 = next picture (finger to the left), 1 = previous, 0 = none. */
export function swipeDirection(dx: number, dy: number): -1 | 0 | 1 {
	if (Math.abs(dx) < 50 || Math.abs(dx) < Math.abs(dy) * 1.5) return 0;
	return dx < 0 ? -1 : 1;
}
/** Videos the browser may play from the authorized stream (backend media=1 allowlist). */
export function streamableVideo(name: string): boolean {
	return /\.(mp4|webm|ogv)$/i.test(name);
}
