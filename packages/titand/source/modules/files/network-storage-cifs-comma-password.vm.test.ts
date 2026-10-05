import {expect, beforeAll, afterAll, describe, test} from 'vitest'
import pRetry from 'p-retry'

import {createTestVm} from '../test-utilities/create-test-titand.js'

describe('Network storage', () => {
	let titand: Awaited<ReturnType<typeof createTestVm>>

	const sharePassword = 'correct,horse,battery,staple'
	const vmDataDirectory = '/home/titan/titan'

	beforeAll(async () => {
		titand = await createTestVm({device: 'titan-home'})
		await titand.vm.powerOn()
		await titand.signup()
		await titand.login()
	})

	afterAll(async () => await titand?.cleanup())

	test('mounts a CIFS share with commas in the password', async () => {
		const shareName = 'network-comma-password-test'

		await titand.client.files.createDirectory.mutate({path: `/Home/${shareName}`})
		await titand.client.files.createDirectory.mutate({path: `/Home/${shareName}/source-marker`})

		// There is no product API for choosing the Samba share password; set the
		// real secret and smbpasswd entry in the VM to exercise comma parsing.
		await pRetry(
			() =>
				titand.vm.sshAsRoot(`
set -eu
mkdir -p ${vmDataDirectory}/secrets
printf '%s' '${sharePassword}' > ${vmDataDirectory}/secrets/share-password
printf '%s\\n%s\\n' '${sharePassword}' '${sharePassword}' | smbpasswd -s -a titan
`),
			{retries: 20, factor: 1, minTimeout: 1000, maxTimeout: 1000},
		)
		await expect(titand.client.files.sharePassword.query()).resolves.toBe(sharePassword)
		await titand.client.files.addShare.mutate({path: `/Home/${shareName}`})

		const mountPath = await pRetry(
			() =>
				titand.client.files.addNetworkShare.mutate({
					host: 'localhost',
					share: `${shareName} (Titan)`,
					username: 'titan',
					password: sharePassword,
				}),
			{retries: 5, factor: 1},
		)

		expect(mountPath).toBe(`/Network/localhost/${shareName} (Titan)`)

		const networkFiles = await titand.client.files.list.query({path: mountPath})
		expect(networkFiles.files.map((file) => file.name)).toContain('source-marker')
	})
})
