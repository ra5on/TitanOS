import {useTranslation} from 'react-i18next'
import {Link} from 'react-router-dom'

import {Button} from '@/components/ui/button'

export function AppStorePreferencesContent() {
	const {t} = useTranslation()
	return (
		<div className='space-y-4'>
			<p className='text-14 leading-relaxed text-white/60'>{t('settings.app-store-preferences.manage-description')}</p>
			<Button asChild>
				<Link to='/settings/app-settings'>{t('settings.app-store-preferences.manage-button')}</Link>
			</Button>
		</div>
	)
}
