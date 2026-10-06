import {createRequire} from 'node:module'
import {afterEach, describe, expect, test, vi} from 'vitest'
import AutomaticMachineBridge, {
	AUTOMATIC_MACHINE_BRIDGE,
	MACHINE_BRIDGE_ROLLBACK_SECONDS,
	NativeBridgeNetworkManager,
	buildBridgeProfiles,
	machineBridgeChangePending,
	normalizeNmVariant,
	type BridgeNetworkManager,
	type BridgeUplink,
	type NmSettings,
} from './machine-bridge.js'
import {hostNetworkChangePending, withHostNetworkChange} from './network-change.js'

const native = vi.hoisted(() => ({invoke: vi.fn(), end: vi.fn(), on: vi.fn()}))
vi.mock('@homebridge/dbus-native', () => ({
	default: {systemBus: () => ({invoke: native.invoke, connection: {on: native.on, end: native.end}})},
}))

const parseSignature = createRequire(import.meta.url)('@homebridge/dbus-native/lib/signature.js') as (
	signature: string,
) => Array<{type: string; child: unknown[]}>
function receivedSettings(settings: NmSettings) {
	return Object.entries(settings).map(([section, data]) => [section, receivedProperties(data)])
}
function receivedProperties(properties: Record<string, [string, unknown]>) {
	return Object.entries(properties).map(([key, [signature, data]]) => [key, [parseSignature(signature), [data]]])
}

function uplink(overrides: Partial<BridgeUplink> = {}): BridgeUplink {
	return {
		device: 'enp1s0',
		devicePath: '/device/1',
		connectionPath: '/settings/1',
		mac: '00:11:22:33:44:55',
		permanentMac: '00:11:22:33:44:55',
		ipv4Addresses: ['192.168.10.18'],
		dhcp4: {},
		dhcp6: {},
		settings: {
			connection: {
				id: ['s', 'Wired connection 1'],
				uuid: ['s', '00000000-0000-0000-0000-000000000001'],
				type: ['s', '802-3-ethernet'],
				'interface-name': ['s', 'enp1s0'],
				autoconnect: ['b', true],
				zone: ['s', 'trusted'],
			},
			'802-3-ethernet': {mtu: ['u', 9_000]},
			ipv4: {method: ['s', 'auto'], 'dns-search': ['as', ['nas.local']], 'route-metric': ['x', 600]},
			ipv6: {method: ['s', 'auto'], 'dns-priority': ['i', -20]},
		},
		...overrides,
	}
}

function fakeNetwork(
	snapshot = uplink(),
): BridgeNetworkManager & {calls: string[]; ready: boolean; profiles: Map<string, NmSettings>} {
	const calls: string[] = []
	const profiles = new Map<string, NmSettings>()
	return {
		calls,
		ready: true,
		profiles,
		uplink: vi.fn(async () => {
			calls.push('uplink')
			return snapshot
		}),
		bridgeReady: vi.fn(async () => {
			calls.push('bridgeReady')
			return false
		}),
		checkpoint: vi.fn(async () => {
			calls.push('checkpoint')
			return '/checkpoint/1'
		}),
		extend: vi.fn(async () => {
			calls.push('extend')
		}),
		add: vi.fn(async (settings) => {
			const path = `/settings/${profiles.size + 2}`
			profiles.set(path, structuredClone(settings))
			calls.push(`add:${path}`)
			return path
		}),
		activate: vi.fn(async (path) => {
			calls.push(`activate:${path}`)
		}),
		verify: vi.fn(async function (this: {ready: boolean}) {
			calls.push('verify')
			return this.ready
		}),
		save: vi.fn(async (path) => {
			calls.push(`save:${path}`)
		}),
		update: vi.fn(async (path) => {
			calls.push(`update:${path}`)
		}),
		destroy: vi.fn(async () => {
			calls.push('destroy')
		}),
		rollback: vi.fn(async () => {
			calls.push('rollback')
		}),
		remove: vi.fn(async (path) => {
			calls.push(`remove:${path}`)
		}),
		close: vi.fn(() => {
			calls.push('close')
		}),
	}
}

const coordinators: AutomaticMachineBridge[] = []
function coordinator(network = fakeNetwork()) {
	vi.useFakeTimers()
	const bridge = new AutomaticMachineBridge(network)
	coordinators.push(bridge)
	return {bridge, network}
}

afterEach(async () => {
	await Promise.all(coordinators.splice(0).map((bridge) => bridge.stop()))
	vi.useRealTimers()
	vi.clearAllMocks()
})

describe('automatic VM LAN bridge profiles', () => {
	test('keeps current MAC, DHCP identity, MTU and all IPv4/IPv6 settings without changing the original', () => {
		const original = uplink({dhcp4: {dhcp_client_identifier: 'ff:01:02:03:04:00:04:01:02'}})
		original.settings.ipv4['route-data'] = [
			'aa{sv}',
			[
				[
					['dest', ['s', '172.20.0.0']],
					['prefix', ['u', 16]],
					['next-hop', ['s', '192.168.10.1']],
					['table', ['u', 100]],
				],
			],
		]
		original.settings.ipv6['address-data'] = [
			'aa{sv}',
			[
				[
					['address', ['s', 'fd00::18']],
					['prefix', ['u', 64]],
				],
			],
		]
		const before = structuredClone(original)
		const {bridge, port} = buildBridgeProfiles(original)
		expect(original).toEqual(before)
		expect(bridge.ipv4).toEqual({...original.settings.ipv4, 'dhcp-client-id': ['s', 'ff:01:02:03:04:00:04:01:02']})
		expect(bridge.ipv6).toEqual(original.settings.ipv6)
		expect(bridge['802-3-ethernet']).toEqual({mtu: ['u', 9_000], 'assigned-mac-address': ['s', original.mac]})
		expect(bridge.bridge['mac-address'][0]).toBe('ay')
		expect(Array.from(bridge.bridge['mac-address'][1] as Uint8Array)).toEqual([0, 17, 34, 51, 68, 85])
		expect(bridge.connection['stable-id']).toEqual(original.settings.connection.uuid)
		expect(bridge.connection.autoconnect).toEqual(['b', false])
		expect(port.connection.autoconnect).toEqual(['b', false])
		expect(bridge.connection['autoconnect-slaves']).toEqual(['i', 1])
		expect(port.connection.master).toEqual(bridge.connection.uuid)
		expect(port.connection['slave-type']).toEqual(['s', 'bridge'])
		expect(port.ipv4).toBeUndefined()
		expect(port.ipv6).toBeUndefined()
	})

	test('supports static IP and DNS routes while pinning a generated physical MAC and stable identity', () => {
		const snapshot = uplink()
		snapshot.settings.ipv4 = {
			method: ['s', 'manual'],
			'address-data': [
				'aa{sv}',
				[
					[
						['address', ['s', '192.168.10.18']],
						['prefix', ['u', 24]],
					],
				],
			],
			gateway: ['s', '192.168.10.1'],
			dns: ['au', [16_777_218]],
			'routing-rules': ['aa{sv}', []],
		}
		snapshot.settings.connection['stable-id'] = ['s', '${CONNECTION}-${DEVICE}-${MAC}']
		snapshot.settings['802-3-ethernet']['assigned-mac-address'] = ['s', 'stable']
		snapshot.settings['802-3-ethernet']['cloned-mac-address'] = ['ay', new Uint8Array([1, 2, 3, 4, 5, 6])]
		const {bridge, port} = buildBridgeProfiles(snapshot)
		expect(bridge.ipv4).toEqual(snapshot.settings.ipv4)
		expect(port['802-3-ethernet']['assigned-mac-address']).toEqual(['s', snapshot.mac])
		expect(port['802-3-ethernet']['cloned-mac-address']).toBeUndefined()
		expect(bridge.connection['stable-id'][1]).toBe('00000000-0000-0000-0000-000000000001-enp1s0-00:11:22:33:44:55')
	})

	test('preserves DHCPv6 lease DUID and IAID on the bridge', () => {
		const snapshot = uplink({dhcp6: {dhcp6_client_id: '00:04:01:02:03:04', iaid: '00:00:00:02'}})
		const {bridge} = buildBridgeProfiles(snapshot)
		expect(bridge.ipv6['dhcp-duid']).toEqual(['s', '00:04:01:02:03:04'])
		expect(bridge.ipv6['dhcp-iaid']).toEqual(['s', '00:00:00:02'])
	})

	test('keeps the effective route priorities when Ethernet defaults would change on a bridge', () => {
		const snapshot = uplink({ipv4Metric: 100, ipv6Metric: 100})
		snapshot.settings.ipv4['route-metric'] = ['x', -1]
		const {bridge} = buildBridgeProfiles(snapshot)
		expect(bridge.ipv4['route-metric']).toEqual(['x', 100])
		expect(bridge.ipv6['route-metric']).toEqual(['x', 100])
		snapshot.settings.ipv4['route-metric'] = ['x', 50]
		expect(buildBridgeProfiles(snapshot).bridge.ipv4['route-metric']).toEqual(['x', 50])
	})

	test.each(['mac', 'perm-mac'])('converts %s DHCPv4 identity into an explicit binary client identifier', (mode) => {
		const snapshot = uplink({permanentMac: '00:11:22:33:44:66'})
		snapshot.settings.ipv4['dhcp-client-id'] = ['s', mode]
		expect(buildBridgeProfiles(snapshot).bridge.ipv4['dhcp-client-id']).toEqual([
			's',
			`01:${mode === 'mac' ? snapshot.mac : snapshot.permanentMac}`,
		])
	})

	test('rejects authenticated or complex source profiles before making a network change', () => {
		const authenticated = uplink()
		authenticated.settings['802-1x'] = {eap: ['as', ['tls']]}
		expect(() => buildBridgeProfiles(authenticated)).toThrow('[machine-bridge-unsupported]')
		const port = uplink()
		port.settings.connection.controller = ['s', 'bond0']
		expect(() => buildBridgeProfiles(port)).toThrow('[machine-bridge-unsupported]')
		const generatedDhcp = uplink()
		generatedDhcp.settings.ipv4['dhcp-client-id'] = ['s', 'stable']
		expect(() => buildBridgeProfiles(generatedDhcp)).toThrow('[machine-bridge-unsupported]')
	})

	test('normalizes real dbus-native nested variants without losing types or binary IPv6 fields', () => {
		type SignatureNode = {type: string; child: SignatureNode[]}
		const node = (type: string, child: SignatureNode[] = []): SignatureNode => ({type, child})
		const raw = [
			[node('a', [node('a', [node('{', [node('s'), node('v')])])])],
			[
				[
					[
						['dest', [[node('s')], ['fd00::']]],
						['prefix', [[node('u')], [64]]],
						['binary', [[node('a', [node('y')])], [Buffer.from([0, 1, 2])]]],
					],
				],
			],
		]
		expect(normalizeNmVariant(raw)).toEqual([
			'aa{sv}',
			[
				[
					['dest', ['s', 'fd00::']],
					['prefix', ['u', 64]],
					['binary', ['ay', Buffer.from([0, 1, 2])]],
				],
			],
		])
	})

	test('native persistence enables autoconnect and writes the original typed settings to disk', async () => {
		native.invoke.mockImplementation((message, callback) => {
			if (message.member === 'GetSettings')
				callback(null, [
					[
						'connection',
						[
							['id', [[{type: 's', child: []}], ['Titan VM Bridge']]],
							['autoconnect', [[{type: 'b', child: []}], [false]]],
						],
					],
				])
			else callback(null)
		})
		const adapter = new NativeBridgeNetworkManager()
		await adapter.save('/settings/bridge')
		const write = native.invoke.mock.calls.find(([message]) => message.member === 'Update')![0]
		expect(write.path).toBe('/settings/bridge')
		expect(write.signature).toBe('a{sa{sv}}')
		expect(write.body).toEqual([
			[
				[
					'connection',
					[
						['id', ['s', 'Titan VM Bridge']],
						['autoconnect', ['b', true]],
					],
				],
			],
		])
		adapter.close()
	})

	test('a restarted daemon only offers its automatically created bridge once both profiles are persistent and no checkpoint is active', async () => {
		const profiles = buildBridgeProfiles(uplink())
		profiles.bridge.connection.autoconnect = ['b', true]
		profiles.port.connection.autoconnect = ['b', true]
		profiles.port.connection.controller = profiles.port.connection.master
		profiles.port.connection['port-type'] = profiles.port.connection['slave-type']
		delete profiles.port.connection.master
		delete profiles.port.connection['slave-type']
		let checkpoint = false
		let unsaved = false
		native.invoke.mockImplementation((message, callback) => {
			if (message.member === 'ListConnections') callback(null, ['/settings/bridge', '/settings/port'])
			else if (message.member === 'GetSettings')
				callback(null, receivedSettings(message.path === '/settings/bridge' ? profiles.bridge : profiles.port))
			else if (message.path === '/org/freedesktop/NetworkManager')
				callback(null, receivedProperties({Checkpoints: ['ao', checkpoint ? ['/checkpoint/1'] : []]}))
			else callback(null, receivedProperties({Unsaved: ['b', unsaved], Flags: ['u', unsaved ? 1 : 0]}))
		})
		const adapter = new NativeBridgeNetworkManager()
		await expect(adapter.bridgeReady()).resolves.toBe(true)
		checkpoint = true
		await expect(adapter.bridgeReady()).resolves.toBe(false)
		checkpoint = false
		unsaved = true
		await expect(adapter.bridgeReady()).resolves.toBe(false)
		unsaved = false
		profiles.port.connection.autoconnect = ['b', false]
		await expect(adapter.bridgeReady()).resolves.toBe(false)
		expect(native.invoke.mock.calls.map(([message]) => message.member)).not.toContain('Update')
		adapter.close()
	})

	test('checkpoint creation and refresh use daemon-owned timeouts and never destroy unrelated checkpoints', async () => {
		native.invoke.mockImplementation((message, callback) =>
			callback(null, message.member === 'CheckpointCreate' ? '/checkpoint/1' : undefined),
		)
		const adapter = new NativeBridgeNetworkManager()
		await expect(adapter.checkpoint('/device/1')).resolves.toBe('/checkpoint/1')
		await adapter.extend('/checkpoint/1')
		expect(native.invoke.mock.calls[0][0]).toMatchObject({
			member: 'CheckpointCreate',
			signature: 'aouu',
			body: [['/device/1'], 120, 6],
		})
		expect(native.invoke.mock.calls[1][0]).toMatchObject({
			member: 'CheckpointAdjustRollbackTimeout',
			signature: 'ou',
			body: ['/checkpoint/1', 120],
		})
		adapter.close()
	})

	test('does not reactivate or randomize the already restored original uplink after rollback', async () => {
		native.invoke.mockImplementation((message, callback) => {
			callback(
				null,
				receivedProperties(
					message.path === '/device/1'
						? {ActiveConnection: ['o', '/active/1']}
						: {Connection: ['o', '/settings/1'], State: ['u', 2]},
				),
			)
		})
		const adapter = new NativeBridgeNetworkManager()
		await adapter.activate('/settings/1', '/device/1', true)
		expect(native.invoke.mock.calls.map(([message]) => message.member)).not.toContain('ActivateConnection')
		adapter.close()
	})
})

describe('automatic VM bridge checkpoint and confirmation', () => {
	test('an existing host IP/DNS/WiFi operation prevents bridge snapshots and setup until it fully settles', async () => {
		const {bridge, network} = coordinator()
		let finishChange!: () => void
		const change = withHostNetworkChange(
			() =>
				new Promise<void>((resolve) => {
					finishChange = resolve
				}),
		)
		expect(hostNetworkChangePending()).toBe(true)
		await expect(bridge.availability()).resolves.toEqual({available: false, reason: 'busy'})
		await expect(bridge.prepare('owner-session')).rejects.toThrow('[machine-bridge-busy]')
		expect(network.uplink).not.toHaveBeenCalled()
		finishChange()
		await change
		expect(hostNetworkChangePending()).toBe(false)
		await expect(bridge.prepare('owner-session')).resolves.toMatchObject({bridge: AUTOMATIC_MACHINE_BRIDGE})
		expect(network.uplink).toHaveBeenCalledOnce()
	})

	test('nested cleanup keeps the outer host-network reservation even when the nested operation fails', async () => {
		const {bridge, network} = coordinator()
		await withHostNetworkChange(async () => {
			await expect(
				withHostNetworkChange(async () => {
					throw new Error('cleanup failed')
				}),
			).rejects.toThrow('cleanup failed')
			expect(hostNetworkChangePending()).toBe(true)
			await expect(bridge.prepare('owner-session')).rejects.toThrow('[machine-bridge-busy]')
		})
		expect(hostNetworkChangePending()).toBe(false)
		expect(network.uplink).not.toHaveBeenCalled()
	})

	test('failed host-network operations release the bridge reservation only after their asynchronous cleanup', async () => {
		const {bridge, network} = coordinator()
		let finishCleanup!: () => void
		const failure = withHostNetworkChange(async () => {
			try {
				throw new Error('IP confirmation expired')
			} finally {
				await new Promise<void>((resolve) => {
					finishCleanup = resolve
				})
			}
		})
		const rejected = expect(failure).rejects.toThrow('IP confirmation expired')
		await expect(bridge.prepare('owner-session')).rejects.toThrow('[machine-bridge-busy]')
		expect(network.uplink).not.toHaveBeenCalled()
		finishCleanup()
		await rejected
		expect(hostNetworkChangePending()).toBe(false)
		await expect(bridge.prepare('owner-session')).resolves.toMatchObject({bridge: AUTOMATIC_MACHINE_BRIDGE})
	})

	test('read-only availability never changes the host network', async () => {
		const {bridge, network} = coordinator()
		await expect(bridge.availability()).resolves.toEqual({available: true, interface: 'enp1s0'})
		expect(network.calls).toEqual(['uplink'])
		expect(machineBridgeChangePending()).toBe(false)
	})

	test('returns a recovery token first, checkpoints before changing anything, and persists only after same-session confirmation', async () => {
		const {bridge, network} = coordinator()
		const prepared = await bridge.prepare('owner-session')
		expect(prepared.bridge).toBe(AUTOMATIC_MACHINE_BRIDGE)
		expect(prepared.token).toMatch(/^[a-f0-9]{64}$/)
		expect(network.calls).toEqual(['uplink'])
		expect(machineBridgeChangePending()).toBe(true)
		await expect(bridge.confirm(prepared.token, 'owner-session')).resolves.toEqual({
			state: 'preparing',
			bridge: AUTOMATIC_MACHINE_BRIDGE,
		})
		await vi.advanceTimersByTimeAsync(750)
		expect(network.calls).toEqual([
			'uplink',
			'checkpoint',
			'add:/settings/2',
			'add:/settings/3',
			'activate:/settings/2',
			'activate:/settings/3',
		])
		expect(network.save).not.toHaveBeenCalled()
		expect(network.update).not.toHaveBeenCalled()
		await expect(bridge.confirm(prepared.token, 'owner-session')).resolves.toEqual({
			state: 'ready',
			bridge: AUTOMATIC_MACHINE_BRIDGE,
		})
		expect(network.calls.slice(-6)).toEqual([
			'verify',
			'extend',
			'save:/settings/2',
			'save:/settings/3',
			'update:/settings/1',
			'destroy',
		])
		expect(network.update).toHaveBeenCalledWith(
			'/settings/1',
			expect.objectContaining({connection: expect.objectContaining({autoconnect: ['b', false]})}),
		)
		expect(machineBridgeChangePending()).toBe(false)
		await expect(bridge.confirm(prepared.token, 'owner-session')).resolves.toEqual({
			state: 'ready',
			bridge: AUTOMATIC_MACHINE_BRIDGE,
		})
		expect(network.save).toHaveBeenCalledTimes(2)
	})

	test('repeated setup calls recover the same token, while another session cannot read or confirm it', async () => {
		const {bridge, network} = coordinator()
		const first = bridge.prepare('owner-session')
		const second = bridge.prepare('owner-session')
		expect(await first).toEqual(await second)
		const prepared = await first
		await expect(bridge.prepare('another-session')).rejects.toThrow('[machine-bridge-busy]')
		await expect(bridge.confirm(prepared.token, 'another-session')).rejects.toThrow(
			'[machine-bridge-confirmation-expired]',
		)
		expect(network.uplink).toHaveBeenCalledTimes(1)
	})

	test('does not commit a bridge until the original host address and active uplink are verified', async () => {
		const {bridge, network} = coordinator()
		network.ready = false
		const prepared = await bridge.prepare('owner-session')
		await vi.advanceTimersByTimeAsync(750)
		await expect(bridge.confirm(prepared.token, 'owner-session')).resolves.toMatchObject({state: 'preparing'})
		expect(network.save).not.toHaveBeenCalled()
		expect(network.destroy).not.toHaveBeenCalled()
	})

	test('automatically restores the original connection when the browser never reconnects', async () => {
		const {bridge, network} = coordinator()
		const prepared = await bridge.prepare('owner-session')
		await vi.advanceTimersByTimeAsync(750 + MACHINE_BRIDGE_ROLLBACK_SECONDS * 1_000)
		expect(network.rollback).toHaveBeenCalledWith('/checkpoint/1')
		expect(network.save).not.toHaveBeenCalled()
		expect(network.update).toHaveBeenCalledWith('/settings/1', uplink().settings)
		expect(machineBridgeChangePending()).toBe(false)
		await expect(bridge.confirm(prepared.token, 'owner-session')).rejects.toThrow(
			'[machine-bridge-confirmation-expired]',
		)
	})

	test('rolls back a failed activation and permits a later setup attempt', async () => {
		const {bridge, network} = coordinator()
		vi.mocked(network.activate).mockRejectedValueOnce(new Error('[machine-bridge-unavailable]'))
		const prepared = await bridge.prepare('owner-session')
		await vi.advanceTimersByTimeAsync(750)
		expect(network.rollback).toHaveBeenCalledTimes(1)
		await expect(bridge.confirm(prepared.token, 'owner-session')).rejects.toThrow('[machine-bridge-unavailable]')
		await expect(bridge.availability()).resolves.toMatchObject({available: true})
		const retry = await bridge.prepare('owner-session')
		expect(retry.token).not.toBe(prepared.token)
	})

	test('rolls back a partial disk commit rather than disabling the original network permanently', async () => {
		const {bridge, network} = coordinator()
		const prepared = await bridge.prepare('owner-session')
		await vi.advanceTimersByTimeAsync(750)
		vi.mocked(network.save).mockRejectedValueOnce(new Error('[machine-bridge-unavailable]'))
		await expect(bridge.confirm(prepared.token, 'owner-session')).rejects.toThrow('[machine-bridge-unavailable]')
		expect(network.rollback).toHaveBeenCalledTimes(1)
		expect(network.update).toHaveBeenLastCalledWith('/settings/1', uplink().settings)
	})

	test('shutdown before activation cancels its timer and never changes the host network', async () => {
		const {bridge, network} = coordinator()
		await bridge.prepare('owner-session')
		await bridge.stop()
		await vi.advanceTimersByTimeAsync(1_000)
		expect(network.checkpoint).not.toHaveBeenCalled()
		expect(machineBridgeChangePending()).toBe(false)
	})

	test('shutdown during profile creation waits for that operation and rolls it back before any activation', async () => {
		const {bridge, network} = coordinator()
		let finishAdd!: (path: string) => void
		vi.mocked(network.add).mockImplementationOnce(
			() =>
				new Promise((resolve) => {
					finishAdd = resolve
				}),
		)
		await bridge.prepare('owner-session')
		await vi.advanceTimersByTimeAsync(750)
		const stopped = bridge.stop()
		finishAdd('/settings/2')
		await stopped
		expect(network.add).toHaveBeenCalledTimes(1)
		expect(network.remove).toHaveBeenCalledWith('/settings/2')
		expect(network.activate).toHaveBeenCalledTimes(1)
		expect(network.activate).toHaveBeenCalledWith('/settings/1', '/device/1', true)
	})

	test('shutdown during the read-only snapshot prevents delayed activation being scheduled afterward', async () => {
		const {bridge, network} = coordinator()
		let finishSnapshot!: (snapshot: BridgeUplink) => void
		vi.mocked(network.uplink).mockImplementationOnce(
			() =>
				new Promise((resolve) => {
					finishSnapshot = resolve
				}),
		)
		const preparation = bridge.prepare('owner-session')
		const rejected = expect(preparation).rejects.toThrow('[machine-bridge-unavailable]')
		expect(machineBridgeChangePending()).toBe(true)
		const stopped = bridge.stop()
		finishSnapshot(uplink())
		await stopped
		await rejected
		await vi.advanceTimersByTimeAsync(1_000)
		expect(network.checkpoint).not.toHaveBeenCalled()
		expect(machineBridgeChangePending()).toBe(false)
	})

	test('a new attempt waits for asynchronous failure cleanup before taking a fresh snapshot', async () => {
		const {bridge, network} = coordinator()
		let finishRollback!: () => void
		vi.mocked(network.rollback).mockImplementationOnce(
			() =>
				new Promise((resolve) => {
					finishRollback = resolve
				}),
		)
		vi.mocked(network.activate).mockRejectedValueOnce(new Error('[machine-bridge-unavailable]'))
		await bridge.prepare('owner-session')
		await vi.advanceTimersByTimeAsync(750)
		const retry = bridge.prepare('owner-session')
		expect(network.uplink).toHaveBeenCalledTimes(1)
		finishRollback()
		await retry
		expect(network.uplink).toHaveBeenCalledTimes(2)
		expect(machineBridgeChangePending()).toBe(true)
	})

	test('extends the daemon rollback window before commit so expiry cannot race a slow persistence write', async () => {
		const {bridge, network} = coordinator()
		const prepared = await bridge.prepare('owner-session')
		await vi.advanceTimersByTimeAsync(119_000)
		let finishSave!: () => void
		vi.mocked(network.save).mockImplementationOnce(
			() =>
				new Promise((resolve) => {
					finishSave = resolve
				}),
		)
		const confirmation = bridge.confirm(prepared.token, 'owner-session')
		await vi.advanceTimersByTimeAsync(5_000)
		expect(network.extend).toHaveBeenCalledWith('/checkpoint/1')
		expect(network.rollback).not.toHaveBeenCalled()
		finishSave()
		await expect(confirmation).resolves.toMatchObject({state: 'ready'})
	})

	test('reads persistent bridge readiness without exposing an in-flight bridge', async () => {
		const {bridge, network} = coordinator()
		await expect(bridge.bridgeReady()).resolves.toBe(false)
		expect(network.bridgeReady).toHaveBeenCalledTimes(1)
		await bridge.prepare('owner-session')
		await expect(bridge.bridgeReady()).resolves.toBe(false)
		expect(network.bridgeReady).toHaveBeenCalledTimes(1)
	})
})
