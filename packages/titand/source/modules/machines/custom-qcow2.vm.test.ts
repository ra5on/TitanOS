import {access} from 'node:fs/promises'
import {afterAll, afterEach, beforeAll, describe, expect, test} from 'vitest'
import pRetry, {AbortError} from 'p-retry'
import {createTestVm} from '../test-utilities/create-test-titand.js'

const image = process.env.TITAN_VM_IMAGE
if (!image) throw new Error('TITAN_VM_IMAGE must identify the freshly built image')
await access(image)

describe('Custom QCOW2 imports on the released OS', () => {
	let titand: Awaited<ReturnType<typeof createTestVm>>
	let id: string
	let imported = false
	const waitForVirtualization = () =>
		pRetry(async () => expect((await titand.client.machines.capabilities.query()).libvirtAvailable).toBe(true), {
			retries: 90,
			minTimeout: 1000,
			maxTimeout: 1000,
		})
	beforeAll(async () => {
		titand = await createTestVm({device: 'titan-home', image, memory: 4096, cores: 2})
		await titand.vm.powerOn()
		await titand.registerAndLogin()
		await waitForVirtualization()
	})
	afterAll(async () => await titand?.cleanup())
	afterEach(async ({task}) => {
		if (task.result?.state === 'fail') {
			console.error(await titand.client.machines.list.query().catch(String))
			console.error(await titand.vm.sshAsRoot('journalctl -u titan -u libvirtd --no-pager -n 160').catch(String))
		}
	})
	test('imports an uploaded bootable disk through the authenticated product API', async () => {
		// A tiny BIOS boot sector halts indefinitely. It needs no OS download and
		// verifies the source bytes really reach the virtual disk, not only a mock.
		const disk = Buffer.alloc(1024 * 1024)
		disk.set([0xfa, 0xf4, 0xeb, 0xfd])
		disk.set([0x55, 0xaa], 510)
		await titand.api.post('files/upload?path=/Home/qcow-import.img', {body: disk})
		await titand.vm.sshAsRoot(
			// Owned by the file owner, like every image uploaded through the UI
			'qemu-img convert -f raw -O qcow2 /home/titan/titan/home/qcow-import.img /home/titan/titan/home/qcow-import.qcow2 && chown --reference=/home/titan/titan/home/qcow-import.img /home/titan/titan/home/qcow-import.qcow2 && chmod 600 /home/titan/titan/home/qcow-import.qcow2',
		)
		const machine = await titand.client.machines.create.mutate({
			name: 'QCOW import',
			imagePath: '/Home/qcow-import.qcow2',
			arch: 'amd64',
			firmware: 'bios',
			diskSizeGb: 1,
			cores: 1,
			memoryGb: 1,
		})
		id = machine.id
		await pRetry(
			async () => {
				const current = (await titand.client.machines.list.query()).find((m) => m.id === id)
				if (current?.state === 'error')
					throw new AbortError(current.errorMessage ?? 'Import failed without a daemon message')
				expect(current?.state).toBe('running')
			},
			{retries: 90, minTimeout: 1000, maxTimeout: 1000},
		)
		const output = await titand.vm.sshAsRoot(
			`qemu-img info --force-share --output=json /home/titan/titan/machines/${id}/disk.qcow2`,
		)
		expect(JSON.parse(output)['backing-filename']).toBeUndefined()
		expect(JSON.parse(output)['virtual-size']).toBe(1024 ** 3)
		imported = true
	})
	test('retains the imported source and independent VM disk after host restart', async ({skip}) => {
		if (!imported) skip()
		await titand.client.machines.forceStop.mutate({id})
		// Stopping a machine no longer keeps it from starting with the OS: that
		// is what the autostart switch is for.
		await titand.client.machines.updateSettings.mutate({id, autostart: false})
		await titand.vm.powerOff()
		await titand.vm.powerOn()
		await titand.login()
		await waitForVirtualization()
		const restored = (await titand.client.machines.list.query()).find((m) => m.id === id)
		expect(restored).toMatchObject({autostart: false, state: 'stopped'})
		await titand.client.machines.start.mutate({id})
		await pRetry(
			async () => {
				const current = (await titand.client.machines.list.query()).find((m) => m.id === id)
				if (current?.state === 'error')
					throw new AbortError(current.errorMessage ?? 'Import failed without a daemon message')
				expect(current?.state).toBe('running')
			},
			{retries: 90, minTimeout: 1000, maxTimeout: 1000},
		)
		await titand.client.machines.forceStop.mutate({id})
		expect(
			await titand.vm.sshAsRoot(
				`qemu-img compare -f qcow2 -F qcow2 /home/titan/titan/home/qcow-import.qcow2 /home/titan/titan/machines/${id}/disk.qcow2 >/dev/null && echo preserved`,
			),
		).toBe('preserved')
		await titand.client.machines.uninstall.mutate({id})
		const listing = await titand.client.files.list.query({path: '/Home'})
		expect(listing.files.some((file) => file.name === 'qcow-import.qcow2')).toBe(true)
	})
	test('refuses an image referencing a host file and cleans up the failed import', async () => {
		await titand.vm.sshAsRoot('qemu-img create -f qcow2 -F raw -b /etc/shadow /home/titan/titan/home/unsafe.qcow2')
		const machine = await titand.client.machines.create.mutate({
			name: 'Unsafe QCOW',
			imagePath: '/Home/unsafe.qcow2',
			diskSizeGb: 1,
			cores: 1,
			memoryGb: 1,
		})
		await pRetry(
			async () => {
				const current = (await titand.client.machines.list.query()).find((m) => m.id === machine.id)
				expect(current?.state).toBe('error')
				expect(current?.errorMessage).toContain('machine-image-backing-chain-not-supported')
			},
			{retries: 60, minTimeout: 500, maxTimeout: 500},
		)
		await titand.client.machines.uninstall.mutate({id: machine.id})
	})
})
