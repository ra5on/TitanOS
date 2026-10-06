import {beforeEach, describe, expect, test, vi} from 'vitest'
import {$} from 'execa'
import fse from 'fs-extra'
import pWaitFor from 'p-wait-for'

import type Titand from '../../index.js'
import {
	clearStaticIp,
	connectToWiFiNetwork,
	confirmStaticIp,
	getNetworkInterfaces,
	restoreStaticIp,
	setStaticIp,
	syncDns,
} from './system.js'
import AutomaticMachineBridge, {machineBridgeChangePending, type BridgeNetworkManager} from './machine-bridge.js'
import {hostNetworkChangePending} from './network-change.js'

vi.mock('execa')
vi.mock('fs-extra')
vi.mock('systeminformation')
vi.mock('p-wait-for')
vi.mock('./machine-bridge.js', async () => ({
	...(await vi.importActual<typeof import('./machine-bridge.js')>('./machine-bridge.js')),
	machineBridgeChangePending: vi.fn(() => false),
}))

const physicalUuid = '11111111-1111-4111-8111-111111111111'
const bridgeUuid = '22222222-2222-4222-8222-222222222222'
const portUuid = '33333333-3333-4333-8333-333333333333'
const unrelatedUuid = '44444444-4444-4444-8444-444444444444'
const mac = '52:54:00:12:34:56'
const staticConfig = {ip: '192.168.1.20', subnetPrefix: 24, gateway: '192.168.1.1', dns: ['192.168.1.1']}

let deviceStatus: string
let activeConnections: Record<string, string>
let bridgePresent: boolean
let profiles: Record<string, {device: string; type: string; controller?: string; portType?: string}>
let settings: Record<string, typeof staticConfig>

function createTitand() {
	return {
		store: {
			get: vi.fn(async () => settings),
			getWriteLock: vi.fn(async (callback) =>
				callback({
					get: async () => settings,
					set: async (_key: string, value: typeof settings) => {
						settings = value
					},
				}),
			),
		},
		logger: {log: vi.fn(), error: vi.fn()},
		lanIngress: {refresh: vi.fn(async () => {})},
	} as unknown as Titand
}

function modifyCalls() {
	return vi
		.mocked($)
		.mock.calls.filter(([template]) => Array.isArray(template) && template.join('').includes('connection modify'))
}

beforeEach(() => {
	vi.resetAllMocks()
	vi.mocked(machineBridgeChangePending).mockReturnValue(false)
	deviceStatus =
		'enp1s0:ethernet:connected:Wired connection\ntitan-br0:bridge:connected:Wired connection\nveth0:ethernet:connected:Virtual'
	activeConnections = {enp1s0: portUuid, 'titan-br0': bridgeUuid}
	bridgePresent = true
	settings = {[mac]: {...staticConfig}}
	profiles = {
		[physicalUuid]: {device: 'enp1s0', type: '802-3-ethernet'},
		[portUuid]: {device: 'enp1s0', type: '802-3-ethernet', controller: bridgeUuid, portType: 'bridge'},
		[bridgeUuid]: {device: 'titan-br0', type: 'bridge'},
		[unrelatedUuid]: {device: 'enp2s0', type: '802-3-ethernet'},
	}
	vi.mocked(fse.pathExists).mockImplementation(
		async (file) =>
			String(file) === '/sys/class/net/enp1s0/device' || String(file) === '/sys/class/net/titan-br0/bridge',
	)
	vi.mocked(fse.realpath).mockImplementation(async (file) => {
		if (bridgePresent && String(file) === '/sys/class/net/enp1s0/master') {
			return '/sys/devices/virtual/net/titan-br0'
		}
		throw new Error('ENOENT')
	})
	vi.mocked(fse.readFile).mockResolvedValue(`${mac}\n` as never)
	const execute = async (template: TemplateStringsArray, ...values: unknown[]) => {
		const command = Array.isArray(template) ? template.join('') : String(template)
		if (command.includes('device status')) return {stdout: deviceStatus} as never
		if (command.includes('GENERAL.CON-UUID')) return {stdout: activeConnections[String(values[0])] ?? '--'} as never
		if (command.includes('ipv4.method,IP4.ADDRESS')) {
			const method = Object.keys(settings).length ? 'manual' : 'auto'
			return {
				stdout: `ipv4.method:${method}\nIP4.ADDRESS[1]:192.168.1.20/24\nIP4.GATEWAY:192.168.1.1\nIP4.DNS[1]:192.168.1.1`,
			} as never
		}
		if (command.includes('--fields UUID connection show')) return {stdout: Object.keys(profiles).join('\n')} as never
		if (command.includes('connection.interface-name,connection.type')) {
			const profile = profiles[String(values[0])]
			if (!profile) throw new Error('Connection not found')
			return {
				stdout: `connection.interface-name:${profile.device}\nconnection.type:${profile.type}\nconnection.master:${profile.controller ?? '--'}\nconnection.slave-type:${profile.portType ?? '--'}`,
			} as never
		}
		return {stdout: ''} as never
	}
	// The subprocess mock needs only the awaited result, not the real process's
	// stdin/stdout handles or callable overloads.
	vi.mocked($).mockImplementation(execute as never)
})

describe('physical interfaces backed by a LAN bridge', () => {
	test('shows the physical NIC with the bridge IP and original saved static settings', async () => {
		const interfaces = await getNetworkInterfaces(createTitand())
		expect(interfaces).toEqual([
			{
				id: 'enp1s0',
				mac,
				type: 'ethernet',
				connected: true,
				configuredStaticSettings: staticConfig,
				ipMethod: 'static',
				ip: '192.168.1.20',
				subnetPrefix: 24,
				gateway: '192.168.1.1',
				dns: ['192.168.1.1'],
			},
		])
		const ipQueries = vi
			.mocked($)
			.mock.calls.filter(
				([template]) => Array.isArray(template) && template.join('').includes('ipv4.method,IP4.ADDRESS'),
			)
		expect(ipQueries).toHaveLength(1)
		expect(ipQueries[0][1]).toBe(bridgeUuid)
	})

	test('does not claim a connected LAN interface when its bridge controller is disconnected', async () => {
		deviceStatus = deviceStatus.replace('titan-br0:bridge:connected', 'titan-br0:bridge:disconnected')
		expect(await getNetworkInterfaces()).toEqual([{id: 'enp1s0', mac, type: 'ethernet', connected: false}])
	})

	test('keeps unbridged NIC addressing unchanged', async () => {
		bridgePresent = false
		deviceStatus = 'enp1s0:ethernet:connected:Wired connection'
		activeConnections = {enp1s0: physicalUuid}
		settings = {}
		expect((await getNetworkInterfaces())[0]).toMatchObject({
			id: 'enp1s0',
			connected: true,
			ipMethod: 'dhcp',
			ip: '192.168.1.20',
		})
	})

	test('restores static IP on the bridge UUID and restarts the IP controller, leaving port profiles untouched', async () => {
		const titand = createTitand()
		await restoreStaticIp(titand)
		expect(modifyCalls()).toHaveLength(1)
		expect(modifyCalls()[0].slice(1)).toEqual([bridgeUuid, '192.168.1.20/24', '192.168.1.1', '192.168.1.1'])
		const reloadCalls = vi
			.mocked($)
			.mock.calls.filter(([template]) => Array.isArray(template) && /connection (down|up)/.test(template.join('')))
		expect(reloadCalls.map((call) => call[1])).toEqual([bridgeUuid, bridgeUuid])
		expect(titand.logger.error).not.toHaveBeenCalled()
	})

	test('clears static addressing on a disconnected saved bridge, rather than the older physical profile', async () => {
		bridgePresent = false
		deviceStatus = 'enp1s0:ethernet:disconnected:--'
		activeConnections = {}
		const titand = createTitand()
		await clearStaticIp(titand, {mac})
		expect(modifyCalls()).toHaveLength(1)
		expect(modifyCalls()[0].slice(1)).toEqual([bridgeUuid, '', '', ''])
		expect(settings).toEqual({})
		expect(titand.lanIngress.refresh).toHaveBeenCalledOnce()
	})

	test('resolves a disconnected port whose bridge controller is recorded by interface name', async () => {
		bridgePresent = false
		deviceStatus = 'enp1s0:ethernet:disconnected:--'
		activeConnections = {}
		profiles[portUuid].controller = 'titan-br0'
		await clearStaticIp(createTitand(), {mac})
		expect(modifyCalls()[0][1]).toBe(bridgeUuid)
	})

	test('clears a disconnected unbridged NIC profile with nmcli placeholders for unset controller fields', async () => {
		bridgePresent = false
		deviceStatus = 'enp1s0:ethernet:disconnected:--'
		activeConnections = {}
		delete profiles[portUuid]
		await clearStaticIp(createTitand(), {mac})
		expect(modifyCalls()[0][1]).toBe(physicalUuid)
		expect(settings).toEqual({})
	})

	test('does not overwrite a bridge port when its controller profile is missing', async () => {
		bridgePresent = false
		deviceStatus = 'enp1s0:ethernet:disconnected:--'
		activeConnections = {}
		delete profiles[bridgeUuid]
		await expect(clearStaticIp(createTitand(), {mac})).rejects.toThrow(
			'No IP configuration found for the bridge controller',
		)
		expect(modifyCalls()).toHaveLength(0)
		expect(settings[mac]).toEqual(staticConfig)
	})

	test('rejects concurrent IP, WiFi, and DNS changes before commands or stored settings are modified', async () => {
		vi.mocked(machineBridgeChangePending).mockReturnValue(true)
		const titand = createTitand()
		await expect(setStaticIp(titand, {mac, ...staticConfig})).rejects.toThrow('[machine-bridge-busy]')
		await expect(clearStaticIp(titand, {mac})).rejects.toThrow('[machine-bridge-busy]')
		await expect(connectToWiFiNetwork({ssid: 'Other LAN'})).rejects.toThrow('[machine-bridge-busy]')
		await expect(syncDns()).rejects.toThrow('[machine-bridge-busy]')
		await restoreStaticIp(titand)
		expect($).not.toHaveBeenCalled()
		expect(settings[mac]).toEqual(staticConfig)
		expect(titand.store.getWriteLock).not.toHaveBeenCalled()
	})

	test('keeps bridge preparation blocked throughout a static IP browser-confirmation operation', async () => {
		let reachConfirmation!: () => void
		const waiting = new Promise<void>((resolve) => {
			reachConfirmation = resolve
		})
		let finishConfirmation!: () => void
		vi.mocked(pWaitFor).mockImplementation(async (condition) => {
			reachConfirmation()
			await new Promise<void>((resolve) => {
				finishConfirmation = resolve
			})
			expect(await condition()).toBe(true)
		})
		const adapter = {
			uplink: vi.fn(async () => {
				throw new Error('[machine-bridge-unsupported]')
			}),
			close: vi.fn(),
		} as unknown as BridgeNetworkManager
		const bridge = new AutomaticMachineBridge(adapter)
		const applying = setStaticIp(createTitand(), {mac, ...staticConfig})
		await waiting
		expect(hostNetworkChangePending()).toBe(true)
		await expect(bridge.availability()).resolves.toEqual({available: false, reason: 'busy'})
		await expect(bridge.prepare('owner-session')).rejects.toThrow('[machine-bridge-busy]')
		expect(adapter.uplink).not.toHaveBeenCalled()
		confirmStaticIp(staticConfig.ip)
		finishConfirmation()
		await applying
		expect(hostNetworkChangePending()).toBe(false)
		await expect(bridge.prepare('owner-session')).rejects.toThrow('[machine-bridge-unsupported]')
		expect(adapter.uplink).toHaveBeenCalledOnce()
		await bridge.stop()
	})

	test('static IP timeout restores DHCP inside its own reservation and releases it after cleanup finishes', async () => {
		settings = {}
		vi.mocked(pWaitFor).mockRejectedValue(new Error('confirmation timeout'))
		let reachCleanup!: () => void
		const cleaning = new Promise<void>((resolve) => {
			reachCleanup = resolve
		})
		let finishCleanup!: () => void
		const execute = vi.mocked($).getMockImplementation()!
		vi.mocked($).mockImplementation((async (template: TemplateStringsArray, ...values: unknown[]) => {
			if (template.join('').includes('ipv4.method auto')) {
				reachCleanup()
				await new Promise<void>((resolve) => {
					finishCleanup = resolve
				})
			}
				return execute(template, ...(values as never[]))
		}) as never)
		const adapter = {
			uplink: vi.fn(async () => {
				throw new Error('[machine-bridge-unsupported]')
			}),
			close: vi.fn(),
		} as unknown as BridgeNetworkManager
		const bridge = new AutomaticMachineBridge(adapter)
		const applying = setStaticIp(createTitand(), {mac, ...staticConfig})
		const rejected = expect(applying).rejects.toThrow('Static IP change was not confirmed within 30 seconds')
		await cleaning
		expect(hostNetworkChangePending()).toBe(true)
		await expect(bridge.prepare('owner-session')).rejects.toThrow('[machine-bridge-busy]')
		expect(adapter.uplink).not.toHaveBeenCalled()
		finishCleanup()
		await rejected
		expect(hostNetworkChangePending()).toBe(false)
		expect(modifyCalls().map((call) => call[0].join(''))).toEqual(
			expect.arrayContaining([expect.stringContaining('ipv4.method auto')]),
		)
		await expect(bridge.prepare('owner-session')).rejects.toThrow('[machine-bridge-unsupported]')
		await bridge.stop()
	})
})
