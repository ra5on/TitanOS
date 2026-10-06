import nodePath from 'node:path'

import {expect, beforeAll, beforeEach, afterAll, afterEach, describe, test} from 'vitest'

import {createTestVm} from '../test-utilities/create-test-titand.js'
import {
	bootWithExternalStorage,
	externalPath,
	latestBackup,
	repositoryPassword,
	vmDataDirectory,
} from './backups.vm-test-helpers.js'

const gib = 1024 ** 3
const restoreBufferBytes = 5 * gib
const largeBackupFileBytes = 8 * gib
const targetFreeBytes = 2 * gib
const largeBackupFilePath = `${vmDataDirectory}/home/restore-space/large-backup-file.bin`
const fillerFilePath = `${vmDataDirectory}/home/restore-space/filler-file.bin`

describe.sequential('Backup restore free space checks', () => {
	let titand: Awaited<ReturnType<typeof createTestVm>>
	let failed = false
	let repositoryId: string
	let backupId: string

	beforeAll(async () => {
		titand = await createTestVm({device: 'titan-home'})
		await bootWithExternalStorage(titand)
	})

	afterAll(async () => await titand?.cleanup())

	afterEach(({task}) => {
		if (task.result?.state === 'fail') failed = true
	})

	beforeEach(({skip}) => {
		if (failed) skip()
	})

	async function allocateDataFile(path: string, sizeBytes: number) {
		const directory = nodePath.posix.dirname(path)

		// The files API is intentionally not used for multi-GB fixtures; allocate
		// data directly in the VM so restore space checks run against real disk usage.
		await titand.vm.sshAsRoot(`
set -eu
mkdir -p '${directory}'
rm -f '${path}'
fallocate -l ${sizeBytes} '${path}'
chown -R titan:titan '${directory}'
sync
`)
	}

	async function availableDataBytes() {
		const output = await titand.vm.sshAsRoot(`df -B1 --output=avail '${vmDataDirectory}' | tail -n 1 | tr -d ' '`)
		return Number(output.trim())
	}

	async function fillDataDiskLeaving(freeBytes: number) {
		const bytesToAllocateOutput = await titand.vm.sshAsRoot(`
set -eu
available="$(df -B1 --output=avail '${vmDataDirectory}' | tail -n 1 | tr -d ' ')"
if [ "$available" -le ${freeBytes} ]; then
	echo 0
else
	echo $((available - ${freeBytes}))
fi
`)
		const bytesToAllocate = Number(bytesToAllocateOutput.trim())
		expect(bytesToAllocate).toBeGreaterThan(0)
		await allocateDataFile(fillerFilePath, bytesToAllocate)
	}

	test('creates a backup large enough to require restore headroom', async () => {
		await allocateDataFile(largeBackupFilePath, largeBackupFileBytes)

		repositoryId = await titand.client.backups.createRepository.mutate({
			path: externalPath,
			password: repositoryPassword,
		})
		await expect(titand.client.backups.backup.mutate({repositoryId})).resolves.toBe(true)

		const backup = await latestBackup(titand, repositoryId)
		backupId = backup.id
		expect(backup.size).toBeGreaterThanOrEqual(largeBackupFileBytes)
	})

	test('rejects restore when the VM has insufficient free space', async () => {
		const backup = await latestBackup(titand, repositoryId)
		await fillDataDiskLeaving(targetFreeBytes)

		const availableBytes = await availableDataBytes()
		expect(availableBytes).toBeLessThan(backup.size + restoreBufferBytes)

		await expect(titand.client.backups.restoreBackup.mutate({backupId})).rejects.toThrow('[not-enough-space]')
		await expect(titand.client.backups.restoreStatus.query()).resolves.toMatchObject({
			running: false,
			error: false,
		})
	})
})
