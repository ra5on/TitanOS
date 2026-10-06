import {expect, beforeAll, beforeEach, afterAll, afterEach, describe, test} from 'vitest'
import pRetry from 'p-retry'

import {createTestVm} from '../test-utilities/create-test-titand.js'

describe.sequential('Network storage lifecycle', () => {
	let titand: Awaited<ReturnType<typeof createTestVm>>
	let failed = false
	let mountPath: string

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

	async function expectMountedShareToContain(name: string) {
		await pRetry(
			async () => {
				await expect(titand.client.files.listNetworkShares.query()).resolves.toEqual(
					expect.arrayContaining([
						expect.objectContaining({
							mountPath,
							isMounted: true,
						}),
					]),
				)

				const listing = await titand.client.files.list.query({path: mountPath})
				expect(listing.files.map((file) => file.name)).toContain(name)
			},
			// The remount watcher retries every 60 seconds. Allow one missed
			// startup mount attempt when Samba is still coming up after reboot.
			{retries: 90, factor: 1, minTimeout: 1000, maxTimeout: 1000},
		)
	}

	const cifsMountCount = async () =>
		Number((await titand.vm.ssh(`awk '$3 == "cifs" {count++} END {print count + 0}' /proc/mounts`)).trim())

	test('adds a CIFS share that later lifecycle checks can remount', async () => {
		const shareName = 'network-lifecycle-test'
		await createLocalSambaShare(shareName)

		mountPath = await mountLocalSambaShare(shareName)
		expect(mountPath).toBe(`/Network/localhost/${shareName} (Titan)`)
		await expectMountedShareToContain('source-marker')
	})

	test('auto-mounts configured network shares after a VM reboot', async () => {
		await titand.vm.powerOff()
		await titand.vm.powerOn()
		await titand.login()

		await expectMountedShareToContain('source-marker')
		await expect(titand.client.files.listNetworkShares.query()).resolves.toEqual(
			expect.arrayContaining([
				expect.objectContaining({
					mountPath,
					isMounted: true,
				}),
			]),
		)
	})

	test('unmounts CIFS shares when titand stops and remounts them on start', async () => {
		await pRetry(
			async () => {
				expect(await cifsMountCount()).toBeGreaterThan(0)
			},
			{retries: 20, factor: 1, minTimeout: 1000, maxTimeout: 1000},
		)

		// The files API has no endpoint for stopping titand while leaving the VM running;
		// restarting the systemd service verifies the real shutdown/startup mount handlers.
		await titand.vm.sshAsRoot('systemctl stop titan')
		await pRetry(
			async () => {
				expect(await cifsMountCount()).toBe(0)
			},
			{retries: 120, factor: 1, minTimeout: 1000, maxTimeout: 1000},
		)

		await titand.vm.sshAsRoot('systemctl start titan')
		await titand.waitForStartup({waitForUser: true})
		await titand.login()
		await expectMountedShareToContain('source-marker')
	})

	test('recovers when the SMB server goes offline and then comes back', async () => {
		// There is no product API for simulating a network file server outage. Stopping
		// smbd keeps the client VM alive while exercising the real CIFS recovery path.
		await titand.vm.sshAsRoot('systemctl stop smbd')

		await pRetry(
			async () => {
				await expect(
					titand.client.files.createDirectory.mutate({path: `${mountPath}/during-outage`}),
				).rejects.toThrow()
			},
			{retries: 20, factor: 1, minTimeout: 1000, maxTimeout: 1000},
		)

		await titand.vm.sshAsRoot('systemctl start smbd')

		// As above: the remount watcher only retries every 60 seconds, so allow a
		// full missed cycle before the share becomes writable again.
		await pRetry(() => titand.client.files.createDirectory.mutate({path: `${mountPath}/after-outage`}), {
			retries: 90,
			factor: 1,
			minTimeout: 1000,
			maxTimeout: 1000,
		})
		await expectMountedShareToContain('after-outage')
	})

	test('removes an unmounted configured share while its SMB server is offline', async () => {
		const offlineShareName = 'network-offline-removal-test'
		await createLocalSambaShare(offlineShareName)
		const offlineMountPath = await mountLocalSambaShare(offlineShareName)

		// Stop Titand while Samba is healthy so the real shutdown handler cleanly
		// detaches the CIFS filesystem and removes its empty mount directory.
		await titand.vm.sshAsRoot('systemctl stop titan')
		await pRetry(
			async () => {
				expect(await cifsMountCount()).toBe(0)
			},
			{retries: 120, factor: 1, minTimeout: 1000, maxTimeout: 1000},
		)
		// This VM also hosts the Samba fixture, and Titand normally starts it
		// for local shares. A runtime mask keeps that simulated remote server
		// offline while allowing Titand itself to start.
		await titand.vm.sshAsRoot('systemctl mask --runtime smbd')
		await titand.vm.sshAsRoot('systemctl start titan')
		await titand.waitForStartup({waitForUser: true})
		await titand.login()

		// The configured share is now offline. Removal must not depend on the
		// server returning or on the timing of internal mount-directory cleanup.
		await pRetry(
			async () => {
				await expect(titand.client.files.listNetworkShares.query()).resolves.toEqual(
					expect.arrayContaining([
						expect.objectContaining({
							mountPath: offlineMountPath,
							isMounted: false,
						}),
					]),
				)
			},
			{retries: 20, factor: 1, minTimeout: 500, maxTimeout: 500},
		)

		await expect(titand.client.files.removeNetworkShare.mutate({mountPath: offlineMountPath})).resolves.toBe(true)
		await expect(titand.client.files.listNetworkShares.query()).resolves.not.toEqual(
			expect.arrayContaining([expect.objectContaining({mountPath: offlineMountPath})]),
		)

		// Returning the server must not resurrect a share the user removed.
		await titand.vm.sshAsRoot('systemctl unmask --runtime smbd')
		await titand.vm.sshAsRoot('systemctl start smbd')
		await new Promise((resolve) => setTimeout(resolve, 1000))
		await expect(titand.client.files.listNetworkShares.query()).resolves.not.toEqual(
			expect.arrayContaining([expect.objectContaining({mountPath: offlineMountPath})]),
		)
	})
})
