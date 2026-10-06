import {hostEnvironmentMap, LOADING_DASH, TitanHostEnvironment, UNKNOWN} from '@/constants'
import {trpcReact} from '@/trpc/trpc'

type UiHostInfo = {
	icon?: string
	title: string
}

type DeviceInfoT =
	| {
			isLoading: true
			data: undefined
			uiData: UiHostInfo
	  }
	| {
			isLoading: false
			data: {
				titanHostEnvironment?: TitanHostEnvironment
				device?: string
				modelNumber?: string
				serialNumber?: string
				osVersionName?: string
			}
			uiData: UiHostInfo
	  }

export function useDeviceInfo(): DeviceInfoT {
	const osQ = trpcReact.system.version.useQuery()
	const deviceInfoQ = trpcReact.systemNg.device.getIdentity.useQuery()

	const isLoading = osQ.isLoading || deviceInfoQ.isLoading
	if (isLoading) {
		return {
			isLoading: true,
			data: undefined,
			uiData: {
				icon: undefined,
				title: LOADING_DASH,
			},
		} as const
	}

	const titanHostEnvironment: TitanHostEnvironment | undefined = deviceInfoToHostEnvironment(deviceInfoQ.data)

	const device = deviceInfoQ.data?.device
	const modelNumber = deviceInfoQ.data?.model
	const serialNumber = deviceInfoQ.data?.serial
	const osVersionName = osQ.data?.name

	return {
		isLoading,
		data: {
			titanHostEnvironment,
			device,
			modelNumber,
			serialNumber,
			osVersionName,
		},
		uiData: titanHostEnvironment
			? {
					icon: hostEnvironmentMap[titanHostEnvironment].icon,
					title: device || LOADING_DASH,
				}
			: {
					icon: undefined,
					title: UNKNOWN(),
				},
	}
}

export function deviceInfoToHostEnvironment(deviceInfo?: {productName: string}): TitanHostEnvironment | undefined {
	if (!deviceInfo) {
		return undefined
	}

	if (deviceInfo.productName.toLowerCase().includes('umbrel pro')) {
		return 'titan-pro'
	}

	if (deviceInfo.productName.toLowerCase().includes('umbrel home')) {
		return 'titan-home'
	}

	if (deviceInfo.productName.toLowerCase().includes('raspberry pi')) {
		return 'raspberry-pi'
	}

	if (deviceInfo.productName.toLowerCase().includes('docker')) {
		return 'docker-container'
	}

	// We return unknown and render a generic server icon
	return 'unknown'
}
