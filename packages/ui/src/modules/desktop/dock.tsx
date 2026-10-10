import {motion, Reorder, useMotionValue} from 'motion/react'
import React, {useEffect, useRef, useState} from 'react'
import {RiAddLine, RiCloseLine} from 'react-icons/ri'
import {useWindowSize} from 'react-use'

import {Button} from '@/components/ui/button'
import {ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuTrigger} from '@/components/ui/context-menu'
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuLabel,
	DropdownMenuSeparator,
	DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {Slider} from '@/components/ui/slider'
import {useIsMobile} from '@/hooks/use-is-mobile'
import {cn} from '@/lib/utils'
import {systemAppsKeyed} from '@/providers/apps'
import {t} from '@/utils/i18n'
import {tw} from '@/utils/tw'

import {
	defaultDockConfig,
	DOCK_ICON_SIZE_MAX,
	DOCK_ICON_SIZE_MIN,
	DOCK_ICON_SIZE_MOBILE_MAX,
	DOCK_MAX_ITEMS,
	dockItemKey,
	fitDockIconSize,
	isRemovableDockItem,
	type DockConfig,
	type DockItem as DockConfigItem,
} from './dock-dimensions'
import {DockItem} from './dock-item'
import {prefetchRouteChunks} from './prefetch-route-chunks'
import {resolveDockEntries, useDockCatalog, useDockConfig, useSaveDockConfig, type DockEntry} from './use-dock'

const DOCK_BOTTOM_PADDING_PX = 10

// The settings preview draws the default dock at a fixed size
const DOCK_PREVIEW_DIMENSIONS_PX = {iconSize: 50, iconSizeZoomed: 80, padding: 12} as const

type DockDimensionsPx = {
	iconSize: number
	iconSizeZoomed: number
	padding: number
	dockHeight: number
}

function useDockDimensions(options?: {isPreview?: boolean}): DockDimensionsPx {
	const isMobile = useIsMobile()
	const {width: viewportWidth} = useWindowSize()
	const {config} = useDockConfig()

	if (options?.isPreview) {
		const {iconSize, iconSizeZoomed, padding} = DOCK_PREVIEW_DIMENSIONS_PX
		return {iconSize, iconSizeZoomed, padding, dockHeight: iconSize + padding * 2}
	}

	// The account's chosen size, shrunk when its icons would not fit the screen
	const chosen = isMobile ? Math.min(config.iconSize, DOCK_ICON_SIZE_MOBILE_MAX) : config.iconSize
	const iconSize = fitDockIconSize(chosen, config.items.length, viewportWidth, isMobile ? 8 : 10)
	const iconSizeZoomed = Math.round(iconSize * (isMobile ? 1.25 : 1.6))
	const padding = isMobile ? 8 : 12
	return {iconSize, iconSizeZoomed, padding, dockHeight: iconSize + padding * 2}
}

export function Dock() {
	const mouseX = useMotionValue(Infinity)
	const isMobile = useIsMobile()
	const {config, isOwner} = useDockConfig()
	const catalog = useDockCatalog(isOwner)
	const entries = resolveDockEntries(config, catalog)
	const {iconSize, iconSizeZoomed, padding, dockHeight} = useDockDimensions()
	const [editing, setEditing] = useState(false)

	return (
		<motion.div
			initial={{translateY: 80, opacity: 0}}
			animate={{translateY: 0, opacity: 1}}
			transition={{type: 'spring', stiffness: 200, damping: 20, delay: 0.2, duration: 0.2}}
			// The pointer reaching the dock precedes a click by a few hundred ms —
			// enough to finish warming a route chunk the idle prefetch didn't get to
			onPointerEnter={prefetchRouteChunks}
			onPointerMove={(e) => !editing && e.pointerType === 'mouse' && mouseX.set(e.pageX)}
			onPointerLeave={() => mouseX.set(Infinity)}
			className='shrink-0 transform-gpu will-change-transform'
		>
			{editing ? (
				<DockEditor
					config={config}
					entries={entries}
					catalog={catalog}
					isOwner={isOwner}
					iconSize={iconSize}
					dockHeight={dockHeight}
					padding={padding}
					onDone={() => setEditing(false)}
				/>
			) : (
				// Right-click (long-press on touch) offers the edit mode
				<ContextMenu>
					<ContextMenuTrigger asChild>
						<div
							data-dock
							className={cn(dockClass, isMobile && 'gap-2')}
							style={{height: dockHeight, paddingBottom: padding}}
						>
							{entries.map((entry) => (
								<DockItem
									key={entry.key}
									iconSize={iconSize}
									iconSizeZoomed={iconSizeZoomed}
									to={entry.to}
									onClick={entry.onClick}
									open={entry.open}
									bg={entry.bg}
									icon={entry.icon}
									className={entry.rounded ? 'rounded-[24%]' : undefined}
									label={entry.label}
									feature={entry.feature}
									notificationCount={entry.notificationCount}
									mouseX={mouseX}
								/>
							))}
						</div>
					</ContextMenuTrigger>
					<ContextMenuContent>
						<ContextMenuItem onSelect={() => setEditing(true)}>{t('dock.edit')}</ContextMenuItem>
					</ContextMenuContent>
				</ContextMenu>
			)}
		</motion.div>
	)
}

// Edit mode: icons wiggle, drag to reorder, × removes, and a toolbar above the
// dock adds entries, sets the size and restores the default. Every change is
// applied and saved immediately; "Done" or Escape only leaves the mode.
function DockEditor({
	config,
	entries,
	catalog,
	isOwner,
	iconSize,
	dockHeight,
	padding,
	onDone,
}: {
	config: DockConfig
	entries: DockEntry[]
	catalog: Map<string, DockEntry>
	isOwner: boolean
	iconSize: number
	dockHeight: number
	padding: number
	onDone: () => void
}) {
	const isMobile = useIsMobile()
	const {save, flush} = useSaveDockConfig()
	const done = () => {
		flush()
		onDone()
	}
	const doneRef = useRef(done)
	doneRef.current = done
	useEffect(() => {
		const onKeyDown = (event: KeyboardEvent) => event.key === 'Escape' && doneRef.current()
		window.addEventListener('keydown', onKeyDown)
		return () => window.removeEventListener('keydown', onKeyDown)
	}, [])

	// Saved items that cannot be shown right now (e.g. an app list still
	// loading) are kept behind the visible ones instead of being dropped
	const hidden = config.items.filter((item) => !catalog.has(dockItemKey(item)))
	const saveItems = (items: DockConfigItem[]) => save({...config, items: [...items, ...hidden]})
	const visible = entries.map((entry) => entry.item)
	const shown = new Set(entries.map((entry) => entry.key))
	const addable = [...catalog.values()].filter((entry) => !shown.has(entry.key))
	const full = config.items.length >= DOCK_MAX_ITEMS
	const groups: [string, DockEntry[]][] = [
		[t('dock.add-system'), addable.filter((entry) => entry.item.type === 'system')],
		[t('dock.add-apps'), addable.filter((entry) => entry.item.type === 'app')],
		[t('dock.add-machines'), addable.filter((entry) => entry.item.type === 'machine')],
		[t('dock.add-shortcuts'), addable.filter((entry) => entry.item.type === 'shortcut')],
	]

	return (
		<div className='relative'>
			<div className='titan-desktop-glass absolute bottom-full left-1/2 mb-3 flex w-max max-w-[calc(100vw-16px)] -translate-x-1/2 flex-wrap items-center justify-center gap-x-3 gap-y-2 rounded-2xl px-3 py-2'>
				<DropdownMenu>
					<DropdownMenuTrigger asChild>
						<Button size='sm' disabled={full || addable.length === 0} title={full ? t('dock.add-full') : undefined}>
							<RiAddLine className='size-3.5' />
							{t('dock.add')}
						</Button>
					</DropdownMenuTrigger>
					<DropdownMenuContent align='start' side='top' className='max-h-[50vh] min-w-48 overflow-y-auto p-1'>
						{groups
							.filter(([, group]) => group.length > 0)
							.map(([label, group], index) => (
								<React.Fragment key={label}>
									{index > 0 && <DropdownMenuSeparator />}
									<DropdownMenuLabel>{label}</DropdownMenuLabel>
									{group.map((entry) => (
										<DropdownMenuItem key={entry.key} onSelect={() => saveItems([...visible, entry.item])}>
											{entry.label}
										</DropdownMenuItem>
									))}
								</React.Fragment>
							))}
					</DropdownMenuContent>
				</DropdownMenu>
				{!isMobile && (
					<label className='flex items-center gap-2 text-12 -tracking-2 text-white/70'>
						{t('dock.size')}
						<Slider
							className='w-28'
							min={DOCK_ICON_SIZE_MIN}
							max={DOCK_ICON_SIZE_MAX}
							step={2}
							value={[config.iconSize]}
							onValueChange={([value]) => save({...config, iconSize: value})}
							aria-label={t('dock.size')}
						/>
					</label>
				)}
				<Button size='sm' onClick={() => save(defaultDockConfig(isOwner))}>
					{t('dock.reset')}
				</Button>
				<Button size='sm' variant='primary' onClick={done}>
					{t('dock.done')}
				</Button>
			</div>
			<Reorder.Group
				as='div'
				data-dock
				data-dock-editing
				axis='x'
				values={entries.map((entry) => entry.key)}
				onReorder={(keys: string[]) => saveItems(keys.flatMap((key) => catalog.get(key)?.item ?? []))}
				className={cn(dockClass, isMobile && 'gap-2')}
				style={{height: dockHeight, paddingBottom: padding}}
			>
				{entries.map((entry) => (
					<Reorder.Item
						as='div'
						data-dock-item={entry.key}
						key={entry.key}
						value={entry.key}
						className='relative shrink-0 cursor-grab touch-none active:cursor-grabbing'
						style={{width: iconSize, height: iconSize}}
					>
						<motion.div
							animate={{rotate: [-1.5, 1.5, -1.5]}}
							transition={{duration: 0.4, repeat: Infinity, ease: 'easeInOut'}}
							className={cn('size-full bg-cover', entry.rounded && 'rounded-[24%]')}
							style={{backgroundImage: entry.icon || !entry.bg ? undefined : `url(${entry.bg})`}}
							title={entry.label}
						>
							{entry.icon}
						</motion.div>
						{isRemovableDockItem(entry.item) && (
							<button
								type='button'
								// Not the start of a drag
								onPointerDown={(event) => event.stopPropagation()}
								onClick={() => saveItems(visible.filter((item) => dockItemKey(item) !== entry.key))}
								aria-label={t('dock.remove-item', {name: entry.label})}
								className='absolute -top-1.5 -left-1.5 grid size-5 place-items-center rounded-full bg-black/80 text-white ring-1 ring-white/30 hover:bg-black focus-visible:ring-2 focus-visible:ring-white/80 focus-visible:outline-hidden'
							>
								<RiCloseLine className='size-3.5' />
							</button>
						)}
					</Reorder.Item>
				))}
			</Reorder.Group>
		</div>
	)
}

export function DockPreview() {
	const mouseX = useMotionValue(Infinity)
	const {iconSize, iconSizeZoomed, padding, dockHeight} = useDockDimensions({isPreview: true})

	return (
		<div
			className={dockPreviewClass}
			style={{
				height: dockHeight,
				paddingBottom: padding,
			}}
		>
			<DockItem
				bg={systemAppsKeyed['TITAN_files'].icon}
				mouseX={mouseX}
				iconSize={iconSize}
				iconSizeZoomed={iconSizeZoomed}
			/>
			<DockItem
				bg={systemAppsKeyed['TITAN_photos'].icon}
				mouseX={mouseX}
				iconSize={iconSize}
				iconSizeZoomed={iconSizeZoomed}
			/>
			<DockItem
				bg={systemAppsKeyed['TITAN_app-store'].icon}
				mouseX={mouseX}
				iconSize={iconSize}
				iconSizeZoomed={iconSizeZoomed}
			/>
			<DockItem
				bg={systemAppsKeyed['TITAN_machines'].icon}
				mouseX={mouseX}
				iconSize={iconSize}
				iconSizeZoomed={iconSizeZoomed}
			/>
			<DockItem
				bg={systemAppsKeyed['TITAN_settings'].icon}
				mouseX={mouseX}
				iconSize={iconSize}
				iconSizeZoomed={iconSizeZoomed}
			/>
			<DockItem
				bg={systemAppsKeyed['TITAN_live-usage'].icon}
				mouseX={mouseX}
				iconSize={iconSize}
				iconSizeZoomed={iconSizeZoomed}
			/>
		</div>
	)
}

// How much of the bottom of the screen the dock takes, in px: what content
// that runs beneath it must leave clear to scroll fully into view
export function useDockClearance() {
	const {dockHeight} = useDockDimensions()
	return dockHeight + DOCK_BOTTOM_PADDING_PX
}

export function DockSpacer({className}: {className?: string}) {
	const height = useDockClearance()
	return <div className={cn('w-full shrink-0', className)} style={{height}} />
}

export function DockBottomPositioner({children}: {children: React.ReactNode}) {
	return (
		<div className='fixed bottom-0 left-1/2 z-50 -translate-x-1/2' style={{paddingBottom: DOCK_BOTTOM_PADDING_PX}}>
			{children}
		</div>
	)
}

const dockClass = tw`titan-desktop-glass mx-auto flex items-end gap-2.5 rounded-2xl px-3 shrink-0`
const dockPreviewClass = tw`titan-desktop-glass mx-auto flex items-end gap-4 rounded-2xl px-3 shrink-0`
