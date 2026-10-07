import {useRef} from 'react'
import {useTranslation} from 'react-i18next'
import {RiLogoutCircleRLine} from 'react-icons/ri'

import {
	AlertDialog,
	AlertDialogAction,
	AlertDialogCancel,
	AlertDialogContent,
	AlertDialogDescription,
	AlertDialogFooter,
	AlertDialogHeader,
	AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import {AccountAvatar} from '@/modules/auth/account-avatar'
import {useAuth} from '@/modules/auth/use-auth'
import {trpcReact} from '@/trpc/trpc'
import {useDialogOpenProps} from '@/utils/dialog'

export default function LogoutDialog() {
	const {t} = useTranslation()
	const cancelButton = useRef<HTMLButtonElement>(null)
	const dialogProps = useDialogOpenProps('logout')
	const {logout} = useAuth()
	const userQ = trpcReact.user.get.useQuery()

	return (
		<AlertDialog {...dialogProps}>
			<AlertDialogContent
				onOpenAutoFocus={(event) => {
					event.preventDefault()
					cancelButton.current?.focus()
				}}
			>
				<AlertDialogHeader icon={userQ.data ? undefined : RiLogoutCircleRLine}>
					{userQ.data && (
						<AccountAvatar
							name={userQ.data.name}
							userId={userQ.data.userId}
							avatarUrl={userQ.data.avatarUrl}
							size={80}
							className='mx-auto'
						/>
					)}
					<AlertDialogTitle>{t('logout.confirm.title')}</AlertDialogTitle>
					<AlertDialogDescription>{t('desktop.power.logout-description')}</AlertDialogDescription>
				</AlertDialogHeader>
				<AlertDialogFooter className='flex-row justify-center' dir='ltr'>
					<AlertDialogAction variant='destructive' className='min-w-0 flex-1 px-6' onClick={logout}>
						{t('yes')}
					</AlertDialogAction>
					<AlertDialogCancel ref={cancelButton} className='min-w-0 flex-1'>
						{t('no')}
					</AlertDialogCancel>
				</AlertDialogFooter>
			</AlertDialogContent>
		</AlertDialog>
	)
}
