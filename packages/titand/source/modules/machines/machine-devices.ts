import fsp from 'node:fs/promises'
import nodePath from 'node:path'
import {z} from 'zod'

// ---------------------------------------------------------------------------
// PCI passthrough
// ---------------------------------------------------------------------------

// A host PCI device handed to a machine. The address is where it sits; vendor
// and device id guard against a different card appearing at that address.
export const machinePciDeviceSchema = z.object({
	address: z.string().regex(/^[0-9a-f]{4}:[0-9a-f]{2}:[0-9a-f]{2}\.[0-7]$/),
	vendorId: z.string().regex(/^[0-9a-f]{4}$/),
	deviceId: z.string().regex(/^[0-9a-f]{4}$/),
	name: z.string().min(1).max(200),
})
export type MachinePciDevice = z.infer<typeof machinePciDeviceSchema>
export const MAX_MACHINE_PCI_DEVICES = 16

export type PciDeviceKind = 'gpu' | 'accelerator' | 'audio' | 'network' | 'storage' | 'usb' | 'other'
// Why a device must stay with the host
export type PciBlockedReason = 'no-iommu' | 'storage-in-use' | 'network-in-use' | 'shared-group'

export type HostPciDevice = MachinePciDevice & {
	kind: PciDeviceKind
	iommuGroup?: number
	driver?: string
	// The firmware's primary display: passing it through blanks the local console
	bootDisplay: boolean
	blockedReason?: PciBlockedReason
}

const PCI_SYSFS_ROOT = '/sys/bus/pci/devices'
const BLOCK_SYSFS_ROOT = '/sys/block'
const PCI_CLASS_BRIDGE = '06'

function pciKind(classCode: string): PciDeviceKind {
	const base = classCode.slice(0, 2)
	if (base === '03') return 'gpu'
	if (base === '12') return 'accelerator'
	if (base === '02') return 'network'
	if (base === '01') return 'storage'
	if (classCode === '0c03') return 'usb'
	if (classCode === '0401' || classCode === '0403') return 'audio'
	return 'other'
}

async function read(path: string) {
	return fsp
		.readFile(path, 'utf8')
		.then((value) => value.trim())
		.catch(() => '')
}

async function linkName(path: string) {
	return fsp
		.readlink(path)
		.then((target) => nodePath.basename(target))
		.catch(() => undefined)
}

// `lspci -D -mm -nn` prints: address "class [cccc]" "vendor [vvvv]" "device [dddd]" ...
export function parseLspciNames(output: string) {
	const names = new Map<string, string>()
	for (const line of output.split('\n')) {
		const address = /^([0-9a-f]{4}:[0-9a-f]{2}:[0-9a-f]{2}\.[0-7])\s/.exec(line)?.[1]
		const fields = [...line.matchAll(/"([^"]*)"/g)].map((match) => match[1].replace(/\s*\[[0-9a-f]{4}\]$/, ''))
		if (!address || fields.length < 3) continue
		const name = [fields[1], fields[2]].filter(Boolean).join(' ').slice(0, 200)
		if (name) names.set(address, name)
	}
	return names
}

// Every PCI function a machine could take over, with the reason when it has to
// stay with the host. Bridges are plumbing and never listed. A device is kept
// when the host stores data through it or reaches the network through it, and
// so is everything sharing its IOMMU group: a group moves as a whole.
export async function listHostPciDevices({
	names = new Map<string, string>(),
	pciRoot = PCI_SYSFS_ROOT,
	blockRoot = BLOCK_SYSFS_ROOT,
}: {names?: Map<string, string>; pciRoot?: string; blockRoot?: string} = {}): Promise<HostPciDevice[]> {
	const addresses = (await fsp.readdir(pciRoot).catch(() => [] as string[])).filter((entry) =>
		machinePciDeviceSchema.shape.address.safeParse(entry).success,
	)
	// Resolved sysfs paths of every disk, e.g. .../0000:00:17.0/ata1/host0/.../block/sda
	const diskPaths = await Promise.all(
		(await fsp.readdir(blockRoot).catch(() => [] as string[])).map((disk) =>
			fsp.realpath(nodePath.join(blockRoot, disk)).catch(() => ''),
		),
	)
	const devices: HostPciDevice[] = []
	for (const address of addresses) {
		const directory = nodePath.join(pciRoot, address)
		const [classValue, vendor, device, bootVga, group, driver] = await Promise.all([
			read(nodePath.join(directory, 'class')),
			read(nodePath.join(directory, 'vendor')),
			read(nodePath.join(directory, 'device')),
			read(nodePath.join(directory, 'boot_vga')),
			linkName(nodePath.join(directory, 'iommu_group')),
			linkName(nodePath.join(directory, 'driver')),
		])
		// class is 0xCCSSPP, vendor and device are 0xVVVV
		const classCode = /^0x([0-9a-f]{4})[0-9a-f]{2}$/.exec(classValue)?.[1]
		const vendorId = /^0x([0-9a-f]{4})$/.exec(vendor)?.[1]
		const deviceId = /^0x([0-9a-f]{4})$/.exec(device)?.[1]
		if (!classCode || !vendorId || !deviceId || classCode.startsWith(PCI_CLASS_BRIDGE)) continue
		const iommuGroup = group !== undefined && /^\d+$/.test(group) ? Number(group) : undefined
		const interfaces = await fsp.readdir(nodePath.join(directory, 'net')).catch(() => [] as string[])
		const linkStates = await Promise.all(
			interfaces.map((name) => read(nodePath.join(directory, 'net', name, 'operstate'))),
		)
		const blockedReason: PciBlockedReason | undefined =
			iommuGroup === undefined
				? 'no-iommu'
				: diskPaths.some((path) => path.split(/[\\/]/).includes(address))
					? 'storage-in-use'
					: linkStates.some((state) => state === 'up')
						? 'network-in-use'
						: undefined
		devices.push({
			address,
			vendorId,
			deviceId,
			name: names.get(address) ?? `PCI ${vendorId}:${deviceId}`,
			kind: pciKind(classCode),
			...(iommuGroup !== undefined ? {iommuGroup} : {}),
			...(driver ? {driver} : {}),
			bootDisplay: bootVga === '1',
			...(blockedReason ? {blockedReason} : {}),
		})
	}
	for (const device of devices) {
		if (device.blockedReason || device.iommuGroup === undefined) continue
		if (devices.some((other) => other.iommuGroup === device.iommuGroup && other.blockedReason)) {
			device.blockedReason = 'shared-group'
		}
	}
	const order: PciDeviceKind[] = ['gpu', 'accelerator', 'network', 'storage', 'usb', 'audio', 'other']
	return devices.sort(
		(a, b) => order.indexOf(a.kind) - order.indexOf(b.kind) || a.address.localeCompare(b.address),
	)
}

export function samePciDevice(a: MachinePciDevice, b: MachinePciDevice) {
	return a.address === b.address && a.vendorId === b.vendorId && a.deviceId === b.deviceId
}

// The host devices behind a machine's assignment, or the reason it cannot be
// used as it stands. Every member of an IOMMU group has to be assigned.
export function resolvePciDevices(
	assigned: MachinePciDevice[],
	present: HostPciDevice[],
): {devices: HostPciDevice[]} | {error: 'unavailable' | 'blocked' | 'group-incomplete'} {
	const devices: HostPciDevice[] = []
	for (const wanted of assigned) {
		const device = present.find((candidate) => samePciDevice(candidate, wanted))
		if (!device) return {error: 'unavailable'}
		if (device.blockedReason) return {error: 'blocked'}
		devices.push(device)
	}
	for (const device of devices) {
		const group = present.filter((other) => other.iommuGroup === device.iommuGroup)
		if (group.some((member) => !devices.includes(member))) return {error: 'group-incomplete'}
	}
	return {devices}
}

export function pciHostdevXml({address}: Pick<MachinePciDevice, 'address'>) {
	const [, domain, bus, slot, pciFunction] = /^([0-9a-f]{4}):([0-9a-f]{2}):([0-9a-f]{2})\.([0-7])$/.exec(address)!
	return `<hostdev mode='subsystem' type='pci' managed='yes'><source><address domain='0x${domain}' bus='0x${bus}' slot='0x${slot}' function='0x${pciFunction}'/></source></hostdev>`
}

// ---------------------------------------------------------------------------
// Shared folders and data disks
// ---------------------------------------------------------------------------

// Locations in Files a machine may reach into or keep a disk in
const MACHINE_STORAGE_ROOTS = ['/Home/', '/External/', '/Network/']

const machineStoragePathSchema = z
	.string()
	.max(1024)
	.refine(
		(value) => nodePath.posix.normalize(value) === value && MACHINE_STORAGE_ROOTS.some((root) => value.startsWith(root)),
	)

// A host folder mounted live inside the guest through virtiofs. The guest
// mounts it by its tag: mount -t virtiofs <tag> /mnt/<tag>
export const machineSharedFolderSchema = z.object({
	path: machineStoragePathSchema,
	tag: z.string().regex(/^[a-z0-9][a-z0-9_-]{0,35}$/),
	readOnly: z.boolean().optional(),
})
export type MachineSharedFolder = z.infer<typeof machineSharedFolderSchema>
export const MAX_MACHINE_SHARED_FOLDERS = 8

// An additional virtual disk whose image lives in a folder chosen in Files
export const machineDataDiskSchema = z.object({
	id: z.string().regex(/^[a-z0-9]{8}$/),
	directory: machineStoragePathSchema,
	sizeGb: z.number().int().min(1).max(10_000),
})
export type MachineDataDisk = z.infer<typeof machineDataDiskSchema>
export const MAX_MACHINE_DATA_DISKS = 8

export function dataDiskFileName(machineId: string, disk: Pick<MachineDataDisk, 'id'>) {
	return `${machineId}-data-${disk.id}.qcow2`
}

// Data disks sit behind the system disk and install media: vdj, vdk, ...
const DATA_DISK_TARGETS = 'jklmnopq'
export function dataDiskTarget(index: number) {
	return `vd${DATA_DISK_TARGETS[index]}`
}
export function isDataDiskTarget(target: string) {
	return /^vd[j-q]$/.test(target)
}

function escapeXml(value: string) {
	return value.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/'/g, '&apos;')
}

export function dataDiskXml(runtimePath: string, index: number) {
	return `<disk type='file' device='disk'><driver name='qemu' type='qcow2' cache='none' discard='unmap'/><source file='${escapeXml(runtimePath)}'/><target dev='${dataDiskTarget(index)}' bus='virtio'/></disk>`
}

export function sharedFolderXml(systemPath: string, folder: Pick<MachineSharedFolder, 'tag' | 'readOnly'>) {
	return `<filesystem type='mount' accessmode='passthrough'><driver type='virtiofs'/><source dir='${escapeXml(systemPath)}'/><target dir='${escapeXml(folder.tag)}'/>${folder.readOnly ? '<readonly/>' : ''}</filesystem>`
}

// virtiofs shares guest memory with the host daemon serving the folder
export const SHARED_FOLDER_MEMORY_BACKING = "<memoryBacking><source type='memfd'/><access mode='shared'/></memoryBacking>"
