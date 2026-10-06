import React from 'react'
import {useTranslation} from 'react-i18next'

export function IframeChecker({children}: {children: React.ReactNode}) {
	const {t} = useTranslation()
	const isIframe = window.self !== window.top

	if (isIframe) {
		return <div className='grid h-screen w-full place-items-center'>{t('iframe-embedding-unavailable')}</div>
	}
	return <>{children}</>
}
