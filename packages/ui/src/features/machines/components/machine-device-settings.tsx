import {PlusCircle, Trash2} from 'lucide-react'
import {lazy, Suspense, useState} from 'react'

import {Button} from '@/components/ui/button'
import {MaturityBadge} from '@/components/ui/feature-maturity-badge'
import {Input} from '@/components/ui/input'
import {Switch} from '@/components/ui/switch'
import {MAX_DISK_SIZE_GB} from '@/features/machines/constants'
import type {Machine} from '@/features/machines/types'
import {trpcReact} from '@/trpc/trpc'
import {t} from '@/utils/i18n'

const MiniBrowser = lazy(() =>
	import('@/features/files/components/mini-browser').then((m) => ({default: m.MiniBrowser})),
)

export type PciDevice = NonNullable<Machine['pciDevices']>[number]
export type SharedFolder = NonNullable<Machine['sharedFolders']>[number]
export type DataDisk = NonNullable<Machine['dataDisks']>[number]

const MAX_SHARED_FOLDERS = 8
const MAX_DATA_DISKS = 8
const DEFAULT_DATA_DISK_GB = 32
const STORAGE_ROOTS = ['/Home', '/External', '/Network']

// Any folder below a storage root: the roots themselves are not shareable
const isStorageFolder = (entry: {type: string; path: string}) =>
	entry.type === 'directory' && entry.path.split('/').filter(Boolean).length >= 2

const rowClass = 'flex items-center justify-between gap-4 rounded-12 border-hpx border-white/10 bg-white/6 px-3.5 py-2.5'
const removeButtonClass =
	'grid size-8 shrink-0 place-items-center rounded-full text-white/35 hover:bg-white/10 hover:text-white disabled:cursor-not-allowed disabled:opacity-35 disabled:hover:bg-transparent disabled:hover:text-white/35'

function Section({
	title,
	description,
	badge,
	action,
	children,
}: {
	title: string
	description: string
	badge?: React.ReactNode
	action?: React.ReactNode
	children: React.ReactNode
}) {
	return (
		<div className='flex flex-col gap-3 py-5'>
			<div className='flex items-start justify-between gap-4'>
				<div className='flex flex-col gap-1'>
					<span className='flex items-center gap-2 text-15 font-medium -tracking-2 text-white'>
						{title}
						{badge}
					</span>
					<p className='max-w-[460px] text-12 leading-snug -tracking-2 text-white/40'>{description}</p>
				</div>
				{action}
			</div>
			{children}
		</div>
	)
}

const EmptyNote = ({children}: {children: React.ReactNode}) => (
	<p className='rounded-8 bg-white/4 px-3 py-2.5 text-12 leading-snug -tracking-2 text-white/35'>{children}</p>
)

function FolderPicker({
	open,
	onOpenChange,
	onSelect,
	title,
}: {
	open: boolean
	onOpenChange: (open: boolean) => void
	onSelect: (path: string) => void
	title: string
}) {
	if (!open) return null
	return (
		<Suspense>
			<MiniBrowser
				open={open}
				onOpenChange={onOpenChange}
				rootPath='/Home'
				rootPaths={STORAGE_ROOTS}
				preselectOnOpen={false}
				selectionMode='folders'
				selectableFilter={isStorageFolder}
				allowNewFolderCreation
				onSelect={onSelect}
				title={title}
			/>
		</Suspense>
	)
}

const samePciDevice = (a: PciDevice, b: PciDevice) =>
	a.address === b.address && a.vendorId === b.vendorId && a.deviceId === b.deviceId
const toPciDevice = ({address, vendorId, deviceId, name}: PciDevice): PciDevice => ({address, vendorId, deviceId, name})

// GPUs, NPUs and other PCI devices. A device moves together with everything
// in its IOMMU group, so one switch drives the whole group.
export function PciDevicesSection({
	machine,
	machines,
	value,
	onChange,
	disabled,
}: {
	machine: Machine
	machines: Machine[]
	value: PciDevice[]
	onChange: (value: PciDevice[]) => void
	disabled: boolean
}) {
	const host = trpcReact.machines.pciDevices.useQuery(undefined, {staleTime: 5_000}).data
	const hostDevices = host?.devices ?? []
	const kindLabel = (kind: (typeof hostDevices)[number]['kind']) =>
		({
			gpu: t('machines.pci-kind-gpu'),
			accelerator: t('machines.pci-kind-accelerator'),
			audio: t('machines.pci-kind-audio'),
			network: t('machines.pci-kind-network'),
			storage: t('machines.pci-kind-storage'),
			usb: t('machines.pci-kind-usb'),
			other: t('machines.pci-kind-other'),
		})[kind]
	const blockedLabel = (reason: NonNullable<(typeof hostDevices)[number]['blockedReason']>) =>
		({
			'no-iommu': t('machines.pci-blocked-no-iommu'),
			'storage-in-use': t('machines.pci-blocked-storage'),
			'network-in-use': t('machines.pci-blocked-network'),
			'shared-group': t('machines.pci-blocked-group'),
		})[reason]
	// Assigned devices that are no longer in the host stay listed so they can be released
	const missing = value.filter((assigned) => !hostDevices.some((device) => samePciDevice(device, assigned)))

	return (
		<Section
			title={t('machines.pci-devices')}
			description={t('machines.pci-devices-description')}
			badge={<MaturityBadge maturity='beta' />}
		>
			{host && !host.supported ? (
				<EmptyNote>{t('machines.pci-unsupported')}</EmptyNote>
			) : host && !host.iommuAvailable ? (
				<EmptyNote>{t('machines.pci-no-iommu')}</EmptyNote>
			) : hostDevices.length === 0 && missing.length === 0 ? (
				<EmptyNote>{t('machines.pci-devices-empty')}</EmptyNote>
			) : (
				<div className='flex flex-col gap-2'>
					{hostDevices.map((device) => {
						const group = hostDevices.filter((other) => other.iommuGroup === device.iommuGroup)
						const assigned = value.some((other) => samePciDevice(other, device))
						const otherMachine = device.machineId && device.machineId !== machine.id ? device.machineId : undefined
						const groupTaken = group.some((other) => other.machineId && other.machineId !== machine.id)
						const notes = [
							kindLabel(device.kind),
							device.address,
							otherMachine
								? t('machines.usb-device-in-use', {
										machineName: machines.find((other) => other.id === otherMachine)?.name ?? otherMachine,
									})
								: device.blockedReason
									? blockedLabel(device.blockedReason)
									: group.length > 1
										? t('machines.pci-group', {count: group.length - 1})
										: undefined,
							device.bootDisplay && !device.blockedReason ? t('machines.pci-boot-display') : undefined,
						].filter(Boolean)
						return (
							<label key={device.address} className={rowClass}>
								<span className='flex min-w-0 flex-col'>
									<span className='truncate text-13 -tracking-2 text-white'>{device.name}</span>
									<span className='text-11 leading-snug -tracking-1 text-white/35'>{notes.join(' · ')}</span>
								</span>
								<Switch
									checked={assigned}
									disabled={disabled || (!assigned && (!!device.blockedReason || groupTaken))}
									onCheckedChange={(checked) =>
										onChange([
											...value.filter((other) => !group.some((member) => samePciDevice(member, other))),
											...(checked ? group.map(toPciDevice) : []),
										])
									}
									aria-label={device.name}
								/>
							</label>
						)
					})}
					{missing.map((device) => (
						<label key={device.address} className={rowClass}>
							<span className='flex min-w-0 flex-col'>
								<span className='truncate text-13 -tracking-2 text-white'>{device.name}</span>
								<span className='text-11 -tracking-1 text-white/35'>
									{device.address} · {t('machines.pci-device-missing')}
								</span>
							</span>
							<Switch
								checked
								disabled={disabled}
								onCheckedChange={() => onChange(value.filter((other) => !samePciDevice(other, device)))}
								aria-label={device.name}
							/>
						</label>
					))}
				</div>
			)}
		</Section>
	)
}

function folderTag(path: string, taken: string[]) {
	const base =
		(path.split('/').filter(Boolean).pop() ?? '')
			.toLowerCase()
			.replace(/[^a-z0-9]+/g, '-')
			.replace(/^-+|-+$/g, '')
			.slice(0, 30) || 'ordner'
	if (!taken.includes(base)) return base
	for (let suffix = 2; ; suffix++) if (!taken.includes(`${base}-${suffix}`)) return `${base}-${suffix}`
}

export const sharedFolderTagValid = (tag: string) => /^[a-z0-9][a-z0-9_-]{0,35}$/.test(tag)

// Folders from Files mounted live inside the guest through virtiofs
export function SharedFoldersSection({
	value,
	onChange,
	disabled,
}: {
	value: SharedFolder[]
	onChange: (value: SharedFolder[]) => void
	disabled: boolean
}) {
	const [browserOpen, setBrowserOpen] = useState(false)
	const update = (index: number, change: Partial<SharedFolder>) =>
		onChange(value.map((folder, position) => (position === index ? {...folder, ...change} : folder)))
	const duplicateTag = (tag: string) => value.filter((folder) => folder.tag === tag).length > 1

	return (
		<Section
			title={t('machines.shared-folders')}
			description={t('machines.shared-folders-description')}
			action={
				<Button
					size='sm'
					className='shrink-0'
					onClick={() => setBrowserOpen(true)}
					disabled={disabled || value.length >= MAX_SHARED_FOLDERS}
				>
					{t('machines.add-shared-folder')}
					<PlusCircle className='h-3 w-3' />
				</Button>
			}
		>
			{value.length === 0 ? (
				<EmptyNote>{t('machines.shared-folders-empty')}</EmptyNote>
			) : (
				<div className='flex flex-col gap-2'>
					{value.map((folder, index) => (
						<div key={folder.path} className='flex items-center gap-2'>
							<div className={`${rowClass} min-w-0 flex-1 flex-wrap`}>
								<span className='flex min-w-0 flex-1 flex-col'>
									<span className='truncate text-13 -tracking-2 text-white'>{folder.path}</span>
									<span className='truncate font-mono text-11 text-white/35'>
										mount -t virtiofs {folder.tag} /mnt/{folder.tag}
									</span>
								</span>
								<Input
									type='text'
									value={folder.tag}
									onValueChange={(tag) => update(index, {tag: tag.toLowerCase()})}
									disabled={disabled}
									sizeVariant='short'
									aria-label={t('machines.shared-folder-tag')}
									aria-invalid={!sharedFolderTagValid(folder.tag) || duplicateTag(folder.tag)}
									className='w-32 text-white'
								/>
								<label className='flex shrink-0 items-center gap-2 text-12 -tracking-2 text-white/50'>
									{t('machines.shared-folder-read-only')}
									<Switch
										checked={!!folder.readOnly}
										disabled={disabled}
										onCheckedChange={(readOnly) => update(index, {readOnly})}
									/>
								</label>
							</div>
							<button
								type='button'
								disabled={disabled}
								className={removeButtonClass}
								onClick={() => onChange(value.filter((_, position) => position !== index))}
								aria-label={t('remove')}
							>
								<Trash2 className='size-3.5' />
							</button>
						</div>
					))}
				</div>
			)}
			{value.some((folder) => !sharedFolderTagValid(folder.tag) || duplicateTag(folder.tag)) && (
				<p className='text-12 text-destructive2-lightest'>{t('machines.shared-folder-tag-invalid')}</p>
			)}
			<FolderPicker
				open={browserOpen}
				onOpenChange={setBrowserOpen}
				title={t('machines.shared-folder-select')}
				onSelect={(path) => {
					if (value.some((folder) => folder.path === path)) return
					onChange([...value, {path, tag: folderTag(path, value.map((folder) => folder.tag))}])
				}}
			/>
		</Section>
	)
}

const newDataDiskId = () =>
	Array.from({length: 8}, () => 'abcdefghijklmnopqrstuvwxyz0123456789'[Math.floor(Math.random() * 36)]).join('')

export const dataDiskSizeValid = (disk: DataDisk, saved: DataDisk[]) =>
	Number.isInteger(disk.sizeGb) &&
	disk.sizeGb >= (saved.find((other) => other.id === disk.id)?.sizeGb ?? 1) &&
	disk.sizeGb <= MAX_DISK_SIZE_GB

// Additional virtual disks whose images live in folders chosen in Files.
// `saved` is what the machine has now: existing disks can only grow, and the
// ones missing from `value` are deleted on save.
export function DataDisksSection({
	value,
	saved,
	onChange,
	disabled,
}: {
	value: DataDisk[]
	saved: DataDisk[]
	onChange: (value: DataDisk[]) => void
	disabled: boolean
}) {
	const [browserOpen, setBrowserOpen] = useState(false)
	const removed = saved.filter((disk) => !value.some((other) => other.id === disk.id))

	return (
		<Section
			title={t('machines.data-disks')}
			description={t('machines.data-disks-description')}
			action={
				<Button
					size='sm'
					className='shrink-0'
					onClick={() => setBrowserOpen(true)}
					disabled={disabled || value.length >= MAX_DATA_DISKS}
				>
					{t('machines.add-data-disk')}
					<PlusCircle className='h-3 w-3' />
				</Button>
			}
		>
			{value.length === 0 ? (
				<EmptyNote>{t('machines.data-disks-empty')}</EmptyNote>
			) : (
				<div className='flex flex-col gap-2'>
					{value.map((disk) => (
						<div key={disk.id} className='flex items-center gap-2'>
							<div className={`${rowClass} min-w-0 flex-1`}>
								<span className='flex min-w-0 flex-1 flex-col'>
									<span className='truncate text-13 -tracking-2 text-white'>{disk.directory}</span>
									<span className='text-11 -tracking-1 text-white/35'>
										{saved.some((other) => other.id === disk.id)
											? t('machines.data-disk-existing')
											: t('machines.data-disk-new')}
									</span>
								</span>
								<div className='relative w-24 shrink-0'>
									<Input
										type='text'
										inputMode='numeric'
										value={disk.sizeGb ? String(disk.sizeGb) : ''}
										onValueChange={(raw) =>
											onChange(
												value.map((other) =>
													other.id === disk.id
														? {...other, sizeGb: Math.min(Number(raw.replace(/[^0-9]/g, '')) || 0, MAX_DISK_SIZE_GB)}
														: other,
												),
											)
										}
										disabled={disabled}
										sizeVariant='short'
										aria-label={t('machines.disk-size')}
										aria-invalid={!dataDiskSizeValid(disk, saved)}
										className='pr-9 text-right text-white tabular-nums'
									/>
									<span className='pointer-events-none absolute top-1/2 right-3.5 -translate-y-1/2 text-13 text-white'>
										GB
									</span>
								</div>
							</div>
							<button
								type='button'
								disabled={disabled}
								className={removeButtonClass}
								onClick={() => onChange(value.filter((other) => other.id !== disk.id))}
								aria-label={t('remove')}
							>
								<Trash2 className='size-3.5' />
							</button>
						</div>
					))}
				</div>
			)}
			{value.some((disk) => !dataDiskSizeValid(disk, saved)) && (
				<p className='text-12 text-destructive2-lightest'>{t('machines.data-disk-size-invalid')}</p>
			)}
			{removed.length > 0 && (
				<p role='status' className='text-12 text-destructive2-lightest'>
					{t('machines.data-disk-delete-warning', {count: removed.length})}
				</p>
			)}
			<FolderPicker
				open={browserOpen}
				onOpenChange={setBrowserOpen}
				title={t('machines.data-disk-select')}
				onSelect={(directory) => onChange([...value, {id: newDataDiskId(), directory, sizeGb: DEFAULT_DATA_DISK_GB}])}
			/>
		</Section>
	)
}
