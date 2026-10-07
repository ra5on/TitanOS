// @vitest-environment jsdom

import {act} from 'react'
import {createRoot, type Root} from 'react-dom/client'
import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest'

import {useRaidProgress} from './use-raid-progress'

const fixtures = vi.hoisted(() => ({role: undefined as string | undefined, subscriptions: vi.fn()}))
vi.mock('react-i18next', () => ({useTranslation: () => ({t: (key: string) => key})}))
vi.mock('@/components/ui/toast', () => ({toast: {error: vi.fn()}}))
vi.mock('../providers/pending-operation-context', () => ({
	usePendingRaidOperation: () => ({setOperationError: vi.fn()}),
}))
vi.mock('@/trpc/trpc', () => ({
	trpcReact: {
		user: {get: {useQuery: () => ({data: fixtures.role ? {role: fixtures.role} : undefined})}},
		eventBus: {listen: {useSubscription: fixtures.subscriptions}},
	},
}))
;(globalThis as {IS_REACT_ACT_ENVIRONMENT?: boolean}).IS_REACT_ACT_ENVIRONMENT = true
let root: Root
let container: HTMLDivElement
function Probe() {
	const operation = useRaidProgress()
	return <span>{operation?.type ?? 'idle'}</span>
}
function render() {
	act(() => root.render(<Probe />))
}
beforeEach(() => {
	vi.clearAllMocks()
	fixtures.role = undefined
	container = document.createElement('div')
	document.body.appendChild(container)
	root = createRoot(container)
})
afterEach(() => {
	act(() => root.unmount())
	document.body.replaceChildren()
})

describe('RAID subscription permissions', () => {
	it.each([undefined, 'member', 'owner'])('only subscribes with a confirmed owner role (%s)', (role) => {
		fixtures.role = role
		render()
		expect(fixtures.subscriptions).toHaveBeenCalledTimes(5)
		for (const [request, options] of fixtures.subscriptions.mock.calls) {
			expect(request.event).toMatch(/^raid:/)
			expect(options.enabled).toBe(role === 'owner')
		}
	})

	it('drops owner progress immediately when the account becomes a member', () => {
		fixtures.role = 'owner'
		render()
		const [, expansion] = fixtures.subscriptions.mock.calls.find(
			([request]) => request.event === 'raid:expansion-progress',
		)!
		act(() => expansion.onData({state: 'expanding', progress: 10}))
		expect(container.textContent).toBe('expansion')
		fixtures.role = 'member'
		render()
		expect(container.textContent).toBe('idle')
	})
})
