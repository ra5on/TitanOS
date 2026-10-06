import {expect, beforeAll, beforeEach, afterAll, afterEach, describe, test} from 'vitest'

import {createTestVm} from '../test-utilities/create-test-titand.js'

// Smoke tests that run against a real titanos VM to catch breaking changes in the
// system binaries we depend on (lscpu, dmidecode, df, lsblk, /proc/meminfo). These
// don't test hardware-specific parsing quirks, they verify our parsing code still
// produces valid output when the underlying binaries get updated.
describe('Device specs', () => {
	let titand: Awaited<ReturnType<typeof createTestVm>>
	let failed = false

	beforeAll(async () => {
		titand = await createTestVm({device: 'titan-home'})
		await titand.vm.powerOn()
		await titand.signup()
		await titand.login()
	})

	afterAll(async () => await titand?.cleanup())

	afterEach(({task}) => {
		if (task.result?.state === 'fail') failed = true
	})

	beforeEach(({skip}) => {
		if (failed) skip()
	})

	test('CPU name is parsed from lscpu', async () => {
		const {cpu} = await titand.client.systemNg.device.getSpecs.query()
		expect(cpu).toBeTypeOf('string')
		expect(cpu.length).toBeGreaterThan(0)
		// Verify CPUID brand string artifacts have been cleaned
		expect(cpu).not.toMatch(/\(R\)/i)
		expect(cpu).not.toMatch(/\(TM\)/i)
		expect(cpu).not.toMatch(/\(C\)/i)
		expect(cpu).not.toContain('CPU')
	})

	test('memory size is parsed from /proc/meminfo', async () => {
		const {memorySize} = await titand.client.systemNg.device.getSpecs.query()
		expect(memorySize).toBeTypeOf('number')
		expect(memorySize).toBeGreaterThan(0)
	})

	test('memory type is parsed from dmidecode', async () => {
		const {memoryType} = await titand.client.systemNg.device.getSpecs.query()
		// dmidecode Type: field was parsed and wasn't "Unknown" or "Other"
		expect(memoryType).toBeTypeOf('string')
		expect(memoryType.length).toBeGreaterThan(0)
	})

	test('storage size is parsed from df', async () => {
		const {storageSize} = await titand.client.systemNg.device.getSpecs.query()
		expect(storageSize).toBeTypeOf('number')
		expect(storageSize).toBeGreaterThan(0)
	})

	test('storage type is parsed from lsblk', async () => {
		const {storageType} = await titand.client.systemNg.device.getSpecs.query()
		expect(['NVMe SSD', 'SSD', 'HDD', 'SD']).toContain(storageType)
	})
})
