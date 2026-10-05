import {beforeEach, describe, expect, test, vi} from 'vitest'

const execute = vi.hoisted(() => vi.fn())
vi.mock('execa', () => ({$: execute}))

const release = {version: '2.0.0', name: 'TitanOS 2.0.0', releaseNotes: 'Signiertes Systemupdate'}
const instance = () => ({version: '2.0.0-titan.3', store: {get: vi.fn().mockResolvedValue('alpha')}, logger: {error: vi.fn()}})

beforeEach(() => {
	vi.resetModules()
	execute.mockReset()
})

describe('signed local Titan update checks', () => {
	test('concurrent checks share one helper request and cache the verified result', async () => {
		const {getLatestRelease} = await import('./update.js')
		let complete!: (value: {stdout: string}) => void
		execute.mockReturnValue(new Promise((resolve) => { complete = resolve }))
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

	test('legacy persisted channels cannot opt the new updater into experimental releases', async () => {
		const {getLatestRelease} = await import('./update.js')
		const system = instance() as unknown as Parameters<typeof getLatestRelease>[0]
		execute.mockResolvedValue({stdout: JSON.stringify(release)})
		await getLatestRelease(system)
		vi.mocked(system.store.get).mockResolvedValue('beta')
		await getLatestRelease(system)
		expect(execute).toHaveBeenCalledTimes(1)
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
})
