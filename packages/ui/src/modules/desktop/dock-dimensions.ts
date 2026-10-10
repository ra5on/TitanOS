// Mirrors the limits titand validates (modules/user/dock.ts)
export const DOCK_ICON_SIZE_MIN = 36
export const DOCK_ICON_SIZE_MAX = 72
export const DOCK_ICON_SIZE_DEFAULT = 50
export const DOCK_MAX_ITEMS = 16
// Phones keep their own ceiling whatever size was chosen on a desktop
export const DOCK_ICON_SIZE_MOBILE_MAX = 48

const DOCK_ICON_SIZE_FLOOR = 24
const DOCK_HORIZONTAL_PADDING = 24
const DOCK_EDGE_MARGIN = 16

/**
 * The largest icon size up to `size` at which `count` icons still fit the
 * viewport: gaps between icons, the dock's own padding and an edge margin.
 */
export function fitDockIconSize(size: number, count: number, viewportWidth: number, gap: number) {
	if (count <= 0) return size
	const available = viewportWidth - DOCK_EDGE_MARGIN - DOCK_HORIZONTAL_PADDING - gap * (count - 1)
	return Math.max(DOCK_ICON_SIZE_FLOOR, Math.min(size, Math.floor(available / count)))
}

export type DockItem =
	| {type: 'system'; id: 'files' | 'photos' | 'app-store' | 'machines' | 'settings' | 'live-usage'}
	| {type: 'app'; id: string}
	| {type: 'machine'; id: string}
	| {type: 'shortcut'; id: string}
export type DockConfig = {items: DockItem[]; iconSize: number}

export const dockItemKey = (item: DockItem) => `${item.type}:${item.id}`

/** The dock every account starts with. Machines are an owner feature. */
export function defaultDockConfig(isOwner: boolean): DockConfig {
	const ids = ['files', 'photos', 'app-store', ...(isOwner ? (['machines'] as const) : []), 'settings', 'live-usage'] as const
	return {items: ids.map((id) => ({type: 'system', id})), iconSize: DOCK_ICON_SIZE_DEFAULT}
}

/** Settings is where the dock is restored, so it always stays */
export const isRemovableDockItem = (item: DockItem) => !(item.type === 'system' && item.id === 'settings')
