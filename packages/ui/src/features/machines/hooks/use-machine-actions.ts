import {toast} from '@/components/ui/toast'
import {trpcReact} from '@/trpc/trpc'
import {t} from '@/utils/i18n'

// Maps backend `[bracketed-error-codes]` to human readable messages.
// Keep translation keys as literal t() arguments so the translation updater
// can discover and preserve them.
export function getMachinesErrorMessage(message: string) {
	const code = message.match(/^\[([^\]]+)\]/)?.[1]
	if (!code) return t('machines-error.generic')

	switch (code) {
		case 'machine-bridge-wifi':
			return t('machines-error.machine-bridge-wifi')
		case 'machine-bridge-unsupported':
			return t('machines-error.machine-bridge-unsupported')
		case 'machine-bridge-busy':
			return t('machines-error.machine-bridge-busy')
		case 'machine-bridge-confirmation-expired':
		case 'machine-bridge-timeout':
			return t('machines-error.machine-bridge-timeout')
		case 'machine-image-download-connection-failed':
			return t('machines-error.machine-image-download-connection-failed')
		case 'machine-disk-shrink-not-allowed':
			return t('machines-error.machine-disk-shrink-not-allowed')
		case 'machine-first-boot-setup-in-progress':
			return t('machines-error.machine-first-boot-setup-in-progress')
		case 'machine-install-media-eject-failed':
			return t('machines-error.machine-install-media-eject-failed')
		case 'machine-external-disk-unavailable':
			return t('machines-error.machine-external-disk-unavailable')
		case 'machine-insufficient-storage':
			return t('machines-error.machine-insufficient-storage')
		case 'machine-install-interrupted':
			return t('machines-error.machine-install-interrupted')
		case 'machine-install-retry-credentials-required':
			return t('machines-error.machine-install-retry-credentials-required')
		case 'machine-port-conflict':
			return t('machines-error.machine-port-conflict')
		case 'machine-shutdown-timeout':
			return t('machines-error.machine-shutdown-timeout')
		case 'machine-start-failed':
			return t('machines-error.machine-start-failed')
		case 'machine-already-stopped':
			return t('machines-error.machine-already-stopped')
		case 'machine-audio-slot-invalid':
			return t('machines-error.machine-audio-slot-invalid')
		case 'machine-audio-unavailable':
			return t('machines-error.machine-audio-unavailable')
		case 'machine-backup-already-running':
			return t('machines-error.machine-backup-already-running')
		case 'machine-backup-in-progress':
			return t('machines-error.machine-backup-in-progress')
		case 'machine-backup-recovery-failed':
			return t('machines-error.machine-backup-recovery-failed')
		case 'machine-backup-release-failed':
			return t('machines-error.machine-backup-release-failed')
		case 'machine-catalog-architecture-mismatch':
			return t('machines-error.machine-catalog-architecture-mismatch')
		case 'machine-console-unavailable':
			return t('machines-error.machine-console-unavailable')
		case 'machine-coordinate-out-of-bounds':
			return t('machines-error.machine-coordinate-out-of-bounds')
		case 'machine-credentials-required':
			return t('machines-error.machine-credentials-required')
		case 'machine-custom-setting-catalog-image':
			return t('machines-error.machine-custom-setting-catalog-image')
		case 'machine-custom-setting-unsupported-on-arm':
			return t('machines-error.machine-custom-setting-unsupported-on-arm')
		case 'machine-disk-size-unavailable':
			return t('machines-error.machine-disk-size-unavailable')
		case 'machine-disk-too-small':
			return t('machines-error.machine-disk-too-small')
		case 'machine-display-closed':
			return t('machines-error.machine-display-closed')
		case 'machine-display-protocol':
			return t('machines-error.machine-display-protocol')
		case 'machine-display-rejected':
			return t('machines-error.machine-display-rejected')
		case 'machine-display-timeout':
			return t('machines-error.machine-display-timeout')
		case 'machine-display-unavailable':
			return t('machines-error.machine-display-unavailable')
		case 'machine-display-unsupported':
			return t('machines-error.machine-display-unsupported')
		case 'machine-dns-server-invalid':
			return t('machines-error.machine-dns-server-invalid')
		case 'machine-external-disk-in-use':
			return t('machines-error.machine-external-disk-in-use')
		case 'machine-external-disk-location-invalid':
			return t('machines-error.machine-external-disk-location-invalid')
		case 'machine-external-disk-location-readonly':
			return t('machines-error.machine-external-disk-location-readonly')
		case 'machine-firmware-unavailable':
			return t('machines-error.machine-firmware-unavailable')
		case 'machine-first-boot-token-invalid':
			return t('machines-error.machine-first-boot-token-invalid')
		case 'machine-force-stop-failed':
			return t('machines-error.machine-force-stop-failed')
		case 'machine-id-attempt-invalid':
			return t('machines-error.machine-id-attempt-invalid')
		case 'machine-image-external-data-not-supported':
			return t('machines-error.machine-image-external-data-not-supported')
		case 'machine-image-backing-chain-not-supported':
			return t('machines-error.machine-image-backing-chain-not-supported')
		case 'machine-image-checksum-mismatch':
			return t('machines-error.machine-image-checksum-mismatch')
		case 'machine-image-download-cancelled':
			return t('machines-error.machine-image-download-cancelled')
		case 'machine-image-download-failed':
			return t('machines-error.machine-image-download-failed')
		case 'machine-image-download-http-error':
			return t('machines-error.machine-image-download-http-error')
		case 'machine-image-format-unsupported':
			return t('machines-error.machine-image-format-unsupported')
		case 'machine-image-invalid':
			return t('machines-error.machine-image-invalid')
		case 'machine-image-redirect-invalid':
			return t('machines-error.machine-image-redirect-invalid')
		case 'machine-image-too-large':
			return t('machines-error.machine-image-too-large')
		case 'machine-image-too-many-redirects':
			return t('machines-error.machine-image-too-many-redirects')
		case 'machine-image-url-invalid':
			return t('machines-error.machine-image-url-invalid')
		case 'machine-image-url-private-address':
			return t('machines-error.machine-image-url-private-address')
		case 'machine-input-invalid':
			return t('machines-error.machine-input-invalid')
		case 'machine-install-cancelled':
			return t('machines-error.machine-install-cancelled')
		case 'machine-install-in-progress':
			return t('machines-error.machine-install-in-progress')
		case 'machine-install-not-complete':
			return t('machines-error.machine-install-not-complete')
		case 'machine-install-not-pending':
			return t('machines-error.machine-install-not-pending')
		case 'machine-ip-address-invalid':
			return t('machines-error.machine-ip-address-invalid')
		case 'machine-key-unknown':
			return t('machines-error.machine-key-unknown')
		case 'machine-lan-interface-unavailable':
			return t('machines-error.machine-lan-interface-unavailable')
		case 'machine-name-taken':
			return t('machines-error.machine-name-taken')
		case 'machine-network-full':
			return t('machines-error.machine-network-full')
		case 'machine-network-incompatible':
			return t('machines-error.machine-network-incompatible')
		case 'machine-bridge-unavailable':
			return t('machines-error.machine-bridge-unavailable')
		case 'machine-bridge-invalid':
			return t('machines-error.machine-bridge-invalid')
		case 'machine-network-change-requires-stopped':
			return t('machines-error.machine-network-change-requires-stopped')
		case 'machine-network-change-during-install':
			return t('machines-error.machine-network-change-during-install')
		case 'machine-network-catalog-install-requires-nat':
			return t('machines-error.machine-network-catalog-install-requires-nat')
		case 'machine-host-only-subnet-conflict':
			return t('machines-error.machine-host-only-subnet-conflict')
		case 'machine-not-found':
			return t('machines-error.machine-not-found')
		case 'machine-not-running':
			return t('machines-error.machine-not-running')
		case 'machine-not-stopped':
			return t('machines-error.machine-not-stopped')
		case 'machine-os-required':
			return t('machines-error.machine-os-required')
		case 'machine-platform-architecture-mismatch':
			return t('machines-error.machine-platform-architecture-mismatch')
		case 'machine-pointer-unsupported':
			return t('machines-error.machine-pointer-unsupported')
		case 'machine-port-outside-reserved-range':
			return t('machines-error.machine-port-outside-reserved-range')
		case 'machine-runtime-unmount-failed':
			return t('machines-error.machine-runtime-unmount-failed')
		case 'machine-state-unavailable':
			return t('machines-error.machine-state-unavailable')
		case 'machine-windows-iso-unsafe-path':
			return t('machines-error.machine-windows-iso-unsafe-path')
		case 'machine-windows-legacy-boot-image-invalid':
			return t('machines-error.machine-windows-legacy-boot-image-invalid')
		case 'machine-windows-license-key-required':
			return t('machines-error.machine-windows-license-key-required')
		case 'machine-windows-license-key-unexpected':
			return t('machines-error.machine-windows-license-key-unexpected')
		case 'machine-windows-source-invalid':
			return t('machines-error.machine-windows-source-invalid')
		case 'machine-windows-unattend-format-invalid':
			return t('machines-error.machine-windows-unattend-format-invalid')
		case 'machine-windows-username-invalid':
			return t('machines-error.machine-windows-username-invalid')
		default:
			return t('machines-error.generic')
	}
}

export function useMachineActions() {
	const utils = trpcReact.useUtils()

	const invalidateMachines = () => utils.machines.list.invalidate()

	const onError = (error: {message: string}) => toast.error(getMachinesErrorMessage(error.message), {area: 'machines'})

	const create = trpcReact.machines.create.useMutation({onError, onSettled: invalidateMachines}).mutateAsync
	const retryInstall = trpcReact.machines.retryInstall.useMutation({onError, onSettled: invalidateMachines}).mutate
	const start = trpcReact.machines.start.useMutation({onError, onSettled: invalidateMachines}).mutate
	const stop = trpcReact.machines.stop.useMutation({onError, onSettled: invalidateMachines}).mutate
	const restart = trpcReact.machines.restart.useMutation({onError, onSettled: invalidateMachines}).mutate
	const forceStop = trpcReact.machines.forceStop.useMutation({onError, onSettled: invalidateMachines}).mutate
	const ejectInstallMedia = trpcReact.machines.ejectInstallMedia.useMutation({
		onError,
		onSettled: invalidateMachines,
	}).mutateAsync
	const uninstall = trpcReact.machines.uninstall.useMutation({onError, onSettled: invalidateMachines}).mutateAsync
	const updateSettings = trpcReact.machines.updateSettings.useMutation({
		onError,
		onSettled: invalidateMachines,
	}).mutateAsync
	const setPinned = trpcReact.machines.setPinned.useMutation({onError, onSettled: invalidateMachines}).mutate

	return {
		create,
		retryInstall,
		start,
		stop,
		restart,
		forceStop,
		ejectInstallMedia,
		uninstall,
		updateSettings,
		setPinned,
	}
}
