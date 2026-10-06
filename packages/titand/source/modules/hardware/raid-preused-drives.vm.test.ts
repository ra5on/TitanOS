import {expect, beforeAll, beforeEach, afterAll, afterEach, describe, test} from 'vitest'

import pWaitFor from 'p-wait-for'

import {createTestVm} from '../test-utilities/create-test-titand.js'

describe('RAID operations with pre-used ZFS drives', () => {
	let titand: Awaited<ReturnType<typeof createTestVm>>
	let device1Id: string
	let device2Id: string
	let device3Id: string
	let device4Id: string
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

	// Phase 1: Create initial RAID with 4 SSDs to "pre-use" them with ZFS
	test('adds four NVMe devices and boots VM', async () => {
		await titand.vm.addNvme({slot: 1})
		await titand.vm.addNvme({slot: 2})
		await titand.vm.addNvme({slot: 3})
		await titand.vm.addNvme({slot: 4})
		await titand.vm.powerOn()
	})

	test('detects all four NVMe devices', async () => {
		const devices = await titand.unauthenticatedClient.hardware.internalStorage.getDevices.query()
		expect(devices).toHaveLength(4)
		device1Id = devices.find((d) => d.slot === 1)!.id!
		device2Id = devices.find((d) => d.slot === 2)!.id!
		device3Id = devices.find((d) => d.slot === 3)!.id!
		device4Id = devices.find((d) => d.slot === 4)!.id!
	})

	test('registers user with storage RAID using all 4 devices', async () => {
		await titand.signup({raidDevices: [device1Id, device2Id, device3Id, device4Id], raidType: 'storage'})
	})

	test('waits for RAID setup to complete', async () => {
		await pWaitFor(
			async () => {
				try {
					return await titand.unauthenticatedClient.hardware.raid.checkInitialRaidSetupStatus.query()
				} catch {
					return false
				}
			},
			{interval: 1000, timeout: 600_000},
		)
		await titand.login()
	})

	test('confirms RAID is set up with all 4 devices', async () => {
		const status = await titand.client.hardware.raid.getStatus.query()
		expect(status.exists).toBe(true)
		expect(status.raidType).toBe('storage')
		expect(status.devices).toHaveLength(4)
	})

	// Phase 2: Reflash to clear titanOS state but leave ZFS metadata on drives
	test('powers off and reflashes VM', async () => {
		await titand.vm.powerOff()
		await titand.vm.reflash()
		await titand.vm.powerOn()
	})

	// Phase 3: Test initial setup with pre-used SSD
	test('detects all devices after factory reset (with ZFS metadata still on them)', async () => {
		const devices = await titand.unauthenticatedClient.hardware.internalStorage.getDevices.query()
		expect(devices).toHaveLength(4)
		// Re-capture device IDs (should be the same)
		device1Id = devices.find((d) => d.slot === 1)!.id!
		device2Id = devices.find((d) => d.slot === 2)!.id!
		device3Id = devices.find((d) => d.slot === 3)!.id!
		device4Id = devices.find((d) => d.slot === 4)!.id!
	})

	test('sets up storage mode RAID with pre-used SSD (device 1)', async () => {
		await titand.signup({raidDevices: [device1Id], raidType: 'storage'})
	})

	test('waits for RAID setup with pre-used drive to complete', async () => {
		await pWaitFor(
			async () => {
				try {
					return await titand.unauthenticatedClient.hardware.raid.checkInitialRaidSetupStatus.query()
				} catch {
					return false
				}
			},
			{interval: 1000, timeout: 600_000},
		)
		await titand.login()
	})

	test('confirms storage mode RAID with 1 device', async () => {
		const status = await titand.client.hardware.raid.getStatus.query()
		expect(status.exists).toBe(true)
		expect(status.raidType).toBe('storage')
		expect(status.status).toBe('ONLINE')
		expect(status.devices).toHaveLength(1)
	})

	// Phase 4: Test transition to failsafe with pre-used SSD
	test('rejects transition with device already in array', async () => {
		await expect(
			titand.client.hardware.raid.transitionToFailsafeRaidz.mutate({newDeviceId: device1Id}),
		).rejects.toThrow('Cannot transition with a device that is already in the RAID array')
	})

	test('transitions to failsafe mode with pre-used SSD (device 2)', async () => {
		await titand.client.hardware.raid.transitionToFailsafeRaidz.mutate({newDeviceId: device2Id})
	})

	test('waits for transition to complete', async () => {
		await titand.waitForStartup({waitForUser: true})
		await titand.login()

		let transitionError: string | undefined
		await pWaitFor(
			async () => {
				try {
					const status = await titand.client.hardware.raid.getStatus.query()
					if (status.failsafeTransitionStatus?.state === 'error') {
						transitionError = status.failsafeTransitionStatus.error
						return true // Exit the loop, we'll throw after
					}
					return status.devices?.length === 2
				} catch {
					return false
				}
			},
			{interval: 1000, timeout: 600_000},
		)
		if (transitionError) throw new Error(transitionError)
	})

	test('confirms failsafe mode with 2 devices', async () => {
		const status = await titand.client.hardware.raid.getStatus.query()
		expect(status.exists).toBe(true)
		expect(status.raidType).toBe('failsafe')
		expect(status.devices).toHaveLength(2)
	})

	test('waits for transition to complete', async () => {
		await pWaitFor(
			async () => {
				try {
					const status = await titand.client.hardware.raid.getStatus.query()
					if (status.failsafeTransitionStatus?.state === 'complete') return true
					if (!status.failsafeTransitionStatus && status.status === 'ONLINE') return true
					return false
				} catch {
					return false
				}
			},
			{interval: 1000, timeout: 600_000},
		)
	})

	// Phase 5: Test expansion with pre-used SSD
	test('rejects expansion with device already in array', async () => {
		await expect(titand.client.hardware.raid.addDevice.mutate({deviceId: device1Id})).rejects.toThrow(
			'Cannot add a device that is already in the RAID array',
		)
	})

	test('expands failsafe array with pre-used SSD (device 3)', async () => {
		await titand.client.hardware.raid.addDevice.mutate({deviceId: device3Id})
	})

	test('waits for expansion to complete', async () => {
		await pWaitFor(
			async () => {
				try {
					const status = await titand.client.hardware.raid.getStatus.query()
					return status.devices?.length === 3
				} catch {
					return false
				}
			},
			{interval: 1000, timeout: 600_000},
		)
	})

	test('confirms failsafe mode with 3 devices after expansion', async () => {
		const status = await titand.client.hardware.raid.getStatus.query()
		expect(status.exists).toBe(true)
		expect(status.raidType).toBe('failsafe')
		expect(status.devices).toHaveLength(3)
	})

	// Phase 6: Test replace with pre-used SSD
	test('rejects replace with device already in array', async () => {
		await expect(
			titand.client.hardware.raid.replaceDevice.mutate({
				oldDevice: device1Id,
				newDevice: device2Id,
			}),
		).rejects.toThrow('Cannot replace with a device that is already in the RAID array')
	})

	test('replaces device 1 with pre-used SSD (device 4)', async () => {
		await titand.client.hardware.raid.replaceDevice.mutate({
			oldDevice: device1Id,
			newDevice: device4Id,
		})
	})

	test('waits for replace to complete', async () => {
		await pWaitFor(
			async () => {
				try {
					const status = await titand.client.hardware.raid.getStatus.query()
					return status.replace?.state === 'finished' || !status.replace
				} catch {
					return false
				}
			},
			{interval: 1000, timeout: 600_000},
		)
	})

	test('confirms array has device 4 instead of device 1', async () => {
		const status = await titand.client.hardware.raid.getStatus.query()
		const deviceIds = status.devices!.map((d) => d.id)
		expect(deviceIds).toContain(device4Id)
		expect(deviceIds).not.toContain(device1Id)
		expect(status.devices).toHaveLength(3)
	})

	// Phase 7: Reflash again to test storage mode expansion
	test('powers off and reflashes VM again', async () => {
		await titand.vm.powerOff()
		await titand.vm.reflash()
		await titand.vm.powerOn()
	})

	test('detects all devices after second factory reset', async () => {
		const devices = await titand.unauthenticatedClient.hardware.internalStorage.getDevices.query()
		expect(devices).toHaveLength(4)
		device1Id = devices.find((d) => d.slot === 1)!.id!
		device2Id = devices.find((d) => d.slot === 2)!.id!
	})

	// Phase 8: Test storage mode expansion with pre-used SSD
	test('sets up storage mode RAID with device 1', async () => {
		await titand.signup({raidDevices: [device1Id], raidType: 'storage'})
	})

	test('waits for storage mode setup to complete', async () => {
		await pWaitFor(
			async () => {
				try {
					return await titand.unauthenticatedClient.hardware.raid.checkInitialRaidSetupStatus.query()
				} catch {
					return false
				}
			},
			{interval: 1000, timeout: 600_000},
		)
		await titand.login()
	})

	test('rejects storage mode expansion with device already in array', async () => {
		await expect(titand.client.hardware.raid.addDevice.mutate({deviceId: device1Id})).rejects.toThrow(
			'Cannot add a device that is already in the RAID array',
		)
	})

	test('expands storage mode array with pre-used SSD (device 2)', async () => {
		await titand.client.hardware.raid.addDevice.mutate({deviceId: device2Id})
	})

	test('confirms storage mode with 2 devices after expansion', async () => {
		const status = await titand.client.hardware.raid.getStatus.query()
		expect(status.exists).toBe(true)
		expect(status.raidType).toBe('storage')
		expect(status.devices).toHaveLength(2)
	})

	test('pool is ONLINE after storage mode expansion', async () => {
		const status = await titand.client.hardware.raid.getStatus.query()
		expect(status.status).toBe('ONLINE')
	})
})
