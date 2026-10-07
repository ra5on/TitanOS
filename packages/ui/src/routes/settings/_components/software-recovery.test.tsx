// @vitest-environment jsdom
import {act, type ReactNode} from 'react'
import {createRoot} from 'react-dom/client'
import {afterEach, beforeEach, describe, expect, test, vi} from 'vitest'

import {SoftwareRecovery} from './software-recovery'

const fixture = vi.hoisted(() => ({
	role: 'owner',
	previous: [] as {version: string; name: string; slot: string; selection: string}[],
	reason: 'Noch kein vorheriger bestätigter Systemstand vorhanden.',
	rollback: vi.fn(),
	refetch: vi.fn(),
	query: vi.fn(),
}))
vi.mock('@/trpc/trpc', () => ({
	trpcReact: {
		user: {get: {useQuery: () => ({data: {role: fixture.role}})}},
		system: {
			recoveryStatus: {
				useQuery: (_input: unknown, options: unknown) => {
					fixture.query(options)
					return {
						data: {previous: fixture.previous, reason: fixture.reason},
						isLoading: false,
						refetch: fixture.refetch,
					}
				},
			},
		},
	},
}))
vi.mock('@/providers/global-system-state', () => ({
	useGlobalSystemState: () => ({rollback: fixture.rollback, isPowerActionPending: false}),
}))
vi.mock('@/components/ui/button', () => ({
	Button: ({children, variant, size, ...props}: {children: ReactNode; variant?: string; size?: string}) => (
		<button {...props}>{children}</button>
	),
}))
vi.mock('@/components/ui/dialog', () => {
	const Wrapper = ({children}: {children: ReactNode}) => <>{children}</>
	return {
		Dialog: ({open, children}: {open: boolean; children: ReactNode}) =>
			open ? <div role='dialog'>{children}</div> : null,
		DialogContent: Wrapper,
		DialogHeader: Wrapper,
		DialogTitle: Wrapper,
		DialogDescription: Wrapper,
	}
})
;(globalThis as {IS_REACT_ACT_ENVIRONMENT?: boolean}).IS_REACT_ACT_ENVIRONMENT = true

let container: HTMLDivElement
let root: ReturnType<typeof createRoot>
const render = () => act(async () => root.render(<SoftwareRecovery />))
const button = (text: string) =>
	[...container.querySelectorAll('button')].find((element) => element.textContent === text)!
beforeEach(() => {
	fixture.role = 'owner'
	fixture.previous = []
	fixture.rollback.mockClear()
	fixture.query.mockClear()
	container = document.createElement('div')
	document.body.appendChild(container)
	root = createRoot(container)
})
afterEach(async () => {
	await act(async () => root.unmount())
	container.remove()
})

describe('system recovery settings', () => {
	test('explains the fresh-install state without offering a clone or a restore action', async () => {
		await render()
		expect(container.textContent).toContain('Noch kein vorheriger bestätigter Systemstand vorhanden.')
		expect(container.querySelector('select')).toBeNull()
		expect(button('Wiederherstellen')).toBeUndefined()
	})

	test('shows a signed selection and requires Yes on the left, No on the right', async () => {
		fixture.previous = [{version: '2.0.3', name: 'TitanOS 2.0.3', slot: 'b', selection: 'a'.repeat(64)}]
		await render()
		expect(container.querySelector('option')?.textContent).toContain('TitanOS 2.0.3')
		await act(async () => button('Wiederherstellen').click())
		const actions = [...container.querySelector('[role="dialog"]')!.querySelectorAll('button')]
		expect(actions.map((item) => item.textContent)).toEqual(['Ja', 'Nein'])
		await act(async () => button('Nein').click())
		expect(fixture.rollback).not.toHaveBeenCalled()
		await act(async () => button('Wiederherstellen').click())
		await act(async () => button('Ja').click())
		expect(fixture.rollback).toHaveBeenCalledExactlyOnceWith('a'.repeat(64))
	})

	test('closes confirmation when its selected systemstand becomes stale', async () => {
		fixture.previous = [{version: '2.0.3', name: 'TitanOS 2.0.3', slot: 'b', selection: 'a'.repeat(64)}]
		await render()
		await act(async () => button('Wiederherstellen').click())
		fixture.previous = [{version: '2.0.2', name: 'TitanOS 2.0.2', slot: 'b', selection: 'b'.repeat(64)}]
		await render()
		expect(container.querySelector('[role="dialog"]')).toBeNull()
		expect(button('Wiederherstellen').disabled).toBe(true)
		expect(fixture.rollback).not.toHaveBeenCalled()
	})

	test('members cannot see recovery or query privileged slot state', async () => {
		fixture.role = 'member'
		await render()
		expect(container.textContent).toBe('')
		expect(fixture.query).toHaveBeenCalledWith(expect.objectContaining({enabled: false}))
	})
})
