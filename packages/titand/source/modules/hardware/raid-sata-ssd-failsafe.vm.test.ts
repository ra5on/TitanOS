import {expect, beforeAll, beforeEach, afterAll, afterEach, describe, test} from 'vitest'
import pWaitFor from 'p-wait-for'

import {createTestVm} from '../test-utilities/create-test-titand.js'

describe('RAID SATA SSD failsafe behavior', () => {
	let titand: Awaited<ReturnType<typeof createTestVm>>
	let sataSsdDeviceId1: string
	let sataSsdDeviceId2: string
	let sataSsdDeviceId3: string
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

	test('detects SATA SSDs as SSD type with sata transport', async () => {
		const devices = await titand.unauthenticatedClient.hardware.internalStorage.getDevices.query()
		const sataSsds = devices.filter((device) => device.type === 'ssd' && device.transport === 'sata')
		const hdds = devices.filter((device) => device.type === 'hdd')

		expect(sataSsds).toHaveLength(2)
		expect(hdds).toHaveLength(1)

		sataSsdDeviceId1 = sataSsds[0].id!
		sataSsdDeviceId2 = sataSsds[1].id!
		hddDeviceId = hdds[0].id!
		expect(sataSsdDeviceId1).toBeDefined()
		expect(sataSsdDeviceId2).toBeDefined()
		expect(hddDeviceId).toBeDefined()
	})

	test('registers user with 2-disk SATA SSD failsafe config (triggers reboot)', async () => {
		await titand.signup({raidDevices: [sataSsdDeviceId1, sataSsdDeviceId2], raidType: 'failsafe'})
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

	test('reports raidz failsafe topology for SATA SSDs', async () => {
		const status = await titand.client.hardware.raid.getStatus.query()
		expect(status.exists).toBe(true)
		expect(status.raidType).toBe('failsafe')
		expect(status.topology).toBe('raidz')
		expect(status.status).toBe('ONLINE')
		const deviceIds = status.devices!.map((device) => device.id).sort()
		expect(deviceIds).toEqual([sataSsdDeviceId1, sataSsdDeviceId2].sort())
	})

	test('rejects addMirror on SATA SSD raidz failsafe arrays', async () => {
		await expect(
			titand.client.hardware.raid.addMirror.mutate({deviceIds: [sataSsdDeviceId1, sataSsdDeviceId2]}),
		).rejects.toThrow('addMirror is only supported for mirror failsafe mode')
	})

	test('rejects adding HDD to SATA SSD failsafe arrays', async () => {
		await expect(titand.client.hardware.raid.addDevice.mutate({deviceId: hddDeviceId})).rejects.toThrow(
			'Cannot mix SSDs and HDDs',
		)
	})

	test('shuts down and adds third SATA SSD', async () => {
		await titand.vm.powerOff()
		await titand.vm.addSataSsd({slot: 4})
		await titand.vm.powerOn()
	})

	test('logs in after adding third SATA SSD', async () => {
		await titand.waitForStartup({waitForUser: true})
		await titand.login()
	})

	test('adds third SATA SSD to raidz failsafe array', async () => {
		const devices = await titand.client.hardware.internalStorage.getDevices.query()
		const availableSataSsds = devices.filter(
			(device) =>
				device.type === 'ssd' &&
				device.transport === 'sata' &&
				device.id !== sataSsdDeviceId1 &&
				device.id !== sataSsdDeviceId2,
		)
		expect(availableSataSsds).toHaveLength(1)
		sataSsdDeviceId3 = availableSataSsds[0].id!
		await titand.client.hardware.raid.addDevice.mutate({deviceId: sataSsdDeviceId3})
	})

	test('reports all SATA SSDs in failsafe raidz array', async () => {
		const status = await titand.client.hardware.raid.getStatus.query()
		expect(status.exists).toBe(true)
		expect(status.raidType).toBe('failsafe')
		expect(status.topology).toBe('raidz')
		expect(status.status).toBe('ONLINE')
		const deviceIds = status.devices!.map((device) => device.id).sort()
		expect(deviceIds).toEqual([sataSsdDeviceId1, sataSsdDeviceId2, sataSsdDeviceId3].sort())
	})
})
