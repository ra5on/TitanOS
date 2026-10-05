import {expect, beforeAll, beforeEach, afterAll, afterEach, describe, test} from 'vitest'

import {createTestVm} from '../test-utilities/create-test-titand.js'

describe('Internal storage device detection', () => {
	let titand: Awaited<ReturnType<typeof createTestVm>>
	let failed = false

	beforeAll(async () => {
		titand = await createTestVm()
		await titand.vm.powerOn()
	})

	afterAll(async () => await titand?.cleanup())

	afterEach(({task}) => {
		if (task.result?.state === 'fail') failed = true
	})

	beforeEach(({skip}) => {
		if (failed) skip()
	})

	test('getDevices() returns empty array when no NVMe devices present', async () => {
		const devices = await titand.client.hardware.internalStorage.getDevices.query()
		expect(devices).toEqual([])
	})

	for (const slot of [1, 2, 3, 4]) {
		test(`getDevices() detects NVMe device in slot ${slot}`, async () => {
			// Power off and add NVMe device to slot
			await titand.vm.powerOff()
			await titand.vm.addNvme({slot})
			await titand.vm.powerOn()

			// Check NVMe device is detected in the correct slot
			const devices = await titand.client.hardware.internalStorage.getDevices.query()
			expect(devices).toHaveLength(1)
			expect(devices[0].slot).toBe(slot)

			// Power off and remove NVMe device from slot
			await titand.vm.powerOff()
			await titand.vm.removeNvme({slot})
		})
	}
})
