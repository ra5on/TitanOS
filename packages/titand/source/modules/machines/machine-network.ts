import {isIPv4} from 'node:net'

import {z} from 'zod'

import {
	MACHINE_NETWORK_NAME,
	MACHINE_HOST_ONLY_NETWORK_NAME,
	type MachineDefinition,
	type MachineNetwork,
} from './domain.js'

export const MACHINE_NETWORK_BRIDGE = 'titan-vm'
export const MACHINE_HOST_ONLY_BRIDGE = 'titan-vm-host'
export const MACHINE_HOST_ONLY_PREFIX = '10.204.0'
export const machineBridgeSchema = z.string().regex(/^[a-zA-Z0-9_.-]{1,15}$(?![\s\S])/, '[machine-bridge-invalid]')
export const machineNetworkSchema = z.discriminatedUnion('mode', [
	z.object({mode: z.literal('nat')}),
	z.object({mode: z.literal('host-only')}),
	z.object({mode: z.literal('bridge'), bridge: machineBridgeSchema}),
])

export function managedMachineNetwork(mode: 'nat' | 'host-only') {
	return mode === 'host-only'
		? {
				name: MACHINE_HOST_ONLY_NETWORK_NAME,
				bridge: MACHINE_HOST_ONLY_BRIDGE,
				prefix: MACHINE_HOST_ONLY_PREFIX,
				host: `${MACHINE_HOST_ONLY_PREFIX}.1`,
			}
		: {
				name: MACHINE_NETWORK_NAME,
				bridge: MACHINE_NETWORK_BRIDGE,
				prefix: MACHINE_NETWORK_PREFIX,
				host: MACHINE_GUEST_HOST_ADDRESS,
			}
}
// Keep this outside Tailscale's 100.64.0.0/10 CGNAT range, Titan's
// 10.21.0.0/16 app network, and Docker's usual 172.16.0.0/12 pools.
export const MACHINE_NETWORK_PREFIX = '10.203.0'
export const MACHINE_GUEST_HOST_ADDRESS = `${MACHINE_NETWORK_PREFIX}.1`
export const MACHINE_GUEST_FIRST_ADDRESS = 2
export const MACHINE_GUEST_LAST_ADDRESS = 254
export const MACHINE_DNS_FALLBACK_SERVERS = ['1.1.1.1', '9.9.9.9', '8.8.8.8'] as const
const MACHINE_DNS_SERVER_LIMIT = 3

function isGuestReachableDnsServer(address: string) {
	if (!isIPv4(address)) return false
	const [first, second] = address.split('.').map(Number)
	return (
		first !== 0 &&
		first !== 127 &&
		first < 224 &&
		!(first === 169 && second === 254) &&
		!address.startsWith(`${MACHINE_NETWORK_PREFIX}.`)
	)
}

export function machineDnsServersFromResolvConf(resolvConf: string) {
	const servers: string[] = []
	for (const line of resolvConf.split('\n')) {
		const address = /^\s*nameserver\s+(\S+)/.exec(line)?.[1]
		if (!address || !isGuestReachableDnsServer(address) || servers.includes(address)) continue
		servers.push(address)
		if (servers.length === MACHINE_DNS_SERVER_LIMIT) break
	}
	return servers.length > 0 ? servers : [...MACHINE_DNS_FALLBACK_SERVERS]
}

export function machineAddressSchema(network: MachineNetwork = {mode: 'nat'}) {
	const prefix = `${managedMachineNetwork(network.mode === 'host-only' ? 'host-only' : 'nat').prefix}.`
	return z.string().refine((value) => {
		if (!value.startsWith(prefix)) return false
		const suffix = value.slice(prefix.length)
		if (!/^\d{1,3}$/.test(suffix)) return false
		const host = Number(suffix)
		return host >= MACHINE_GUEST_FIRST_ADDRESS && host <= MACHINE_GUEST_LAST_ADDRESS
	}, '[machine-ip-address-invalid]')
}

export const machineIpAddressSchema = machineAddressSchema()

export function nextMachineIpAddress(
	definitions: Array<Pick<MachineDefinition, 'ipAddress'>>,
	network: MachineNetwork = {mode: 'nat'},
) {
	const used = new Set(definitions.map(({ipAddress}) => ipAddress).filter(Boolean))
	for (let host = MACHINE_GUEST_FIRST_ADDRESS; host <= MACHINE_GUEST_LAST_ADDRESS; host++) {
		const candidate = `${managedMachineNetwork(network.mode === 'host-only' ? 'host-only' : 'nat').prefix}.${host}`
		if (!used.has(candidate)) return candidate
	}
	throw new Error('[machine-network-full]')
}

export function parseActiveMachineLeaseAddresses(output: string, mode: 'nat' | 'host-only' = 'nat') {
	const addresses = new Set<string>()
	for (const match of output.matchAll(/\b(\d{1,3}(?:\.\d{1,3}){3})\/\d{1,3}\b/g)) {
		if (machineAddressSchema({mode}).safeParse(match[1]).success) addresses.add(match[1])
	}
	return [...addresses]
}

export function hostOnlyNetworkConflicts(routes: Array<{dst?: string; dev?: string}>) {
	const target = MACHINE_HOST_ONLY_PREFIX.split('.').reduce((value, octet) => value * 256 + Number(octet), 0) * 256
	return routes.some(({dst, dev}) => {
		if (!dst || dst === 'default' || dev === MACHINE_HOST_ONLY_BRIDGE) return false
		const [address, bits = '32'] = dst.split('/')
		if (!isIPv4(address) || !/^\d+$/.test(bits) || Number(bits) < 1 || Number(bits) > 32) return false
		const size = 2 ** (32 - Number(bits))
		const number = address.split('.').reduce((value, octet) => value * 256 + Number(octet), 0)
		const start = Math.floor(number / size) * size
		return start <= target + 255 && start + size - 1 >= target
	})
}

function escapeXml(value: string) {
	return value
		.replace(/&/g, '&amp;')
		.replace(/</g, '&lt;')
		.replace(/>/g, '&gt;')
		.replace(/"/g, '&quot;')
		.replace(/'/g, '&apos;')
}

export type MachineDhcpLease = {macAddress: string; ipAddress: string; name?: string}

export function machineDhcpLeases(
	definitions: MachineDefinition[],
	mode: 'nat' | 'host-only' = 'nat',
): MachineDhcpLease[] {
	return definitions
		.filter((definition) => (definition.network?.mode ?? 'nat') === mode)
		.map((definition) => ({
			macAddress: definition.macAddress,
			ipAddress: machineAddressSchema({mode}).parse(definition.ipAddress),
			name: definition.id,
		}))
}

export function buildMachineNetworkXml(
	definitions: MachineDefinition[],
	dnsServers: readonly string[] = MACHINE_DNS_FALLBACK_SERVERS,
	mode: 'nat' | 'host-only' = 'nat',
) {
	const network = managedMachineNetwork(mode)
	if (dnsServers.length === 0 || dnsServers.some((server) => !isGuestReachableDnsServer(server))) {
		throw new Error('[machine-dns-server-invalid]')
	}
	const hosts = machineDhcpLeases(definitions, mode)
		.map(
			({macAddress, ipAddress, name}) =>
				`<host mac='${escapeXml(macAddress)}' name='${escapeXml(name!)}' ip='${escapeXml(ipAddress)}'/>`,
		)
		.join('')
	const dnsOption = escapeXml(`dhcp-option=option:dns-server,${dnsServers.join(',')}`)
	return `<?xml version='1.0' encoding='UTF-8'?>
<network xmlns:dnsmasq='http://libvirt.org/schemas/network/dnsmasq/1.0'>
  <name>${network.name}</name>
  ${mode === 'nat' ? "<forward mode='nat'><nat><port start='1024' end='65535'/></nat></forward>" : ''}
  <bridge name='${network.bridge}' stp='on' delay='0'/>
  <port isolated='yes'/>
  <dns enable='no'/>
  <ip address='${network.host}' netmask='255.255.255.0'>
    <dhcp>${hosts}</dhcp>
  </ip>
  <dnsmasq:options>
    ${mode === 'nat' ? `<dnsmasq:option value='${dnsOption}'/>` : "<dnsmasq:option value='dhcp-option=option:router'/><dnsmasq:option value='dhcp-option=option:dns-server'/>"}
  </dnsmasq:options>
</network>
`
}

function unescapeXml(value: string) {
	return value
		.replace(/&apos;/g, "'")
		.replace(/&quot;/g, '"')
		.replace(/&gt;/g, '>')
		.replace(/&lt;/g, '<')
		.replace(/&amp;/g, '&')
}

export function parseMachineDhcpLeases(xml: string): MachineDhcpLease[] {
	const leases: MachineDhcpLease[] = []
	for (const match of xml.matchAll(/<host\b([^>]*)\/>/g)) {
		const attributes = new Map<string, string>()
		for (const attribute of match[1].matchAll(/([a-z]+)=(?:'([^']*)'|"([^"]*)")/gi)) {
			attributes.set(attribute[1], unescapeXml(attribute[2] ?? attribute[3] ?? ''))
		}
		const macAddress = attributes.get('mac')
		const ipAddress = attributes.get('ip')
		if (macAddress && ipAddress) leases.push({macAddress, ipAddress, name: attributes.get('name')})
	}
	return leases
}

export function dhcpHostXml({macAddress, ipAddress, name}: MachineDhcpLease) {
	return `<host mac='${escapeXml(macAddress)}'${name ? ` name='${escapeXml(name)}'` : ''} ip='${escapeXml(ipAddress)}'/>`
}

export function buildMachinePortForwardNftables(definitions: MachineDefinition[], lanInterface?: string) {
	const forwards = definitions
		.filter((definition) => (definition.network?.mode ?? 'nat') === 'nat')
		.flatMap((definition) =>
			definition.portForwards.map((forward) => ({
				...forward,
				ipAddress: machineIpAddressSchema.parse(definition.ipAddress),
			})),
		)
	if (forwards.length > 0 && !lanInterface) {
		throw new Error('[machine-lan-interface-unavailable]')
	}
	const preroutingRules = forwards.map(
		(forward) =>
			`iifname ${JSON.stringify(lanInterface)} fib daddr type local ${forward.protocol} dport ${forward.hostPort} counter dnat ip to ${forward.ipAddress}:${forward.guestPort}`,
	)
	const outputRules = forwards.map(
		(forward) =>
			`ip daddr != 127.0.0.0/8 fib daddr type local ${forward.protocol} dport ${forward.hostPort} counter dnat ip to ${forward.ipAddress}:${forward.guestPort}`,
	)
	return `table ip titan_machines {
	chain prerouting {
		type nat hook prerouting priority -110; policy accept;
		${preroutingRules.join('\n\t\t')}
	}
	chain output {
		type nat hook output priority -110; policy accept;
		${outputRules.join('\n\t\t')}
	}
}
`
}
