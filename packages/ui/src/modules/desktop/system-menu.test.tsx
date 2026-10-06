// @vitest-environment jsdom

import {act, type ReactElement, type ReactNode} from 'react'
import {createRoot, type Root} from 'react-dom/client'
import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest'

import {SystemMenu} from './system-menu'

const fixtures = vi.hoisted(() => ({
	role: 'owner',
	pending: false,
	confirm: vi.fn(),
	logout: vi.fn(),
	restart: vi.fn(),
	shutdown: vi.fn(),
}))
vi.mock('react-i18next', () => ({useTranslation: () => ({t: (key: string) => key})}))
vi.mock('@/trpc/trpc', () => ({
	trpcReact: {user: {get: {useQuery: () => ({data: {role: fixtures.role, name: 'Owner'}})}}},
}))
vi.mock('@/modules/auth/use-auth', () => ({useAuth: () => ({logout: fixtures.logout})}))
vi.mock('@/providers/global-system-state', () => ({
	useGlobalSystemState: () => ({
		restart: fixtures.restart,
		shutdown: fixtures.shutdown,
		isPowerActionPending: fixtures.pending,
	}),
}))
vi.mock('@/providers/confirmation', () => ({useConfirmation: () => fixtures.confirm}))
// Isolate our confirmation/permission logic from Radix's already-tested menu primitive.
vi.mock('@/components/ui/dropdown-menu', () => ({
	DropdownMenu: ({children}: {children: ReactNode}) => <div>{children}</div>,
	DropdownMenuTrigger: ({children}: {children: ReactElement}) => children,
	DropdownMenuContent: ({children}: {children: ReactNode}) => <div>{children}</div>,
	DropdownMenuItem: ({children, onSelect}: {children: ReactNode; onSelect: () => void}) => (
		<button onClick={onSelect}>{children}</button>
	),
	DropdownMenuLabel: ({children}: {children: ReactNode}) => <span>{children}</span>,
	DropdownMenuSeparator: () => <hr />,
}))
;(globalThis as {IS_REACT_ACT_ENVIRONMENT?: boolean}).IS_REACT_ACT_ENVIRONMENT = true
let root: Root | undefined
let container: HTMLDivElement
beforeEach(() => {
	fixtures.role = 'owner'
	fixtures.pending = false
	vi.clearAllMocks()
	container = document.createElement('div')
	document.body.appendChild(container)
	root = createRoot(container)
})
afterEach(() => {
	if (root) act(() => root?.unmount())
	document.body.replaceChildren()
})

function render() {
	act(() => root?.render(<SystemMenu />))
}
async function choose(label: string) {
	const button = [...container.querySelectorAll('button')].find((item) => item.textContent === label)
	expect(button).toBeDefined()
	await act(async () => button?.click())
}

describe('desktop system menu', () => {
	it.each(['logout', 'restart', 'shut-down'])('requires Yes before executing %s', async (label) => {
		let resolve: (result: {actionValue: string}) => void = () => {}
		fixtures.confirm.mockImplementation(
			() =>
				new Promise((done) => {
					resolve = done
				}),
		)
		render()
		await choose(label)
		expect(fixtures.confirm).toHaveBeenCalledWith(
			expect.objectContaining({
				actions: [
					{label: 'no', value: 'cancel', variant: 'default'},
					{label: 'yes', value: 'confirm', variant: 'destructive'},
				],
			}),
		)
		for (const action of [fixtures.logout, fixtures.restart, fixtures.shutdown]) expect(action).not.toHaveBeenCalled()
		await act(async () => resolve({actionValue: 'confirm'}))
		const expected = label === 'logout' ? fixtures.logout : label === 'restart' ? fixtures.restart : fixtures.shutdown
		expect(expected).toHaveBeenCalledOnce()
	})

	it('does nothing on No or dismissal', async () => {
		fixtures.confirm.mockRejectedValue('cancel')
		render()
		await choose('restart')
		await choose('shut-down')
		await choose('logout')
		for (const action of [fixtures.logout, fixtures.restart, fixtures.shutdown]) expect(action).not.toHaveBeenCalled()
	})

	it('offers members only logout and suppresses actions while power changes are pending', async () => {
		fixtures.role = 'member'
		render()
		expect(container.textContent).not.toContain('restart')
		expect(container.textContent).not.toContain('shut-down')
		fixtures.pending = true
		render()
		await choose('logout')
		expect(fixtures.confirm).not.toHaveBeenCalled()
		expect(container.querySelector('button[aria-label="desktop.system-menu"]')?.hasAttribute('disabled')).toBe(true)
	})
})
