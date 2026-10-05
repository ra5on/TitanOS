import {expect, beforeAll, beforeEach, afterAll, afterEach, describe, test} from 'vitest'
import pWaitFor from 'p-wait-for'

import {createTestVm} from '../test-utilities/create-test-titand.js'

describe('RAID device type validation - SATA SSD array rejects HDD', () => {
	let titand: Awaited<ReturnType<typeof createTestVm>>
	let ssdDeviceId1: string
	let ssdDeviceId2: string
	let hddDeviceId: string
	let failed = false

	beforeAll(async () => {
		titand = await createTestVm({device: 'nas'})
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

	test('adds SATA SSD and HDD devices and boots VM', async () => {
		await titand.vm.addSataSsd({slot: 1})
		await titand.vm.addSataSsd({slot: 2})
		await titand.vm.addHdd({slot: 3})
		await titand.vm.powerOn()
	})

	test('detects SATA SSDs and HDD with correct types', async () => {
		const devices = await titand.unauthenticatedClient.hardware.internalStorage.getDevices.query()
		expect(devices).toHaveLength(3)

		const sataSsds = devices.filter((d) => d.transport === 'sata' && d.type === 'ssd')
		const hdds = devices.filter((d) => d.type === 'hdd')

		expect(sataSsds).toHaveLength(2)
		expect(hdds).toHaveLength(1)
		expect(sataSsds.every((d) => d.type === 'ssd')).toBe(true)

		ssdDeviceId1 = sataSsds[0].id!
		ssdDeviceId2 = sataSsds[1].id!
		hddDeviceId = hdds[0].id!
		expect(ssdDeviceId1).toBeDefined()
		expect(ssdDeviceId2).toBeDefined()
		expect(hddDeviceId).toBeDefined()
	})

	test('registers user with single SSD RAID config (triggers reboot)', async () => {
		await titand.signup({raidDevices: [ssdDeviceId1], raidType: 'storage'})
	})

	test('waits for RAID setup to complete and logs in', async () => {
		await pWaitFor(
			async () => {
				try {
					return await titand.unauthenticatedClient.hardware.raid.checkInitialRaidSetupStatus.query()
				} catch (error) {
					if (error instanceof Error && error.message.includes('fetch failed')) return false
					throw error
				}
			},
			{interval: 2000, timeout: 600_000},
		)
		await titand.login()
	})

	test('reports correct RAID status with SATA SSD device', async () => {
		const status = await titand.client.hardware.raid.getStatus.query()
		expect(status.exists).toBe(true)
		expect(status.raidType).toBe('storage')
		expect(status.status).toBe('ONLINE')
		expect(status.devices).toHaveLength(1)
		expect(status.devices![0].id).toBe(ssdDeviceId1)
	})

	test('rejects adding HDD to SATA SSD RAID array', async () => {
		await expect(titand.client.hardware.raid.addDevice.mutate({deviceId: hddDeviceId})).rejects.toThrow(
			'Cannot mix SSDs and HDDs',
		)
	})

	test('rejects replacing SATA SSD with HDD', async () => {
		await expect(
			titand.client.hardware.raid.replaceDevice.mutate({oldDevice: ssdDeviceId1, newDevice: hddDeviceId}),
		).rejects.toThrow('Cannot mix SSDs and HDDs')
	})

	test('allows adding second SATA SSD to RAID array', async () => {
		await titand.client.hardware.raid.addDevice.mutate({deviceId: ssdDeviceId2})
	})

	test('reports correct RAID status with both SATA SSDs', async () => {
		const status = await titand.client.hardware.raid.getStatus.query()
		expect(status.exists).toBe(true)
		expect(status.raidType).toBe('storage')
		expect(status.status).toBe('ONLINE')
		expect(status.devices).toHaveLength(2)
		const deviceIds = status.devices!.map((d) => d.id).sort()
		expect(deviceIds).toEqual([ssdDeviceId1, ssdDeviceId2].sort())
	})
})

describe('RAID device type validation - NVMe SSD array rejects HDD', () => {
	let titand: Awaited<ReturnType<typeof createTestVm>>
	let ssdDeviceId1: string
	let ssdDeviceId2: string
	let hddDeviceId: string
	let failed = false

	beforeAll(async () => {
		titand = await createTestVm({device: 'nas'})
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

	test('adds NVMe SSD and HDD devices and boots VM', async () => {
		await titand.vm.addNvme({slot: 1})
		await titand.vm.addNvme({slot: 2})
		await titand.vm.addHdd({slot: 3})
		await titand.vm.powerOn()
	})

	test('detects NVMe SSDs and HDD with correct types', async () => {
		const devices = await titand.unauthenticatedClient.hardware.internalStorage.getDevices.query()
		expect(devices).toHaveLength(3)

		const nvmeSsds = devices.filter((d) => d.transport === 'nvme' && d.type === 'ssd')
		const hdds = devices.filter((d) => d.type === 'hdd')

		expect(nvmeSsds).toHaveLength(2)
		expect(hdds).toHaveLength(1)
		expect(nvmeSsds.every((d) => d.type === 'ssd')).toBe(true)

		ssdDeviceId1 = nvmeSsds[0].id!
		ssdDeviceId2 = nvmeSsds[1].id!
		hddDeviceId = hdds[0].id!
		expect(ssdDeviceId1).toBeDefined()
		expect(ssdDeviceId2).toBeDefined()
		expect(hddDeviceId).toBeDefined()
	})

	test('registers user with single SSD RAID config (triggers reboot)', async () => {
		await titand.signup({raidDevices: [ssdDeviceId1], raidType: 'storage'})
	})

	test('waits for RAID setup to complete and logs in', async () => {
		await pWaitFor(
			async () => {
				try {
					return await titand.unauthenticatedClient.hardware.raid.checkInitialRaidSetupStatus.query()
				} catch (error) {
					if (error instanceof Error && error.message.includes('fetch failed')) return false
					throw error
				}
			},
			{interval: 2000, timeout: 600_000},
		)
		await titand.login()
	})

	test('reports correct RAID status with NVMe SSD device', async () => {
		const status = await titand.client.hardware.raid.getStatus.query()
		expect(status.exists).toBe(true)
		expect(status.raidType).toBe('storage')
		expect(status.status).toBe('ONLINE')
		expect(status.devices).toHaveLength(1)
		expect(status.devices![0].id).toBe(ssdDeviceId1)
	})

	test('rejects adding HDD to NVMe SSD RAID array', async () => {
		await expect(titand.client.hardware.raid.addDevice.mutate({deviceId: hddDeviceId})).rejects.toThrow(
			'Cannot mix SSDs and HDDs',
		)
	})

	test('rejects replacing NVMe SSD with HDD', async () => {
		await expect(
			titand.client.hardware.raid.replaceDevice.mutate({oldDevice: ssdDeviceId1, newDevice: hddDeviceId}),
		).rejects.toThrow('Cannot mix SSDs and HDDs')
	})

	test('allows adding second NVMe SSD to RAID array', async () => {
		await titand.client.hardware.raid.addDevice.mutate({deviceId: ssdDeviceId2})
	})

	test('reports correct RAID status with both NVMe SSDs', async () => {
		const status = await titand.client.hardware.raid.getStatus.query()
		expect(status.exists).toBe(true)
		expect(status.raidType).toBe('storage')
		expect(status.status).toBe('ONLINE')
		expect(status.devices).toHaveLength(2)
		const deviceIds = status.devices!.map((d) => d.id).sort()
		expect(deviceIds).toEqual([ssdDeviceId1, ssdDeviceId2].sort())
	})
})
