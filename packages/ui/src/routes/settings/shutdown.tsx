import {useRef} from 'react'
import {useTranslation} from 'react-i18next'
import {RiShutDownLine} from 'react-icons/ri'

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

export default function ShutdownDialog() {
	const {t} = useTranslation()
	const cancelButton = useRef<HTMLButtonElement>(null)
	const dialogProps = useDialogOpenProps('shutdown')

	const {shutdown, isPowerActionPending} = useGlobalSystemState()

	return (
		<AlertDialog {...dialogProps}>
			<AlertDialogContent
				onOpenAutoFocus={(event) => {
					event.preventDefault()
					cancelButton.current?.focus()
				}}
			>
				<AlertDialogHeader icon={RiShutDownLine}>
					<AlertDialogTitle>{t('shut-down.confirm.title')}</AlertDialogTitle>
					<AlertDialogDescription>{t('desktop.power.shutdown-description')}</AlertDialogDescription>
				</AlertDialogHeader>
				<AlertDialogFooter className='flex-row justify-center' dir='ltr'>
					<AlertDialogAction
						variant='destructive'
						onClick={(e) => {
							// Prevent closing by default
							e.preventDefault()
							shutdown()
						}}
						disabled={isPowerActionPending}
					>
						{t('yes')}
					</AlertDialogAction>
					<AlertDialogCancel ref={cancelButton} className='min-w-0 flex-1' disabled={isPowerActionPending}>
						{t('no')}
					</AlertDialogCancel>
				</AlertDialogFooter>
			</AlertDialogContent>
		</AlertDialog>
	)
}
