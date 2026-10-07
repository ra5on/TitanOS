// @vitest-environment jsdom

import {act} from 'react'
import {createRoot, type Root} from 'react-dom/client'
import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest'

import {Prefetcher} from './prefetch'

const fixtures = vi.hoisted(() => ({
	user: undefined as {userId: string; role: string; homePath: string} | undefined,
	requests: [] as {path: string; input: unknown}[],
}))
vi.mock('@tanstack/react-query', () => ({useIsFetching: () => 0, useQueryClient: () => ({prefetchQuery: vi.fn()})}))
vi.mock('@/features/app-store/data/storefront-query', () => ({storefrontQueryOptions: () => ({})}))
vi.mock('@/features/files/utils/last-files-path', () => ({getLastFilesPath: () => undefined}))
vi.mock('@/features/files/hooks/use-navigate', () => ({toFsPath: (path: string) => path}))
vi.mock('./wallpaper', () => ({wallpapers: [], getWallpaperAvifUrl: () => ''}))
vi.mock('@/trpc/trpc', () => {
	const query = (parts: string[] = []): unknown =>
		new Proxy(() => {}, {
			get(_target, prop) {
				if (prop === 'fetch' || prop === 'prefetch')
					return async (input: unknown) => {
						const path = parts.join('.')
						fixtures.requests.push({path, input})
						if (path === 'user.get') return fixtures.user
						if (path === 'files.viewPreferences') return {sortBy: 'name', sortOrder: 'ascending'}
						return {}
					}
				return query([...parts, String(prop)])
			},
		})
	return {
		trpcReact: {
			useUtils: () => query(),
			user: {isLoggedIn: {useQuery: () => ({data: true})}, get: {useQuery: () => ({data: fixtures.user})}},
		},
	}
})
;(globalThis as {IS_REACT_ACT_ENVIRONMENT?: boolean}).IS_REACT_ACT_ENVIRONMENT = true
let root: Root
let container: HTMLDivElement
function render() {
	act(() => root.render(<Prefetcher />))
}
async function advance() {
	await act(async () => vi.advanceTimersByTimeAsync(600))
}
beforeEach(() => {
	vi.useFakeTimers()
	fixtures.requests = []
	fixtures.user = undefined
	container = document.createElement('div')
	document.body.appendChild(container)
	root = createRoot(container)
})
afterEach(() => {
	act(() => root.unmount())
	document.body.replaceChildren()
	vi.useRealTimers()
})

describe('role-aware prefetch', () => {
	it('waits for account hydration, then warms only member routes and their home', async () => {
		render()
		await advance()
		expect(fixtures.requests).toEqual([])
		fixtures.user = {userId: 'Alice', role: 'member', homePath: '/Users/Alice'}
		render()
		await advance()
		const paths = fixtures.requests.map((request) => request.path)
		expect(paths).toContain('files.favorites')
		expect(paths).not.toContain('hardware.raid.getStatus')
		expect(paths).not.toContain('apps.getTorEnabled')
		expect(paths).not.toContain('machines.osImages')
		expect(paths).not.toContain('system.getReleaseChannel')
		expect(fixtures.requests.find((request) => request.path === 'files.list')?.input).toMatchObject({
			path: '/Users/Alice',
		})
	})

	it('keeps owner device and system warming', async () => {
		fixtures.user = {userId: '0', role: 'owner', homePath: '/Home'}
		render()
		await advance()
		expect(fixtures.requests.map((request) => request.path)).toContain('hardware.raid.getStatus')
		expect(fixtures.requests.find((request) => request.path === 'files.list')?.input).toMatchObject({path: '/Home'})
	})
})
