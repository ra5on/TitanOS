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
					{label: t('no'), value: 'cancel', variant: 'default'},
					{label: t('yes'), value: 'confirm', variant: 'destructive'},
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
						className={`flex min-h-11 items-center gap-2 rounded-full border border-white/15 bg-black/25 px-3 text-13 font-semibold text-white backdrop-blur-xl transition-colors hover:bg-black/40 disabled:opacity-40 ${focusRingOnWallpaperClass}`}
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
					className='w-56 max-w-[calc(100vw-24px)]'
				>
					<DropdownMenuLabel className='truncate text-12 text-white/50'>{user?.name}</DropdownMenuLabel>
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
