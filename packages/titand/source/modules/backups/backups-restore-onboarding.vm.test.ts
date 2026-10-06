import {expect, beforeAll, beforeEach, afterAll, afterEach, describe, test} from 'vitest'

import {createTestVm} from '../test-utilities/create-test-titand.js'
import {
	bootWithExternalStorage,
	externalPath,
	repositoryPassword,
	restoreBackupAndWait,
	waitForBackupsKopiaReady,
	waitForExternalStorage,
} from './backups.vm-test-helpers.js'

describe.sequential('Backup restore during onboarding', () => {
	let titand: Awaited<ReturnType<typeof createTestVm>>
	let failed = false

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

	test('creates a backup that can be restored after a reflash', async () => {
		await titand.client.files.createDirectory.mutate({path: '/Home/fresh-restore-marker'})
		const repositoryId = await titand.client.backups.createRepository.mutate({
			path: externalPath,
			password: repositoryPassword,
		})
		await expect(titand.client.backups.backup.mutate({repositoryId})).resolves.toBe(true)
		await expect(titand.client.backups.listBackups.query({repositoryId})).resolves.toHaveLength(1)
	})

	test('connects to an existing repository and restores before a user exists', async () => {
		await titand.vm.powerOff()
		await titand.vm.reflash()
		await titand.vm.powerOn()

		await waitForExternalStorage(titand, {authenticated: false})
		await expect(titand.unauthenticatedClient.user.exists.query()).resolves.toBe(false)
		await waitForBackupsKopiaReady(titand, {authenticated: false})

		await expect(
			titand.unauthenticatedClient.backups.connectToExistingRepository.mutate({
				path: externalPath,
				password: 'incorrect-password',
			}),
		).rejects.toThrow('invalid repository password')

		const restoredRepositoryId = await titand.unauthenticatedClient.backups.connectToExistingRepository.mutate({
			path: externalPath,
			password: repositoryPassword,
		})
		const backups = await titand.unauthenticatedClient.backups.listBackups.query({
			repositoryId: restoredRepositoryId,
		})
		const backup = backups.at(-1)
		expect(backup).toBeDefined()

		await restoreBackupAndWait({titand, backupId: backup!.id, authenticated: false})

		await expect(titand.unauthenticatedClient.user.exists.query()).resolves.toBe(true)
		// The backup contains the account, not a reusable login session. Restores
		// intentionally revoke all sessions, including the one from before reflash.
		const homeListing = await titand.client.files.list.query({path: '/Home'})
		expect(homeListing.files.map((file) => file.name)).toContain('fresh-restore-marker')
		// A real VM restore reboots into a fresh titand process, so the
		// in-memory restoreStatus resets after the restored install starts.
		await expect(titand.client.backups.restoreStatus.query()).resolves.toMatchObject({
			running: false,
			error: false,
		})
	})
})
