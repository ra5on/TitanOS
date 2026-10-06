import {expect, beforeAll, beforeEach, afterAll, afterEach, describe, test} from 'vitest'
import pWaitFor from 'p-wait-for'

import {createTestVm} from '../test-utilities/create-test-titand.js'

describe('RAID storage mode', () => {
	let titand: Awaited<ReturnType<typeof createTestVm>>
	let firstDeviceId: string
	let secondDeviceId: string
	let initialTotalSpace: number
	let failed = false

	beforeAll(async () => {
		titand = await createTestVm()
	})

	afterAll(async () => {
		await titand?.cleanup()
	})

	afterEach(({task}) => {
		if (task.result?.state === 'fail') failed = true
	})

	beforeEach(({skip}) => {
		if (failed) skip()
	})

	test('adds NVMe device and boots VM', async () => {
		await titand.vm.addNvme({slot: 1})
		await titand.vm.powerOn()
	})

	test('detects NVMe device in slot 1', async () => {
		const devices = await titand.unauthenticatedClient.hardware.internalStorage.getDevices.query()
		expect(devices).toHaveLength(1)
		expect(devices[0].slot).toBe(1)
		firstDeviceId = devices[0].id!
		expect(firstDeviceId).toBeDefined()
	})

	test('registers user with RAID config (triggers reboot)', async () => {
		await titand.signup({raidDevices: [firstDeviceId], raidType: 'storage'})
	})

	test('waits for RAID setup to complete and logs in', async () => {
		await pWaitFor(
			async () => {
				try {
					return await titand.unauthenticatedClient.hardware.raid.checkInitialRaidSetupStatus.query()
				} catch (error) {
					// Ignore connection errors while VM is rebooting
					if (error instanceof Error && error.message.includes('fetch failed')) {
						return false
					}
					// Rethrow server errors (e.g., initialRaidSetupError)
					throw error
				}
			},
			{interval: 2000, timeout: 600_000},
		)
		await titand.login()
	})

	test('reports correct RAID status after setup', async () => {
		const status = await titand.client.hardware.raid.getStatus.query()
		expect(status.exists).toBe(true)
		expect(status.raidType).toBe('storage')
		expect(status.status).toBe('ONLINE')
		expect(status.devices).toHaveLength(1)
		expect(status.devices![0].id).toBe(firstDeviceId)
		initialTotalSpace = status.totalSpace!
		expect(initialTotalSpace).toBeGreaterThan(0)
	})

	test('creates marker directory to verify data consistency', async () => {
		await titand.client.files.createDirectory.mutate({path: '/Home/data-consistency-marker'})
		const listing = await titand.client.files.list.query({path: '/Home'})
		expect(listing.files.some((f) => f.name === 'data-consistency-marker')).toBe(true)
	})

	test('shuts down and adds second SSD', async () => {
		await titand.vm.powerOff()
		await titand.vm.addNvme({slot: 2})
		await titand.vm.powerOn()
	})

	test('logs in after adding second SSD', async () => {
		await titand.waitForStartup({waitForUser: true})
		await titand.login()
	})

	test('detects both NVMe devices after reboot', async () => {
		const devices = await titand.client.hardware.internalStorage.getDevices.query()
		expect(devices).toHaveLength(2)
		const secondDevice = devices.find((d) => d.slot === 2)
		expect(secondDevice).toBeDefined()
		secondDeviceId = secondDevice!.id!
		expect(secondDeviceId).toBeDefined()
	})

	test('adds second SSD to RAID array', async () => {
		await titand.client.hardware.raid.addDevice.mutate({deviceId: secondDeviceId})
	})

	test('reports correct RAID status with both devices', async () => {
		const status = await titand.client.hardware.raid.getStatus.query()
		expect(status.exists).toBe(true)
		expect(status.raidType).toBe('storage')
		expect(status.status).toBe('ONLINE')
		expect(status.devices).toHaveLength(2)
	})

	test('has both devices in the array', async () => {
		const status = await titand.client.hardware.raid.getStatus.query()
		const deviceIds = status.devices!.map((d) => d.id).sort()
		expect(deviceIds).toEqual([firstDeviceId, secondDeviceId].sort())
	})

	test('rejects replacing a storage device with another device already in the RAID array', async () => {
		await expect(
			titand.client.hardware.raid.replaceDevice.mutate({
				oldDevice: firstDeviceId,
				newDevice: secondDeviceId,
			}),
		).rejects.toThrow('Cannot replace with a device that is already in the RAID array')
	})

	test('total space increased after adding second device', async () => {
		const status = await titand.client.hardware.raid.getStatus.query()
		expect(status.totalSpace!).toBeGreaterThan(initialTotalSpace)
	})

	test('marker directory still exists after expansion', async () => {
		const listing = await titand.client.files.list.query({path: '/Home'})
		expect(listing.files.some((f) => f.name === 'data-consistency-marker')).toBe(true)
	})
})
