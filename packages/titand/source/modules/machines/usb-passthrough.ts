import fsp from 'node:fs/promises'
import nodePath from 'node:path'
import {z} from 'zod'

// A USB device assigned to a machine. Bus and device numbers change with every
// replug, so the stable identity is vendor, product and (when the device has
// one) its serial number.
export const machineUsbDeviceSchema = z.object({
	vendorId: z.string().regex(/^[0-9a-f]{4}$/),
	productId: z.string().regex(/^[0-9a-f]{4}$/),
	serial: z.string().min(1).max(126).optional(),
	name: z.string().min(1).max(200),
})
export type MachineUsbDevice = z.infer<typeof machineUsbDeviceSchema>
export const MAX_MACHINE_USB_DEVICES = 8

export type HostUsbDevice = MachineUsbDevice & {bus: number; device: number}
export type UsbAddress = {bus: number; device: number}

const USB_SYSFS_ROOT = '/sys/bus/usb/devices'
const USB_CLASS_HUB = '09'
const USB_CLASS_MASS_STORAGE = '08'

async function attribute(directory: string, name: string) {
	return fsp
		.readFile(nodePath.join(directory, name), 'utf8')
		.then((value) => value.trim())
		.catch(() => '')
}

// USB devices currently plugged into the host that a machine may take over.
// Hubs stay with the host, and so do storage devices: Files manages those as
// external drives.
export async function listHostUsbDevices(root = USB_SYSFS_ROOT): Promise<HostUsbDevice[]> {
	const entries = await fsp.readdir(root).catch(() => [] as string[])
	const devices: HostUsbDevice[] = []
	for (const entry of entries) {
		// 'usbN' are root hubs and 'bus-port:config.interface' are interfaces
		if (entry.includes(':') || entry.startsWith('usb')) continue
		const directory = nodePath.join(root, entry)
		const [vendorId, productId, deviceClass, bus, device] = await Promise.all(
			['idVendor', 'idProduct', 'bDeviceClass', 'busnum', 'devnum'].map((name) => attribute(directory, name)),
		)
		if (!/^[0-9a-f]{4}$/.test(vendorId) || !/^[0-9a-f]{4}$/.test(productId)) continue
		if (!/^\d+$/.test(bus) || !/^\d+$/.test(device) || deviceClass === USB_CLASS_HUB) continue
		const interfaceClasses = await Promise.all(
			entries
				.filter((other) => other.startsWith(`${entry}:`))
				.map((other) => attribute(nodePath.join(root, other), 'bInterfaceClass')),
		)
		if (deviceClass === USB_CLASS_MASS_STORAGE || interfaceClasses.includes(USB_CLASS_MASS_STORAGE)) continue
		const [manufacturer, product, serial] = await Promise.all(
			['manufacturer', 'product', 'serial'].map((name) => attribute(directory, name)),
		)
		const name = [manufacturer, product].filter(Boolean).join(' ').slice(0, 200)
		devices.push({
			vendorId,
			productId,
			...(serial ? {serial: serial.slice(0, 126)} : {}),
			name: name || `USB ${vendorId}:${productId}`,
			bus: Number(bus),
			device: Number(device),
		})
	}
	return devices.sort((a, b) => a.bus - b.bus || a.device - b.device)
}

export function sameUsbDevice(a: MachineUsbDevice, b: MachineUsbDevice) {
	return a.vendorId === b.vendorId && a.productId === b.productId && (a.serial ?? '') === (b.serial ?? '')
}

// Where each assigned device currently sits on the host. Unplugged devices are
// left out; a host device is never handed out twice.
export function resolveUsbAddresses(assigned: MachineUsbDevice[], present: HostUsbDevice[]): UsbAddress[] {
	const free = [...present]
	const addresses: UsbAddress[] = []
	for (const wanted of assigned) {
		const index = free.findIndex((candidate) => sameUsbDevice(wanted, candidate))
		if (index === -1) continue
		const [{bus, device}] = free.splice(index, 1)
		addresses.push({bus, device})
	}
	return addresses
}

export function usbHostdevXml({bus, device}: UsbAddress) {
	return `<hostdev mode='subsystem' type='usb' managed='yes'><source><address bus='${Math.trunc(bus)}' device='${Math.trunc(device)}'/></source></hostdev>`
}

// Host addresses of the USB devices attached to a running domain
export function parseAttachedUsbAddresses(domainXml: string): UsbAddress[] {
	const addresses: UsbAddress[] = []
	for (const [, hostdev] of domainXml.matchAll(/<hostdev\b[^>]*\btype='usb'[^>]*>([\s\S]*?)<\/hostdev>/g)) {
		const source = /<source\b[^>]*>([\s\S]*?)<\/source>/.exec(hostdev)?.[1] ?? ''
		const address = /<address\b[^>]*\bbus='(\d+)'[^>]*\bdevice='(\d+)'/.exec(source)
		if (address) addresses.push({bus: Number(address[1]), device: Number(address[2])})
	}
	return addresses
}
