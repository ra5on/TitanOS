import {expect, beforeAll, beforeEach, afterAll, afterEach, describe, test} from 'vitest'
import pRetry from 'p-retry'

import {createTestVm} from '../test-utilities/create-test-titand.js'

describe.sequential('Network storage', () => {
	let titand: Awaited<ReturnType<typeof createTestVm>>
	let failed = false
	let primaryMountPath: string
	let primaryShareName: string

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

	async function createLocalSambaShare(shareName: string) {
		await titand.client.files.createDirectory.mutate({path: `/Home/${shareName}`})
		await titand.client.files.createDirectory.mutate({path: `/Home/${shareName}/source-marker`})
		await titand.api.post(`files/upload?path=/Home/${shareName}/test-file.txt`, {body: 'test content'})
		await titand.client.files.addShare.mutate({path: `/Home/${shareName}`})
	}

	async function mountLocalSambaShare(shareName: string) {
		const sharePassword = await titand.client.files.sharePassword.query()

		return pRetry(
			() =>
				titand.client.files.addNetworkShare.mutate({
					host: 'localhost',
					share: `${shareName} (Titan)`,
					username: 'titan',
					password: sharePassword,
				}),
			{retries: 10, factor: 1, minTimeout: 1000, maxTimeout: 1000},
		)
	}

	async function expectMountedShareToContain(mountPath: string, name: string) {
		await pRetry(
			async () => {
				const listing = await titand.client.files.list.query({path: mountPath})
				expect(listing.files.map((file) => file.name)).toContain(name)
			},
			{retries: 20, factor: 1, minTimeout: 1000, maxTimeout: 1000},
		)
	}

	test('rejects network-storage RPCs without authentication after setup', async () => {
		await expect(titand.unauthenticatedClient.files.listNetworkShares.query()).rejects.toThrow('Invalid token')
		await expect(
			titand.unauthenticatedClient.files.addNetworkShare.mutate({
				host: 'localhost',
				share: 'test',
				username: 'user',
				password: 'pass',
			}),
		).rejects.toThrow('Invalid token')
		await expect(
			titand.unauthenticatedClient.files.removeNetworkShare.mutate({mountPath: '/Network/test/share'}),
		).rejects.toThrow('Invalid token')
		await expect(titand.unauthenticatedClient.files.discoverNetworkShareServers.query()).rejects.toThrow(
			'Invalid token',
		)
		await expect(
			titand.unauthenticatedClient.files.discoverNetworkSharesOnServer.query({
				host: 'localhost',
				username: 'user',
				password: 'pass',
			}),
		).rejects.toThrow('Invalid token')
		await expect(
			titand.unauthenticatedClient.files.isServerAnTitanDevice.query({address: 'localhost'}),
		).rejects.toThrow('Invalid token')
	})

	test('starts with no configured network shares', async () => {
		await expect(titand.client.files.listNetworkShares.query()).resolves.toEqual([])
	})

	test('cleans up the mount directory when mounting fails', async () => {
		await expect(
			titand.client.files.addNetworkShare.mutate({
				host: '127.0.0.1',
				share: 'missing-share',
				username: 'titan',
				password: 'wrong-password',
			}),
		).rejects.toThrow()

		await pRetry(
			async () => {
				const networkRoot = await titand.client.files.list.query({path: '/Network'})
				expect(networkRoot.files).toHaveLength(0)
			},
			{retries: 20, factor: 1, minTimeout: 1000, maxTimeout: 1000},
		)
	})

	test('adds a local Samba share as a CIFS network share', async () => {
		primaryShareName = 'network-vm-test'
		await createLocalSambaShare(primaryShareName)

		primaryMountPath = await mountLocalSambaShare(primaryShareName)
		expect(primaryMountPath).toBe(`/Network/localhost/${primaryShareName} (Titan)`)

		await expect(titand.client.files.listNetworkShares.query()).resolves.toEqual([
			{
				host: 'localhost',
				share: `${primaryShareName} (Titan)`,
				mountPath: primaryMountPath,
				isMounted: true,
			},
		])
		await expectMountedShareToContain(primaryMountPath, 'test-file.txt')
		await expectMountedShareToContain(primaryMountPath, 'source-marker')

		await titand.client.files.createDirectory.mutate({path: `${primaryMountPath}/new-directory`})
		await expectMountedShareToContain(primaryMountPath, 'new-directory')
	})

	test('rejects duplicate network shares', async () => {
		const sharePassword = await titand.client.files.sharePassword.query()

		await expect(
			titand.client.files.addNetworkShare.mutate({
				host: 'localhost',
				share: `${primaryShareName} (Titan)`,
				username: 'titan',
				password: sharePassword,
			}),
		).rejects.toThrow('already exists')
	})

	test('rejects invalid credentials for mounting and discovery', async () => {
		await expect(
			titand.client.files.addNetworkShare.mutate({
				host: 'localhost',
				share: `${primaryShareName} (Titan)`,
				username: 'titan',
				password: 'wrong-password',
			}),
		).rejects.toThrow()

		await expect(
			titand.client.files.discoverNetworkSharesOnServer.query({
				host: 'localhost',
				username: 'titan',
				password: 'wrong-password',
			}),
		).rejects.toThrow()
	})

	test('discovers shares on the local Samba server', async () => {
		const secondShareName = 'network-vm-discover-test'
		await createLocalSambaShare(secondShareName)
		const sharePassword = await titand.client.files.sharePassword.query()

		await pRetry(
			async () => {
				const shares = await titand.client.files.discoverNetworkSharesOnServer.query({
					host: 'localhost',
					username: 'titan',
					password: sharePassword,
				})

				expect(shares).toEqual(expect.arrayContaining([`${primaryShareName} (Titan)`, `${secondShareName} (Titan)`]))
			},
			{retries: 20, factor: 1, minTimeout: 1000, maxTimeout: 1000},
		)
	})

	test('detects whether a network address is an Titan device', async () => {
		await expect(titand.client.files.isServerAnTitanDevice.query({address: 'localhost'})).resolves.toBe(true)
		await expect(titand.client.files.isServerAnTitanDevice.query({address: 'localhost:9'})).resolves.toBe(false)
	})

	test('enforces network-file permissions and protected mount paths', async () => {
		const hostnamePath = '/Network/localhost'
		const networkPath = '/Network'
		const networkFilePath = `${primaryMountPath}/test-file.txt`

		await expect(titand.client.files.trash.mutate({path: networkFilePath})).rejects.toThrow('[operation-not-allowed]')

		for (const path of [networkPath, hostnamePath, primaryMountPath]) {
			await expect(titand.client.files.trash.mutate({path})).rejects.toThrow('[operation-not-allowed]')
			await expect(titand.client.files.delete.mutate({path})).rejects.toThrow('[operation-not-allowed]')
			await expect(titand.client.files.move.mutate({path, toDirectory: '/Home'})).rejects.toThrow(
				'[operation-not-allowed]',
			)
			await expect(titand.client.files.rename.mutate({path, newName: 'Renamed Network Share'})).rejects.toThrow(
				'[operation-not-allowed]',
			)
		}

		await expect(titand.client.files.createDirectory.mutate({path: '/Network/localhost/test'})).rejects.toThrow(
			'[operation-not-allowed]',
		)
		await expect(
			titand.client.files.createDirectory.mutate({path: `/Network/localhost/${primaryShareName} Sibling`}),
		).rejects.toThrow('[operation-not-allowed]')

		for (const path of [networkPath, hostnamePath, primaryMountPath, networkFilePath]) {
			await expect(titand.client.files.addShare.mutate({path})).rejects.toThrow('[operation-not-allowed]')
		}

		await expect(titand.client.files.delete.mutate({path: networkFilePath})).resolves.toBe(true)
		const listing = await titand.client.files.list.query({path: primaryMountPath})
		expect(listing.files.map((file) => file.name)).not.toContain('test-file.txt')
	})

	test('removes a configured network share', async () => {
		await expect(titand.client.files.removeNetworkShare.mutate({mountPath: primaryMountPath})).resolves.toBe(true)
		await expect(titand.client.files.listNetworkShares.query()).resolves.not.toEqual(
			expect.arrayContaining([expect.objectContaining({mountPath: primaryMountPath})]),
		)
		await expect(
			titand.client.files.removeNetworkShare.mutate({mountPath: '/Network/non-existent/share'}),
		).rejects.toThrow('Share with mount path /Network/non-existent/share not found')
	})
})
