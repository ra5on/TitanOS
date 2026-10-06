import {expect, beforeAll, beforeEach, afterAll, afterEach, describe, test} from 'vitest'
import pWaitFor from 'p-wait-for'

import {createTestVm} from '../test-utilities/create-test-titand.js'

describe('Reflink copy on ZFS', () => {
	let titand: Awaited<ReturnType<typeof createTestVm>>
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

	test('sets up RAID storage mode', async () => {
		const devices = await titand.unauthenticatedClient.hardware.internalStorage.getDevices.query()
		expect(devices).toHaveLength(1)
		const deviceId = devices[0].id!
		expect(deviceId).toBeDefined()
		await titand.signup({raidDevices: [deviceId], raidType: 'storage'})
	})

	test('waits for RAID setup to complete and logs in', async () => {
		await pWaitFor(
			async () => {
				try {
					return await titand.unauthenticatedClient.hardware.raid.checkInitialRaidSetupStatus.query()
				} catch (error) {
					if (error instanceof Error && error.message.includes('fetch failed')) {
						return false
					}
					throw error
				}
			},
			{interval: 2000, timeout: 600_000},
		)
		await titand.login()
	})

	test('copies a file using reflink (block cloning) instead of rsync', async () => {
		const testFileSizeMb = 100

		// Create a 100MB test file on the ZFS filesystem
		await titand.vm.ssh(`dd if=/dev/urandom of=~/titan/home/test-file.bin bs=1M count=${testFileSizeMb} 2>/dev/null`)
		// BRT clone accounting is applied during a pool sync, so force a txg
		// boundary before reading bclonesaved.
		await titand.vm.ssh('zpool sync')

		// Get block clone savings before copy
		const savedBefore = Number((await titand.vm.ssh('zpool get -Hp -o value bclonesaved')).trim())

		// Copy the file via the API
		await titand.client.files.copy.mutate({
			path: '/Home/test-file.bin',
			toDirectory: '/Home',
			collision: 'keep-both',
		})
		await titand.vm.ssh('zpool sync')

		// Verify the copy exists and has the correct content
		const listing = await titand.client.files.list.query({path: '/Home'})
		expect(listing.files.some((f) => f.name === 'test-file (2).bin')).toBe(true)
		const sourceHash = (await titand.vm.ssh('md5sum ~/titan/home/test-file.bin')).trim().split(/\s+/)[0]
		const copyHash = (await titand.vm.ssh('md5sum ~/titan/home/"test-file (2).bin"')).trim().split(/\s+/)[0]
		expect(copyHash).toBe(sourceHash)

		// Verify block cloning was used by checking bclonesaved increased
		// If rsync was used instead of reflink, bclonesaved would not change
		const savedAfter = Number((await titand.vm.ssh('zpool get -Hp -o value bclonesaved')).trim())
		const savedMb = (savedAfter - savedBefore) / (1024 * 1024)
		expect(savedMb).toBeGreaterThan(testFileSizeMb / 2)
	})
})
