import {describe, expect, test} from 'vitest'

import {canChangeMachineNetwork, machineNetworkAvailable, sameMachineNetwork} from './network-settings'

describe('machine network selection', () => {
	test('compares actual interface choices rather than just the mode', () => {
		expect(sameMachineNetwork({mode: 'nat'}, {mode: 'nat'})).toBe(true)
		expect(sameMachineNetwork({mode: 'nat'}, {mode: 'host-only'})).toBe(false)
		expect(sameMachineNetwork({mode: 'bridge', bridge: 'br0'}, {mode: 'bridge', bridge: 'br1'})).toBe(false)
		expect(sameMachineNetwork({mode: 'bridge', bridge: 'br0'}, {mode: 'bridge', bridge: 'br0'})).toBe(true)
	})
	test('requires an existing bridge but permits owned NAT and Host-only without a LAN bridge', () => {
		expect(machineNetworkAvailable({mode: 'nat'}, [])).toBe(true)
		expect(machineNetworkAvailable({mode: 'host-only'}, [])).toBe(true)
		expect(machineNetworkAvailable({mode: 'bridge', bridge: 'br0'}, ['br0'])).toBe(true)
		expect(machineNetworkAvailable({mode: 'bridge', bridge: 'br0'}, ['br1'])).toBe(false)
	})
	test('locks changes while a machine is running or its install/setup is pending', () => {
		expect(canChangeMachineNetwork({state: 'stopped', networkChangeAllowed: true})).toBe(true)
		for (const state of [
			'running',
			'suspended',
			'installing',
			'starting',
			'stopping',
			'restarting',
			'error',
		] as const) {
			expect(canChangeMachineNetwork({state, networkChangeAllowed: true})).toBe(false)
		}
		expect(canChangeMachineNetwork({state: 'stopped', networkChangeAllowed: false})).toBe(false)
	})
})
