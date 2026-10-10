import {z} from 'zod'

// What an account pinned to its dock, in order. Entries only reference things
// by id: an uninstalled app or removed machine simply stops being shown.
export const DOCK_SYSTEM_ITEMS = ['files', 'photos', 'app-store', 'machines', 'settings', 'live-usage'] as const
export const DOCK_ICON_SIZE_MIN = 36
export const DOCK_ICON_SIZE_MAX = 72
export const DOCK_MAX_ITEMS = 16

const dockItemSchema = z.discriminatedUnion('type', [
	z.object({type: z.literal('system'), id: z.enum(DOCK_SYSTEM_ITEMS)}),
	z.object({type: z.literal('app'), id: z.string().min(1).max(200)}),
	z.object({type: z.literal('machine'), id: z.string().min(1).max(200)}),
	// A homescreen shortcut, referenced by its URL
	z.object({type: z.literal('shortcut'), id: z.string().min(1).max(2000)}),
])

export const dockConfigSchema = z
	.object({
		items: z.array(dockItemSchema).min(1).max(DOCK_MAX_ITEMS),
		iconSize: z.number().int().min(DOCK_ICON_SIZE_MIN).max(DOCK_ICON_SIZE_MAX),
	})
	.strict()
	.refine(({items}) => new Set(items.map((item) => `${item.type}:${item.id}`)).size === items.length, {
		message: 'Dock items must be unique',
	})
	// Settings is where the dock is restored: it can never be removed
	.refine(({items}) => items.some((item) => item.type === 'system' && item.id === 'settings'), {
		message: 'The dock must keep Settings',
	})

export type DockConfig = z.infer<typeof dockConfigSchema>
