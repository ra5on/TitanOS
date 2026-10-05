import {expect, beforeAll, beforeEach, afterAll, afterEach, describe, test} from 'vitest'
import pRetry from 'p-retry'

import {createTestVm} from '../test-utilities/create-test-umbreld.js'

describe('Hostname configuration', () => {
	let umbreld: Awaited<ReturnType<typeof createTestVm>>
	let failed = false
	let configuredHostname = ''

	beforeAll(async () => {
		umbreld = await createTestVm({device: 'umbrel-home'})
		await umbreld.vm.powerOn()
		await umbreld.registerAndLogin()
	})

	afterAll(async () => await umbreld?.cleanup())

	afterEach(({task}) => {
		if (task.result?.state === 'fail') failed = true
	})

	beforeEach(({skip}) => {
		if (failed) skip()
	})

	async function expectHostname(hostname: string) {
		expect(await umbreld.client.system.getHostname.query()).toBe(hostname)
		expect((await umbreld.vm.ssh('hostname')).trim()).toBe(hostname)
		expect((await umbreld.vm.ssh('cat /etc/hostname')).trim()).toBe(hostname)
	}

	test('a new Titan image uses its hostname in the OS and LAN certificate', async () => {
		await pRetry(async () => expectHostname('titan'), {retries: 100, minTimeout: 100, maxTimeout: 100})
		const status = await umbreld.client.lanIngress.getCertificateStatus.query()
		expect(status.serverSans.dns).toContain('titan.local')
	})

	test('setHostname() updates the live system hostname', async () => {
		const currentHostname = await umbreld.client.system.getHostname.query()
		expect((await umbreld.vm.ssh('hostname')).trim()).toBe(currentHostname)

		configuredHostname = currentHostname === 'titan-test' ? 'titan-host' : 'titan-test'
		await umbreld.client.system.setHostname.mutate({hostname: configuredHostname})

		await expectHostname(configuredHostname)

		const etcHosts = await umbreld.vm.ssh('cat /etc/hosts')
		expect(etcHosts).toMatch(new RegExp(`^127\\.0\\.(?:0|1)\\.1\\s+${configuredHostname}$`, 'm'))
	})

	test('configured hostname is restored after reboot', async () => {
		await umbreld.vm.powerOff()
		await umbreld.vm.powerOn()
		await umbreld.login()

		// Hostname restore runs in the background during startup, so wait for it to settle.
		await pRetry(async () => expectHostname(configuredHostname), {
			retries: 100,
			minTimeout: 100,
			maxTimeout: 100,
		})
	})
})
