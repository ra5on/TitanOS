import {access, stat} from 'node:fs/promises'
import {setTimeout as delay} from 'node:timers/promises'
import {afterAll, afterEach, beforeAll, beforeEach, describe, expect, test} from 'vitest'
import got from 'got'
import pRetry from 'p-retry'
import pWaitFor from 'p-wait-for'

import {createTestVm} from '../test-utilities/create-test-titand.js'

// Release validation must boot the image just built, never an older default image.
const image = process.env.TITAN_VM_IMAGE
if (!image)
	throw new Error('TITAN_VM_IMAGE must point to the current built TitanOS IMG for the automatic bridge VM tests')
await access(image)
if (!(await stat(image)).isFile()) throw new Error('TITAN_VM_IMAGE must be a built IMG file')

const bridgeName = 'titan-br0'
type TestVm = Awaited<ReturnType<typeof createTestVm>>
type NetworkInterface = Awaited<ReturnType<TestVm['client']['system']['getNetworkInterfaces']['query']>>[number]
type ExpectedNetwork = {
	id: string
	mac: string
	ip: string
	subnetPrefix: number
	gateway: string
	dns: string[]
}
type OsNetwork = {
	master: string | null
	links: {ifname: string; address: string}[]
	addresses: {ifname: string; addr_info: {family: string; local: string; prefixlen: number}[]}[]
	routes: {dst: string; gateway?: string; dev: string}[]
	profiles: string[]
	checkpoints: string
}

function freshVm() {
	let titand: TestVm
	let failed = false
	beforeAll(async () => {
		titand = await createTestVm({device: 'titan-home', image, memory: 4096, cores: 2})
		await titand.vm.powerOn()
		await titand.registerAndLogin()
	})
	afterAll(async () => await titand?.cleanup())
	afterEach(async ({task}) => {
		if (task.result?.state === 'fail') {
			failed = true
			// Retain the real daemon cause before the failed guest is destroyed.
			console.error(await titand.vm.sshAsRoot('journalctl -u titan -u NetworkManager --no-pager -n 120').catch(String))
		}
	})
	beforeEach(({skip}) => {
		if (failed) skip()
	})
	return () => titand
}

async function captureNetwork(titand: TestVm): Promise<ExpectedNetwork> {
	return pRetry(
		async () => {
			const interfaces = await titand.client.system.getNetworkInterfaces.query()
			expect(interfaces).toHaveLength(1)
			const iface = interfaces[0]
			expect(iface.type).toBe('ethernet')
			expect(iface.connected).toBe(true)
			expect(iface.id).toMatch(/^[a-zA-Z0-9_.-]{1,15}$/)
			expect(iface.mac).toMatch(/^(?:[0-9a-f]{2}:){5}[0-9a-f]{2}$/i)
			expect(iface.ipMethod).toBe('dhcp')
			expect(iface.ip).toBeTypeOf('string')
			expect(iface.subnetPrefix).toBeTypeOf('number')
			expect(iface.gateway).toBeTypeOf('string')
			expect(iface.dns?.length).toBeGreaterThan(0)
			return {
				id: iface.id,
				mac: iface.mac,
				ip: iface.ip!,
				subnetPrefix: iface.subnetPrefix!,
				gateway: iface.gateway!,
				dns: iface.dns!,
			}
		},
		{retries: 60, minTimeout: 500, maxTimeout: 500},
	)
}

// Read kernel/NetworkManager facts over SSH. All changes use public product RPCs.
async function readOsNetwork(titand: TestVm, device: string): Promise<OsNetwork> {
	if (!/^[a-zA-Z0-9_.-]{1,15}$/.test(device)) throw new Error('Invalid test network interface')
	const output = await titand.vm.sshAsRoot(`python3 - <<'PY'
import json
from pathlib import Path
import subprocess
def output(*args):
    return subprocess.check_output(args, text=True).strip()
master = Path('/sys/class/net/${device}/master')
links = json.loads(output('ip', '-json', 'link', 'show'))
addresses = json.loads(output('ip', '-json', 'address', 'show'))
routes = json.loads(output('ip', '-json', 'route', 'show', 'default'))
# Docker and libvirt can create unrelated bridges during daemon startup.
# Snapshot the physical LAN and its new controller, not asynchronous app networks.
interfaces = {'${device}', '${bridgeName}'}
profiles = output('nmcli', '--terse', '--fields', 'UUID,TYPE,DEVICE', 'connection', 'show').splitlines()
print(json.dumps({
    'master': master.resolve().name if master.exists() else None,
    'links': sorted([{'ifname': link['ifname'], 'address': link.get('address', '')} for link in links if link['ifname'] in interfaces], key=lambda link: link['ifname']),
    'addresses': sorted([{'ifname': entry['ifname'], 'addr_info': [{'family': address['family'], 'local': address['local'], 'prefixlen': address['prefixlen']} for address in entry['addr_info']]} for entry in addresses if entry['ifname'] in interfaces], key=lambda entry: entry['ifname']),
    'routes': [{key: route[key] for key in ('dst', 'gateway', 'dev') if key in route} for route in routes],
    'profiles': sorted(profile for profile in profiles if ':802-3-ethernet:' in profile or profile.endswith(':${bridgeName}')),
    'checkpoints': output('busctl', 'get-property', 'org.freedesktop.NetworkManager', '/org/freedesktop/NetworkManager', 'org.freedesktop.NetworkManager', 'Checkpoints'),
}))
PY`)
	return JSON.parse(output) as OsNetwork
}

function assertNetwork(iface: NetworkInterface | undefined, expected: ExpectedNetwork, method: 'dhcp' | 'static') {
	expect(iface).toMatchObject({
		id: expected.id,
		mac: expected.mac,
		type: 'ethernet',
		connected: true,
		ipMethod: method,
		ip: expected.ip,
		subnetPrefix: expected.subnetPrefix,
		gateway: expected.gateway,
		dns: expected.dns,
	})
}

async function assertBridgeNetwork(titand: TestVm, expected: ExpectedNetwork, method: 'dhcp' | 'static') {
	const iface = (await titand.client.system.getNetworkInterfaces.query()).find((entry) => entry.mac === expected.mac)
	assertNetwork(iface, expected, method)
	expect(await titand.client.system.getIpAddresses.query()).toContain(expected.ip)
	const osNetwork = await readOsNetwork(titand, expected.id)
	expect(osNetwork.master).toBe(bridgeName)
	expect(osNetwork.links.find((link) => link.ifname === bridgeName)?.address.toLowerCase()).toBe(
		expected.mac.toLowerCase(),
	)
	expect(osNetwork.addresses.find((entry) => entry.ifname === bridgeName)?.addr_info).toEqual(
		expect.arrayContaining([
			expect.objectContaining({family: 'inet', local: expected.ip, prefixlen: expected.subnetPrefix}),
		]),
	)
	expect(
		osNetwork.addresses
			.find((entry) => entry.ifname === expected.id)
			?.addr_info.filter((address) => address.family === 'inet'),
	).toEqual([])
	expect(osNetwork.routes).toEqual(
		expect.arrayContaining([expect.objectContaining({dst: 'default', gateway: expected.gateway, dev: bridgeName})]),
	)
	return osNetwork
}

async function requestStatus(titand: TestVm) {
	return got(`http://127.0.0.1:${titand.vm.httpPort}/manager-api/v1/system/update-status`, {
		retry: {limit: 0},
		timeout: {request: 2_000},
	}).json()
}

async function prepareBridge(titand: TestVm) {
	const prepared = await titand.client.machines.prepareBridge.mutate()
	expect(prepared.bridge).toBe(bridgeName)
	if (!('token' in prepared)) throw new Error('Fresh VM did not create a bridge confirmation token')
	expect(prepared.token).toMatch(/^[a-f0-9]{64}$/)
	expect(prepared.expiresAt).toBeGreaterThan(Date.now())
	expect(prepared.expiresAt).toBeLessThan(Date.now() + 135_000)
	return prepared
}

async function confirmBridge(titand: TestVm, token: string) {
	await pWaitFor(
		async () => {
			// A new HTTP request must cross the changed network before committing it.
			try {
				await requestStatus(titand)
			} catch {
				return false
			}
			let result: Awaited<ReturnType<TestVm['client']['machines']['confirmBridge']['mutate']>>
			try {
				result = await titand.client.machines.confirmBridge.mutate({token})
			} catch (error) {
				// A reconnect can race the request. Application errors must still fail the gate.
				if (error instanceof Error && /fetch failed|socket|ECONN|other side closed/i.test(error.message)) return false
				throw error
			}
			expect(result.bridge).toBe(bridgeName)
			expect(['preparing', 'ready']).toContain(result.state)
			return result.state === 'ready'
		},
		{interval: 500, timeout: 90_000},
	)
}

async function powerCycle(titand: TestVm) {
	await titand.vm.powerOff()
	await titand.vm.powerOn()
	await titand.login()
}

describe.sequential('Automatic VM bridge on a DHCP uplink', () => {
	const vm = freshVm()
	let before: ExpectedNetwork

	test('denies unauthenticated network inventory, preparation and confirmation without changing the NIC', async () => {
		before = await captureNetwork(vm())
		const original = await readOsNetwork(vm(), before.id)
		await expect(vm().unauthenticatedClient.machines.networks.query()).rejects.toMatchObject({
			data: {code: 'UNAUTHORIZED'},
		})
		await expect(vm().unauthenticatedClient.machines.prepareBridge.mutate()).rejects.toMatchObject({
			data: {code: 'UNAUTHORIZED'},
		})
		await expect(
			vm().unauthenticatedClient.machines.confirmBridge.mutate({token: 'a'.repeat(64)}),
		).rejects.toMatchObject({data: {code: 'UNAUTHORIZED'}})
		expect(await readOsNetwork(vm(), before.id)).toEqual(original)
	})

	test('offers automatic setup while read-only inventory leaves physical networking unchanged', async () => {
		const original = await readOsNetwork(vm(), before.id)
		expect(original.master).toBeNull()
		expect(original.links.some((entry) => entry.ifname === bridgeName)).toBe(false)
		expect(original.checkpoints).toBe('ao 0')
		for (let index = 0; index < 3; index++) {
			const inventory = await vm().client.machines.networks.query()
			expect(inventory.bridges).toEqual([])
			expect(inventory.automaticBridge).toMatchObject({available: true, interface: before.id})
		}
		expect(await readOsNetwork(vm(), before.id)).toEqual(original)
	})

	test('creates and confirms a bridge while preserving the DHCP address, MAC and gateway', async () => {
		const prepared = await prepareBridge(vm())
		await confirmBridge(vm(), prepared.token)
		await pRetry(() => assertBridgeNetwork(vm(), before, 'dhcp'), {retries: 30, minTimeout: 500, maxTimeout: 500})
		const inventory = await vm().client.machines.networks.query()
		expect(inventory.bridges).toContain(bridgeName)
		expect(inventory.automaticBridge.available).toBe(false)
		expect((await readOsNetwork(vm(), before.id)).checkpoints).toBe('ao 0')
		// Repeated setup uses the existing bridge and does not create more profiles.
		const original = await readOsNetwork(vm(), before.id)
		expect(await vm().client.machines.prepareBridge.mutate()).toEqual({bridge: bridgeName})
		expect(await readOsNetwork(vm(), before.id)).toEqual(original)
	})

	test('keeps the confirmed bridge and displayed DHCP configuration after a real power cycle', async () => {
		await powerCycle(vm())
		await pRetry(() => assertBridgeNetwork(vm(), before, 'dhcp'), {retries: 90, minTimeout: 1_000, maxTimeout: 1_000})
		expect((await vm().client.machines.networks.query()).bridges).toContain(bridgeName)
		expect((await readOsNetwork(vm(), before.id)).checkpoints).toBe('ao 0')
	})
})

describe.sequential('Unconfirmed automatic VM bridge rollback', () => {
	const vm = freshVm()
	let before: ExpectedNetwork
	let original: OsNetwork
	let token: string

	test('activates a provisional bridge with a real NetworkManager checkpoint but does not publish it as ready', async () => {
		before = await captureNetwork(vm())
		original = await readOsNetwork(vm(), before.id)
		const prepared = await prepareBridge(vm())
		token = prepared.token
		await pRetry(
			async () => {
				const pending = await assertBridgeNetwork(vm(), before, 'dhcp')
				expect(pending.checkpoints).toMatch(/^ao [1-9]/)
			},
			{retries: 60, minTimeout: 500, maxTimeout: 500},
		)
		const inventory = await vm().client.machines.networks.query()
		expect(inventory.bridges).not.toContain(bridgeName)
		expect(inventory.automaticBridge).toMatchObject({available: false, reason: 'busy'})
	})

	test('restores the original DHCP NIC and removes provisional profiles when confirmation expires', async () => {
		// Do not call confirmBridge: NetworkManager must restore the real NIC itself.
		await pWaitFor(
			async () => {
				try {
					await requestStatus(vm())
					const current = await readOsNetwork(vm(), before.id)
					return (
						current.master === null &&
						!current.links.some((entry) => entry.ifname === bridgeName) &&
						current.checkpoints === 'ao 0'
					)
				} catch {
					return false
				}
			},
			{interval: 1_000, timeout: 175_000},
		)
		assertNetwork(
			(await vm().client.system.getNetworkInterfaces.query()).find((entry) => entry.mac === before.mac),
			before,
			'dhcp',
		)
		expect((await readOsNetwork(vm(), before.id)).profiles).toEqual(original.profiles)
		await expect(vm().client.machines.confirmBridge.mutate({token})).rejects.toThrow(
			'machine-bridge-confirmation-expired',
		)
		expect((await vm().client.machines.networks.query()).automaticBridge.available).toBe(true)
	})

	test('retains the restored DHCP connection after a real power cycle', async () => {
		await powerCycle(vm())
		await pRetry(
			async () => {
				assertNetwork(
					(await vm().client.system.getNetworkInterfaces.query()).find((entry) => entry.mac === before.mac),
					before,
					'dhcp',
				)
				const current = await readOsNetwork(vm(), before.id)
				expect(current.master).toBeNull()
				expect(current.links.some((entry) => entry.ifname === bridgeName)).toBe(false)
				expect(current.profiles).toEqual(original.profiles)
			},
			{retries: 90, minTimeout: 1_000, maxTimeout: 1_000},
		)
	})
})

describe.sequential('Automatic VM bridge with a saved static IP', () => {
	const vm = freshVm()
	let before: ExpectedNetwork

	test('configures and confirms a static address through the normal network settings API', async () => {
		before = await captureNetwork(vm())
		const {mac, ip, subnetPrefix, gateway, dns} = before
		// Reuse the assigned values so QEMU's ordinary HTTP/SSH forwarding remains valid.
		const mutation = vm().client.system.setStaticIp.mutate({mac, ip, subnetPrefix, gateway, dns})
		const settled = mutation.then(
			() => ({success: true as const}),
			(error: unknown) => ({success: false as const, error}),
		)
		await delay(2_000)
		await pWaitFor(
			async () => {
				try {
					await requestStatus(vm())
					const iface = (await vm().client.system.getNetworkInterfaces.query()).find((entry) => entry.mac === mac)
					if (!iface?.connected || iface.ipMethod !== 'static' || iface.ip !== ip) return false
					await vm().client.system.confirmStaticIp.mutate({ip})
					return true
				} catch {
					return false
				}
			},
			{interval: 250, timeout: 25_000},
		)
		const result = await settled
		if (!result.success) throw result.error
		await pRetry(
			async () => {
				const iface = (await vm().client.system.getNetworkInterfaces.query()).find((entry) => entry.mac === mac)
				assertNetwork(iface, before, 'static')
				expect(iface?.configuredStaticSettings).toEqual({ip, subnetPrefix, gateway, dns})
			},
			{retries: 30, minTimeout: 500, maxTimeout: 500},
		)
	})

	test('moves the static address onto the bridge while physical NIC settings remain visible', async () => {
		const prepared = await prepareBridge(vm())
		await confirmBridge(vm(), prepared.token)
		await pRetry(() => assertBridgeNetwork(vm(), before, 'static'), {retries: 30, minTimeout: 500, maxTimeout: 500})
		const {ip, subnetPrefix, gateway, dns} = before
		const iface = (await vm().client.system.getNetworkInterfaces.query()).find((entry) => entry.mac === before.mac)
		expect(iface?.configuredStaticSettings).toEqual({ip, subnetPrefix, gateway, dns})
	})

	test('preserves bridge topology, static address and saved settings across a power cycle', async () => {
		await powerCycle(vm())
		await pRetry(
			async () => {
				await assertBridgeNetwork(vm(), before, 'static')
				const {ip, subnetPrefix, gateway, dns} = before
				expect(
					(await vm().client.system.getNetworkInterfaces.query()).find((entry) => entry.mac === before.mac)
						?.configuredStaticSettings,
				).toEqual({ip, subnetPrefix, gateway, dns})
			},
			{retries: 90, minTimeout: 1_000, maxTimeout: 1_000},
		)
	})

	test('clears the saved static IP through settings without removing the bridge', async () => {
		await vm().client.system.clearStaticIp.mutate({mac: before.mac})
		await pRetry(
			async () => {
				await assertBridgeNetwork(vm(), before, 'dhcp')
				expect(
					(await vm().client.system.getNetworkInterfaces.query()).find((entry) => entry.mac === before.mac)
						?.configuredStaticSettings,
				).toBeUndefined()
			},
			{retries: 60, minTimeout: 500, maxTimeout: 500},
		)
	})

	test('keeps DHCP on the existing bridge after clearing static settings and rebooting', async () => {
		await powerCycle(vm())
		await pRetry(
			async () => {
				await assertBridgeNetwork(vm(), before, 'dhcp')
				expect(
					(await vm().client.system.getNetworkInterfaces.query()).find((entry) => entry.mac === before.mac)
						?.configuredStaticSettings,
				).toBeUndefined()
			},
			{retries: 90, minTimeout: 1_000, maxTimeout: 1_000},
		)
	})
})
