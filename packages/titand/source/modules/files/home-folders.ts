// Default folders created in every home. TitanOS names them in German.
export const DOCUMENTS_FOLDER = 'Dokumente'
export const DOWNLOADS_FOLDER = 'Downloads'
export const PHOTOS_FOLDER = 'Fotos'
export const VIDEOS_FOLDER = 'Videos'
export const HOME_FOLDERS = [DOWNLOADS_FOLDER, DOCUMENTS_FOLDER, PHOTOS_FOLDER, VIDEOS_FOLDER]

// English names used before TitanOS 2.0.7, renamed once on startup
export const LEGACY_HOME_FOLDERS: Record<string, string> = {
	Documents: DOCUMENTS_FOLDER,
	Photos: PHOTOS_FOLDER,
}

export type PathRename = {from: string; to: string}

// Map a path inside a renamed folder to its new location
export function renamePath(path: string, renames: PathRename[]) {
	for (const {from, to} of renames) {
		if (path === from || path.startsWith(`${from}/`)) return to + path.slice(from.length)
	}
	return path
}

// Rewrite every string value and object key that points into a renamed folder
export function renamePathsDeep(value: unknown, renames: PathRename[]): unknown {
	if (typeof value === 'string') return renamePath(value, renames)
	if (Array.isArray(value)) return value.map((item) => renamePathsDeep(item, renames))
	// Only plain records; Dates and other instances are kept as they are
	const prototype = value && typeof value === 'object' ? Object.getPrototypeOf(value) : undefined
	if (prototype === Object.prototype || prototype === null) {
		return Object.fromEntries(
			Object.entries(value as Record<string, unknown>).map(([key, item]) => [
				renamePath(key, renames),
				renamePathsDeep(item, renames),
			]),
		)
	}
	return value
}
