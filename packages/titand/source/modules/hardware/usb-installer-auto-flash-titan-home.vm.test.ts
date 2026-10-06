import path from 'node:path'
import {fileURLToPath} from 'node:url'

import {expect, beforeAll, beforeEach, afterAll, afterEach, describe, test} from 'vitest'
import {$} from 'execa'
import fse from 'fs-extra'

import {createTestVm} from '../test-utilities/create-test-titand.js'

const currentDirectory = path.dirname(fileURLToPath(import.meta.url))
const osDirectory = path.resolve(currentDirectory, '../../../../os')
const osImage = path.join(osDirectory, 'build/titanos-amd64.img')
const installerBuildScript = path.join(osDirectory, 'usb-installer/build.sh')
const installerIso = path.join(osDirectory, 'build/titanos-amd64-usb-installer.iso')

describe('USB installer auto-flashes an unflashed Titan Home', () => {
	let titand: Awaited<ReturnType<typeof createTestVm>>
	let failed = false

	beforeAll(async () => {
		// An unflashed Titan Home: no OS on any disk, just a blank internal NVMe.
		// First boot after a flash does one-time provisioning so allow extra time.
		titand = await createTestVm({device: 'titan-home', bootDisk: 'none', startupTimeout: 600_000})
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

	test('compresses the titanOS image for the installer', async () => {
		// The installer expects an xz stream, compression ratio doesn't matter
		// here so use the fastest level
		await $`xz --keep --force -0 --threads=0 ${osImage}`
	}, 1_200_000)

	test('builds the USB installer ISO', async () => {
		await $`${installerBuildScript}`
		expect(await fse.pathExists(installerIso)).toBe(true)
	}, 1_800_000)

	// The boot test timeouts are longer than the VM startupTimeout so VM boot
	// failures surface the VM console output instead of a bare vitest timeout
	test('boots the installer which auto-flashes the blank NVMe and powers off', async () => {
		await titand.vm.addNvme({slot: 1})
		await titand.vm.powerOn({cdrom: installerIso, waitForShutdown: true})
	}, 900_000)

	test('boots the freshly flashed NVMe into onboarding', async () => {
		await titand.vm.powerOn({bootNvmeSlot: 1})
		const userExists = await titand.unauthenticatedClient.user.exists.query()
		expect(userExists).toBe(false)
	}, 900_000)

	test('completes onboarding on the flashed install', async () => {
		await titand.registerAndLogin()
		const userExists = await titand.unauthenticatedClient.user.exists.query()
		expect(userExists).toBe(true)
	})
})
