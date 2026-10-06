import {expect, beforeAll, beforeEach, afterAll, afterEach, describe, test} from 'vitest'
import pWaitFor from 'p-wait-for'

import {createTestVm} from '../test-utilities/create-test-titand.js'
import {triggerFactoryReset} from '../test-utilities/rebooting-action.js'

describe('Factory reset', () => {
	let titand: Awaited<ReturnType<typeof createTestVm>>
	let failed = false

	const markerName = 'factory-reset-marker.txt'

	beforeAll(async () => {
		titand = await createTestVm({device: 'titan-home'})
		await titand.vm.powerOn()
		await titand.registerAndLogin()
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

	test('creates a marker file', async () => {
		await titand.api.post(`files/upload?path=/Home/${markerName}`, {body: 'factory reset marker'})

		const homeListing = await titand.client.files.list.query({path: '/Home'})
		expect(homeListing.files.map((file) => file.name)).toContain(markerName)
	})

	test('factory resets back to registration', async () => {
		await triggerFactoryReset(titand.client.system.factoryReset.mutate({password: 'moneyprintergobrrr'}))

		await pWaitFor(
			async () => {
				try {
					return !(await titand.unauthenticatedClient.user.exists.query())
				} catch {
					return false
				}
			},
			{interval: 2000, timeout: 600_000},
		)
	})

	test('does not keep the marker file after setup', async () => {
		await titand.registerAndLogin()

		const homeListing = await titand.client.files.list.query({path: '/Home'})
		expect(homeListing.files.map((file) => file.name)).not.toContain(markerName)
	})
})
