import {expect, beforeAll, afterAll, describe, test} from 'vitest'
import pWaitFor from 'p-wait-for'

import {createTestVm} from '../test-utilities/create-test-titand.js'
import {
	createLegacyUsbInstall,
	piStartupTimeout,
	rebootIntoLegacyUsbInstall,
	waitForUsbDisks,
} from './pi-storage-test-helpers.js'

describe('Pi with two legacy USB data installations', () => {
	let titand: Awaited<ReturnType<typeof createTestVm>>
	let secondDeviceId = ''
	let secondMountpoint = ''
	let firstUuid = ''

	beforeAll(async () => {
		titand = await createTestVm({device: 'pi', bootDisk: 'sdcard', startupTimeout: piStartupTimeout})
		await titand.vm.addUsbStorage({slot: 1})
		await titand.vm.powerOn()
	}, piStartupTimeout + 60_000)

	afterAll(async () => await titand?.cleanup())

	test(
		'creates the first legacy installation through the historical setup path',
		async () => {
			await createLegacyUsbInstall(titand)
			firstUuid = (await titand.vm.ssh('lsblk -no UUID /dev/disk/by-label/titan')).trim()
			expect(firstUuid).not.toBe('')
			await rebootIntoLegacyUsbInstall(titand)
			await titand.registerAndLogin()
		},
		piStartupTimeout + 600_000,
	)

	test(
		'adds a second disk and gives it an unambiguous second legacy marker',
		async () => {
			await titand.vm.powerOff()
			await titand.vm.addUsbStorage({slot: 2})
			await rebootIntoLegacyUsbInstall(titand, 2)
			await titand.login()

			const devices = await titand.client.files.externalDevices.query()
			expect(devices).toHaveLength(1)
			secondDeviceId = devices[0].id
			await titand.client.files.formatExternalDevice.mutate({
				deviceId: secondDeviceId,
				filesystem: 'ext4',
				label: 'PI-SECOND',
			})
			await pWaitFor(
				async () => {
					const current = await titand.client.files.externalDevices.query()
					return (
						current
							.find((device) => device.id === secondDeviceId)
							?.partitions.some((partition) => partition.mountpoints.includes('/External/PI-SECOND')) ?? false
					)
				},
				{interval: 1000, timeout: 120_000},
			)

			// There is no product API for creating the retired .titan marker.
			const mountTargets = (await titand.vm.ssh(`findmnt -n -o TARGET --source /dev/${secondDeviceId}1`))
				.trim()
				.split('\n')
				.map((target) => target.trim())
			secondMountpoint = '/home/titan/titan/external/PI-SECOND'
			expect(mountTargets).toContain(secondMountpoint)
			await titand.vm.sshAsRoot(`
				mkdir -p ${secondMountpoint}/titan
				touch ${secondMountpoint}/titan/.titan
				sync
			`)
		},
		piStartupTimeout + 600_000,
	)

	test(
		'refuses to choose between two visible legacy installations without modifying either disk',
		async () => {
			await waitForUsbDisks(titand, 2)
			const result = await titand.vm.sshAsRoot(`
				set -eu
				sync
				umount /dev/${secondDeviceId}1
				systemctl stop titan.service docker.service
				swapoff /swap/swapfile || true
				umount /home/titan/titan /var/lib/docker /swap /sd-root /mnt/data
				set +e
				output=$(/opt/titan-external-storage/titan-external-storage 2>&1)
				status=$?
				set -e
				printf 'status=%s\n%s\n' "$status" "$output"
			`)

			expect(result).toContain('status=1')
			expect(result).toContain('Multiple legacy Titan data drives found; refusing to choose between them')
			expect(await titand.vm.ssh('findmnt -n -o SOURCE --target /home/titan/titan || true')).not.toMatch(/\/dev\/sd/)
			expect((await titand.vm.ssh('lsblk -no UUID /dev/disk/by-label/titan')).trim()).toBe(firstUuid)
			expect((await titand.vm.ssh(`lsblk -no UUID /dev/${secondDeviceId}1`)).trim()).not.toBe('')
		},
		piStartupTimeout + 600_000,
	)
})
