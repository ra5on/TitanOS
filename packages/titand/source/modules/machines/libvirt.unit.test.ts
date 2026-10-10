import {randomUUID} from 'node:crypto'
import fsp from 'node:fs/promises'
import os from 'node:os'
import nodePath from 'node:path'

import fse from 'fs-extra'
import {afterEach, describe, expect, test, vi} from 'vitest'

import type Titand from '../../index.js'
import type {MachineDefinition} from './domain.js'
import {MACHINE_INSTALL_COMMAND_TIMEOUT_MS, MACHINE_INSTALL_SHORT_COMMAND_TIMEOUT_MS} from './install-command.js'
import Libvirt, {
	audioDevicesForSlot,
	buildWindows98Mbr,
	createWithGraphicsFallback,
	findGpuRenderNode,
	findMachineBridges,
	parseMachineInterfaceAddress,
	isMissingDomainError,
	parseQemuImgProgress,
	parseDomainResourceStats,
	MACHINE_SHORT_CONTROL_TIMEOUT_MS,
	qemuImageConvertArguments,
	qemuImageFormatForPath,
	qemuImageInfoArguments,
	supportsVirglGraphics,
} from './libvirt.js'
import {buildMachineNetworkXml} from './machine-network.js'

const execaMock = vi.hoisted(() =>
	vi.fn(async (_command?: string, _arguments?: string[], _options?: Record<string, unknown>) => ({
		stdout: '',
		stderr: '',
		exitCode: 0,
	})),
)

vi.mock('execa', () => ({execa: execaMock}))

const temporaryDirectories: string[] = []

afterEach(async () => {
	execaMock.mockReset()
	execaMock.mockResolvedValue({stdout: '', stderr: '', exitCode: 0})
	await Promise.all(temporaryDirectories.splice(0).map((directory) => fse.remove(directory)))
})

function definition(overrides: Partial<MachineDefinition> = {}): MachineDefinition {
	return {
		version: 1,
		id: randomUUID(),
		name: 'Test machine',
		osId: 'ubuntu',
		osName: 'Ubuntu Desktop',
		osVersion: 'Ubuntu 26.04 LTS',
		osVariant: 'Desktop',
		arch: 'amd64',
		platformProfile: 'modern-x86',
		machineType: 'pc-q35-9.2',
		firmware: 'uefi',
		uuid: randomUUID(),
		macAddress: '02:00:00:00:00:01',
		diskSizeGb: 20,
		cores: 2,
		memoryMb: 4_096,
		autostart: false,
		pinned: false,
		createdAt: 1,
		portForwards: [],
		...overrides,
	}
}

describe('selectable machine networks', () => {
	test('discovers physical LAN bridges including VLAN/bond uplinks and excludes isolated bridges', async () => {
		const root = await fsp.mkdtemp(nodePath.join(os.tmpdir(), 'titan-bridges-'))
		temporaryDirectories.push(root)
		for (const path of [
			'eth0/device',
			'bond0/lower_eth0',
			'br0/bridge',
			'br0/brif/bond0',
			'docker0/bridge',
			'docker0/brif/veth0',
			'veth0',
			'virbr0/bridge',
			'virbr0/brif',
			'eth1',
		])
			await fse.ensureDir(nodePath.join(root, path))
		await expect(findMachineBridges(root)).resolves.toEqual(['br0'])
	})

	test('reads only the guest MAC-matched IPv4 address from host ARP observations', () => {
		const output = 'vnet0 02:00:00:00:00:09 ipv4 192.168.1.20/24\nvnet1 02:00:00:00:00:01 ipv4 192.168.1.42/24\n'
		expect(parseMachineInterfaceAddress(output, '02:00:00:00:00:01')).toBe('192.168.1.42')
		expect(parseMachineInterfaceAddress(output, '02:00:00:00:00:02')).toBeUndefined()
		expect(
			parseMachineInterfaceAddress('vnet1 02:00:00:00:00:01 ipv4 169.254.1.2/16', '02:00:00:00:00:01'),
		).toBeUndefined()
		expect(
			parseMachineInterfaceAddress(
				'vnet1 02:00:00:00:00:01 ipv4 10.203.0.2/24\nvnet1 02:00:00:00:00:01 ipv4 192.168.1.42/24',
				'02:00:00:00:00:01',
				['192.168.1.42'],
			),
		).toBe('192.168.1.42')
	})

	test('corroborates bridge IP against that bridge neighbours and memoizes optional observations', async () => {
		const libvirt = new Libvirt({
			logger: {createChildLogger: () => ({log: vi.fn(), error: vi.fn()})},
		} as unknown as Titand)
		const machine = definition({network: {mode: 'bridge', bridge: 'br0'}})
		execaMock.mockResolvedValueOnce({
			stdout: 'vnet0 02:00:00:00:00:01 ipv4 10.203.0.2/24\nvnet0 02:00:00:00:00:01 ipv4 192.168.1.42/24',
			stderr: '',
			exitCode: 0,
		})
		execaMock.mockResolvedValueOnce({
			stdout: JSON.stringify([{dst: '192.168.1.42', lladdr: machine.macAddress, state: ['REACHABLE']}]),
			stderr: '',
			exitCode: 0,
		})
		await expect(libvirt.interfaceAddress(machine)).resolves.toBe('192.168.1.42')
		await expect(libvirt.interfaceAddress(machine)).resolves.toBe('192.168.1.42')
		expect(execaMock).toHaveBeenCalledTimes(2)
		expect(execaMock.mock.calls[0][1]).toContain('--full')
		expect(execaMock.mock.calls[1][1]).toEqual(['-json', '-4', 'neigh', 'show', 'dev', 'br0'])
	})

	test('returns unknown for failed optional ARP discovery without breaking list callers', async () => {
		const libvirt = new Libvirt({
			logger: {createChildLogger: () => ({log: vi.fn(), error: vi.fn()})},
		} as unknown as Titand)
		execaMock.mockRejectedValueOnce(new Error('timed out'))
		const machine = definition({network: {mode: 'bridge', bridge: 'br0'}})
		await expect(libvirt.interfaceAddress(machine)).resolves.toBeUndefined()
		await expect(libvirt.interfaceAddress(machine)).resolves.toBeUndefined()
		expect(execaMock).toHaveBeenCalledOnce()
	})

	test('removes the owned Host-only network when its last guest is gone, including rollback', async () => {
		const root = await fsp.mkdtemp(nodePath.join(os.tmpdir(), 'titan-network-runtime-'))
		temporaryDirectories.push(root)
		const previous = process.env.TITAN_MACHINES_RUNTIME_DIR
		process.env.TITAN_MACHINES_RUNTIME_DIR = root
		try {
			const libvirt = new Libvirt({
				logger: {createChildLogger: () => ({log: vi.fn(), error: vi.fn()})},
			} as unknown as Titand)
			execaMock.mockImplementation(async (_command, args) => ({
				stdout: args?.includes('net-dumpxml')
					? buildMachineNetworkXml([], ['1.1.1.1'], args.includes('titan-machines-host-only') ? 'host-only' : 'nat')
					: '',
				stderr: '',
				exitCode: 0,
			}))
			await fsp.writeFile(nodePath.join(root, 'host-only-network.xml'), 'owned temporary config')
			await libvirt.reconcileNetwork([])
			expect(
				execaMock.mock.calls.some(
					([command, args]) =>
						command === 'virsh' && args?.includes('net-destroy') && args?.includes('titan-machines-host-only'),
				),
			).toBe(true)
			await expect(fse.pathExists(nodePath.join(root, 'host-only-network.xml'))).resolves.toBe(false)
		} finally {
			if (previous === undefined) delete process.env.TITAN_MACHINES_RUNTIME_DIR
			else process.env.TITAN_MACHINES_RUNTIME_DIR = previous
		}
	})

	test('rejects a mode switch when an existing Host-only network cannot be removed', async () => {
		const root = await fsp.mkdtemp(nodePath.join(os.tmpdir(), 'titan-network-remove-'))
		temporaryDirectories.push(root)
		const previous = process.env.TITAN_MACHINES_RUNTIME_DIR
		process.env.TITAN_MACHINES_RUNTIME_DIR = root
		try {
			const libvirt = new Libvirt({
				logger: {createChildLogger: () => ({log: vi.fn(), error: vi.fn()})},
			} as unknown as Titand)
			execaMock.mockImplementation(async (_command, args) => {
				if (args?.includes('net-destroy')) throw new Error('could not destroy network')
				return {
					stdout: args?.includes('net-dumpxml')
						? buildMachineNetworkXml([], ['1.1.1.1'], args.includes('titan-machines-host-only') ? 'host-only' : 'nat')
						: '',
					stderr: '',
					exitCode: 0,
				}
			})
			await expect(libvirt.reconcileNetwork([])).rejects.toThrow('could not destroy network')
		} finally {
			if (previous === undefined) delete process.env.TITAN_MACHINES_RUNTIME_DIR
			else process.env.TITAN_MACHINES_RUNTIME_DIR = previous
		}
	})

	test('rejects a Host-only subnet already routed through LAN before any network mutation', async () => {
		const libvirt = new Libvirt({
			logger: {createChildLogger: () => ({log: vi.fn(), error: vi.fn()})},
		} as unknown as Titand)
		execaMock.mockResolvedValueOnce({
			stdout: JSON.stringify([{dst: '10.204.0.0/24', dev: 'eth0'}]),
			stderr: '',
			exitCode: 0,
		})
		await expect(libvirt.validateNetwork({mode: 'host-only'})).rejects.toThrow('[machine-host-only-subnet-conflict]')
		expect(execaMock.mock.calls.map(([command]) => command)).toEqual(['ip'])
	})
})

describe('virgl graphics', () => {
	test('selects the first character render device and ignores ordinary files', async () => {
		const root = await fsp.mkdtemp(nodePath.join(os.tmpdir(), 'titan-render-nodes-'))
		temporaryDirectories.push(root)
		await fsp.writeFile(nodePath.join(root, 'renderD128'), 'not a device')
		await fsp.symlink('/dev/null', nodePath.join(root, 'renderD129'))

		await expect(findGpuRenderNode(root)).resolves.toBe(nodePath.join(root, 'renderD129'))
	})

	test('returns no render node when the host has no GPU device', async () => {
		await expect(findGpuRenderNode('/path/that/does/not/exist')).resolves.toBeUndefined()
	})

	test('only enables virgl for built-in modern Linux desktops', () => {
		expect(supportsVirglGraphics(definition())).toBe(true)
		expect(supportsVirglGraphics(definition({osId: 'omarchy'}))).toBe(true)
		expect(supportsVirglGraphics(definition({osId: 'omarchy', osVariant: undefined}))).toBe(true)
		expect(supportsVirglGraphics(definition({arch: 'arm64', platformProfile: 'modern-arm64'}))).toBe(true)
		expect(supportsVirglGraphics(definition({osId: 'android', osVariant: undefined}))).toBe(true)
		expect(supportsVirglGraphics(definition({osVariant: 'Server'}))).toBe(false)
		expect(supportsVirglGraphics(definition({osId: 'custom'}))).toBe(false)
		expect(supportsVirglGraphics(definition({osId: 'windows-11'}))).toBe(false)
		expect(supportsVirglGraphics(definition({platformProfile: 'legacy-x86'}))).toBe(false)
	})

	test('starts directly with software graphics when no render node exists', async () => {
		const create = vi.fn(async () => {})
		const onFallback = vi.fn()

		await createWithGraphicsFallback(undefined, create, onFallback)

		expect(create).toHaveBeenCalledOnce()
		expect(create).toHaveBeenCalledWith()
		expect(onFallback).not.toHaveBeenCalled()
	})

	test('retries with software graphics when accelerated startup fails', async () => {
		const error = new Error('EGL initialization failed')
		const create = vi.fn(async (renderNode?: string) => {
			if (renderNode) throw error
		})
		const onFallback = vi.fn()

		await createWithGraphicsFallback('/dev/dri/renderD128', create, onFallback)

		expect(create.mock.calls).toEqual([['/dev/dri/renderD128'], []])
		expect(onFallback).toHaveBeenCalledWith(error)
	})
})

describe('machine audio slots', () => {
	test('maps each slot to an independent snd-aloop playback/capture pair', () => {
		expect(audioDevicesForSlot(0)).toEqual({
			cardIndex: 8,
			playback: 'hw:8,0,0',
			capture: 'hw:8,1,0',
			playbackStatus: '/proc/asound/card8/pcm0p/sub0/status',
		})
		expect(audioDevicesForSlot(12)).toEqual({
			cardIndex: 9,
			playback: 'hw:9,0,4',
			capture: 'hw:9,1,4',
			playbackStatus: '/proc/asound/card9/pcm0p/sub4/status',
		})
		expect(audioDevicesForSlot(63)).toEqual({
			cardIndex: 15,
			playback: 'hw:15,0,7',
			capture: 'hw:15,1,7',
			playbackStatus: '/proc/asound/card15/pcm0p/sub7/status',
		})
		expect(() => audioDevicesForSlot(64)).toThrow('[machine-audio-slot-invalid]')
	})
})

describe('qemu-img conversion progress', () => {
	test('parses the newest streamed percentage', () => {
		expect(parseQemuImgProgress('    (12.34/100%)\r    (67.89/100%)\r')).toBe(67.89)
		expect(parseQemuImgProgress('no progress here')).toBeUndefined()
	})
})

describe('qemu-img input format', () => {
	test.each([
		['disk.qcow2', 'qcow2'],
		['disk.img', 'raw'],
		['disk.vmdk', 'vmdk'],
		['disk.vdi', 'vdi'],
		['disk.vhdx', 'vhdx'],
		['disk.vhd', 'vpc'],
	])('maps %s to an explicit %s driver', (path, expected) => {
		expect(qemuImageFormatForPath(path)).toBe(expected)
	})

	test('rejects formats that would require qemu-img probing', () => {
		expect(() => qemuImageFormatForPath('disk.unknown')).toThrow('[machine-image-format-unsupported]')
	})

	test('passes the selected input driver to both inspection and conversion', () => {
		expect(qemuImageInfoArguments('/input/disk.vmdk', 'vmdk')).toEqual([
			'info',
			'-f',
			'vmdk',
			'--output=json',
			'/input/disk.vmdk',
		])
		expect(qemuImageConvertArguments('/input/disk.vmdk', '/output/disk.qcow2', 'vmdk')).toEqual([
			'convert',
			'-p',
			'-f',
			'vmdk',
			'-O',
			'qcow2',
			'/input/disk.vmdk',
			'/output/disk.qcow2',
		])
	})

	test('cancels and bounds inspection, conversion, and resize commands', async () => {
		const controller = new AbortController()
		const libvirt = new Libvirt({
			logger: {createChildLogger: () => ({log: vi.fn(), error: vi.fn()})},
		} as unknown as Titand)
		execaMock.mockResolvedValueOnce({stdout: JSON.stringify({'virtual-size': 1}), stderr: '', exitCode: 0})

		await libvirt.convertDisk('/input/disk.img', '/output/disk.qcow2', 1, 'raw', controller.signal)

		const calls = execaMock.mock.calls as unknown as Array<[string, string[], Record<string, unknown>]>
		expect(calls.map(([, , options]) => options)).toEqual([
			{signal: controller.signal, timeout: MACHINE_INSTALL_SHORT_COMMAND_TIMEOUT_MS, cleanup: true},
			{signal: controller.signal, timeout: MACHINE_INSTALL_COMMAND_TIMEOUT_MS, cleanup: true},
			{signal: controller.signal, timeout: MACHINE_INSTALL_SHORT_COMMAND_TIMEOUT_MS, cleanup: true},
		])
	})
})

describe('libvirt machine resource stats', () => {
	test('parses CPU time and host RSS for every running Titan machine', () => {
		const stats = parseDomainResourceStats(`Domain: 'titan-machine-11111111-1111-4111-8111-111111111111'
  cpu.time=1500000000
  balloon.current=2097152
  balloon.rss=707476

Domain: 'unrelated-domain'
  cpu.time=999
  balloon.rss=999

Domain: 'titan-machine-22222222-2222-4222-8222-222222222222'
  cpu.time=2500000000
  balloon.rss=104164
`)

		expect(stats).toEqual([
			{
				id: '11111111-1111-4111-8111-111111111111',
				cpuTimeNs: 1_500_000_000,
				memoryBytes: 707_476 * 1024,
			},
			{
				id: '22222222-2222-4222-8222-222222222222',
				cpuTimeNs: 2_500_000_000,
				memoryBytes: 104_164 * 1024,
			},
		])
	})
})

describe('libvirt domain state', () => {
	test.each([
		['running', 'running'],
		['idle', 'running'],
		['blocked', 'running'],
		['paused', 'paused'],
		['pmsuspended', 'suspended'],
		['shut off', 'stopped'],
		['in shutdown', 'stopped'],
		['crashed', 'stopped'],
	] as const)('maps %s to %s', async (reported, expected) => {
		const libvirt = new Libvirt({
			logger: {createChildLogger: () => ({log: vi.fn(), error: vi.fn()})},
		} as unknown as Titand)
		execaMock.mockResolvedValueOnce({stdout: reported, stderr: '', exitCode: 0})

		await expect(libvirt.state('machine')).resolves.toBe(expected)
	})

	test('fails closed for an unrecognized successful response', async () => {
		const libvirt = new Libvirt({
			logger: {createChildLogger: () => ({log: vi.fn(), error: vi.fn()})},
		} as unknown as Titand)
		execaMock.mockResolvedValueOnce({stdout: 'no state', stderr: '', exitCode: 0})

		await expect(libvirt.state('machine')).rejects.toThrow('[machine-state-unavailable] Unexpected state: no state')
	})

	test('only treats an explicitly missing domain as stopped', async () => {
		const libvirt = new Libvirt({
			logger: {createChildLogger: () => ({log: vi.fn(), error: vi.fn()})},
		} as unknown as Titand)

		execaMock.mockResolvedValueOnce({
			stdout: '',
			stderr: "error: Domain not found: no domain with matching name 'vm'",
			exitCode: 1,
		})
		await expect(libvirt.state('missing')).resolves.toBe('stopped')
		execaMock.mockResolvedValueOnce({stdout: '', stderr: 'error: failed to connect to the hypervisor', exitCode: 1})
		await expect(libvirt.state('unknown')).rejects.toThrow('[machine-state-unavailable]')
	})

	test('force-stops a power-management-suspended domain with destroy', async () => {
		const libvirt = new Libvirt({
			logger: {createChildLogger: () => ({log: vi.fn(), error: vi.fn()})},
		} as unknown as Titand)
		let destroyed = false
		execaMock.mockImplementation(async (command?: string, args?: string[]) => {
			if (command === 'virsh' && args?.includes('domstate')) {
				return {stdout: destroyed ? 'shut off' : 'pmsuspended', stderr: '', exitCode: 0}
			}
			if (command === 'virsh' && args?.includes('destroy')) destroyed = true
			return {stdout: '', stderr: '', exitCode: 0}
		})

		await expect(libvirt.stop('sleeping', {force: true})).resolves.toBeUndefined()
		expect(execaMock).toHaveBeenCalledWith(
			'virsh',
			['--connect', 'qemu:///system', 'destroy', 'titan-machine-sleeping'],
			{reject: false, timeout: 10_000},
		)
	})

	test('recognizes only libvirt missing-domain diagnostics', () => {
		expect(isMissingDomainError('Domain not found: no domain with matching name')).toBe(true)
		expect(isMissingDomainError("error: failed to get domain 'titan-machine-missing'")).toBe(true)
		expect(isMissingDomainError('failed to connect to the hypervisor')).toBe(false)
		expect(isMissingDomainError("error: failed to get domain 'titan-machine-missing': monitor unavailable")).toBe(false)
	})
})

describe('installation media eject', () => {
	test('ejects both Omarchy CDs after setup', async () => {
		const libvirt = new Libvirt({
			logger: {createChildLogger: () => ({log: vi.fn(), error: vi.fn()})},
		} as unknown as Titand)
		const machine = definition({osId: 'omarchy', installMedia: 'media/install.iso', seedMedia: 'media/seed.iso'})
		execaMock.mockResolvedValueOnce({stdout: 'running', stderr: '', exitCode: 0})
		await libvirt.ejectInstallMedia(machine)
		const targets = execaMock.mock.calls
			.filter(([, args]) => args?.includes('change-media'))
			.map(([, args]) => args?.[4])
		expect(targets).toEqual(['sda', 'sdb'])
	})

	test('detaches a SATA installer from its actual live target', async () => {
		const libvirt = new Libvirt({
			logger: {createChildLogger: () => ({log: vi.fn(), error: vi.fn()})},
		} as unknown as Titand)
		const machine = definition({diskBus: 'sata', installMedia: 'media/install.iso'})
		execaMock
			.mockResolvedValueOnce({stdout: 'running', stderr: '', exitCode: 0})
			.mockResolvedValueOnce({stdout: '', stderr: '', exitCode: 0})

		await expect(libvirt.ejectInstallMedia(machine)).resolves.toBeUndefined()
		expect(execaMock.mock.calls[1]).toEqual([
			'virsh',
			expect.arrayContaining(['change-media', 'titan-machine-' + machine.id, 'sdb', '--eject', '--live']),
			{reject: false, timeout: 10_000},
		])
	})

	test('fails safely when libvirt cannot detach live media', async () => {
		const libvirt = new Libvirt({
			logger: {createChildLogger: () => ({log: vi.fn(), error: vi.fn()})},
		} as unknown as Titand)
		const machine = definition({installMedia: 'media/install.iso'})
		execaMock
			.mockResolvedValueOnce({stdout: 'running', stderr: '', exitCode: 0})
			.mockResolvedValueOnce({stdout: '', stderr: 'device is busy', exitCode: 1})

		await expect(libvirt.ejectInstallMedia(machine)).rejects.toThrow(
			'[machine-install-media-eject-failed] device is busy',
		)
		expect(execaMock.mock.calls[1]).toEqual([
			'virsh',
			expect.arrayContaining(['change-media', 'titan-machine-' + machine.id, '--eject', '--live']),
			{reject: false, timeout: 10_000},
		])
	})
})

describe('disk resize safety', () => {
	test('reads the virtual capacity from the qcow2 header', async () => {
		const libvirt = new Libvirt({
			logger: {createChildLogger: () => ({log: vi.fn(), error: vi.fn()})},
		} as unknown as Titand)
		execaMock.mockResolvedValueOnce({
			stdout: JSON.stringify({'virtual-size': 64 * 1_024 ** 3}),
			stderr: '',
			exitCode: 0,
		})

		await expect(libvirt.diskVirtualSizeBytes('/data/machines/test/disk.qcow2')).resolves.toBe(64 * 1_024 ** 3)
		expect(execaMock).toHaveBeenCalledWith(
			'qemu-img',
			['info', '-f', 'qcow2', '--force-share', '--output=json', '/data/machines/test/disk.qcow2'],
			{reject: false},
		)
	})

	test('grows a live disk through libvirt', async () => {
		const libvirt = new Libvirt({
			logger: {createChildLogger: () => ({log: vi.fn(), error: vi.fn()})},
		} as unknown as Titand)
		const machine = definition({id: 'safe-resize'})
		execaMock
			.mockResolvedValueOnce({stdout: 'running', stderr: '', exitCode: 0})
			.mockResolvedValueOnce({stdout: '', stderr: '', exitCode: 0})

		await libvirt.resizeDisk(machine, '/data/machines/safe-resize/disk.qcow2', 64)

		expect(execaMock.mock.calls[1]).toEqual([
			'virsh',
			['--connect', 'qemu:///system', 'blockresize', 'titan-machine-safe-resize', 'vda', '64G'],
		])
	})
})

describe('libvirt runtime cleanup', () => {
	test('removes runtime data after confirming failed unmounts are already detached', async () => {
		const root = await fsp.mkdtemp(nodePath.join(os.tmpdir(), 'titan-machine-runtime-'))
		temporaryDirectories.push(root)
		const previousRoot = process.env.TITAN_MACHINES_RUNTIME_DIR
		process.env.TITAN_MACHINES_RUNTIME_DIR = root
		const libvirt = new Libvirt({
			logger: {createChildLogger: () => ({log: vi.fn(), error: vi.fn()})},
		} as unknown as Titand)
		if (previousRoot === undefined) delete process.env.TITAN_MACHINES_RUNTIME_DIR
		else process.env.TITAN_MACHINES_RUNTIME_DIR = previousRoot
		await fse.ensureDir(libvirt.storageDirectory('detached'))
		execaMock.mockImplementation(async (command?: string) =>
			command === 'umount' ? {stdout: '', stderr: 'not mounted', exitCode: 32} : {stdout: '', stderr: '', exitCode: 32},
		)

		await libvirt.cleanupRuntime('detached')

		await expect(fse.pathExists(libvirt.runtimeDirectory('detached'))).resolves.toBe(false)
	})

	test('removes runtime data when its mount paths do not exist', async () => {
		const root = await fsp.mkdtemp(nodePath.join(os.tmpdir(), 'titan-machine-runtime-'))
		temporaryDirectories.push(root)
		const previousRoot = process.env.TITAN_MACHINES_RUNTIME_DIR
		process.env.TITAN_MACHINES_RUNTIME_DIR = root
		const libvirt = new Libvirt({
			logger: {createChildLogger: () => ({log: vi.fn(), error: vi.fn()})},
		} as unknown as Titand)
		if (previousRoot === undefined) delete process.env.TITAN_MACHINES_RUNTIME_DIR
		else process.env.TITAN_MACHINES_RUNTIME_DIR = previousRoot
		execaMock.mockImplementation(async (command?: string) => {
			if (command === 'mountpoint') throw new Error('mountpoint should not inspect a missing path')
			return {stdout: '', stderr: 'not mounted', exitCode: 32}
		})

		await expect(libvirt.cleanupRuntime('missing')).resolves.toBeUndefined()
	})

	test('preserves runtime data when a mount cannot be detached', async () => {
		const root = await fsp.mkdtemp(nodePath.join(os.tmpdir(), 'titan-machine-runtime-'))
		temporaryDirectories.push(root)
		const previousRoot = process.env.TITAN_MACHINES_RUNTIME_DIR
		process.env.TITAN_MACHINES_RUNTIME_DIR = root
		const libvirt = new Libvirt({
			logger: {createChildLogger: () => ({log: vi.fn(), error: vi.fn()})},
		} as unknown as Titand)
		if (previousRoot === undefined) delete process.env.TITAN_MACHINES_RUNTIME_DIR
		else process.env.TITAN_MACHINES_RUNTIME_DIR = previousRoot
		await fse.ensureDir(libvirt.storageDirectory('mounted'))
		execaMock.mockImplementation(async (command?: string) =>
			command === 'umount'
				? {stdout: '', stderr: 'permission denied', exitCode: 32}
				: {stdout: '', stderr: '', exitCode: 0},
		)

		await expect(libvirt.cleanupRuntime('mounted')).rejects.toThrow('[machine-runtime-unmount-failed]')
		await expect(fse.pathExists(libvirt.runtimeDirectory('mounted'))).resolves.toBe(true)
	})
})

describe('backup control command timeouts', () => {
	test('bounds suspend, resume, and external snapshot creation', async () => {
		const libvirt = new Libvirt({
			logger: {createChildLogger: () => ({log: vi.fn(), error: vi.fn()})},
		} as unknown as Titand)
		const machine = definition({id: 'backup-timeout'})

		await libvirt.pause(machine.id)
		await libvirt.resume(machine.id)
		// The domain's disks: its system disk, install media and one data disk
		execaMock.mockImplementation(async (_command, args) => ({
			stdout: args?.includes('domblklist')
				? ' Target   Source\n----------------\n vda      /run/disk.qcow2\n sda      -\n vdj      /run/data-disk-abcd1234.qcow2\n'
				: '',
			stderr: '',
			exitCode: 0,
		}))
		await libvirt.pivotToBackupOverlay(machine, '/data/machines/backup-timeout/operations/overlay.qcow2')
		const calls = execaMock.mock.calls as unknown as Array<[string, string[], Record<string, unknown>]>

		expect(calls.map(([, , options]) => options)).toEqual([
			{timeout: MACHINE_SHORT_CONTROL_TIMEOUT_MS},
			{reject: false, timeout: MACHINE_SHORT_CONTROL_TIMEOUT_MS},
			{reject: false, timeout: MACHINE_SHORT_CONTROL_TIMEOUT_MS},
			{timeout: MACHINE_SHORT_CONTROL_TIMEOUT_MS},
		])
		expect(calls[2][1]).toContain('domblklist')
		expect(calls[3][1]).toEqual(
			expect.arrayContaining(['snapshot-create-as', 'titan-machine-backup-timeout', '--atomic']),
		)
		// Data disks are user data outside the machine backup: only the system disk is snapshotted
		expect(calls[3][1].filter((argument) => argument.endsWith('snapshot=no'))).toEqual(['vdj,snapshot=no'])
	})
})

describe('Windows 98 disk initialization', () => {
	test('creates one active FAT32 LBA partition after the 1 MiB alignment gap', () => {
		const diskSizeBytes = 16_000_000_000
		const mbr = buildWindows98Mbr(diskSizeBytes)
		const partition = 446

		expect(mbr).toHaveLength(512)
		expect(mbr[partition]).toBe(0x80)
		expect(mbr[partition + 4]).toBe(0x0c)
		expect(mbr.subarray(partition + 1, partition + 4)).toEqual(Buffer.from([0x20, 0x21, 0x00]))
		expect(mbr.subarray(partition + 5, partition + 8)).not.toEqual(Buffer.from([0xff, 0xff, 0xff]))
		expect(mbr.readUInt32LE(partition + 8)).toBe(2_048)
		expect(mbr.readUInt32LE(partition + 12)).toBe(Math.floor(diskSizeBytes / 512) - 2_048)
		expect(mbr.subarray(510)).toEqual(Buffer.from([0x55, 0xaa]))
	})

	test('rejects disks beyond the MBR sector-count limit', () => {
		expect(() => buildWindows98Mbr(3_000_000_000_000)).toThrow('[machine-windows-98-disk-size-unsupported]')
	})
})
