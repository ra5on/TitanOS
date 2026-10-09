import {beforeEach, describe, expect, test, vi} from 'vitest'

const execute = vi.hoisted(() => vi.fn())
vi.mock('execa', () => ({$: execute}))

const release = {version: '2.0.2', name: 'TitanOS 2.0.2', releaseNotes: 'Signiertes Systemupdate'}
const instance = () => ({
	version: '2.0.1',
	systemBootConfirmed: true,
	store: {get: vi.fn().mockResolvedValue('stable')},
	logger: {error: vi.fn(), log: vi.fn()},
})

beforeEach(() => {
	vi.resetModules()
	execute.mockReset()
})

describe('signed local Titan update checks', () => {
	test('concurrent checks share one helper request and cache the verified result', async () => {
		const {getLatestRelease} = await import('./update.js')
		let complete!: (value: {stdout: string}) => void
		execute.mockReturnValue(
			new Promise((resolve) => {
				complete = resolve
			}),
		)
		const system = instance() as unknown as Parameters<typeof getLatestRelease>[0]
		const first = getLatestRelease(system)
		const second = getLatestRelease(system)
		await vi.waitFor(() => expect(execute).toHaveBeenCalledTimes(1))
		complete({stdout: JSON.stringify(release)})
		expect(await first).toEqual(release)
		expect(await second).toEqual(release)
		expect(await getLatestRelease(system)).toEqual(release)
		expect(execute).toHaveBeenCalledTimes(1)
	})

	test('a failed shared request is cleared so a later check can retry', async () => {
		const {getLatestRelease} = await import('./update.js')
		const system = instance() as unknown as Parameters<typeof getLatestRelease>[0]
		execute.mockRejectedValueOnce(new Error('Updatekanal nicht erreichbar'))
		const results = await Promise.allSettled([getLatestRelease(system), getLatestRelease(system)])
		expect(results.map((result) => result.status)).toEqual(['rejected', 'rejected'])
		expect(execute).toHaveBeenCalledTimes(1)
		execute.mockResolvedValueOnce({stdout: JSON.stringify(release)})
		expect(await getLatestRelease(system)).toEqual(release)
		expect(execute).toHaveBeenCalledTimes(2)
	})

	test('system update checks always use the stable helper channel', async () => {
		const {getLatestRelease} = await import('./update.js')
		const system = instance() as unknown as Parameters<typeof getLatestRelease>[0]
		execute.mockResolvedValue({stdout: JSON.stringify(release)})
		await getLatestRelease(system)
		await getLatestRelease(system)
		expect(execute).toHaveBeenCalledTimes(1)
		expect(system.store.get).not.toHaveBeenCalled()
		expect(execute.mock.calls[0].slice(-1)).toEqual(['stable'])
	})

	test('no offered update does not invoke the installer', async () => {
		const {performUpdate, getUpdateStatus} = await import('./update.js')
		const system = instance() as unknown as Parameters<typeof performUpdate>[0]
		execute.mockResolvedValue({stdout: JSON.stringify({...release, version: system.version})})
		expect(await performUpdate(system)).toBe(false)
		expect(execute).toHaveBeenCalledTimes(1)
		expect(getUpdateStatus()).toMatchObject({running: false, error: 'Kein neueres Titan-Systemupdate verfügbar'})
	})
	test('an early boot update never races the health confirmation helper', async () => {
		const {performUpdate, getUpdateStatus} = await import('./update.js')
		const system = {...instance(), systemBootConfirmed: false} as never
		expect(await performUpdate(system)).toBe(false)
		expect(execute).not.toHaveBeenCalled()
		expect(getUpdateStatus()).toMatchObject({
			running: false,
			error: 'Der Systemstart wird noch bestätigt. Bitte kurz warten.',
		})
	})
	test('healthy boot is exposed only after confirmation has finished', async () => {
		const {commitOsPartition} = await import('./system.js')
		let finish!: () => void
		execute.mockReturnValue(
			new Promise<void>((resolve) => {
				finish = resolve
			}),
		)
		const system = {...instance(), systemBootConfirmed: false, port: 3001}
		const confirming = commitOsPartition(system as never)
		expect(system.systemBootConfirmed).toBe(false)
		finish()
		expect(await confirming).toBe(true)
		expect(system.systemBootConfirmed).toBe(true)
	})
})

describe('signed local system recovery', () => {
	const state = {
		current: {version: '2.0.1', name: 'TitanOS 2.0.1', slot: 'a', confirmed: true},
		previous: [{version: '2.0.0', name: 'TitanOS 2.0.0', slot: 'b', selection: 'a'.repeat(64)}],
		reason: '',
	}
	test('passes only the updater-validated previous slot to the UI', async () => {
		const {getRecoveryStatus} = await import('./update.js')
		execute.mockResolvedValue({stdout: JSON.stringify(state)})
		expect(await getRecoveryStatus(instance() as never)).toEqual(state)
		expect(execute.mock.calls[0].slice(-1)).toEqual(['2.0.1'])
	})
	test('a native default slot is not advertised as ready before this daemon confirms startup', async () => {
		const {getRecoveryStatus, performRollback} = await import('./update.js')
		execute.mockResolvedValue({stdout: JSON.stringify(state)})
		const system = {...instance(), systemBootConfirmed: false} as never
		expect(await getRecoveryStatus(system)).toMatchObject({current: {...state.current, confirmed: false}, previous: []})
		execute.mockClear()
		expect(await performRollback(system, state.previous[0].selection)).toBe(false)
		expect(execute).not.toHaveBeenCalled()
	})

	test.each([
		{...state, current: {...state.current, version: '8.0.0'}},
		{...state, previous: [{...state.previous[0], slot: 'a'}]},
		{...state, previous: [{...state.previous[0], selection: '../evil'}]},
		{...state, previous: [state.previous[0], state.previous[0]]},
	])('rejects malformed recovery output', async (value) => {
		const {getRecoveryStatus} = await import('./update.js')
		execute.mockResolvedValue({stdout: JSON.stringify(value)})
		await expect(getRecoveryStatus(instance() as never)).rejects.toThrow('Ungültige Antwort')
	})

	test('serializes rollback against updates and preserves an operation failure', async () => {
		const {performRollback, performUpdate, getUpdateStatus} = await import('./update.js')
		let fail!: (error: Error) => void
		execute.mockReturnValue(
			new Promise((_resolve, reject) => {
				fail = reject
			}),
		)
		const system = instance() as never
		const pending = performRollback(system, state.previous[0].selection)
		await expect(performUpdate(system)).rejects.toThrow('läuft bereits')
		fail(new Error('Native restart failed'))
		expect(await pending).toBe(false)
		expect(getUpdateStatus()).toMatchObject({running: false, error: 'Native restart failed'})
	})
})
