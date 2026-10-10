import fsp from 'node:fs/promises'
import os from 'node:os'
import nodePath from 'node:path'
import {afterEach, describe, expect, test} from 'vitest'

import {
	listHostUsbDevices,
	parseLsusbNames,
	usbDisplayName,
	parseAttachedUsbAddresses,
	resolveUsbAddresses,
	usbHostdevXml,
	type HostUsbDevice,
} from './usb-passthrough.js'

const directories: string[] = []
afterEach(async () => {
	await Promise.all(directories.splice(0).map((path) => fsp.rm(path, {recursive: true, force: true})))
})

// A sysfs USB tree: one directory per device or interface, one file per attribute
async function sysfs(entries: Record<string, Record<string, string>>) {
	const root = await fsp.mkdtemp(nodePath.join(os.tmpdir(), 'titan-usb-'))
	directories.push(root)
	for (const [entry, attributes] of Object.entries(entries)) {
		// ':' is not allowed in Windows file names; interfaces are only matched by prefix
		await fsp.mkdir(nodePath.join(root, entry), {recursive: true})
		for (const [name, value] of Object.entries(attributes)) {
			await fsp.writeFile(nodePath.join(root, entry, name), `${value}\n`)
		}
	}
	return root
}

const stick = {idVendor: '10c4', idProduct: 'ea60', bDeviceClass: '00', busnum: '1', devnum: '4'}

describe('USB passthrough', () => {
	test('lists pluggable devices and leaves hubs, root hubs and storage with the host', async () => {
		const root = await sysfs({
			usb1: {idVendor: '1d6b', idProduct: '0002', bDeviceClass: '09', busnum: '1', devnum: '1'},
			'1-1': {idVendor: '0bda', idProduct: '5411', bDeviceClass: '09', busnum: '1', devnum: '2'},
			'1-2': {...stick, manufacturer: 'Silicon Labs', product: 'Sonoff Zigbee 3.0 USB Dongle Plus', serial: 'abc123'},
			'1-3': {idVendor: '0781', idProduct: '5581', bDeviceClass: '08', busnum: '1', devnum: '5', product: 'Ultra'},
			'2-1': {idVendor: '8087', idProduct: '0029', bDeviceClass: 'e0', busnum: '2', devnum: '3'},
			'2-2': {idVendor: 'zzzz', idProduct: '0001', bDeviceClass: '00', busnum: '2', devnum: '9'},
		})
		expect(await listHostUsbDevices(root)).toEqual([
			{
				vendorId: '10c4',
				productId: 'ea60',
				serial: 'abc123',
				name: 'Silicon Labs Sonoff Zigbee 3.0 USB Dongle Plus',
				bus: 1,
				device: 4,
				kind: 'other',
			},
			{vendorId: '8087', productId: '0029', name: 'USB 8087:0029', bus: 2, device: 3, kind: 'wireless'},
		])
		// Devices without a description of their own are named from the USB ID database
		const names = parseLsusbNames(
			'Bus 002 Device 003: ID 8087:0029 Intel Corp. AX200 Bluetooth\nBus 001 Device 001: ID 1d6b:0002 Linux Foundation 2.0 root hub\n',
		)
		expect((await listHostUsbDevices(root, names)).map((device) => device.name)).toEqual([
			'Silicon Labs Sonoff Zigbee 3.0 USB Dongle Plus',
			'Intel Corp. AX200 Bluetooth',
		])
		expect(await listHostUsbDevices(nodePath.join(root, 'missing'))).toEqual([])
	})

	test('does not repeat a manufacturer the product name already starts with', () => {
		expect(usbDisplayName('QEMU', 'QEMU USB Tablet')).toBe('QEMU USB Tablet')
		expect(usbDisplayName('Silicon Labs', 'CP2102N USB to UART Bridge')).toBe('Silicon Labs CP2102N USB to UART Bridge')
		expect(usbDisplayName('', 'Dongle')).toBe('Dongle')
		expect(usbDisplayName('Vendor', '')).toBe('Vendor')
		expect(usbDisplayName('', '')).toBe('')
	})

	test('resolves assigned devices to their current host address', () => {
		const present: HostUsbDevice[] = [
			{vendorId: '10c4', productId: 'ea60', serial: 'one', name: 'Stick', bus: 1, device: 4, kind: 'other'},
			{vendorId: '10c4', productId: 'ea60', serial: 'two', name: 'Stick', bus: 1, device: 7, kind: 'other'},
			{vendorId: '0a12', productId: '0001', name: 'Bluetooth', bus: 3, device: 2, kind: 'wireless'},
			{vendorId: '0a12', productId: '0001', name: 'Bluetooth', bus: 3, device: 5, kind: 'wireless'},
		]
		// The serial tells identical sticks apart; unplugged devices are skipped
		expect(
			resolveUsbAddresses(
				[
					{vendorId: '10c4', productId: 'ea60', serial: 'two', name: 'Stick'},
					{vendorId: 'dead', productId: 'beef', name: 'Unplugged'},
				],
				present,
			),
		).toEqual([{bus: 1, device: 7}])
		// Two identical assignments without a serial take two different host devices
		const bluetooth = {vendorId: '0a12', productId: '0001', name: 'Bluetooth'}
		expect(resolveUsbAddresses([bluetooth, bluetooth, bluetooth], present)).toEqual([
			{bus: 3, device: 2},
			{bus: 3, device: 5},
		])
	})

	test('builds and reads back libvirt host device addresses', () => {
		const xml = usbHostdevXml({bus: 1, device: 4})
		expect(xml).toBe(
			"<hostdev mode='subsystem' type='usb' managed='yes'><source><address bus='1' device='4'/></source></hostdev>",
		)
		// libvirt adds an alias and the guest-side USB address to the live definition
		const live = `<devices>
			<hostdev mode='subsystem' type='usb' managed='yes'>
				<source>
					<address bus='1' device='4'/>
				</source>
				<alias name='hostdev0'/>
				<address type='usb' bus='0' port='3'/>
			</hostdev>
			<hostdev mode='subsystem' type='pci' managed='yes'><source><address domain='0x0000' bus='0x01' slot='0x00' function='0x0'/></source></hostdev>
			${usbHostdevXml({bus: 2, device: 11})}
		</devices>`
		expect(parseAttachedUsbAddresses(live)).toEqual([
			{bus: 1, device: 4},
			{bus: 2, device: 11},
		])
	})
})
