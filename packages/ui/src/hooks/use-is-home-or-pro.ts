import {useTranslation} from 'react-i18next'

import {useDeviceInfo} from '@/hooks/use-device-info'

// Consider consolidating device detection hooks. Currently we have:
// - useIsTitanHome (uses trpcReact.migration.isTitanHome)
// - useIsTitanPro (uses trpcReact.hardware.titanPro.isTitanPro)
// - useDeviceInfo (uses trpcReact.systemNg.device.getIdentity)
// - useIsHomeOrPro (uses useDeviceInfo)
// These could potentially be unified into a single source of truth.
export function useIsHomeOrPro() {
	const {t} = useTranslation()
	const {isLoading, data} = useDeviceInfo()
	const isTitanHome = data?.titanHostEnvironment === 'titan-home'
	const isTitanPro = data?.titanHostEnvironment === 'titan-pro'
	const isHomeOrPro = isTitanHome || isTitanPro
	const deviceName = isTitanPro ? 'Umbrel Pro' : isTitanHome ? 'Umbrel Home' : t('titan')

	return {
		isHomeOrPro,
		isLoading,
		deviceName,
		isTitanPro,
		isTitanHome,
	}
}
