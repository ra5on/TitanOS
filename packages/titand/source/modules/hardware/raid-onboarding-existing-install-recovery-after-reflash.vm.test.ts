import {expect, beforeAll, beforeEach, afterAll, afterEach, describe, test} from 'vitest'
import pWaitFor from 'p-wait-for'

import {createTestVm} from '../test-utilities/create-test-titand.js'

describe('Degraded RAID onboarding recovery after boot disk reflash', () => {
	let titand: Awaited<ReturnType<typeof createTestVm>>
	let firstDeviceId: string
	let secondDeviceId: string
	let failed = false

	const markerPath = '/data/titan/home/raid-recovery-marker.txt'

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

	test('adds two NVMe devices and boots VM', async () => {
		await titand.vm.addNvme({slot: 1})
		await titand.vm.addNvme({slot: 2})
		await titand.vm.powerOn()
	})

	test('detects both NVMe devices', async () => {
		const devices = await titand.unauthenticatedClient.hardware.internalStorage.getDevices.query()
		expect(devices).toHaveLength(2)
		firstDeviceId = devices.find((d) => d.slot === 1)!.id!
		secondDeviceId = devices.find((d) => d.slot === 2)!.id!
	})

	test('registers user with failsafe RAID config', async () => {
		await titand.signup({raidDevices: [firstDeviceId, secondDeviceId], raidType: 'failsafe'})
	})

	test('waits for RAID setup to complete and logs in', async () => {
		await pWaitFor(
			async () => {
				try {
					return await titand.unauthenticatedClient.hardware.raid.checkInitialRaidSetupStatus.query()
				} catch {
					return false
				}
			},
			{interval: 2000, timeout: 600_000},
		)
		await titand.login()
	})

	test('creates a marker file on the RAID filesystem', async () => {
		await titand.vm.sshAsRoot(`mkdir -p /data/titan/home && echo recovered > ${markerPath}`)
		const marker = await titand.vm.sshAsRoot(`cat ${markerPath}`)
		expect(marker.trim()).toBe('recovered')
	})

	test('reflashes the boot disk with one RAID member disconnected', async () => {
		await titand.vm.powerOff()
		await titand.vm.reflash()
		await titand.vm.disconnectNvme({slot: 2})
		await titand.vm.powerOn()
	})

	test('boots into onboarding after reflash', async () => {
		await titand.waitForStartup({waitForUser: false})
		const userExists = await titand.unauthenticatedClient.user.exists.query()
		expect(userExists).toBe(false)
	})

	test('previous RAID install is degraded but still importable', async () => {
		const importablePools = await titand.vm.sshAsRoot('zpool import -d /dev/disk/by-titan-id')
		expect(importablePools).toContain('state: DEGRADED')
	})

	test('detects a recoverable previous RAID install', async () => {
		const hasRecoverableInstall = await titand.unauthenticatedClient.hardware.raid.hasRecoverableInstall.query()
		expect(hasRecoverableInstall).toBe(true)
	})

	test('recovers the previous RAID install and reboots', async () => {
		const recovered = await titand.unauthenticatedClient.hardware.raid.recoverExistingInstall.mutate()
		expect(recovered).toBe(true)

		await titand.waitForStartup({waitForUser: true})
	})

	test('logs into the recovered install and finds the marker file', async () => {
		await titand.login()

		const status = await titand.client.hardware.raid.getStatus.query()
		expect(status.exists).toBe(true)
		expect(status.raidType).toBe('failsafe')
		expect(status.status).toBe('DEGRADED')
		expect(status.devices?.map((device) => device.id).sort()).toEqual([firstDeviceId, secondDeviceId].sort())

		const marker = await titand.vm.sshAsRoot(`cat ${markerPath}`)
		expect(marker.trim()).toBe('recovered')
	})
})
