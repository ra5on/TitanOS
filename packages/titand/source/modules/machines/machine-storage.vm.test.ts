import {access} from 'node:fs/promises'
import {afterAll, afterEach, beforeAll, describe, expect, test} from 'vitest'
import pRetry, {AbortError} from 'p-retry'
import {createTestVm} from '../test-utilities/create-test-titand.js'

const image = process.env.TITAN_VM_IMAGE
if (!image) throw new Error('TITAN_VM_IMAGE must identify the freshly built image')
await access(image)

// Shared folders need virtiofsd and shared guest memory, data disks a bind
// mount per image: both only prove themselves with real libvirt on the OS.
describe('Shared folders and data disks on the released OS', () => {
	let titand: Awaited<ReturnType<typeof createTestVm>>
	let id: string
	const waitForState = (state: 'running' | 'stopped') =>
		pRetry(
			async () => {
				const current = (await titand.client.machines.list.query()).find((machine) => machine.id === id)
				if (current?.state === 'error')
					throw new AbortError(current.errorMessage ?? 'Machine failed without a daemon message')
				expect(current?.state).toBe(state)
			},
			{retries: 90, minTimeout: 1000, maxTimeout: 1000},
		)
	beforeAll(async () => {
		titand = await createTestVm({device: 'titan-home', image, memory: 4096, cores: 2})
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
			console.error(await titand.vm.sshAsRoot('journalctl -u titan -u libvirtd --no-pager -n 160').catch(String))
		}
	})

	test('starts a machine with a shared folder and a data disk and removes the disk with the machine', async () => {
		// A tiny BIOS boot sector that halts: enough for QEMU to run with the devices
		const disk = Buffer.alloc(1024 * 1024)
		disk.set([0xfa, 0xf4, 0xeb, 0xfd])
		disk.set([0x55, 0xaa], 510)
		await titand.api.post('files/upload?path=/Home/storage-test.img', {body: disk})
		await titand.client.files.createDirectory.mutate({path: '/Home/Freigabe'})
		await titand.client.files.createDirectory.mutate({path: '/Home/VM-Daten'})
		const machine = await titand.client.machines.create.mutate({
			name: 'Storage test',
			imagePath: '/Home/storage-test.img',
			arch: 'amd64',
			firmware: 'bios',
			diskSizeGb: 1,
			cores: 1,
			memoryGb: 1,
		})
		id = machine.id
		await waitForState('running')

		// Devices are fixed while the machine runs
		const devices = {
			sharedFolders: [{path: '/Home/Freigabe', tag: 'freigabe'}],
			dataDisks: [{id: 'abcd1234', directory: '/Home/VM-Daten', sizeGb: 2}],
		}
		await expect(titand.client.machines.updateSettings.mutate({id, ...devices})).rejects.toThrow(
			'machine-devices-require-stopped',
		)
		await titand.client.machines.forceStop.mutate({id})
		await waitForState('stopped')
		await titand.client.machines.updateSettings.mutate({id, ...devices})
		const diskImage = `/home/titan/titan/home/VM-Daten/${id}-data-abcd1234.qcow2`
		expect(JSON.parse(await titand.vm.sshAsRoot(`qemu-img info --output=json ${diskImage}`))['virtual-size']).toBe(
			2 * 1024 ** 3,
		)

		await titand.client.machines.start.mutate({id})
		await waitForState('running')
		const xml = await titand.vm.sshAsRoot(`virsh --connect qemu:///system dumpxml titan-machine-${id}`)
		expect(xml).toContain("<driver type='virtiofs'")
		expect(xml).toContain("<target dir='freigabe'/>")
		expect(xml).toContain("<target dev='vdj' bus='virtio'/>")
		expect(await titand.vm.sshAsRoot('pgrep -c virtiofsd')).not.toBe('0')

		await titand.client.machines.uninstall.mutate({id})
		expect(await titand.vm.sshAsRoot(`test -e ${diskImage} && echo present || echo removed`)).toBe('removed')
		// The shared folder is user data and stays
		expect(await titand.vm.sshAsRoot('test -d /home/titan/titan/home/Freigabe && echo kept')).toBe('kept')
	})
})
