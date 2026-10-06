import {describe, expect, test, vi} from 'vitest'

import {prepareAutomaticBridge} from './prepare-bridge'

function scenario() {
	const controller = new AbortController()
	let time = 1_000
	return {
		controller,
		signal: controller.signal,
		now: () => time,
		wait: async (ms: number) => {
			time += ms
		},
		prepare: vi.fn(async (_signal: AbortSignal) => ({bridge: 'titan-br0', token: 'ticket', expiresAt: time + 120_000})),
		confirm: vi.fn(
			async (_token: string, _signal: AbortSignal): Promise<{state: 'preparing' | 'ready'; bridge: string}> => ({
				state: 'ready',
				bridge: 'titan-br0',
			}),
		),
	}
}

describe('automatic bridge browser confirmation', () => {
	test('existing bridges return without waiting for a network change', async () => {
		const s = scenario()
		const prepare = vi.fn(async () => ({bridge: 'br0'}))
		expect(await prepareAutomaticBridge({...s, prepare})).toBe('br0')
		expect(s.confirm).not.toHaveBeenCalled()
	})
	test('waits for activation and retries a dropped HTTP connection before choosing the bridge', async () => {
		const s = scenario()
		s.confirm.mockRejectedValueOnce(new TypeError('Failed to fetch'))
		s.confirm.mockResolvedValueOnce({state: 'preparing', bridge: 'titan-br0'})
		expect(await prepareAutomaticBridge(s)).toBe('titan-br0')
		expect(s.prepare).toHaveBeenCalledOnce()
		expect(s.confirm).toHaveBeenCalledTimes(3)
		expect(s.confirm.mock.calls.every(([token]) => token === 'ticket')).toBe(true)
	})
	test.each(['UNAUTHORIZED', 'FORBIDDEN', 'CONFLICT'])('stops immediately on a %s server refusal', async (code) => {
		const s = scenario()
		const error = Object.assign(new Error('Server refusal'), {data: {code}})
		s.confirm.mockRejectedValue(error)
		await expect(prepareAutomaticBridge(s)).rejects.toBe(error)
		expect(s.confirm).toHaveBeenCalledOnce()
	})
	test('does not loop on an expired checkpoint or retry preparation side effects', async () => {
		const s = scenario()
		s.confirm.mockRejectedValue(new Error('[machine-bridge-confirmation-expired]'))
		await expect(prepareAutomaticBridge(s)).rejects.toThrow('confirmation-expired')
		expect(s.prepare).toHaveBeenCalledOnce()
		expect(s.confirm).toHaveBeenCalledOnce()
	})
	test('bounds reconnect attempts before NetworkManager checkpoint timeout', async () => {
		const s = scenario()
		s.confirm.mockRejectedValue(new TypeError('Failed to fetch'))
		await expect(prepareAutomaticBridge(s)).rejects.toThrow('[machine-bridge-timeout]')
		expect(s.now()).toBe(91_000)
		expect(s.confirm).toHaveBeenCalledTimes(90)
	})
	test('honors a shorter backend expiry', async () => {
		const s = scenario()
		s.prepare.mockResolvedValue({bridge: 'titan-br0', token: 'ticket', expiresAt: 4_000})
		s.confirm.mockRejectedValue(new TypeError('Failed to fetch'))
		await expect(prepareAutomaticBridge(s)).rejects.toThrow('timeout')
		expect(s.confirm).toHaveBeenCalledTimes(3)
	})
	test('leaving the screen cancels confirmation so the daemon can roll back', async () => {
		const s = scenario()
		s.confirm.mockRejectedValue(new TypeError('Failed to fetch'))
		await expect(prepareAutomaticBridge({...s, wait: async () => s.controller.abort()})).rejects.toThrow('cancelled')
		expect(s.confirm).toHaveBeenCalledOnce()
	})
	test('an already cancelled selection never changes the host network', async () => {
		const s = scenario()
		s.controller.abort()
		await expect(prepareAutomaticBridge(s)).rejects.toThrow('cancelled')
		expect(s.prepare).not.toHaveBeenCalled()
	})
})
