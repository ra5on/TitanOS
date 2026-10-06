import {expect, beforeAll, beforeEach, afterAll, afterEach, describe, test} from 'vitest'

import {createTestVm} from '../test-utilities/create-test-titand.js'

// Tests that SSDs previously used in another Titan installation don't interfere
// with the current installation. The system should mount the correct pool based
// on the pool name stored in the config, ignoring any foreign pools.
describe('RAID with previously used SSDs', () => {
	let titand: Awaited<ReturnType<typeof createTestVm>>
	let currentPoolDevices: string[]
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

	// Phase 1: Set up an Titan with SSDs in slots 1+2 (simulates a previous installation)
	test('adds two NVMe devices (slots 1+2) and boots VM', async () => {
		await titand.vm.addNvme({slot: 1})
		await titand.vm.addNvme({slot: 2})
		await titand.vm.powerOn()
	})

	test('detects both NVMe devices', async () => {
		const devices = await titand.unauthenticatedClient.hardware.internalStorage.getDevices.query()
		expect(devices).toHaveLength(2)
	})

	test('registers user with storage RAID using slots 1+2', async () => {
		const devices = await titand.unauthenticatedClient.hardware.internalStorage.getDevices.query()
		await titand.signup({raidDevices: devices.map((d) => d.id!), raidType: 'storage'})
	})

	test('waits for setup to complete', async () => {
		await titand.waitForStartup({waitForUser: true})
		await titand.login()
	})

	test('verifies RAID setup', async () => {
		const status = await titand.client.hardware.raid.getStatus.query()
		expect(status.exists).toBe(true)
		expect(status.raidType).toBe('storage')
		expect(status.devices).toHaveLength(2)
	})

	// Phase 2: Simulate obtaining SSDs from a different Titan
	// Disconnect the SSDs, reflash to a fresh OS, then set up with new SSDs
	test('powers off and disconnects SSDs', async () => {
		await titand.vm.powerOff()
		await titand.vm.disconnectNvme({slot: 1})
		await titand.vm.disconnectNvme({slot: 2})
	})

	test('reflashes to simulate fresh Titan', async () => {
		await titand.vm.reflash()
	})

	test('adds new NVMe devices (slots 3+4) and boots fresh', async () => {
		await titand.vm.addNvme({slot: 3})
		await titand.vm.addNvme({slot: 4})
		await titand.vm.powerOn()
	})

	test('detects new NVMe devices', async () => {
		const devices = await titand.unauthenticatedClient.hardware.internalStorage.getDevices.query()
		expect(devices).toHaveLength(2)
		expect(devices.map((d) => d.slot).sort()).toEqual([3, 4])
	})

	test('registers user with storage RAID using slots 3+4', async () => {
		const devices = await titand.unauthenticatedClient.hardware.internalStorage.getDevices.query()
		currentPoolDevices = devices.map((d) => d.id!)
		await titand.signup({raidDevices: currentPoolDevices, raidType: 'storage'})
	})

	test('waits for setup to complete', async () => {
		await titand.waitForStartup({waitForUser: true})
		await titand.login()
	})

	test('verifies RAID setup', async () => {
		const status = await titand.client.hardware.raid.getStatus.query()
		expect(status.exists).toBe(true)
		expect(status.raidType).toBe('storage')
		expect(status.devices).toHaveLength(2)
	})

	// Phase 3: Connect the SSDs from the previous Titan and verify they're ignored
	test('powers off and connects SSDs from previous Titan', async () => {
		await titand.vm.powerOff()
		await titand.vm.connectNvme({slot: 1})
		await titand.vm.connectNvme({slot: 2})
	})

	test('boots with all four SSDs', async () => {
		await titand.vm.powerOn()
	})

	test('mounts the current pool and ignores the foreign pool', async () => {
		await titand.waitForStartup({waitForUser: true})
		await titand.login()

		const status = await titand.client.hardware.raid.getStatus.query()
		expect(status.exists).toBe(true)
		expect(status.devices?.map((d) => d.id).sort()).toEqual(currentPoolDevices.sort())
	})
})
