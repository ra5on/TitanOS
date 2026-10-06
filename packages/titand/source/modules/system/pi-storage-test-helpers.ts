import {expect} from 'vitest'
import pRetry from 'p-retry'
import pWaitFor from 'p-wait-for'

import {createTestVm} from '../test-utilities/create-test-titand.js'

export type PiTestVm = Awaited<ReturnType<typeof createTestVm>>

type LsblkDevice = {
	name: string
	type: string
	tran?: string | null
	label?: string | null
	uuid?: string | null
	children?: LsblkDevice[]
}

// Pi first boot performs Rugix's A/B bootstrap through a TCG-emulated SD card.
export const piStartupTimeout = 2_700_000

export async function getUsbDisks(titand: PiTestVm) {
	const {blockdevices} = JSON.parse(await titand.vm.ssh('lsblk --json --output NAME,TYPE,TRAN')) as {
		blockdevices: LsblkDevice[]
	}
	return blockdevices.filter((device) => device.type === 'disk' && device.tran === 'usb')
}

export async function waitForUsbDisks(titand: PiTestVm, count: number) {
	let disks: LsblkDevice[] = []
	await pRetry(
		async () => {
			disks = await getUsbDisks(titand)
			expect(disks).toHaveLength(count)
		},
		{retries: 60, minTimeout: 1000, maxTimeout: 1000},
	)
	return disks
}

export async function waitForUsbPartition(titand: PiTestVm, deviceId: string, label: string) {
	await pRetry(
		async () => {
			const {blockdevices} = JSON.parse(await titand.vm.ssh('lsblk --json --output NAME,TYPE,TRAN,LABEL')) as {
				blockdevices: LsblkDevice[]
			}
			const disk = blockdevices.find((device) => device.type === 'disk' && device.name === deviceId)
			expect(disk?.children?.some((partition) => partition.type === 'part' && partition.label === label)).toBe(true)
		},
		{retries: 120, minTimeout: 1000, maxTimeout: 1000},
	)
}

export async function waitForUsbPartitionByUuid(titand: PiTestVm, uuid: string) {
	let deviceId = ''
	await pRetry(
		async () => {
			const {blockdevices} = JSON.parse(await titand.vm.ssh('lsblk --json --output NAME,TYPE,TRAN,UUID')) as {
				blockdevices: LsblkDevice[]
			}
			const disk = blockdevices.find(
				(device) =>
					device.type === 'disk' &&
					device.tran === 'usb' &&
					device.children?.some((partition) => partition.type === 'part' && partition.uuid === uuid),
			)
			expect(disk).toBeDefined()
			deviceId = disk!.name
		},
		{retries: 120, minTimeout: 1000, maxTimeout: 1000},
	)
	return deviceId
}

export const topmostMountSource = (findmntOutput: string) => findmntOutput.trim().split('\n').at(-1)!.trim()

// Use the script's retired setup path to construct exactly the same disk
// layout that historical Pi installs received during their first boot.
export async function createLegacyUsbInstall(titand: PiTestVm) {
	await waitForUsbDisks(titand, 1)

	for (let attempt = 0; attempt < 2; attempt += 1) {
		const bootId = (await titand.vm.ssh('cat /proc/sys/kernel/random/boot_id')).trim()
		const output = await titand.vm.sshAsRoot(`
			set -eu
			systemctl stop titan.service docker.service
			systemctl reset-failed titan-external-storage.service || true

			set +e
			output=$(/opt/titan-external-storage/titan-external-storage --allow-legacy-setup 2>&1)
			status=$?
			set -e
			printf '%s\\n' "$output"
			if [ "$status" -ne 0 ]; then exit "$status"; fi

			case "$output" in
				*'UAS was blacklisted and device is rebooting'*) exit 0 ;;
			esac

			systemctl start docker.service titan.service
		`)

		if (output.includes('UAS was blacklisted and device is rebooting')) {
			await pWaitFor(
				async () => {
					try {
						return (await titand.vm.ssh('cat /proc/sys/kernel/random/boot_id')).trim() !== bootId
					} catch {
						return false
					}
				},
				{interval: 2000, timeout: 600_000},
			)
			await titand.waitForStartup()
			await waitForUsbDisks(titand, 1)
			continue
		}

		await titand.waitForStartup()
		const marker = (await titand.vm.ssh('test -f /mnt/data/titan/.titan && echo present')).trim()
		if (marker !== 'present') throw new Error(`Legacy setup completed without creating its marker:\n${output}`)
		return
	}

	throw new Error('Legacy setup still requested a UAS reboot on its second attempt')
}

// QEMU's emulated Pi USB bus can enumerate more slowly than real hardware.
// If the historical boot timeout elapsed on the first attempt, the attached
// disk is warm by the next power cycle. The production boot service is always
// responsible for mounting the legacy installation.
export async function rebootIntoLegacyUsbInstall(titand: PiTestVm, usbDiskCount = 1) {
	for (let attempt = 0; attempt < 2; attempt += 1) {
		await titand.vm.powerOff()
		await titand.vm.powerOn()
		await waitForUsbDisks(titand, usbDiskCount)

		const serviceStatus = (await titand.vm.ssh('systemctl is-active titan-external-storage.service || true')).trim()
		const titanSource = topmostMountSource(
			await titand.vm.ssh('findmnt -n -o SOURCE --target /home/titan/titan || true'),
		)
		if (serviceStatus === 'active' && /^\/dev\/sd[a-z]\d/.test(titanSource)) return
	}

	throw new Error('Legacy USB installation was not mounted during boot')
}

export async function expectLegacySystemMounts(titand: PiTestVm) {
	for (const target of ['/home/titan/titan', '/var/lib/docker', '/swap']) {
		const source = topmostMountSource(await titand.vm.ssh(`findmnt -n -o SOURCE --target ${target}`))
		expect(source).toMatch(/^\/dev\/sd[a-z]\d/)
	}
	expect((await titand.vm.ssh('findmnt -n -o TARGET --target /sd-root')).trim()).toBe('/sd-root')
}
