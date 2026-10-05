import {expect, beforeAll, afterAll, test} from 'vitest'
import fse from 'fs-extra'

import createTestTitand from '../test-utilities/create-test-titand.js'

// The rest of move() is covered end-to-end in files.move.vm.test.ts. This
// test remains an integration test because it installs a fixture app from
// the local test app store, which isn't possible against a VM.

let titand: Awaited<ReturnType<typeof createTestTitand>>

beforeAll(async () => {
	titand = await createTestTitand()
	await titand.registerAndLogin()
})

afterAll(async () => {
	await titand.cleanup()
})

test('move() throws when trying to move a protected path out of /Apps/', async () => {
	// Install a test app
	await expect(titand.client.apps.install.mutate({appId: 'sparkles-hello-world'})).resolves.toStrictEqual(true)

	const testDirectory = `${titand.instance.dataDirectory}/home/protected-app-move-test`
	await fse.mkdir(testDirectory)

	await expect(
		titand.client.files.move.mutate({
			path: '/Apps/sparkles-hello-world',
			toDirectory: '/Home/protected-app-move-test',
		}),
	).rejects.toThrow('[operation-not-allowed]')

	// Clean up
	await fse.remove(testDirectory)
	await titand.client.apps.uninstall.mutate({appId: 'sparkles-hello-world'})
})
