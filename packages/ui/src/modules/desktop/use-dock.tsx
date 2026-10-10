import {useEffect, useRef} from 'react'
import {useLocation, useNavigate, type LinkProps} from 'react-router-dom'

import {toast} from '@/components/ui/toast'
import type {TitanFeature} from '@/constants/feature-maturity'
import {MachineAppIcon} from '@/features/machines/components/machine-app-icon'
import {machinePath} from '@/features/machines/constants'
import {useMachines} from '@/features/machines/hooks/use-machines'
import {getLastFilesPath} from '@/features/files/utils/last-files-path'
import {useAppsWithUpdates} from '@/hooks/use-apps-with-updates'
import {useLaunchApp} from '@/hooks/use-launch-app'
import {useSettingsNotificationCount} from '@/hooks/use-settings-notification-count'
import {useApps} from '@/providers/apps'
import {trpcReact} from '@/trpc/trpc'
import {useLinkToDialog} from '@/utils/dialog'
import {t} from '@/utils/i18n'

import {defaultDockConfig, dockItemKey, type DockConfig, type DockItem} from './dock-dimensions'
import {resolveShortcutUrl} from './shortcut-dialog'
import {resolveShortcutIcon, ShortcutIconImage} from './shortcut-icon-image'

// One thing in the dock, resolved from its id to what is drawn and opened
export type DockEntry = {
	item: DockItem
	key: string
	label: string
	// System apps and installed apps are drawn from an image URL...
	bg?: string
	// ...machines and shortcuts bring their own icon
	icon?: React.ReactNode
	rounded?: boolean
	to?: LinkProps['to']
	onClick?: (event: React.MouseEvent) => void
	open: boolean
	notificationCount?: number
	feature?: TitanFeature
}

/** The account's dock, or the default while nothing was saved */
export function useDockConfig(): {config: DockConfig; isOwner: boolean; ready: boolean} {
	const {data: user} = trpcReact.user.get.useQuery()
	const isOwner = user?.role === 'owner'
	return {config: (user?.dock as DockConfig | undefined) ?? defaultDockConfig(isOwner), isOwner, ready: !!user}
}

/**
 * Save the dock. The query cache is updated first so the dock, its spacer and
 * every other consumer follow a drag or the size slider immediately; the
 * request itself is debounced.
 */
export function useSaveDockConfig() {
	const utils = trpcReact.useUtils()
	const mutation = trpcReact.user.set.useMutation()
	const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
	const pending = useRef<DockConfig | undefined>(undefined)

	const flush = () => {
		clearTimeout(timer.current)
		const dock = pending.current
		pending.current = undefined
		if (!dock) return
		mutation.mutate(
			{dock},
			{
				onError: () => {
					toast.error(t('dock.save-failed'))
					void utils.user.get.invalidate()
				},
			},
		)
	}
	// Leaving the page must not lose the last change
	const flushRef = useRef(flush)
	flushRef.current = flush
	useEffect(() => () => flushRef.current(), [])

	return {
		save: (dock: DockConfig) => {
			utils.user.get.setData(undefined, (previous) => (previous ? {...previous, dock} : previous))
			pending.current = dock
			clearTimeout(timer.current)
			timer.current = setTimeout(flush, 300)
		},
		flush,
	}
}

/**
 * Everything that can be in the dock for this account, keyed like dock items.
 * Members have no machines or shortcuts; apps are the ones they may open.
 */
export function useDockCatalog(isOwner: boolean) {
	const {systemAppsKeyed, userApps} = useApps()
	const {pathname, search} = useLocation()
	const navigate = useNavigate()
	const linkToDialog = useLinkToDialog()
	const launchApp = useLaunchApp()
	const settingsNotificationCount = useSettingsNotificationCount()
	const {appsWithUpdates} = useAppsWithUpdates()
	const {data: user} = trpcReact.user.get.useQuery()
	const {machines} = useMachines({enabled: isOwner})
	const shortcuts = trpcReact.shortcuts.list.useQuery(undefined, {enabled: isOwner, staleTime: Infinity}).data ?? []

	const system = (id: Extract<DockItem, {type: 'system'}>['id']): DockEntry | undefined => {
		const item: DockItem = {type: 'system', id}
		const app = systemAppsKeyed[`TITAN_${id}`]
		if (!app) return undefined
		const base = {item, key: dockItemKey(item), label: app.name, bg: app.icon}
		switch (id) {
			case 'files':
				return {
					...base,
					to: app.systemAppTo,
					// Read sessionStorage at click time, not render time, because React
					// Compiler may cache the render-time read and return a stale value.
					onClick: (event) => {
						event.preventDefault()
						navigate(getLastFilesPath(user?.userId) || app.systemAppTo!)
					},
					open: pathname.startsWith('/files'),
					feature: 'files',
				}
			case 'photos':
				return {...base, to: app.systemAppTo, open: pathname.startsWith(app.systemAppTo!), feature: 'photos'}
			case 'app-store':
				return {
					...base,
					to: app.systemAppTo,
					// Community stores live outside /app-store but are still the App Store
					open: pathname.startsWith(app.systemAppTo!) || pathname.startsWith('/community-app-store'),
					feature: 'app-store',
					// Members browse the app store read-only, updates are owner-only
					notificationCount: isOwner ? appsWithUpdates.length : undefined,
				}
			case 'machines':
				if (!isOwner) return undefined
				return {...base, to: app.systemAppTo, open: pathname.startsWith(app.systemAppTo!), feature: 'machines'}
			case 'settings':
				return {
					...base,
					to: app.systemAppTo,
					open: pathname.startsWith(app.systemAppTo!),
					notificationCount: settingsNotificationCount,
				}
			case 'live-usage':
				return {
					...base,
					to: linkToDialog('live-usage'),
					open: new URLSearchParams(search).get('dialog') === 'live-usage',
				}
		}
	}

	const entries: DockEntry[] = [
		...(['files', 'photos', 'app-store', 'machines', 'settings', 'live-usage'] as const).flatMap((id) => system(id) ?? []),
		...(userApps ?? []).map((app): DockEntry => {
			const item: DockItem = {type: 'app', id: app.id}
			return {
				item,
				key: dockItemKey(item),
				label: app.name,
				bg: app.icon,
				rounded: true,
				onClick: (event) => {
					event.preventDefault()
					launchApp(app.id)
				},
				open: false,
			}
		}),
		...machines.map((machine): DockEntry => {
			const item: DockItem = {type: 'machine', id: machine.id}
			return {
				item,
				key: dockItemKey(item),
				label: machine.name,
				icon: <MachineAppIcon osId={machine.osId} state={machine.state} className='size-full' />,
				to: machinePath(machine.id),
				open: pathname.startsWith(machinePath(machine.id)),
			}
		}),
		...shortcuts.map((shortcut): DockEntry => {
			const item: DockItem = {type: 'shortcut', id: shortcut.url}
			return {
				item,
				key: dockItemKey(item),
				label: shortcut.title,
				icon: (
					<ShortcutIconImage
						src={resolveShortcutIcon(shortcut)}
						title={shortcut.title}
						className='size-full rounded-[24%]'
					/>
				),
				onClick: (event) => {
					event.preventDefault()
					window.open(resolveShortcutUrl(shortcut), '_blank')?.focus()
				},
				open: false,
			}
		}),
	]
	return new Map(entries.map((entry) => [entry.key, entry]))
}

/** The entries shown in the dock: saved items that still exist, in order */
export function resolveDockEntries(config: DockConfig, catalog: Map<string, DockEntry>) {
	return config.items.flatMap((item) => catalog.get(dockItemKey(item)) ?? [])
}
