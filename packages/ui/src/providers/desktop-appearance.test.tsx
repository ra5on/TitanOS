// @vitest-environment jsdom

import {act} from 'react'
import {createRoot, type Root} from 'react-dom/client'
import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest'

import {DesktopAppearanceProvider, normalizeDesktopTransparency, useDesktopAppearance} from './desktop-appearance'

const fixtures = vi.hoisted(() => ({
	user: {userId: '0', desktopTransparency: 75} as {userId: string; desktopTransparency: number} | undefined,
	mutateAsync: vi.fn(),
	setData: vi.fn(),
}))
vi.mock('@/trpc/trpc', () => ({
	trpcReact: {
		user: {
			get: {useQuery: () => ({data: fixtures.user})},
			set: {useMutation: () => ({mutateAsync: fixtures.mutateAsync, isPending: false})},
		},
		useUtils: () => ({user: {get: {setData: fixtures.setData}}}),
	},
}))
;(globalThis as {IS_REACT_ACT_ENVIRONMENT?: boolean}).IS_REACT_ACT_ENVIRONMENT = true
let root: Root
let container: HTMLDivElement
let controls: ReturnType<typeof useDesktopAppearance>
function Probe() {
	controls = useDesktopAppearance()
	return (
		<span>
			{controls.transparency}:{String(controls.saveFailed)}
		</span>
	)
}
function render() {
	act(() =>
		root.render(
			<DesktopAppearanceProvider>
				<Probe />
			</DesktopAppearanceProvider>,
		),
	)
}
async function advance() {
	await act(async () => vi.advanceTimersByTimeAsync(400))
}
beforeEach(() => {
	vi.useFakeTimers()
	fixtures.user = {userId: '0', desktopTransparency: 75}
	vi.clearAllMocks()
	fixtures.mutateAsync.mockResolvedValue(true)
	fixtures.setData.mockImplementation((_key, update) => {
		fixtures.user = update(fixtures.user)
	})
	container = document.createElement('div')
	document.body.appendChild(container)
	root = createRoot(container)
	render()
})
afterEach(() => {
	act(() => root.unmount())
	document.body.replaceChildren()
	vi.useRealTimers()
})

describe('per-account desktop transparency', () => {
	it('hydrates a pending account query without reading an absent draft', () => {
		fixtures.user = undefined
		render()
		expect(container.textContent).toBe('75:false')
		fixtures.user = {userId: '0', desktopTransparency: 60}
		render()
		expect(container.textContent).toBe('60:false')
		expect(document.documentElement.style.getPropertyValue('--desktop-glass-opacity')).toBe('0.4')
	})

	it('previews immediately and coalesces slider movements before persisting', async () => {
		act(() => {
			controls.setTransparency(20)
			controls.setTransparency(100)
		})
		expect(container.textContent).toBe('100:false')
		expect(document.documentElement.style.getPropertyValue('--desktop-glass-opacity')).toBe('0')
		expect(fixtures.mutateAsync).not.toHaveBeenCalled()
		await advance()
		expect(fixtures.mutateAsync).toHaveBeenCalledExactlyOnceWith({desktopTransparency: 100})
		expect(fixtures.user?.desktopTransparency).toBe(100)
	})

	it('restores the default and persists a fully opaque endpoint', async () => {
		act(() => controls.setTransparency(0))
		expect(document.documentElement.style.getPropertyValue('--desktop-glass-opacity')).toBe('1')
		await advance()
		await act(async () => controls.resetTransparency())
		expect(fixtures.mutateAsync).toHaveBeenLastCalledWith({desktopTransparency: 75})
		expect(document.documentElement.style.getPropertyValue('--desktop-glass-opacity')).toBe('0.25')
	})

	it('keeps preview and reports failed saves, then recovers on reset', async () => {
		fixtures.mutateAsync.mockRejectedValueOnce(new Error('Offline'))
		act(() => controls.setTransparency(35))
		await advance()
		expect(container.textContent).toBe('35:true')
		await act(async () => controls.resetTransparency())
		expect(container.textContent).toBe('75:false')
	})

	it('cancels unsent preferences when changing account', async () => {
		act(() => controls.setTransparency(95))
		fixtures.user = {userId: 'Alice', desktopTransparency: 30}
		render()
		await advance()
		expect(fixtures.mutateAsync).not.toHaveBeenCalled()
		expect(container.textContent).toBe('30:false')
	})

	it('serializes writes so the final slider choice wins', async () => {
		let finish: (value: boolean) => void = () => {}
		fixtures.mutateAsync.mockImplementationOnce(
			() =>
				new Promise((resolve) => {
					finish = resolve
				}),
		)
		act(() => controls.setTransparency(20))
		await advance()
		act(() => controls.setTransparency(80))
		await advance()
		expect(fixtures.mutateAsync).toHaveBeenCalledTimes(1)
		await act(async () => {
			finish(true)
			await Promise.resolve()
		})
		expect(fixtures.mutateAsync).toHaveBeenCalledTimes(2)
		expect(fixtures.mutateAsync).toHaveBeenLastCalledWith({desktopTransparency: 80})
		expect(fixtures.user?.desktopTransparency).toBe(80)
	})

	it('does not send queued writes after switching accounts', async () => {
		let finish: (value: boolean) => void = () => {}
		fixtures.mutateAsync.mockImplementationOnce(
			() =>
				new Promise((resolve) => {
					finish = resolve
				}),
		)
		act(() => controls.setTransparency(20))
		await advance()
		act(() => controls.setTransparency(80))
		await advance()
		fixtures.user = {userId: 'Alice', desktopTransparency: 45}
		render()
		await act(async () => {
			finish(true)
			await Promise.resolve()
		})
		expect(fixtures.mutateAsync).toHaveBeenCalledTimes(1)
		expect(fixtures.setData).not.toHaveBeenCalled()
		expect(container.textContent).toBe('45:false')
	})

	it('normalizes older profiles and invalid persisted values defensively', () => {
		expect(normalizeDesktopTransparency(undefined)).toBe(75)
		expect(normalizeDesktopTransparency(NaN)).toBe(75)
		expect(normalizeDesktopTransparency(-2)).toBe(0)
		expect(normalizeDesktopTransparency(999)).toBe(100)
	})
})
