import {useTranslation} from 'react-i18next'
import {RiRestartLine} from 'react-icons/ri'

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
import {useGlobalSystemState} from '@/providers/global-system-state/index'
import {useDialogOpenProps} from '@/utils/dialog'

export default function RestartDialog() {
	const {t} = useTranslation()
	const dialogProps = useDialogOpenProps('restart')

	const {restart, isPowerActionPending} = useGlobalSystemState()

	return (
		<AlertDialog {...dialogProps}>
			<AlertDialogContent>
				<AlertDialogHeader icon={RiRestartLine}>
					<AlertDialogTitle>{t('restart.confirm.title')}</AlertDialogTitle>
					<AlertDialogDescription>{t('desktop.power.restart-description')}</AlertDialogDescription>
				</AlertDialogHeader>
				<AlertDialogFooter>
					<AlertDialogCancel disabled={isPowerActionPending}>{t('no')}</AlertDialogCancel>
					<AlertDialogAction
						variant='destructive'
						className='px-6'
						onClick={(e) => {
							// Prevent closing by default
							e.preventDefault()
							restart()
						}}
						disabled={isPowerActionPending}
					>
						{t('yes')}
					</AlertDialogAction>
				</AlertDialogFooter>
			</AlertDialogContent>
		</AlertDialog>
	)
}
