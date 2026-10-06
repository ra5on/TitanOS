import path from 'node:path'

import {$} from 'execa'
import fse from 'fs-extra'

import type Titand from '../../index.js'
import {reboot} from './system.js'

const BACKUP_PREFIX = 'titan-factory-reset'
const RAID_FACTORY_RESET_PREPARE_HOOK = '/etc/rugix/hooks/state-reset/prepare/10-titan.sh'

// Factory reset using Rugix Ctrl's state management. This triggers an immediate reboot.
// We use the --backup flag which renames the old state directory instead of deleting
// it during boot. This makes boot fast (mv is instant) and we clean up the old
// state in the background after titand starts.
export async function performReset(titand: Titand) {
	const isRunningFromRaidData = await titand.hardware.raid.isRunningFromRaidData().catch(() => false)

	if (isRunningFromRaidData) {
		// Rugix resets the active data mount. When that mount is the RAID dataset,
		// using Rugix would move/reset the previous install and hide the saved boot
		// config needed for onboarding recovery. For RAID-backed data we only reset
		// the boot/internal state: clear the config partition, wipe the boot disk
		// data partition, leave the RAID pool untouched, then reboot to onboarding.
		await $`${RAID_FACTORY_RESET_PREPARE_HOOK}`
		await reboot()
		return
	}

	const timestamp = Math.floor(Date.now() / 1000)
	await $`rugix-ctrl state reset --backup --backup-name ${BACKUP_PREFIX}-${timestamp}`
}

// Clean up state backups from factory resets
export async function cleanupFactoryResetBackups(titand: Titand) {
	const stateDir = '/run/rugix/mounts/data/state'

	try {
		const entries = await fse.readdir(stateDir)

		// Loop through all backups in case multiple exist
		for (const entry of entries) {
			if (entry.startsWith(BACKUP_PREFIX)) {
				const backupPath = path.join(stateDir, entry)
				titand.logger.log(`Cleaning up factory reset backup: ${entry}`)
				await fse.remove(backupPath).catch((error) => titand.logger.error(`Failed to remove backup ${entry}`, error))
			}
		}
	} catch (error) {
		titand.logger.error('Failed to cleanup factory reset backups', error)
	}
}
