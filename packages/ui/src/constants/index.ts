import {t} from '@/utils/i18n'

export const UNKNOWN = () => t('unknown')
// This is an en dash (U+2013)
export const LOADING_DASH = '–'

export const SETTINGS_SYSTEM_CARDS_ID = 'settings-system-cards'

export const hostEnvironments = ['titan-pro', 'titan-home', 'raspberry-pi', 'docker-container', 'unknown'] as const
export type TitanHostEnvironment = (typeof hostEnvironments)[number]

export const hostEnvironmentMap = {
	'titan-pro': {
		icon: '/assets/system-titan-pro.webp',
	},
	'titan-home': {
		icon: '/assets/system-titan-home.png',
	},
	'raspberry-pi': {
		icon: '/assets/system-pi.svg',
	},
	'docker-container': {
		icon: '/assets/system-docker.svg',
	},
	unknown: {
		icon: '/assets/system-generic-device.svg',
	},
} satisfies Record<
	TitanHostEnvironment,
	{
		icon?: string
	}
>
