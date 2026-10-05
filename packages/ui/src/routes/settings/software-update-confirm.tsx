import {useTranslation} from 'react-i18next'

import {Markdown} from '@/components/markdown'
import {Button} from '@/components/ui/button'
import {Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle} from '@/components/ui/dialog'
import {ScrollArea} from '@/components/ui/scroll-area'
import {useGlobalSystemState} from '@/providers/global-system-state/index'
import {useSettingsDialogProps} from '@/routes/settings/_components/shared'
import {trpcReact} from '@/trpc/trpc'

export function SoftwareUpdateConfirmDialog() {
	const {t} = useTranslation()
	const {update} = useGlobalSystemState()
	const latestVersionQ = trpcReact.system.checkUpdate.useQuery()
	const dialogProps = useSettingsDialogProps()

	if (latestVersionQ.isLoading) {
		return null
	}

	return (
		<Dialog {...dialogProps}>
			<DialogContent className='px-0'>
				<DialogHeader className='px-4 sm:px-8'>
					<DialogTitle>{latestVersionQ.data?.name}</DialogTitle>
				</DialogHeader>
				<ScrollArea className='flex max-h-[500px] flex-col gap-5 px-4 sm:px-8'>
					<Markdown>{latestVersionQ.data?.releaseNotes}</Markdown>
				</ScrollArea>
				<DialogFooter className='px-4 sm:px-8'>
					<Button
						variant='primary'
						size='dialog'
						onClick={() => {
							dialogProps.onOpenChange(false)
							update()
						}}
					>
						{t('software-update.install-now')}
					</Button>
					<Button size='dialog' onClick={() => dialogProps.onOpenChange(false)}>
						{t('cancel')}
					</Button>
					{/* <DialogAction variant='destructive' className='px-6' onClick={logout}>
						{t('logout.confirm.submit')}
					</DialogAction>
					<DialogCancel>{t('cancel')}</DialogCancel> */}
				</DialogFooter>
			</DialogContent>
		</Dialog>
	)
}
