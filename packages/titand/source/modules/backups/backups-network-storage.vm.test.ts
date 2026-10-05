import {expect, beforeAll, beforeEach, afterAll, afterEach, describe, test} from 'vitest'
import pRetry from 'p-retry'

import {createTestVm} from '../test-utilities/create-test-titand.js'
import {createNetworkBackupShare, repositoryPassword} from './backups.vm-test-helpers.js'

describe.sequential('Backups on network storage', () => {
	let titand: Awaited<ReturnType<typeof createTestVm>>
	let failed = false
	let repositoryId: string

	beforeAll(async () => {
		titand = await createTestVm({device: 'titan-home'})
		await titand.vm.powerOn()
		await titand.registerAndLogin()
	})

	afterAll(async () => await titand?.cleanup())

	afterEach(({task}) => {
		if (task.result?.state === 'fail') failed = true
	})

	beforeEach(({skip}) => {
		if (failed) skip()
	})

	test('creates a repository on a real CIFS network share', async () => {
		const networkSharePath = await createNetworkBackupShare(titand)
		await expect(titand.client.files.list.query({path: networkSharePath})).resolves.toBeDefined()

		repositoryId = await titand.client.backups.createRepository.mutate({
			path: networkSharePath,
			password: repositoryPassword,
		})
		expect(repositoryId).toMatch(/[a-f0-9]{8}$/)

		await expect(titand.client.backups.getRepositories.query()).resolves.toEqual([
			expect.objectContaining({
				id: repositoryId,
				path: `${networkSharePath}/Titan Backup.backup`,
			}),
		])
	})

	test('backs up to the network repository', async () => {
		await expect(titand.client.backups.listBackups.query({repositoryId})).resolves.toHaveLength(0)
		await expect(titand.client.backups.backup.mutate({repositoryId})).resolves.toBe(true)
		await expect(titand.client.backups.listBackups.query({repositoryId})).resolves.toHaveLength(1)
	})

	test('handles disconnected network shares gracefully', async () => {
		// There is no product API for simulating the remote SMB server going
		// offline while the configured repository remains in place.
		await titand.vm.sshAsRoot('systemctl stop smbd')
		await expect(titand.client.backups.backup.mutate({repositoryId})).rejects.toThrow()

		await titand.vm.sshAsRoot('systemctl start smbd')
		await pRetry(() => expect(titand.client.backups.backup.mutate({repositoryId})).resolves.toBe(true), {
			retries: 10,
			factor: 1,
			minTimeout: 1000,
			maxTimeout: 1000,
		})

		await expect(titand.client.backups.listBackups.query({repositoryId})).resolves.toHaveLength(2)
	})
})
