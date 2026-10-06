import {DialogPortal} from '@radix-ui/react-dialog'
import {useState} from 'react'
import {useTranslation} from 'react-i18next'
import {useNavigate} from 'react-router-dom'

import {ImmersiveDialog, ImmersiveDialogOverlay} from '@/components/ui/immersive-dialog'
import {AppDropdown, ImmersivePickerDialogContentInit, ImmersivePickerItem} from '@/modules/immersive-picker'
import {usePickerTarget} from '@/modules/immersive-picker/target'
import {TroubleshootApp} from '@/routes/settings/troubleshoot/app'
import TroubleshootTitanOs from '@/routes/settings/troubleshoot/titanos'
import {useDialogOpenProps} from '@/utils/dialog'

export default function TroubleshootDialog() {
	const dialogProps = useDialogOpenProps('troubleshoot')
	const {target} = usePickerTarget('troubleshoot')

	return (
		<ImmersiveDialog {...dialogProps}>
			<DialogPortal>
				<ImmersiveDialogOverlay />
				{target.type === 'picker' && <PickerDialogContent />}
				{target.type === 'titanos' && <TroubleshootTitanOs />}
				{target.type === 'app' && <TroubleshootApp appId={target.appId} />}
			</DialogPortal>
		</ImmersiveDialog>
	)
}

function PickerDialogContent() {
	const {t} = useTranslation()
	const navigate = useNavigate()
	const {linkToTarget} = usePickerTarget('troubleshoot')
	const [appDialogOpen, setAppDialogOpen] = useState(false)

	return (
		<ImmersivePickerDialogContentInit title={t('troubleshoot-pick-title')}>
			<ImmersivePickerItem
				title={t('titanos')}
				description={t('troubleshoot.titanos-description')}
				to={linkToTarget({type: 'titanos'})}
			/>
			<ImmersivePickerItem
				title={t('troubleshoot.app')}
				description={t('troubleshoot.app-description')}
				onClick={() => setAppDialogOpen(true)}
			>
				<AppDropdown
					open={appDialogOpen}
					onOpenChange={setAppDialogOpen}
					setAppId={(appId) => navigate(linkToTarget({type: 'app', appId}))}
				/>
			</ImmersivePickerItem>
		</ImmersivePickerDialogContentInit>
	)
}
