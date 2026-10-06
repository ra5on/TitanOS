import {randomBytes, randomUUID} from 'node:crypto'
import fse from 'fs-extra'
import dbus from '@homebridge/dbus-native'
import {hostNetworkChangePending} from './network-change.js'

// Keep the address-bearing bridge outside getIpAddresses' Docker exclusions.
export const AUTOMATIC_MACHINE_BRIDGE = 'titan-br0'
export const MACHINE_BRIDGE_ROLLBACK_SECONDS = 120
let bridgeChangePending = false
export function machineBridgeChangePending() {
	return bridgeChangePending
}
const NM = 'org.freedesktop.NetworkManager'
const NM_PATH = '/org/freedesktop/NetworkManager'
const SETTINGS_PATH = `${NM_PATH}/Settings`
const ACTIVE = `${NM}.Connection.Active`
const DEVICE = `${NM}.Device`
const CONNECTION = `${NM}.Settings.Connection`

export type NmVariant = [signature: string, value: unknown]
export type NmProperties = Record<string, NmVariant>
export type NmSettings = Record<string, NmProperties>
type SignatureNode = {type: string; child: SignatureNode[]}

function signature(node: SignatureNode): string {
	const children = node.child.map(signature).join('')
	return node.type + children + (node.type === '{' ? '}' : node.type === '(' ? ')' : '')
}

// dbus-native receives variants as [signature-tree, [value]], but sends them as
// [signature-string, value]. Preserve the signatures of every nested route,
// DNS option and IPv6 address, rather than reconstructing a few selected fields.
export function normalizeNmVariant(raw: unknown): NmVariant {
	const [tree, body] = raw as [SignatureNode[], unknown[]]
	if (!Array.isArray(tree) || tree.length !== 1 || !Array.isArray(body) || body.length !== 1)
		throw new Error('[machine-bridge-unsupported]')
	const normalize = (node: SignatureNode, value: unknown): unknown => {
		if (node.type === 'v') return normalizeNmVariant(value)
		if (node.type === 'a') {
			if (node.child[0].type === 'y') return value
			return (value as unknown[]).map((entry) => normalize(node.child[0], entry))
		}
		if (node.type === '{' || node.type === '(')
			return (value as unknown[]).map((entry, index) => normalize(node.child[index], entry))
		return value
	}
	return [signature(tree[0]), normalize(tree[0], body[0])]
}

function properties(raw: unknown): NmProperties {
	return Object.fromEntries((raw as Array<[string, unknown]>).map(([key, value]) => [key, normalizeNmVariant(value)]))
}

function value<T>(properties: NmProperties, key: string): T | undefined {
	return properties[key]?.[1] as T | undefined
}

function set(properties: NmProperties, key: string, type: string, data: unknown) {
	properties[key] = [type, data]
}

export type BridgeUplink = {
	device: string
	devicePath: string
	connectionPath: string
	settings: NmSettings
	mac: string
	permanentMac: string
	ipv4Addresses: string[]
	ipv4Metric?: number
	ipv6Metric?: number
	dhcp4: Record<string, string>
	dhcp6: Record<string, string>
}

export type AutomaticBridgeAvailability = {
	available: boolean
	interface?: string
	reason?: 'wifi' | 'unavailable' | 'unsupported' | 'busy'
}

export interface BridgeNetworkManager {
	uplink(): Promise<BridgeUplink>
	bridgeReady(): Promise<boolean>
	checkpoint(devicePath: string): Promise<string>
	extend(checkpoint: string): Promise<void>
	add(settings: NmSettings): Promise<string>
	activate(connectionPath: string, devicePath?: string, onlyIfInactive?: boolean): Promise<void>
	verify(bridgePath: string, portPath: string, uplink: BridgeUplink): Promise<boolean>
	save(path: string): Promise<void>
	update(path: string, settings: NmSettings): Promise<void>
	destroy(checkpoint: string): Promise<void>
	rollback(checkpoint: string): Promise<void>
	remove(path: string): Promise<void>
	close(): void
}

type NativeBus = {
	invoke(message: Record<string, unknown>, callback: (error: unknown, ...body: unknown[]) => void): void
	connection: {on(event: string, listener: (...args: unknown[]) => void): void; end(): void}
}

/** NetworkManager owns the timeout; rollback remains effective if titand exits. */
export class NativeBridgeNetworkManager implements BridgeNetworkManager {
	#bus?: NativeBus
	#closed = false
	#requests = new Set<(error: Error) => void>()

	#disconnect(error: Error) {
		const bus = this.#bus
		this.#bus = undefined
		for (const reject of this.#requests) reject(error)
		bus?.connection.end()
	}

	async #call(path: string, iface: string, member: string, callSignature = '', body: unknown[] = []) {
		if (this.#closed) throw new Error('[machine-bridge-unavailable]')
		if (!this.#bus) {
			this.#bus = dbus.systemBus() as unknown as NativeBus
			const bus = this.#bus
			const disconnect = () => {
				if (this.#bus === bus) this.#disconnect(new Error('[machine-bridge-unavailable]'))
			}
			bus.connection.on('error', disconnect)
			bus.connection.on('end', disconnect)
		}
		return new Promise<unknown[]>((resolve, reject) => {
			const fail = (error: Error) => {
				clearTimeout(timer)
				this.#requests.delete(fail)
				reject(error)
			}
			const timer = setTimeout(() => this.#disconnect(new Error('[machine-bridge-unavailable]')), 15_000)
			timer.unref()
			this.#requests.add(fail)
			try {
				this.#bus!.invoke(
					{destination: NM, path, interface: iface, member, ...(callSignature ? {signature: callSignature, body} : {})},
					(error, ...result) => {
						clearTimeout(timer)
						this.#requests.delete(fail)
						if (error) reject(new Error('[machine-bridge-unavailable]', {cause: error}))
						else resolve(result)
					},
				)
			} catch (error) {
				fail(new Error('[machine-bridge-unavailable]', {cause: error}))
			}
		})
	}

	async #properties(path: string, iface: string) {
		return properties((await this.#call(path, 'org.freedesktop.DBus.Properties', 'GetAll', 's', [iface]))[0])
	}

	async #settings(path: string): Promise<NmSettings> {
		const [settings] = await this.#call(path, CONNECTION, 'GetSettings')
		return Object.fromEntries(
			(settings as Array<[string, unknown]>).map(([section, data]) => [section, properties(data)]),
		)
	}

	async #dhcp(path: string | undefined, version: 4 | 6) {
		if (!path || path === '/') return {}
		const options =
			value<Array<[string, NmVariant]>>(await this.#properties(path, `${NM}.DHCP${version}Config`), 'Options') ?? []
		return Object.fromEntries(
			options
				.filter(([, variant]) => typeof variant[1] === 'string')
				.map(([key, variant]) => [key, variant[1] as string]),
		)
	}

	async #addresses(path: string | undefined) {
		if (!path || path === '/') return []
		const data =
			value<Array<Array<[string, NmVariant]>>>(await this.#properties(path, `${NM}.IP4Config`), 'AddressData') ?? []
		return data
			.map((entry) => entry.find(([key]) => key === 'address')?.[1][1])
			.filter((address): address is string => typeof address === 'string')
	}

	async #routeMetric(path: string | undefined, version: 4 | 6) {
		if (!path || path === '/') return undefined
		const data =
			value<Array<Array<[string, NmVariant]>>>(await this.#properties(path, `${NM}.IP${version}Config`), 'RouteData') ??
			[]
		const route = data.map((entry) => Object.fromEntries(entry)).find((entry) => entry.prefix?.[1] === 0)
		const metric = route?.metric?.[1]
		return typeof metric === 'number' && Number.isSafeInteger(metric) && metric >= 0 ? metric : undefined
	}

	async uplink(): Promise<BridgeUplink> {
		const manager = await this.#properties(NM_PATH, NM)
		if ((value<string[]>(manager, 'Checkpoints') ?? []).length) throw new Error('[machine-bridge-busy]')
		const primaryPath = value<string>(manager, 'PrimaryConnection')
		if (!primaryPath || primaryPath === '/') throw new Error('[machine-bridge-unavailable]')
		const primary = await this.#properties(primaryPath, ACTIVE)
		if (value<string>(primary, 'Type') === '802-11-wireless') throw new Error('[machine-bridge-wifi]')
		if (value<string>(primary, 'Type') !== '802-3-ethernet' || value<number>(primary, 'State') !== 2)
			throw new Error('[machine-bridge-unsupported]')
		const devices = value<string[]>(primary, 'Devices') ?? []
		if (devices.length !== 1) throw new Error('[machine-bridge-unsupported]')
		const devicePath = devices[0]
		const device = await this.#properties(devicePath, DEVICE)
		const name = value<string>(device, 'Interface') ?? ''
		if (
			!/^[a-zA-Z0-9_.-]{1,15}$/.test(name) ||
			!(await fse.pathExists(`/sys/class/net/${name}/device`)) ||
			value<boolean>(device, 'Managed') !== true
		)
			throw new Error('[machine-bridge-unsupported]')
		const wired = await this.#properties(devicePath, `${DEVICE}.Wired`)
		const mac = value<string>(wired, 'HwAddress') ?? value<string>(device, 'HwAddress') ?? ''
		const permanentMac = value<string>(wired, 'PermHwAddress') ?? mac
		if (!/^(?:[0-9a-f]{2}:){5}[0-9a-f]{2}$/i.test(mac) || !/^(?:[0-9a-f]{2}:){5}[0-9a-f]{2}$/i.test(permanentMac))
			throw new Error('[machine-bridge-unsupported]')
		const connectionPath = value<string>(primary, 'Connection')!
		const settings = await this.#settings(connectionPath)
		// Never replace an administrator-owned disconnected bridge or profile.
		const [connections] = await this.#call(SETTINGS_PATH, `${NM}.Settings`, 'ListConnections')
		for (const path of connections as string[]) {
			const candidate = await this.#settings(path)
			if (value<string>(candidate.connection, 'interface-name') === AUTOMATIC_MACHINE_BRIDGE)
				throw new Error('[machine-bridge-unsupported]')
		}
		if (await fse.pathExists(`/sys/class/net/${AUTOMATIC_MACHINE_BRIDGE}`))
			throw new Error('[machine-bridge-unsupported]')
		const uplink = {
			device: name,
			devicePath,
			connectionPath,
			settings,
			mac,
			permanentMac,
			ipv4Addresses: await this.#addresses(value<string>(device, 'Ip4Config')),
			ipv4Metric: await this.#routeMetric(value<string>(device, 'Ip4Config'), 4),
			ipv6Metric: await this.#routeMetric(value<string>(device, 'Ip6Config'), 6),
			dhcp4: await this.#dhcp(value<string>(device, 'Dhcp4Config'), 4),
			dhcp6: await this.#dhcp(value<string>(device, 'Dhcp6Config'), 6),
		}
		if (!uplink.ipv4Addresses.length) throw new Error('[machine-bridge-unsupported]')
		buildBridgeProfiles(uplink)
		return uplink
	}

	async checkpoint(devicePath: string) {
		return (
			await this.#call(NM_PATH, NM, 'CheckpointCreate', 'aouu', [
				[devicePath],
				MACHINE_BRIDGE_ROLLBACK_SECONDS,
				// DISCONNECT_NEW_DEVICES requires an empty device list (all NICs).
				// Snapshot only the actual uplink; delete the new profiles on rollback.
				0x02,
			])
		)[0] as string
	}

	async bridgeReady() {
		try {
			const manager = await this.#properties(NM_PATH, NM)
			if ((value<string[]>(manager, 'Checkpoints') ?? []).length) return false
			const [paths] = await this.#call(SETTINGS_PATH, `${NM}.Settings`, 'ListConnections')
			const profiles = await Promise.all(
				(paths as string[]).map(async (path) => ({path, settings: await this.#settings(path)})),
			)
			const bridge = profiles.find(
				({settings}) =>
					value<string>(settings.connection, 'interface-name') === AUTOMATIC_MACHINE_BRIDGE &&
					value<string>(settings.connection, 'type') === 'bridge',
			)
			if (!bridge) return false
			const origin = value<Array<[string, string]>>(bridge.settings.user ?? {}, 'data')?.find(
				([key]) => key === 'titan.origin-uuid',
			)
			// Administrator-created bridges are never converted or taken over.
			if (!origin) return true
			const uuid = value<string>(bridge.settings.connection, 'uuid')
			const port = profiles.find(
				({settings}) =>
					(value<string>(settings.connection, 'controller') || value<string>(settings.connection, 'master')) === uuid &&
					(value<string>(settings.connection, 'port-type') || value<string>(settings.connection, 'slave-type')) ===
						'bridge',
			)
			if (!port) return false
			for (const profile of [bridge, port]) {
				const flags = await this.#properties(profile.path, CONNECTION)
				if (
					// GetSettings omits defaults; autoconnect defaults to true.
					value<boolean>(profile.settings.connection, 'autoconnect') === false ||
					value<boolean>(flags, 'Unsaved') !== false ||
					((value<number>(flags, 'Flags') ?? 1) & 1) !== 0
				)
					return false
			}
			return true
		} catch {
			return false
		}
	}

	async extend(path: string) {
		await this.#call(NM_PATH, NM, 'CheckpointAdjustRollbackTimeout', 'ou', [path, MACHINE_BRIDGE_ROLLBACK_SECONDS])
	}

	async add(settings: NmSettings) {
		return (
			await this.#call(SETTINGS_PATH, `${NM}.Settings`, 'AddConnection2', 'a{sa{sv}}ua{sv}', [
				Object.entries(settings).map(([section, data]) => [section, Object.entries(data)]),
				0x02 | 0x20,
				[],
			])
		)[0] as string
	}

	async activate(path: string, devicePath = '/', onlyIfInactive = false) {
		if (onlyIfInactive && devicePath !== '/') {
			const device = await this.#properties(devicePath, DEVICE)
			const activePath = value<string>(device, 'ActiveConnection')
			if (activePath && activePath !== '/') {
				const active = await this.#properties(activePath, ACTIVE)
				if (value<string>(active, 'Connection') === path && [1, 2].includes(value<number>(active, 'State') ?? 0)) return
			}
		}
		await this.#call(NM_PATH, NM, 'ActivateConnection', 'ooo', [path, devicePath, '/'])
	}

	async verify(bridgePath: string, portPath: string, uplink: BridgeUplink) {
		const manager = await this.#properties(NM_PATH, NM)
		const activePaths = value<string[]>(manager, 'ActiveConnections') ?? []
		const active = await Promise.all(activePaths.map((path) => this.#properties(path, ACTIVE)))
		const bridge = active.find(
			(entry) => value<string>(entry, 'Connection') === bridgePath && value<number>(entry, 'State') === 2,
		)
		const port = active.find(
			(entry) => value<string>(entry, 'Connection') === portPath && value<number>(entry, 'State') === 2,
		)
		if (!bridge || !port || !(value<string[]>(port, 'Devices') ?? []).includes(uplink.devicePath)) return false
		const devicePath = value<string[]>(bridge, 'Devices')?.[0]
		if (!devicePath) return false
		const device = await this.#properties(devicePath, DEVICE)
		if (value<string>(device, 'Interface') !== AUTOMATIC_MACHINE_BRIDGE) return false
		const currentAddresses = await this.#addresses(value<string>(device, 'Ip4Config'))
		// Same address is essential for the browser that requested the change.
		return (
			uplink.ipv4Addresses.length > 0 && uplink.ipv4Addresses.every((address) => currentAddresses.includes(address))
		)
	}

	async save(path: string) {
		const settings = await this.#settings(path)
		set(settings.connection, 'autoconnect', 'b', true)
		await this.update(path, settings)
	}
	async update(path: string, settings: NmSettings) {
		await this.#call(path, CONNECTION, 'Update', 'a{sa{sv}}', [
			Object.entries(settings).map(([section, data]) => [section, Object.entries(data)]),
		])
	}
	async destroy(path: string) {
		await this.#call(NM_PATH, NM, 'CheckpointDestroy', 'o', [path])
	}
	async rollback(path: string) {
		await this.#call(NM_PATH, NM, 'CheckpointRollback', 'o', [path])
	}
	async remove(path: string) {
		await this.#call(path, CONNECTION, 'Delete')
	}
	close() {
		this.#closed = true
		this.#disconnect(new Error('[machine-bridge-unavailable]'))
	}
}

function hexadecimal(value: string | undefined): string | undefined {
	if (!value) return undefined
	const compact = value.replace(/[:\s-]/g, '')
	if (!/^(?:[0-9a-f]{2}){2,128}$/i.test(compact)) return undefined
	return compact.match(/../g)!.join(':').toLowerCase()
}

/** Create independent profiles; the original profile remains a rollback target. */
export function buildBridgeProfiles(uplink: BridgeUplink, bridgeUuid = randomUUID(), portUuid = randomUUID()) {
	const original = uplink.settings
	const connection = original.connection ?? {}
	const originalUuid = value<string>(connection, 'uuid')
	const permitted = new Set(['connection', '802-3-ethernet', 'ipv4', 'ipv6', 'ethtool', 'proxy', 'user', 'match'])
	if (
		!originalUuid ||
		Object.keys(original).some((section) => !permitted.has(section)) ||
		value<string>(connection, 'master') ||
		value<string>(connection, 'controller') ||
		!['auto', 'manual'].includes(value<string>(original.ipv4 ?? {}, 'method') ?? '')
	)
		throw new Error('[machine-bridge-unsupported]')
	const bridge: NmSettings = structuredClone(original)
	// NetworkManager's default metric depends on the device type. Moving an
	// Ethernet address to a bridge must not reorder routes against Wi-Fi/NICs.
	for (const [section, metric] of [
		['ipv4', uplink.ipv4Metric],
		['ipv6', uplink.ipv6Metric],
	] as const) {
		if (bridge[section] && metric !== undefined && (value<number>(bridge[section], 'route-metric') ?? -1) < 0)
			set(bridge[section], 'route-metric', 'x', metric)
	}
	for (const section of ['match', 'ethtool']) delete bridge[section]
	bridge['802-3-ethernet'] = {}
	const mtu = original['802-3-ethernet']?.mtu
	if (mtu) bridge['802-3-ethernet'].mtu = structuredClone(mtu)
	set(bridge['802-3-ethernet'], 'assigned-mac-address', 's', uplink.mac)
	set(bridge.connection, 'id', 's', 'Titan VM Bridge')
	set(bridge.connection, 'uuid', 's', bridgeUuid)
	set(bridge.connection, 'type', 's', 'bridge')
	set(bridge.connection, 'interface-name', 's', AUTOMATIC_MACHINE_BRIDGE)
	set(bridge.connection, 'autoconnect', 'b', false)
	set(bridge.connection, 'autoconnect-slaves', 'i', 1)
	delete bridge.connection['autoconnect-ports']
	delete bridge.connection.timestamp
	const stableId = value<string>(connection, 'stable-id') || originalUuid
	if (/\$\{(?!CONNECTION\}|DEVICE\}|MAC\})/.test(stableId)) throw new Error('[machine-bridge-unsupported]')
	set(
		bridge.connection,
		'stable-id',
		's',
		stableId
			.replaceAll('${CONNECTION}', originalUuid)
			.replaceAll('${DEVICE}', uplink.device)
			.replaceAll('${MAC}', uplink.mac),
	)
	bridge.bridge = {stp: ['b', false], 'mac-address': ['ay', Buffer.from(uplink.mac.replaceAll(':', ''), 'hex')]}
	bridge.user ??= {}
	const userData = value<Array<[string, string]>>(bridge.user, 'data') ?? []
	set(bridge.user, 'data', 'a{ss}', [
		...userData.filter(([key]) => key !== 'titan.origin-uuid'),
		['titan.origin-uuid', originalUuid],
	])
	// DHCP reservations must keep the original identity, including profiles that
	// used a generated DUID. Prefer the identifier of the current DHCP lease.
	if (value<string>(bridge.ipv4, 'method') === 'auto') {
		const configured = value<string>(bridge.ipv4, 'dhcp-client-id')
		const actual = hexadecimal(uplink.dhcp4.dhcp_client_identifier)
		if (actual) set(bridge.ipv4, 'dhcp-client-id', 's', actual)
		else if (!configured || configured === 'mac') set(bridge.ipv4, 'dhcp-client-id', 's', `01:${uplink.mac}`)
		else if (configured === 'perm-mac') set(bridge.ipv4, 'dhcp-client-id', 's', `01:${uplink.permanentMac}`)
		else if (['duid', 'ipv6-duid', 'stable'].includes(configured)) throw new Error('[machine-bridge-unsupported]')
	}
	if (bridge.ipv6 && ['auto', 'dhcp'].includes(value<string>(bridge.ipv6, 'method') ?? '')) {
		const duid = hexadecimal(uplink.dhcp6.dhcp6_client_id)
		if (duid) set(bridge.ipv6, 'dhcp-duid', 's', duid)
		const iaid = uplink.dhcp6.iaid ?? uplink.dhcp6.dhcp6_iaid
		if (iaid && /^(?:\d+|0x[\da-f]+|(?:[\da-f]{2}:){3}[\da-f]{2})$/i.test(iaid))
			set(bridge.ipv6, 'dhcp-iaid', 's', iaid)
		else if (Object.keys(uplink.dhcp6).length && !/^\d+$/.test(value<string>(bridge.ipv6, 'dhcp-iaid') ?? ''))
			throw new Error('[machine-bridge-unsupported]')
	}
	const port: NmSettings = {
		connection: structuredClone(connection),
		'802-3-ethernet': structuredClone(original['802-3-ethernet'] ?? {}),
		'bridge-port': {},
	}
	if (original.ethtool) port.ethtool = structuredClone(original.ethtool)
	if (original.match) port.match = structuredClone(original.match)
	set(port.connection, 'id', 's', `Titan VM Bridge ${uplink.device}`)
	set(port.connection, 'uuid', 's', portUuid)
	set(port.connection, 'interface-name', 's', uplink.device)
	set(port.connection, 'type', 's', '802-3-ethernet')
	set(port.connection, 'master', 's', bridgeUuid)
	set(port.connection, 'slave-type', 's', 'bridge')
	delete port.connection.controller
	delete port.connection['port-type']
	set(port.connection, 'autoconnect', 'b', false)
	set(port['802-3-ethernet'], 'assigned-mac-address', 's', uplink.mac)
	delete port['802-3-ethernet']['cloned-mac-address']
	set(port.connection, 'stable-id', 's', value<string>(bridge.connection, 'stable-id'))
	delete port.connection.timestamp
	return {bridge, port}
}

type PendingBridge = {
	sessionId: string
	token: string
	expiresAt: number
	uplink: BridgeUplink
	checkpoint?: string
	bridgePath?: string
	portPath?: string
	error?: Error
	state: 'preparing' | 'awaiting-confirmation'
	activationTimer?: NodeJS.Timeout
	activation?: Promise<void>
	expiryTimer?: NodeJS.Timeout
	rollback?: Promise<void>
	confirmation?: Promise<{state: 'ready'; bridge: string} | undefined>
}

export default class AutomaticMachineBridge {
	#network: BridgeNetworkManager
	#pending?: PendingBridge
	#preparing?: Promise<{bridge: string; token: string; expiresAt: number}>
	#preparingSession?: string
	#confirmed?: {token: string; sessionId: string; expiresAt: number}
	#log: (error: unknown) => void
	#stopped = false

	constructor(
		network: BridgeNetworkManager = new NativeBridgeNetworkManager(),
		log: (error: unknown) => void = () => {},
	) {
		this.#network = network
		this.#log = log
	}

	get pending() {
		return !!this.#preparing || (!!this.#pending && !this.#pending.error)
	}
	async bridgeReady() {
		return !this.pending && (await this.#network.bridgeReady())
	}

	async availability(): Promise<AutomaticBridgeAvailability> {
		if (hostNetworkChangePending() || (this.#pending && !this.#pending.error) || this.#preparing)
			return {available: false, reason: 'busy'}
		try {
			const uplink = await this.#network.uplink()
			return {available: true, interface: uplink.device}
		} catch (error) {
			const message = error instanceof Error ? error.message : ''
			return {
				available: false,
				reason: message.includes('wifi')
					? 'wifi'
					: message.includes('busy')
						? 'busy'
						: message.includes('unsupported')
							? 'unsupported'
							: 'unavailable',
			}
		}
	}

	async prepare(sessionId: string) {
		if (this.#stopped) throw new Error('[machine-bridge-unavailable]')
		if (!sessionId) throw new Error('[machine-bridge-confirmation-expired]')
		if (this.#pending?.error) await this.#rollback(this.#pending)
		if (hostNetworkChangePending()) throw new Error('[machine-bridge-busy]')
		if (this.#pending && !this.#pending.error && this.#pending.expiresAt > Date.now()) {
			if (this.#pending.sessionId !== sessionId) throw new Error('[machine-bridge-busy]')
			return this.#response(this.#pending)
		}
		if (this.#preparing) {
			if (this.#preparingSession !== sessionId) throw new Error('[machine-bridge-busy]')
			return this.#preparing
		}
		this.#preparingSession = sessionId
		bridgeChangePending = true
		this.#preparing = (async () => {
			const uplink = await this.#network.uplink()
			if (this.#stopped) throw new Error('[machine-bridge-unavailable]')
			const pending: PendingBridge = {
				sessionId,
				token: randomBytes(32).toString('hex'),
				expiresAt: Date.now() + (MACHINE_BRIDGE_ROLLBACK_SECONDS + 5) * 1_000,
				uplink,
				state: 'preparing',
			}
			this.#pending = pending
			bridgeChangePending = true
			// Let the HTTP response containing the recovery token reach the browser
			// before the active physical interface moves under the bridge.
			pending.activationTimer = setTimeout(() => {
				pending.activation = this.#activate(pending)
			}, 750)
			pending.activationTimer.unref()
			return this.#response(pending)
		})()
		try {
			return await this.#preparing
		} finally {
			this.#preparing = undefined
			this.#preparingSession = undefined
			if (!this.#pending || this.#pending.error) bridgeChangePending = false
		}
	}

	#response(pending: PendingBridge) {
		return {bridge: AUTOMATIC_MACHINE_BRIDGE, token: pending.token, expiresAt: pending.expiresAt}
	}

	async #activate(pending: PendingBridge) {
		try {
			const ensureActive = () => {
				if (pending.error) throw pending.error
			}
			const profiles = buildBridgeProfiles(pending.uplink)
			pending.checkpoint = await this.#network.checkpoint(pending.uplink.devicePath)
			ensureActive()
			pending.expiryTimer = setTimeout(
				() => void this.#abort(pending, new Error('[machine-bridge-confirmation-expired]')),
				MACHINE_BRIDGE_ROLLBACK_SECONDS * 1_000,
			)
			pending.expiryTimer.unref()
			pending.bridgePath = await this.#network.add(profiles.bridge)
			ensureActive()
			pending.portPath = await this.#network.add(profiles.port)
			ensureActive()
			await this.#network.activate(pending.bridgePath)
			ensureActive()
			await this.#network.activate(pending.portPath, pending.uplink.devicePath)
			ensureActive()
			pending.state = 'awaiting-confirmation'
		} catch (error) {
			pending.error ??= error instanceof Error ? error : new Error('[machine-bridge-unavailable]')
			this.#log(pending.error)
			await this.#rollback(pending)
		}
	}

	async #abort(pending: PendingBridge, error: Error) {
		pending.error ??= error
		if (pending.expiryTimer) clearTimeout(pending.expiryTimer)
		if (pending.activationTimer) clearTimeout(pending.activationTimer)
		await pending.activation?.catch(this.#log)
		await pending.confirmation?.catch(this.#log)
		await this.#rollback(pending)
	}

	async #rollback(pending: PendingBridge) {
		if (this.#confirmed?.token === pending.token) return
		if (pending.rollback) return pending.rollback
		pending.rollback = (async () => {
			if (pending.expiryTimer) clearTimeout(pending.expiryTimer)
			if (pending.activationTimer) clearTimeout(pending.activationTimer)
			if (pending.checkpoint) {
				await this.#network.rollback(pending.checkpoint).catch(this.#log)
				// The checkpoint may already have expired inside NetworkManager.
				// Delete only the two paths we created, including partially saved ones.
				for (const path of [pending.portPath, pending.bridgePath])
					if (path) await this.#network.remove(path).catch(this.#log)
				await this.#network.update(pending.uplink.connectionPath, pending.uplink.settings).catch(this.#log)
				await this.#network.activate(pending.uplink.connectionPath, pending.uplink.devicePath, true).catch(this.#log)
				await this.#network.destroy(pending.checkpoint).catch(this.#log)
			}
			bridgeChangePending = false
		})()
		return pending.rollback
	}

	async confirm(token: string, sessionId: string): Promise<{state: 'preparing' | 'ready'; bridge: string}> {
		if (
			this.#confirmed?.token === token &&
			this.#confirmed.sessionId === sessionId &&
			this.#confirmed.expiresAt > Date.now()
		)
			return {state: 'ready', bridge: AUTOMATIC_MACHINE_BRIDGE}
		const pending = this.#pending
		if (!pending || pending.token !== token || pending.sessionId !== sessionId || pending.expiresAt <= Date.now())
			throw new Error('[machine-bridge-confirmation-expired]')
		if (pending.error) throw pending.error
		if (pending.state !== 'awaiting-confirmation' || !pending.bridgePath || !pending.portPath)
			return {state: 'preparing', bridge: AUTOMATIC_MACHINE_BRIDGE}
		if (pending.confirmation)
			return (await pending.confirmation) ?? {state: 'preparing', bridge: AUTOMATIC_MACHINE_BRIDGE}
		pending.confirmation = (async () => {
			if (!(await this.#network.verify(pending.bridgePath!, pending.portPath!, pending.uplink))) return undefined
			try {
				const ensureActive = () => {
					if (pending.error) throw pending.error
				}
				ensureActive()
				// Leave a full daemon-owned rollback window for the disk commit too.
				await this.#network.extend(pending.checkpoint!)
				ensureActive()
				if (pending.expiryTimer) clearTimeout(pending.expiryTimer)
				pending.expiresAt = Date.now() + MACHINE_BRIDGE_ROLLBACK_SECONDS * 1_000
				pending.expiryTimer = setTimeout(
					() => void this.#abort(pending, new Error('[machine-bridge-confirmation-expired]')),
					MACHINE_BRIDGE_ROLLBACK_SECONDS * 1_000,
				)
				pending.expiryTimer.unref()
				// A fresh authenticated browser request has crossed the changed LAN.
				// Persist only after the same NAS address is back on a working bridge.
				for (const path of [pending.bridgePath!, pending.portPath!]) {
					await this.#network.save(path)
					ensureActive()
				}
				const original = structuredClone(pending.uplink.settings)
				set(original.connection, 'autoconnect', 'b', false)
				await this.#network.update(pending.uplink.connectionPath, original)
				ensureActive()
				await this.#network.destroy(pending.checkpoint!)
				if (pending.expiryTimer) clearTimeout(pending.expiryTimer)
				this.#confirmed = {token, sessionId, expiresAt: Date.now() + 10 * 60_000}
				this.#pending = undefined
				bridgeChangePending = false
				return {state: 'ready' as const, bridge: AUTOMATIC_MACHINE_BRIDGE}
			} catch (error) {
				pending.error ??= error instanceof Error ? error : new Error('[machine-bridge-unavailable]')
				this.#log(pending.error)
				await this.#rollback(pending)
				throw pending.error
			}
		})()
		try {
			return (await pending.confirmation) ?? {state: 'preparing', bridge: AUTOMATIC_MACHINE_BRIDGE}
		} finally {
			pending.confirmation = undefined
		}
	}

	async stop() {
		this.#stopped = true
		if (this.#preparing && !this.#pending) this.#network.close()
		await this.#preparing?.catch(this.#log)
		if (this.#pending) await this.#abort(this.#pending, new Error('[machine-bridge-confirmation-expired]'))
		this.#network.close()
	}
}
