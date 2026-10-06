import {describe, expect, test} from 'vitest'

import {machineSettingsRequireShutdown, type MachineSettingsThatRequireShutdown} from './settings'

const current: MachineSettingsThatRequireShutdown = {
	cores: 2,
	memoryGb: 4,
	diskSizeGb: 40,
	firmware: 'uefi',
	diskBus: 'virtio',
}

describe('machineSettingsRequireShutdown', () => {
	test('detects every machine setting that needs a shutdown to take effect', () => {
		expect(machineSettingsRequireShutdown(current, current)).toBe(false)
		expect(machineSettingsRequireShutdown(current, {...current, cores: 4})).toBe(true)
		expect(machineSettingsRequireShutdown(current, {...current, memoryGb: 8})).toBe(true)
		expect(machineSettingsRequireShutdown(current, {...current, diskSizeGb: 80})).toBe(true)
		expect(machineSettingsRequireShutdown(current, {...current, firmware: 'bios'})).toBe(true)
		expect(machineSettingsRequireShutdown(current, {...current, diskBus: 'sata'})).toBe(true)
	})

	test('treats an omitted disk bus as the default VirtIO bus', () => {
		expect(machineSettingsRequireShutdown({...current, diskBus: undefined}, current)).toBe(false)
	})

	test('detects network mode and bridge changes while preserving the legacy NAT default', () => {
		expect(machineSettingsRequireShutdown(current, {...current, network: {mode: 'nat'}})).toBe(false)
		expect(machineSettingsRequireShutdown(current, {...current, network: {mode: 'host-only'}})).toBe(true)
		expect(
			machineSettingsRequireShutdown(
				{...current, network: {mode: 'bridge', bridge: 'br0'}},
				{...current, network: {mode: 'bridge', bridge: 'br1'}},
			),
		).toBe(true)
	})
})
