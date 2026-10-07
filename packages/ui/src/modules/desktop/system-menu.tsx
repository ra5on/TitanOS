import {useId, useRef} from 'react'
import {useTranslation} from 'react-i18next'
import {TbChevronDown, TbLogout, TbPower, TbRefresh} from 'react-icons/tb'

import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuLabel,
	DropdownMenuSeparator,
	DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {useAuth} from '@/modules/auth/use-auth'
import {useConfirmation} from '@/providers/confirmation'
import {useDesktopAppearance} from '@/providers/desktop-appearance'
import {useGlobalSystemState} from '@/providers/global-system-state'
import {trpcReact} from '@/trpc/trpc'
import {focusRingOnWallpaperClass} from '@/utils/element-classes'

type SystemAction = 'logout' | 'restart' | 'shutdown'

export function SystemMenu() {
	const {t} = useTranslation()
	const {data: user} = trpcReact.user.get.useQuery()
	const {logout} = useAuth()
	const {restart, shutdown, isPowerActionPending} = useGlobalSystemState()
	const confirm = useConfirmation()
	const isOwner = user?.role === 'owner'

	const requestAction = async (action: SystemAction) => {
		if (isPowerActionPending || !user || (action !== 'logout' && !isOwner)) return
		const title =
			action === 'logout'
				? t('logout.confirm.title')
				: action === 'restart'
					? t('restart.confirm.title')
					: t('shut-down.confirm.title')
		const message =
			action === 'logout'
				? t('desktop.power.logout-description')
				: action === 'restart'
					? t('desktop.power.restart-description')
					: t('desktop.power.shutdown-description')
		try {
			const result = await confirm({
				title,
				message,
				actions: [
					{label: t('yes'), value: 'confirm', variant: 'destructive'},
					{label: t('no'), value: 'cancel', variant: 'default'},
				],
			})
			if (result.actionValue !== 'confirm') return
			if (action === 'logout') logout()
			else if (action === 'restart') restart()
			else shutdown()
		} catch {
			// No, Escape, or clicking outside dismisses without performing the action.
		}
	}

	return (
		<div className='absolute top-4 left-4 z-10 md:top-5 md:left-5'>
			<DropdownMenu>
				<DropdownMenuTrigger asChild>
					<button
						type='button'
						aria-label={t('desktop.system-menu')}
						disabled={!user || isPowerActionPending}
						className={`titan-desktop-glass flex min-h-11 items-center gap-2 rounded-full px-3 text-13 font-semibold text-white transition-colors hover:border-white/30 disabled:opacity-40 ${focusRingOnWallpaperClass}`}
					>
						<TbPower className='size-4.5' aria-hidden='true' />
						<span className='hidden min-[360px]:inline'>Titan</span>
						<TbChevronDown className='size-3.5 text-white/60' aria-hidden='true' />
					</button>
				</DropdownMenuTrigger>
				<DropdownMenuContent
					align='start'
					sideOffset={8}
					collisionPadding={12}
					className='titan-desktop-glass max-h-[var(--radix-dropdown-menu-content-available-height)] w-64 max-w-[calc(100vw-24px)] overflow-y-auto overscroll-contain'
				>
					<DropdownMenuLabel className='truncate text-12 text-white/70'>{user?.name}</DropdownMenuLabel>
					<DesktopAppearanceControl />
					<DropdownMenuSeparator />
					<DropdownMenuItem className='min-h-11 gap-2' onSelect={() => void requestAction('logout')}>
						<TbLogout className='size-4' aria-hidden='true' />
						{t('logout')}
					</DropdownMenuItem>
					{isOwner && (
						<>
							<DropdownMenuSeparator />
							<DropdownMenuItem className='min-h-11 gap-2' onSelect={() => void requestAction('restart')}>
								<TbRefresh className='size-4' aria-hidden='true' />
								{t('restart')}
							</DropdownMenuItem>
							<DropdownMenuItem className='min-h-11 gap-2 text-red-300' onSelect={() => void requestAction('shutdown')}>
								<TbPower className='size-4' aria-hidden='true' />
								{t('shut-down')}
							</DropdownMenuItem>
						</>
					)}
				</DropdownMenuContent>
			</DropdownMenu>
		</div>
	)
}

export function DesktopAppearanceControl() {
	const {t} = useTranslation()
	const {transparency, setTransparency, resetTransparency, isSaving, saveFailed} = useDesktopAppearance()
	const sliderId = useId()
	const input = useRef<HTMLInputElement>(null)
	const reset = useRef<HTMLDivElement>(null)

	return (
		<>
			<DropdownMenuSeparator />
			<DropdownMenuLabel className='text-12'>{t('desktop.appearance.title')}</DropdownMenuLabel>
			<DropdownMenuItem
				className='block cursor-default px-2 py-2 focus:bg-transparent'
				onSelect={(event) => event.preventDefault()}
				onFocus={(event) => {
					if (event.target === event.currentTarget) input.current?.focus()
				}}
			>
				<label htmlFor={sliderId} className='flex items-center justify-between gap-2 text-12'>
					<span>{t('desktop.appearance.transparency')}</span>
					<output htmlFor={sliderId} className='tabular-nums'>
						{transparency}%
					</output>
				</label>
				<input
					ref={input}
					id={sliderId}
					type='range'
					min={0}
					max={100}
					step={1}
					value={transparency}
					aria-valuetext={t('desktop.appearance.value', {value: transparency})}
					onChange={(event) => setTransparency(Number(event.currentTarget.value))}
					onKeyDown={(event) => {
						// Let the native range handle arrows/Home/End; Escape still closes the menu.
						if (event.key === 'Tab') {
							event.preventDefault()
							reset.current?.focus()
						}
						if (event.key !== 'Escape') event.stopPropagation()
					}}
					className='mt-1 h-10 w-full min-w-0 cursor-pointer accent-white'
				/>
				<div className='flex justify-between gap-2 text-11 text-white/80' aria-hidden='true'>
					<span>{t('desktop.appearance.opaque')}</span>
					<span>{t('desktop.appearance.transparent')}</span>
				</div>
			</DropdownMenuItem>
			<DropdownMenuItem
				ref={reset}
				className='min-h-11 text-12'
				onKeyDown={(event) => {
					if (event.key === 'Tab' && event.shiftKey) {
						event.preventDefault()
						event.stopPropagation()
						input.current?.focus()
					}
				}}
				onSelect={(event) => {
					event.preventDefault()
					resetTransparency()
				}}
			>
				{t('desktop.appearance.reset')}
			</DropdownMenuItem>
			<p role='status' className='px-2 pb-2 text-11 text-white/80'>
				{saveFailed
					? t('desktop.appearance.save-failed')
					: isSaving
						? t('desktop.appearance.saving')
						: t('desktop.appearance.personal')}
			</p>
		</>
	)
}
