import {beforeEach, describe, expect, test, vi} from 'vitest'

import type {Context} from '../server/trpc/context.js'

const mocks = vi.hoisted(() => ({
	getRecoveryStatus: vi.fn(),
	performRollback: vi.fn(),
	getUpdateStatus: vi.fn(),
	performUpdate: vi.fn(),
	afterResponse: vi.fn(),
	reboot: vi.fn(),
	shutdown: vi.fn(),
}))
vi.mock('./update.js', () => ({...mocks, getLatestRelease: vi.fn()}))
vi.mock('../server/run-after-response.js', () => ({runAfterResponse: mocks.afterResponse}))
vi.mock('./system.js', async (original) => ({
	...(await original<object>()),
	reboot: mocks.reboot,
	shutdown: mocks.shutdown,
}))

import routes, {setSystemStatus} from './routes.js'

const selection = 'a'.repeat(64)
const state = {
	current: {version: '2.0.4', name: 'TitanOS 2.0.4', slot: 'a', confirmed: true},
	previous: [{version: '2.0.3', name: 'TitanOS 2.0.3', slot: 'b', selection}],
	reason: '',
}
function caller(accountId?: string) {
	return routes.createCaller({
		transport: 'ws',
		principal: accountId ? {sessionId: 's', accountId, actor: 'account'} : undefined,
		logger: {verbose: vi.fn(), error: vi.fn()},
		user: {exists: async () => true},
		titand: {version: '2.0.4', auth: {validatePrincipal: vi.fn(async () => {})}, logger: {error: vi.fn()}},
	} as unknown as Context)
}

beforeEach(() => {
	vi.clearAllMocks()
	setSystemStatus('running')
	mocks.getUpdateStatus.mockReturnValue({running: false})
	mocks.getRecoveryStatus.mockResolvedValue(state)
	mocks.performRollback.mockResolvedValue(true)
})

describe('owner-only system recovery', () => {
	test.each([undefined, 'member'])('rejects %s for status and rollback before invoking a helper', async (accountId) => {
		const api = caller(accountId)
		const code = accountId ? 'FORBIDDEN' : 'UNAUTHORIZED'
		await expect(api.recoveryStatus()).rejects.toMatchObject({code})
		await expect(api.rollback({selection})).rejects.toMatchObject({code})
		expect(mocks.getRecoveryStatus).not.toHaveBeenCalled()
		expect(mocks.performRollback).not.toHaveBeenCalled()
	})

	test('does not offer a factory clone and rejects an outdated selected slot', async () => {
		mocks.getRecoveryStatus.mockResolvedValue({...state, previous: []})
		const api = caller('0')
		expect((await api.recoveryStatus()).previous).toEqual([])
		await expect(api.rollback({selection})).rejects.toMatchObject({code: 'CONFLICT'})
		expect(await api.status()).toBe('running')
		expect(mocks.afterResponse).not.toHaveBeenCalled()
		expect(mocks.performRollback).not.toHaveBeenCalled()
	})

	test('acknowledges a valid selection before native reboot and excludes competing power actions', async () => {
		const api = caller('0')
		await expect(api.rollback({selection})).resolves.toBe(true)
		expect(mocks.performRollback).not.toHaveBeenCalled()
		await expect(api.restart()).rejects.toMatchObject({code: 'CONFLICT'})
		await expect(api.update()).rejects.toMatchObject({code: 'CONFLICT'})
		expect(mocks.reboot).not.toHaveBeenCalled()
		expect(mocks.performUpdate).not.toHaveBeenCalled()
		mocks.afterResponse.mock.calls[0][1]()
		await vi.waitFor(() => expect(mocks.performRollback).toHaveBeenCalledWith(expect.anything(), selection))
	})

	test('acknowledges a verified update before closing its own server', async () => {
		mocks.performUpdate.mockResolvedValue(true)
		const api = caller('0')
		await expect(api.update()).resolves.toBe(true)
		expect(await api.status()).toBe('updating')
		expect(mocks.afterResponse).toHaveBeenCalledOnce()
		expect(mocks.reboot).not.toHaveBeenCalled()
	})

	test('returns to running if the rechecked native operation fails', async () => {
		mocks.performRollback.mockResolvedValue(false)
		const api = caller('0')
		await api.rollback({selection})
		mocks.afterResponse.mock.calls[0][1]()
		await vi.waitFor(async () => expect(await api.status()).toBe('running'))
	})

	test('reserves the action while the selected slot is being checked', async () => {
		let resolve!: (value: typeof state) => void
		mocks.getRecoveryStatus.mockReturnValue(
			new Promise((done) => {
				resolve = done
			}),
		)
		const api = caller('0')
		const first = api.rollback({selection})
		await vi.waitFor(() => expect(mocks.getRecoveryStatus).toHaveBeenCalledOnce())
		await expect(api.shutdown()).rejects.toMatchObject({code: 'CONFLICT'})
		await expect(api.rollback({selection})).rejects.toMatchObject({code: 'CONFLICT'})
		resolve(state)
		await expect(first).resolves.toBe(true)
	})
})
