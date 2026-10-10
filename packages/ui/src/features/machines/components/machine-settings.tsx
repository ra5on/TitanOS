import {ChevronDown, MoveRight, Pencil, PlusCircle, Power, Trash2} from 'lucide-react'
import {motion} from 'motion/react'
import {useState} from 'react'
import {Navigate, useNavigate, useParams} from 'react-router-dom'

import {
	AlertDialog,
	AlertDialogAction,
	AlertDialogCancel,
	AlertDialogContent,
	AlertDialogDescription,
	AlertDialogFooter,
	AlertDialogHeader,
	AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import {Button} from '@/components/ui/button'
import {CopyButton} from '@/components/ui/copy-button'
import {DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger} from '@/components/ui/dropdown-menu'
import {Input} from '@/components/ui/input'
import {Spinner} from '@/components/ui/loading'
import {Switch} from '@/components/ui/switch'
import {toast} from '@/components/ui/toast'
import {
	DataDisksSection,
	dataDiskSizeValid,
	PciDevicesSection,
	SharedFoldersSection,
	SnapshotsSection,
	sharedFolderTagValid,
	type DataDisk,
	type PciDevice,
	type SharedFolder,
} from '@/features/machines/components/machine-device-settings'
import {MachineNetworkField} from '@/features/machines/components/network-field'
import {OsIcon} from '@/features/machines/components/os-icon'
import {SpecRow, Stepper} from '@/features/machines/components/spec-form'
import {
	DEFAULT_CORES,
	DEFAULT_MEMORY_GB,
	diskStepGb,
	getOsVisuals,
	hostReservedMemoryBytes,
	machinePath,
	MACHINES_PATH,
	MAX_DISK_SIZE_GB,
	MIN_MEMORY_GB,
} from '@/features/machines/constants'
import {useMachineActions} from '@/features/machines/hooks/use-machine-actions'
import {useMachine, useMachineNetworks, useMachines} from '@/features/machines/hooks/use-machines'
import {
	canChangeMachineNetwork,
	machineNetworkAvailable,
	sameMachineNetwork,
	type MachineNetwork,
} from '@/features/machines/network-settings'
import {machineSettingsRequireShutdown} from '@/features/machines/settings'
import type {Machine} from '@/features/machines/types'
import {createBrowserUuid} from '@/features/machines/utils'
import {useCpu} from '@/hooks/use-cpu'
import {useMemory} from '@/hooks/use-memory'
import {cn} from '@/lib/utils'
import {trpcReact} from '@/trpc/trpc'
import {t} from '@/utils/i18n'

type UsbDevice = NonNullable<Machine['usbDevices']>[number]
const MAX_USB_DEVICES = 8
const usbKey = (usbDevice: UsbDevice) => `${usbDevice.vendorId}:${usbDevice.productId}:${usbDevice.serial ?? ''}`
const toUsbDevice = ({vendorId, productId, serial, name}: UsbDevice): UsbDevice => ({
	vendorId,
	productId,
	...(serial ? {serial} : {}),
	name,
})

const usbKindLabel = (kind: 'input' | 'wireless' | 'serial' | 'audio' | 'video' | 'printer' | 'smartcard') =>
	({
		input: t('machines.usb-kind-input'),
		wireless: t('machines.usb-kind-wireless'),
		serial: t('machines.usb-kind-serial'),
		audio: t('machines.usb-kind-audio'),
		video: t('machines.usb-kind-video'),
		printer: t('machines.usb-kind-printer'),
		smartcard: t('machines.usb-kind-smartcard'),
	})[kind]

// How long an autostart machine waits after TitanOS is ready
const AUTOSTART_DELAYS = [0, 15, 30, 60, 120, 300, 600]
const autostartDelayLabel = (seconds: number) =>
	seconds === 0
		? t('machines.autostart-delay-none')
		: seconds % 60 === 0
			? t('machines.autostart-delay-minutes', {count: seconds / 60})
			: t('machines.autostart-delay-seconds', {count: seconds})

const segmentButtonClass = (active: boolean) =>
	cn(
		'h-9 rounded-full border px-4 text-11 font-semibold uppercase transition-colors disabled:opacity-35',
		active ? 'border-brand bg-brand/20 text-white' : 'border-white/10 bg-white/5 text-white/60 hover:bg-white/10',
	)

// Edit a machine's name and resources at /machines/:machineId/settings, in the
// same spec-sheet layout as the create form. Fields fall back to the machine's
// live values until touched, so background updates never clobber edits.
export default function MachineSettings() {
	const navigate = useNavigate()
	const {machineId} = useParams<{machineId: string}>()
	const {machine, isLoading} = useMachine(machineId)
	const {machines} = useMachines()
	const networks = useMachineNetworks()
	const {updateSettings, stop} = useMachineActions()
	const {threads} = useCpu()
	const {data: memory} = useMemory()
	// Plugging a device in or out should show up while the page is open
	const hostUsbDevices = trpcReact.machines.usbDevices.useQuery(undefined, {refetchInterval: 5_000}).data ?? []

	const [nameInput, setNameInput] = useState<string | null>(null)
	const [editingName, setEditingName] = useState(false)
	const [coresChoice, setCoresChoice] = useState<number | null>(null)
	const [memoryChoice, setMemoryChoice] = useState<number | null>(null)
	// Kept as a string so the field can be cleared while typing (empty = invalid)
	const [diskChoice, setDiskChoice] = useState<string | null>(null)
	const [firmwareChoice, setFirmwareChoice] = useState<'uefi' | 'bios' | null>(null)
	const [diskBusChoice, setDiskBusChoice] = useState<'virtio' | 'sata' | null>(null)
	const [videoChoice, setVideoChoice] = useState<'virtio' | 'vga' | null>(null)
	const [autostartChoice, setAutostartChoice] = useState<boolean | null>(null)
	const [autostartDelayChoice, setAutostartDelayChoice] = useState<number | null>(null)
	const [usbChoice, setUsbChoice] = useState<UsbDevice[] | null>(null)
	const [pciChoice, setPciChoice] = useState<PciDevice[] | null>(null)
	const [foldersChoice, setFoldersChoice] = useState<SharedFolder[] | null>(null)
	const [disksChoice, setDisksChoice] = useState<DataDisk[] | null>(null)
	const [networkChoice, setNetworkChoice] = useState<MachineNetwork | null>(null)
	const [forwardsChoice, setForwardsChoice] = useState<Machine['portForwards'] | null>(null)
	const [isSaving, setIsSaving] = useState(false)
	const [shutdownDialogOpen, setShutdownDialogOpen] = useState(false)

	if (isLoading) {
		return (
			<div className='grid min-h-[320px] w-full place-items-center p-12'>
				<Spinner />
			</div>
		)
	}
	if (!machine) return <Navigate to={MACHINES_PATH} replace />

	const {color} = getOsVisuals(machine.osId)

	const name = nameInput ?? machine.name
	const trimmedName = name.trim()
	const cores = coresChoice ?? machine.cores
	const memoryGb = memoryChoice ?? machine.memoryGb
	const diskInput = diskChoice ?? String(machine.diskSizeGb)
	const firmware = firmwareChoice ?? machine.firmware
	const diskBus = diskBusChoice ?? machine.diskBus ?? 'virtio'
	const videoModel = videoChoice ?? machine.videoModel ?? 'virtio'
	const autostart = autostartChoice ?? machine.autostart
	const savedAutostartDelay = machine.autostartDelaySeconds ?? 0
	const autostartDelay = autostartDelayChoice ?? savedAutostartDelay
	// A delay set elsewhere may lie between the steps offered here
	const autostartDelayStep = (direction: 1 | -1) =>
		direction === 1
			? AUTOSTART_DELAYS.find((seconds) => seconds > autostartDelay)
			: AUTOSTART_DELAYS.findLast((seconds) => seconds < autostartDelay)
	const usbDevices = usbChoice ?? machine.usbDevices ?? []
	const usbChanged =
		JSON.stringify(usbDevices.map(usbKey).sort()) !== JSON.stringify((machine.usbDevices ?? []).map(usbKey).sort())
	// PCI devices, shared folders and data disks are fixed while the machine runs
	const savedPciDevices = machine.pciDevices ?? []
	const savedSharedFolders = machine.sharedFolders ?? []
	const savedDataDisks = machine.dataDisks ?? []
	const pciDevices = pciChoice ?? savedPciDevices
	const sharedFolders = foldersChoice ?? savedSharedFolders
	const dataDisks = disksChoice ?? savedDataDisks
	const pciChanged =
		JSON.stringify(pciDevices.map((device) => device.address).sort()) !==
		JSON.stringify(savedPciDevices.map((device) => device.address).sort())
	const foldersChanged = JSON.stringify(sharedFolders) !== JSON.stringify(savedSharedFolders)
	const disksChanged = JSON.stringify(dataDisks) !== JSON.stringify(savedDataDisks)
	const devicesValid =
		sharedFolders.every(
			(folder) =>
				sharedFolderTagValid(folder.tag) && sharedFolders.filter((other) => other.tag === folder.tag).length === 1,
		) && dataDisks.every((disk) => dataDiskSizeValid(disk, savedDataDisks))
	const devicesEditable = machine.state === 'stopped'
	const supportsStorageDevices = !['legacy-x86', 'windows-7-x86', 'windows-98-x86'].includes(machine.platformProfile)
	// Every plugged-in device, plus assigned ones that are currently unplugged
	const usbRows = [
		...hostUsbDevices.map((usbDevice) => ({
			...usbDevice,
			present: true,
			assigned: usbDevices.some((assigned) => usbKey(assigned) === usbKey(usbDevice)),
		})),
		...usbDevices
			.filter((assigned) => !hostUsbDevices.some((usbDevice) => usbKey(usbDevice) === usbKey(assigned)))
			.map((usbDevice) => ({
				...usbDevice,
				kind: 'other' as const,
				machineId: machine.id,
				present: false,
				assigned: true,
			})),
	]
	const portForwards = forwardsChoice ?? machine.portForwards
	const network = networkChoice ?? machine.network
	const networkChanged = !sameMachineNetwork(machine.network, network)
	const networkEditable = canChangeMachineNetwork(machine)
	const networkValid =
		!networkChanged ||
		(networkEditable &&
			machineNetworkAvailable(network, networks.bridges) &&
			(network.mode !== 'bridge' || (!networks.isLoading && !networks.isError)))

	// Never below the machine's current allocation so its value stays selectable
	const maxCores = Math.max(1, threads || DEFAULT_CORES, machine.cores)
	const reservedBytes = hostReservedMemoryBytes(memory)
	const computedMaxMemoryGb = memory?.size ? Math.floor((memory.size - reservedBytes) / 1e9) : DEFAULT_MEMORY_GB
	const maxMemoryGb = Math.max(MIN_MEMORY_GB, machine.memoryGb, computedMaxMemoryGb)

	// Validation
	const nameEmpty = trimmedName === ''
	const nameTaken = machines.some(
		(other) => other.id !== machine.id && other.name.toLowerCase() === trimmedName.toLowerCase(),
	)
	const diskValue = Number(diskInput)
	const diskEmpty = diskInput.trim() === ''
	// Grow-only disk: min is the machine's current size (shrinking corrupts the guest FS)
	const diskShrink = !diskEmpty && Number.isFinite(diskValue) && diskValue < machine.diskSizeGb
	const diskValid =
		!diskEmpty && Number.isFinite(diskValue) && diskValue >= machine.diskSizeGb && diskValue <= MAX_DISK_SIZE_GB
	const forwardsValid = portForwards.every(
		(forward, index) =>
			forward.hostPort >= 40_000 &&
			forward.hostPort <= 49_999 &&
			forward.guestPort >= 1 &&
			forward.guestPort <= 65_535 &&
			portForwards.findIndex((other) => other.protocol === forward.protocol && other.hostPort === forward.hostPort) ===
				index,
	)

	// Settings can't change while the OS is being installed (backend rejects too)
	const isInstalling = machine.state === 'installing'
	const hasFixedMemory = machine.platformProfile === 'windows-98-x86'
	const isPowerTransition =
		machine.state === 'starting' || machine.state === 'stopping' || machine.state === 'restarting'
	const disabled = isSaving || isInstalling || isPowerTransition || networks.isPreparingBridge
	const shutdownRequiredAfterSave =
		machine.state === 'running' &&
		machineSettingsRequireShutdown(machine, {
			cores,
			memoryGb: hasFixedMemory ? machine.memoryGb : memoryGb,
			diskSizeGb: Math.round(diskValue),
			firmware: machine.osId === 'custom' ? firmware : machine.firmware,
			diskBus: machine.osId === 'custom' ? diskBus : machine.diskBus,
			videoModel: machine.osId === 'custom' ? videoModel : machine.videoModel,
			network,
		})

	// Save only lights up once something actually differs from the machine's
	// live values (untouched fields resolve to those, so this is a value diff)
	const dirty =
		trimmedName !== machine.name ||
		cores !== machine.cores ||
		memoryGb !== machine.memoryGb ||
		diskValue !== machine.diskSizeGb ||
		autostart !== machine.autostart ||
		autostartDelay !== savedAutostartDelay ||
		(machine.osId === 'custom' &&
			(firmware !== machine.firmware ||
				diskBus !== (machine.diskBus ?? 'virtio') ||
				videoModel !== (machine.videoModel ?? 'virtio'))) ||
		usbChanged ||
		pciChanged ||
		foldersChanged ||
		disksChanged ||
		JSON.stringify(portForwards) !== JSON.stringify(machine.portForwards) ||
		networkChanged

	const canSave =
		dirty && !disabled && !nameEmpty && !nameTaken && diskValid && forwardsValid && networkValid && devicesValid

	const handleDiskChange = (raw: string) => {
		const digits = raw.replace(/[^0-9]/g, '')
		if (digits === '') return setDiskChoice('')
		setDiskChoice(String(Math.min(Number(digits), MAX_DISK_SIZE_GB)))
	}
	const stepDisk = (direction: 1 | -1) => {
		const current = Number(diskInput) || 0
		const next = Math.min(MAX_DISK_SIZE_GB, Math.max(machine.diskSizeGb, current + direction * diskStepGb(current)))
		setDiskChoice(String(next))
	}
	const handlePortChange = (raw: string) => Number(raw.replace(/[^0-9]/g, '')) || 0

	const handleSave = async () => {
		if (!canSave) return
		setIsSaving(true)
		try {
			await updateSettings({
				id: machine.id,
				name: trimmedName,
				cores,
				...(!hasFixedMemory && {memoryGb}),
				diskSizeGb: Math.round(diskValue),
				autostart,
				autostartDelaySeconds: autostartDelay,
				...(machine.osId === 'custom' ? {firmware, diskBus, videoModel} : {}),
				...(usbChanged ? {usbDevices: usbDevices.map(toUsbDevice)} : {}),
				...(pciChanged ? {pciDevices} : {}),
				...(foldersChanged ? {sharedFolders} : {}),
				...(disksChanged ? {dataDisks} : {}),
				portForwards,
				...(networkChanged ? {network} : {}),
			})
			toast.success(t('machines.settings-saved'), {area: 'machines'})
			setIsSaving(false)
			if (shutdownRequiredAfterSave) {
				setShutdownDialogOpen(true)
				return
			}
			navigate(machinePath(machine.id))
		} catch {
			// Error toast is handled by the mutation
			setIsSaving(false)
		}
	}

	const handleDoItLater = () => {
		setShutdownDialogOpen(false)
		navigate(machinePath(machine.id))
	}

	const handleShutDownNow = () => {
		if (machine.state === 'running') stop({id: machine.id})
		setShutdownDialogOpen(false)
		navigate(MACHINES_PATH)
	}

	return (
		<motion.div
			initial={{opacity: 0}}
			animate={{opacity: 1}}
			transition={{duration: 0.2, ease: 'easeOut'}}
			className='flex flex-col gap-8 px-4 py-6 md:p-12'
		>
			<div className='flex flex-col gap-10 md:flex-row md:gap-10 lg:gap-14'>
				{/* Identity: the machine is the hero, its name editable in place */}
				<div className='flex flex-col items-center gap-4 pt-4 md:w-[200px] md:shrink-0 md:pt-6 lg:w-[280px]'>
					<div className='relative'>
						<div
							aria-hidden
							className='absolute inset-2 rounded-full opacity-50 blur-3xl'
							style={{backgroundColor: color}}
						/>
						<OsIcon osId={machine.osId} state={machine.state} className='relative size-32 lg:size-36' />
					</div>
					{editingName ? (
						<input
							autoFocus
							value={name}
							onChange={(e) => setNameInput(e.target.value)}
							onBlur={() => setEditingName(false)}
							onKeyDown={(e) => e.key === 'Enter' && setEditingName(false)}
							maxLength={100}
							disabled={disabled}
							aria-label={t('machines.vm-name')}
							className='w-full bg-transparent text-center text-24 font-semibold -tracking-2 text-white outline-none'
						/>
					) : (
						<button
							onClick={() => setEditingName(true)}
							disabled={disabled}
							title={t('machines.vm-name')}
							className='group flex max-w-full items-center gap-2 rounded-8 px-2 py-0.5 transition-colors hover:bg-white/5'
						>
							<span className='min-w-0 truncate text-24 font-semibold -tracking-2 text-white'>
								{trimmedName || t('machines.vm-name')}
							</span>
							<Pencil className='size-4 shrink-0 text-white/30 transition-colors group-hover:text-white/60' />
						</button>
					)}
					{nameTaken && (
						<span className='-mt-2 text-12 -tracking-2 text-destructive2-lightest'>{t('machines.name-taken')}</span>
					)}
					<span className='-mt-3 text-13 -tracking-2 text-white/40'>{machine.osVersion}</span>
					<span className='mt-2 rounded-full border border-white/10 bg-white/5 px-3 py-1.5 text-12 -tracking-2 text-white/50 tabular-nums'>
						{t('machines.cores-count', {count: cores})} · {t('machines.gb', {value: memoryGb})} ·{' '}
						{diskValid ? t('machines.gb', {value: diskValue}) : '—'}
					</span>
					{machine.diskPath && (
						<div className='mt-2 rounded-12 border border-amber-400/20 bg-amber-400/10 px-3 py-2.5 text-left text-12 leading-snug text-amber-100/80'>
							<p className='truncate font-medium'>{machine.diskPath}</p>
							<p className='mt-1 text-amber-100/60'>{t('machines.external-install-warning')}</p>
						</div>
					)}
				</div>

				{/* The spec sheet */}
				<div className='min-w-0 flex-1'>
					<div className='titan-divide-y'>
						<MachineNetworkField
							value={network}
							onChange={setNetworkChoice}
							bridges={networks.bridges}
							automaticBridge={networks.automaticBridge}
							isPreparingBridge={networks.isPreparingBridge}
							bridgeError={networks.bridgeError}
							onPrepareBridge={networks.prepareBridge}
							disabled={disabled || !networkEditable}
							note={
								!networkEditable
									? machine.installationState || machine.firstBootSetup
										? t('machines.network-install-note')
										: t('machines.network-stop-note')
									: undefined
							}
							isLoading={networks.isLoading}
							isError={networks.isError}
							onRefresh={() => void networks.refetch()}
						/>
						<SpecRow
							label={t('machines.configure-processor')}
							note={t('machines.configure-processor-note', {count: maxCores})}
						>
							<Stepper
								display={t('machines.cores-count', {count: cores})}
								onStep={(direction) => setCoresChoice(Math.min(maxCores, Math.max(1, cores + direction)))}
								canDecrement={cores > 1}
								canIncrement={cores < maxCores}
								decrementLabel={t('machines.decrease-value', {label: t('machines.configure-processor')})}
								incrementLabel={t('machines.increase-value', {label: t('machines.configure-processor')})}
								disabled={disabled}
							/>
						</SpecRow>
						<SpecRow
							label={t('machines.memory')}
							note={
								hasFixedMemory
									? t('machines.memory-fixed-for-os', {value: machine.memoryGb, os: machine.osName})
									: t('machines.configure-memory-note', {max: maxMemoryGb})
							}
						>
							<Stepper
								display={t('machines.gb', {value: memoryGb})}
								onStep={(direction) =>
									setMemoryChoice(Math.min(maxMemoryGb, Math.max(MIN_MEMORY_GB, memoryGb + direction)))
								}
								canDecrement={!hasFixedMemory && memoryGb > MIN_MEMORY_GB}
								canIncrement={!hasFixedMemory && memoryGb < maxMemoryGb}
								decrementLabel={t('machines.decrease-value', {label: t('machines.memory')})}
								incrementLabel={t('machines.increase-value', {label: t('machines.memory')})}
								disabled={disabled || hasFixedMemory}
							/>
						</SpecRow>
						<SpecRow label={t('machines.configure-storage')} note={t('machines.settings-storage-note')}>
							<div className='flex flex-col items-end gap-1.5'>
								<Stepper
									middle={
										<div className='relative w-24'>
											<Input
												type='text'
												inputMode='numeric'
												value={diskInput}
												onValueChange={handleDiskChange}
												disabled={disabled}
												sizeVariant='short'
												aria-label={t('machines.disk-size')}
												className='pr-9 text-right text-white tabular-nums'
											/>
											<span className='pointer-events-none absolute top-1/2 right-3.5 -translate-y-1/2 text-13 text-white'>
												GB
											</span>
										</div>
									}
									onStep={stepDisk}
									canDecrement={(Number(diskInput) || 0) > machine.diskSizeGb}
									canIncrement={(Number(diskInput) || 0) < MAX_DISK_SIZE_GB}
									decrementLabel={t('machines.decrease-value', {label: t('machines.configure-storage')})}
									incrementLabel={t('machines.increase-value', {label: t('machines.configure-storage')})}
									disabled={disabled}
								/>
								{diskShrink && (
									<span className='text-12 -tracking-2 text-destructive2-lightest'>
										{t('machines-error.machine-disk-shrink-not-allowed')}
									</span>
								)}
							</div>
						</SpecRow>
						<SpecRow label={t('machines.autostart')} note={t('machines.autostart-description')}>
							<Switch
								checked={autostart}
								disabled={disabled}
								onCheckedChange={setAutostartChoice}
								aria-label={t('machines.autostart')}
							/>
						</SpecRow>
						{autostart && (
							<SpecRow label={t('machines.autostart-delay')} note={t('machines.autostart-delay-description')}>
								<Stepper
									display={autostartDelayLabel(autostartDelay)}
									onStep={(direction) => setAutostartDelayChoice(autostartDelayStep(direction) ?? autostartDelay)}
									canDecrement={autostartDelayStep(-1) !== undefined}
									canIncrement={autostartDelayStep(1) !== undefined}
									decrementLabel={t('machines.decrease-value', {label: t('machines.autostart-delay')})}
									incrementLabel={t('machines.increase-value', {label: t('machines.autostart-delay')})}
									disabled={disabled}
								/>
							</SpecRow>
						)}
						{machine.osId === 'custom' && (
							<>
								<SpecRow label={t('machines.firmware')}>
									<div className='flex gap-2'>
										{(['uefi', 'bios'] as const).map((option) => (
											<button
												key={option}
												type='button'
												disabled={disabled || (machine.arch === 'arm64' && option === 'bios')}
												onClick={() => setFirmwareChoice(option)}
												className={segmentButtonClass(firmware === option)}
											>
												{option}
											</button>
										))}
									</div>
								</SpecRow>
								<SpecRow label={t('machines.disk-compatibility')} note={t('machines.disk-compatibility-description')}>
									<div className='flex gap-2'>
										{(['virtio', 'sata'] as const).map((option) => (
											<button
												key={option}
												type='button'
												disabled={disabled || (machine.arch === 'arm64' && option === 'sata')}
												onClick={() => setDiskBusChoice(option)}
												className={segmentButtonClass(diskBus === option)}
											>
												{option}
											</button>
										))}
									</div>
								</SpecRow>
								<SpecRow label={t('machines.display-adapter')} note={t('machines.display-adapter-description')}>
									<div className='flex gap-2'>
										{(['virtio', 'vga'] as const).map((option) => (
											<button
												key={option}
												type='button'
												disabled={disabled}
												onClick={() => setVideoChoice(option)}
												className={segmentButtonClass(videoModel === option)}
											>
												{option}
											</button>
										))}
									</div>
								</SpecRow>
							</>
						)}

						{/* USB passthrough: host devices follow the switch immediately, even
						    while the machine runs. Assigned devices that are unplugged stay
						    listed so they can be released. */}
						<div className='flex flex-col gap-3 py-5'>
							<div className='flex flex-col gap-1'>
								<span className='text-15 font-medium -tracking-2 text-white'>{t('machines.usb-devices')}</span>
								<p className='max-w-[460px] text-12 leading-snug -tracking-2 text-white/40'>
									{t('machines.usb-devices-description')}
								</p>
							</div>
							{usbRows.length === 0 ? (
								<p className='rounded-8 bg-white/4 px-3 py-2.5 text-12 leading-snug -tracking-2 text-white/35'>
									{t('machines.usb-devices-empty')}
								</p>
							) : (
								<div className='flex flex-col gap-2'>
									{usbRows.map((row) => {
										const otherMachine = row.machineId && row.machineId !== machine.id ? row.machineId : undefined
										const status = otherMachine
											? t('machines.usb-device-in-use', {
													machineName: machines.find((other) => other.id === otherMachine)?.name ?? otherMachine,
												})
											: row.present
												? undefined
												: t('machines.usb-device-unplugged')
										return (
											<label
												key={usbKey(row)}
												className='flex items-center justify-between gap-4 rounded-12 border-hpx border-white/10 bg-white/6 px-3.5 py-2.5'
											>
												<span className='flex min-w-0 flex-col'>
													<span className='truncate text-13 -tracking-2 text-white'>{row.name}</span>
													<span className='truncate text-11 -tracking-1 text-white/35 tabular-nums'>
														{row.kind !== 'other' ? `${usbKindLabel(row.kind)} · ` : ''}
														{row.vendorId}:{row.productId}
														{status ? ` · ${status}` : ''}
													</span>
												</span>
												<Switch
													checked={row.assigned}
													disabled={disabled || !!otherMachine || (!row.assigned && usbDevices.length >= MAX_USB_DEVICES)}
													onCheckedChange={(checked) =>
														setUsbChoice(
															checked
																? [...usbDevices, toUsbDevice(row)]
																: usbDevices.filter((usbDevice) => usbKey(usbDevice) !== usbKey(row)),
														)
													}
													aria-label={row.name}
												/>
											</label>
										)
									})}
								</div>
							)}
						</div>

						{!devicesEditable && (
							<p className='pt-5 text-12 leading-snug -tracking-2 text-white/40'>{t('machines.devices-stop-note')}</p>
						)}
						{supportsStorageDevices && (
							<>
								<SharedFoldersSection
									value={sharedFolders}
									onChange={setFoldersChoice}
									disabled={disabled || !devicesEditable}
								/>
								<DataDisksSection
									value={dataDisks}
									saved={savedDataDisks}
									onChange={setDisksChoice}
									disabled={disabled || !devicesEditable}
								/>
							</>
						)}
						<PciDevicesSection
							machine={machine}
							machines={machines}
							value={pciDevices}
							onChange={setPciChoice}
							disabled={disabled || !devicesEditable}
						/>
						<SnapshotsSection machine={machine} disabled={disabled || machine.installPending} />

						{/* Port forwards: the last row of the spec sheet, full-width. The
						    copy leads with the why (machines live on a private network),
						    each rule reads in the user's direction (port inside the machine
						    → the address you actually type), and the network side shows the
						    real reachable address using the hostname this page is open on. */}
						{network.mode === 'nat' ? (
							<div className='flex flex-col gap-3 py-5'>
								<div className='flex items-start justify-between gap-4'>
									<div className='flex flex-col gap-1'>
										<span className='text-15 font-medium -tracking-2 text-white'>{t('machines.port-forwards')}</span>
										<p className='max-w-[460px] text-12 leading-snug -tracking-2 text-white/40'>
											{t('machines.port-forwards-description')}
										</p>
									</div>
									<Button
										size='sm'
										className='shrink-0'
										onClick={() => {
											const used = new Set(portForwards.map((forward) => forward.hostPort))
											let hostPort = 40_000
											while (used.has(hostPort) && hostPort <= 49_999) hostPort++
											setForwardsChoice([
												...portForwards,
												{id: createBrowserUuid(), protocol: 'tcp', hostPort, guestPort: 22},
											])
										}}
										disabled={disabled || portForwards.length >= 32}
									>
										{t('machines.add-forward')}
										<PlusCircle className='h-3 w-3' />
									</Button>
								</div>
								{portForwards.length === 0 ? (
									<p className='rounded-8 bg-white/4 px-3 py-2.5 text-12 leading-snug -tracking-2 text-white/35'>
										{t('machines.port-forwards-empty', {address: `${window.location.hostname}:40000`})}
									</p>
								) : (
									<div className='flex flex-col gap-2'>
										{/* Column labels, once for the whole list. The protocol segment
									    has a fixed width so the first two line up exactly; the
									    Titan label splits the remaining space with Machine's. */}
										<div className='flex items-center text-[10px] -tracking-1 text-white/30'>
											<span className='w-[58px] shrink-0 pl-3.5'>{t('machines.port-forward-protocol')}</span>
											{/* The machine column is effectively constant (addresses are
										    always 10.203.0.x); narrower on mobile where it's hidden */}
											<span className='w-[110px] shrink-0 pl-3 sm:w-[164px]'>
												{t('machines.port-forward-machine-port')}
											</span>
											<span className='min-w-0 flex-1'>{t('machines.port-forward-titan-port')}</span>
										</div>
										{portForwards.map((forward) => (
											<div key={forward.id} className='flex items-center gap-2'>
												{/* One pill = one routing rule: protocol, the machine's own
										    address with its port editable, then the Titan address
										    people will actually type, with its port editable */}
												<div className='flex h-10 min-w-0 flex-1 items-center rounded-full border-hpx border-white/10 bg-white/6 transition-colors focus-within:border-white/25'>
													<DropdownMenu>
														<DropdownMenuTrigger asChild>
															<button
																type='button'
																disabled={disabled}
																className='flex h-full w-[58px] shrink-0 items-center gap-1 rounded-l-full border-r border-white/6 pl-3.5 text-11 font-semibold text-white/70 uppercase outline-hidden transition-colors hover:bg-white/5 hover:text-white focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset disabled:cursor-not-allowed disabled:opacity-35 disabled:hover:bg-transparent'
															>
																{forward.protocol}
																<ChevronDown className='size-3 opacity-50' />
															</button>
														</DropdownMenuTrigger>
														{/* p-1 matches the context menu's density (dropdowns default
												    to a roomier p-2.5) */}
														<DropdownMenuContent align='start' className='min-w-24 p-1'>
															{(['tcp', 'udp'] as const).map((protocol) => (
																<DropdownMenuItem
																	key={protocol}
																	disabled={disabled}
																	onSelect={() =>
																		setForwardsChoice(
																			portForwards.map((item) => (item.id === forward.id ? {...item, protocol} : item)),
																		)
																	}
																>
																	{protocol.toUpperCase()}
																</DropdownMenuItem>
															))}
														</DropdownMenuContent>
													</DropdownMenu>
													<label className='flex min-w-0 shrink-0 cursor-text items-center gap-1.5 pl-3'>
														<OsIcon osId={machine.osId} state={machine.state} className='size-5 shrink-0' />
														<span className='truncate text-13 -tracking-2 text-white/40 tabular-nums max-sm:hidden'>
															{machine.ipAddress}:
														</span>
														<input
															type='text'
															inputMode='numeric'
															value={forward.guestPort}
															disabled={disabled}
															onChange={(e) =>
																setForwardsChoice(
																	portForwards.map((item) =>
																		item.id === forward.id
																			? {...item, guestPort: handlePortChange(e.target.value)}
																			: item,
																	),
																)
															}
															aria-label={t('machines.guest-port')}
															// field-sizing keeps the input hugging its digits so the
															// arrow sits close; the fixed width is the fallback
															className='field-sizing-content w-11 min-w-6 bg-transparent pr-1.5 pl-0.5 text-13 -tracking-2 text-white tabular-nums outline-none disabled:cursor-not-allowed disabled:text-white/35 supports-[field-sizing:content]:w-auto'
														/>
													</label>
													<MoveRight className='size-4 shrink-0 text-white/30' strokeWidth={1.5} />
													<label className='flex min-w-0 flex-1 cursor-text items-center gap-1.5 pl-2'>
														<img
															src='/favicon/favicon-32x32.png'
															alt=''
															draggable={false}
															className='size-4 shrink-0 rounded-[4px]'
														/>
														<span className='truncate text-13 -tracking-2 text-white/40 tabular-nums max-sm:hidden'>
															{window.location.hostname}:
														</span>
														<input
															type='text'
															inputMode='numeric'
															value={forward.hostPort}
															disabled={disabled}
															onChange={(e) =>
																setForwardsChoice(
																	portForwards.map((item) =>
																		item.id === forward.id
																			? {...item, hostPort: handlePortChange(e.target.value)}
																			: item,
																	),
																)
															}
															aria-label={t('machines.host-port')}
															className='w-full min-w-0 flex-1 bg-transparent pl-0.5 text-13 -tracking-2 text-white tabular-nums outline-none disabled:cursor-not-allowed disabled:text-white/35'
														/>
													</label>
													<div className='flex shrink-0 items-center pr-2.5 text-14'>
														<CopyButton value={`${window.location.hostname}:${forward.hostPort}`} />
													</div>
												</div>
												<button
													type='button'
													disabled={disabled}
													className='grid size-8 shrink-0 place-items-center rounded-full text-white/35 hover:bg-white/10 hover:text-white disabled:cursor-not-allowed disabled:opacity-35 disabled:hover:bg-transparent disabled:hover:text-white/35'
													onClick={() => setForwardsChoice(portForwards.filter((item) => item.id !== forward.id))}
													aria-label={t('remove')}
												>
													<Trash2 className='size-3.5' />
												</button>
											</div>
										))}
									</div>
								)}
								{!forwardsValid && <p className='text-12 text-destructive2-lightest'>{t('machines.port-invalid')}</p>}
								{machine.osId.startsWith('windows') && (
									<p className='text-11 leading-relaxed -tracking-2 text-white/30'>
										{t('machines.windows-rdp-forward')}
									</p>
								)}
							</div>
						) : (
							<div className='flex flex-col gap-2 py-5'>
								<p className='text-12 leading-snug text-white/40'>{t('machines.network-forwards-inactive')}</p>
								{!forwardsValid && (
									<>
										<p role='status' className='text-12 text-destructive2-lightest'>
											{t('machines.port-invalid')}
										</p>
										<Button size='sm' disabled={disabled} onClick={() => setForwardsChoice(null)}>
											{t('machines.network-discard-forward-edits')}
										</Button>
									</>
								)}
							</div>
						)}
					</div>
				</div>
			</div>

			{/* Footer: context on the left, commitment on the right */}
			<div className='flex flex-col items-stretch justify-between gap-4 border-t border-white/6 pt-6 sm:flex-row sm:items-center'>
				<span className='text-13 -tracking-2 text-white/35'>
					{isInstalling ? t('machines.settings-unavailable-installing') : ''}
				</span>
				<div className='flex shrink-0 flex-col-reverse gap-2.5 sm:flex-row sm:items-center'>
					<Button size='dialog' onClick={() => navigate(machinePath(machine.id))} disabled={isSaving || networks.isPreparingBridge}>
						{t('cancel')}
					</Button>
					<Button variant='primary' size='dialog' onClick={handleSave} disabled={!canSave}>
						{t('machines.settings-save')}
					</Button>
				</div>
			</div>
			<AlertDialog
				open={shutdownDialogOpen}
				onOpenChange={(open) => {
					if (open) setShutdownDialogOpen(true)
					else handleDoItLater()
				}}
			>
				<AlertDialogContent>
					<AlertDialogHeader icon={Power}>
						<AlertDialogTitle>{t('machines.settings-shutdown-required-title')}</AlertDialogTitle>
						<AlertDialogDescription>
							{t('machines.settings-shutdown-required-description', {machineName: machine.name})}
						</AlertDialogDescription>
					</AlertDialogHeader>
					<AlertDialogFooter>
						<AlertDialogAction variant='primary' onClick={handleShutDownNow}>
							{t('machines.settings-shut-down-now')}
						</AlertDialogAction>
						<AlertDialogCancel onClick={handleDoItLater}>{t('machines.settings-do-it-later')}</AlertDialogCancel>
					</AlertDialogFooter>
				</AlertDialogContent>
			</AlertDialog>
		</motion.div>
	)
}
