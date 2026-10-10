import {createReadStream} from 'node:fs'
import {access, stat} from 'node:fs/promises'
import {afterAll, afterEach, beforeAll, describe, expect, test} from 'vitest'
import pRetry, {AbortError} from 'p-retry'
import {createTestVm} from '../test-utilities/create-test-titand.js'

// Imports a real-world appliance disk (e.g. Home Assistant OS) on a published
// image, the way the UI does: upload through the Files API, then create the
// machine with the UI defaults. The tiny synthetic disk of the release gate
// cannot show size- or content-dependent failures.
const image = process.env.TITAN_VM_IMAGE
const sample = process.env.TITAN_QCOW2_SAMPLE
if (!image || !sample) throw new Error('TITAN_VM_IMAGE and TITAN_QCOW2_SAMPLE are required')
await access(image)
await access(sample)
const diskSizeGb = Number(process.env.TITAN_QCOW2_SAMPLE_DISK_GB ?? 32)

describe('Real-world appliance QCOW2 import on the released OS', () => {
	let titand: Awaited<ReturnType<typeof createTestVm>>
	beforeAll(async () => {
		titand = await createTestVm({device: 'titan-home', image, memory: 6144, cores: 2})
		await titand.vm.powerOn()
		await titand.registerAndLogin()
		await pRetry(async () => expect((await titand.client.machines.capabilities.query()).libvirtAvailable).toBe(true), {
			retries: 90,
			minTimeout: 1000,
			maxTimeout: 1000,
		})
	})
	afterAll(async () => await titand?.cleanup())
	afterEach(async ({task}) => {
		if (task.result?.state === 'fail') {
			console.error(await titand.client.machines.list.query().catch(String))
			console.error(await titand.vm.sshAsRoot('journalctl -u titan -u libvirtd --no-pager -n 200').catch(String))
			console.error(await titand.vm.sshAsRoot('df -h /home/titan/titan /run; findmnt -R /run | tail -5').catch(String))
		}
	})

	test('imports the uploaded appliance disk and starts it with the UI defaults', async () => {
		const {size} = await stat(sample)
		await titand.api.post('files/upload?path=/Home/appliance.qcow2', {
			body: createReadStream(sample),
			headers: {'content-length': String(size)},
		})
		const machine = await titand.client.machines.create.mutate({
			name: 'Appliance',
			imagePath: '/Home/appliance.qcow2',
			arch: 'amd64',
			firmware: 'uefi',
			diskBus: 'virtio',
			diskSizeGb,
			cores: 2,
			memoryGb: 2,
		})
		await pRetry(
			async () => {
				const current = (await titand.client.machines.list.query()).find((m) => m.id === machine.id)
				if (current?.state === 'error')
					throw new AbortError(current.errorMessage ?? 'Import failed without a daemon message')
				expect(current?.state).toBe('running')
			},
			{retries: 600, minTimeout: 1000, maxTimeout: 1000},
		)
		const info = JSON.parse(
			await titand.vm.sshAsRoot(
				`qemu-img info --force-share --output=json /home/titan/titan/machines/${machine.id}/disk.qcow2`,
			),
		)
		expect(info['backing-filename']).toBeUndefined()
		expect(info['virtual-size']).toBe(diskSizeGb * 1024 ** 3)
	})
})
