import fsp from 'node:fs/promises'
import os from 'node:os'
import nodePath from 'node:path'
import {afterEach, describe, expect, test} from 'vitest'

import {
	dataDiskFileName,
	dataDiskTarget,
	dataDiskXml,
	isDataDiskTarget,
	listHostPciDevices,
	machineDataDiskSchema,
	machineSharedFolderSchema,
	parseLspciNames,
	pciHostdevXml,
	resolvePciDevices,
	sharedFolderXml,
	type HostPciDevice,
} from './machine-devices.js'

const directories: string[] = []
afterEach(async () => {
	await Promise.all(directories.splice(0).map((path) => fsp.rm(path, {recursive: true, force: true})))
})

type PciFixture = {
	class: string
	vendor: string
	device: string
	group?: number
	driver?: string
	bootVga?: boolean
	// Network interfaces with their link state
	net?: Record<string, string>
	// Disks reached through this controller
	disks?: string[]
}

// sysfs as the kernel lays it out: devices link to their IOMMU group and
// driver, and /sys/block entries link into the controller they hang off
async function sysfs(devices: Record<string, PciFixture>) {
	const root = await fsp.mkdtemp(nodePath.join(os.tmpdir(), 'titan-pci-'))
	directories.push(root)
	const pciRoot = nodePath.join(root, 'bus-pci-devices')
	const blockRoot = nodePath.join(root, 'block')
	await fsp.mkdir(pciRoot)
	await fsp.mkdir(blockRoot)
	for (const [address, fixture] of Object.entries(devices)) {
		const directory = nodePath.join(pciRoot, address)
		await fsp.mkdir(directory)
		await fsp.writeFile(nodePath.join(directory, 'class'), `${fixture.class}\n`)
		await fsp.writeFile(nodePath.join(directory, 'vendor'), `${fixture.vendor}\n`)
		await fsp.writeFile(nodePath.join(directory, 'device'), `${fixture.device}\n`)
		if (fixture.bootVga !== undefined)
			await fsp.writeFile(nodePath.join(directory, 'boot_vga'), fixture.bootVga ? '1\n' : '0\n')
		if (fixture.group !== undefined) {
			const group = nodePath.join(root, 'iommu_groups', String(fixture.group))
			await fsp.mkdir(group, {recursive: true})
			await fsp.symlink(group, nodePath.join(directory, 'iommu_group'), 'dir')
		}
		if (fixture.driver) {
			const driver = nodePath.join(root, 'drivers', fixture.driver)
			await fsp.mkdir(driver, {recursive: true})
			await fsp.symlink(driver, nodePath.join(directory, 'driver'), 'dir')
		}
		for (const [name, state] of Object.entries(fixture.net ?? {})) {
			await fsp.mkdir(nodePath.join(directory, 'net', name), {recursive: true})
			await fsp.writeFile(nodePath.join(directory, 'net', name, 'operstate'), `${state}\n`)
		}
		for (const disk of fixture.disks ?? []) {
			const target = nodePath.join(directory, 'ata1', 'host0', 'block', disk)
			await fsp.mkdir(target, {recursive: true})
			await fsp.symlink(target, nodePath.join(blockRoot, disk), 'dir')
		}
	}
	return {pciRoot, blockRoot}
}

const host = (overrides: Partial<HostPciDevice> & Pick<HostPciDevice, 'address'>): HostPciDevice => ({
	vendorId: '10de',
	deviceId: '2684',
	name: 'GPU',
	kind: 'gpu',
	iommuGroup: 1,
	bootDisplay: false,
	...overrides,
})

describe('PCI passthrough', () => {
	test('reads device names from lspci machine output', () => {
		const names = parseLspciNames(
			[
				'0000:00:02.0 "VGA compatible controller [0300]" "Intel Corporation [8086]" "Alder Lake-N [UHD Graphics] [46d1]" -r00 "Intel Corporation [8086]" "Device [7270]"',
				'0000:01:00.0 "VGA compatible controller [0300]" "NVIDIA Corporation [10de]" "AD102 [GeForce RTX 4090] [2684]" -ra1',
				'garbage',
			].join('\n'),
		)
		expect(names.get('0000:00:02.0')).toBe('Intel Corporation Alder Lake-N [UHD Graphics]')
		expect(names.get('0000:01:00.0')).toBe('NVIDIA Corporation AD102 [GeForce RTX 4090]')
		expect(names.size).toBe(2)
	})

	// Windows cannot create the symlinks sysfs is made of without elevation
	test.skipIf(process.platform === 'win32')(
		'keeps bridges, the storage in use, the active network and their IOMMU groups with the host',
		async () => {
			const roots = await sysfs({
				'0000:00:00.0': {class: '0x060000', vendor: '0x8086', device: '0x4601', group: 0},
				'0000:00:02.0': {class: '0x030000', vendor: '0x8086', device: '0x46d1', group: 1, driver: 'i915', bootVga: true},
				'0000:00:0b.0': {class: '0x120000', vendor: '0x8086', device: '0x7d1d', group: 2, driver: 'intel_vpu'},
				'0000:01:00.0': {class: '0x030000', vendor: '0x10de', device: '0x2684', group: 3, driver: 'nouveau', bootVga: false},
				'0000:01:00.1': {class: '0x040300', vendor: '0x10de', device: '0x22ba', group: 3},
				'0000:00:17.0': {class: '0x010601', vendor: '0x8086', device: '0x54d3', group: 4, disks: ['sda']},
				'0000:00:1f.3': {class: '0x040300', vendor: '0x8086', device: '0x54c8', group: 4},
				'0000:02:00.0': {class: '0x020000', vendor: '0x8086', device: '0x125c', group: 5, net: {enp2s0: 'up'}},
				'0000:03:00.0': {class: '0x020000', vendor: '0x8086', device: '0x125c', group: 6, net: {enp3s0: 'down'}},
				'0000:04:00.0': {class: '0x010802', vendor: '0x144d', device: '0xa80a', group: 7},
				'0000:05:00.0': {class: '0x0c0330', vendor: '0x1b21', device: '0x2142'},
			})
			const devices = await listHostPciDevices({...roots, names: new Map([['0000:01:00.0', 'NVIDIA RTX 4090']])})
			const summary = Object.fromEntries(
				devices.map((device) => [device.address, [device.kind, device.blockedReason ?? 'ok'].join(' ')]),
			)
			expect(summary).toEqual({
				'0000:00:02.0': 'gpu ok',
				'0000:01:00.0': 'gpu ok',
				'0000:00:0b.0': 'accelerator ok',
				'0000:02:00.0': 'network network-in-use',
				'0000:03:00.0': 'network ok',
				'0000:00:17.0': 'storage storage-in-use',
				'0000:04:00.0': 'storage ok',
				'0000:05:00.0': 'usb no-iommu',
				'0000:01:00.1': 'audio ok',
				// Shares its group with the SATA controller holding the host's disk
				'0000:00:1f.3': 'audio shared-group',
			})
			expect(devices[0]).toEqual({
				address: '0000:00:02.0',
				vendorId: '8086',
				deviceId: '46d1',
				name: 'PCI 8086:46d1',
				kind: 'gpu',
				iommuGroup: 1,
				driver: 'i915',
				bootDisplay: true,
			})
			expect(devices.find((device) => device.address === '0000:01:00.0')?.name).toBe('NVIDIA RTX 4090')
		},
	)

	test('requires every assigned device to be present, free and assigned with its whole IOMMU group', () => {
		const gpu = host({address: '0000:01:00.0'})
		const audio = host({address: '0000:01:00.1', deviceId: '22ba', kind: 'audio', name: 'GPU audio'})
		const npu = host({address: '0000:00:0b.0', vendorId: '8086', deviceId: '7d1d', kind: 'accelerator', iommuGroup: 2})
		const present = [gpu, audio, npu]
		expect(resolvePciDevices([gpu, audio], present)).toEqual({devices: [gpu, audio]})
		expect(resolvePciDevices([npu], present)).toEqual({devices: [npu]})
		expect(resolvePciDevices([gpu], present)).toEqual({error: 'group-incomplete'})
		// A different card now sits in that slot
		expect(resolvePciDevices([{...npu, deviceId: 'ffff'}], present)).toEqual({error: 'unavailable'})
		expect(resolvePciDevices([npu], [{...npu, blockedReason: 'no-iommu'}])).toEqual({error: 'blocked'})
	})

	test('builds the libvirt host device for a PCI address', () => {
		expect(pciHostdevXml({address: '0000:01:00.1'})).toBe(
			"<hostdev mode='subsystem' type='pci' managed='yes'><source><address domain='0x0000' bus='0x01' slot='0x00' function='0x1'/></source></hostdev>",
		)
	})
})

describe('Shared folders and data disks', () => {
	test('only accepts normalized locations inside user storage', () => {
		const folder = (path: string) => machineSharedFolderSchema.safeParse({path, tag: 'daten'}).success
		expect(folder('/Home/Dokumente')).toBe(true)
		expect(folder('/External/Drive/Media')).toBe(true)
		expect(folder('/Home/../Apps')).toBe(false)
		expect(folder('/Apps/nextcloud')).toBe(false)
		expect(folder('/Home')).toBe(false)
		expect(machineSharedFolderSchema.safeParse({path: '/Home/a', tag: 'Has Space'}).success).toBe(false)
		expect(machineDataDiskSchema.safeParse({id: 'abcd1234', directory: '/Home/VM', sizeGb: 0}).success).toBe(false)
		expect(machineDataDiskSchema.safeParse({id: 'abcd1234', directory: '/Machines/x', sizeGb: 10}).success).toBe(false)
	})

	test('names data disk images per machine and keeps guest targets away from system disks', () => {
		expect(dataDiskFileName('home-assistant', {id: 'abcd1234'})).toBe('home-assistant-data-abcd1234.qcow2')
		expect(dataDiskTarget(0)).toBe('vdj')
		expect(dataDiskTarget(7)).toBe('vdq')
		expect(isDataDiskTarget('vdj')).toBe(true)
		expect(isDataDiskTarget('vda')).toBe(false)
		expect(isDataDiskTarget('sda')).toBe(false)
		expect(dataDiskXml("/run/x/data-disk-abcd1234.qcow2", 1)).toBe(
			"<disk type='file' device='disk'><driver name='qemu' type='qcow2' cache='none' discard='unmap'/><source file='/run/x/data-disk-abcd1234.qcow2'/><target dev='vdk' bus='virtio'/></disk>",
		)
	})

	test('escapes shared folder paths and marks read-only shares', () => {
		expect(sharedFolderXml("/home/titan/titan/home/Tom's & Co", {tag: 'daten', readOnly: true})).toBe(
			"<filesystem type='mount' accessmode='passthrough'><driver type='virtiofs'/><source dir='/home/titan/titan/home/Tom&apos;s &amp; Co'/><target dir='daten'/><readonly/></filesystem>",
		)
		expect(sharedFolderXml('/data', {tag: 'daten'})).not.toContain('readonly')
	})
})
