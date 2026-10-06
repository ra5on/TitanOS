// @vitest-environment jsdom
import {act} from 'react'
import {createRoot, type Root} from 'react-dom/client'
import {afterEach, beforeEach, describe, expect, test, vi} from 'vitest'

import {MachineNetworkField} from './network-field'

vi.mock('@/utils/i18n', () => ({t: (key: string) => key}))
;(globalThis as {IS_REACT_ACT_ENVIRONMENT?: boolean}).IS_REACT_ACT_ENVIRONMENT = true
let root: Root
let container: HTMLDivElement
beforeEach(() => {
	container = document.createElement('div')
	document.body.appendChild(container)
	root = createRoot(container)
})
afterEach(() => {
	act(() => root.unmount())
	document.body.replaceChildren()
})
function render(overrides: Partial<Parameters<typeof MachineNetworkField>[0]> = {}) {
	const props = {
		value: {mode: 'nat' as const},
		onChange: vi.fn(),
		bridges: [] as string[],
		automaticBridge: {available: true, interface: 'enp1s0'},
		onRefresh: vi.fn(),
		onPrepareBridge: vi.fn(async (): Promise<string | undefined> => 'titan-br0'),
		...overrides,
	}
	act(() => root.render(<MachineNetworkField {...props} />))
	return props
}
async function openMenu() {
	const trigger = container.querySelector<HTMLButtonElement>('button[aria-label="machines.network"]')!
	await act(async () => {
		trigger.focus()
		trigger.dispatchEvent(new KeyboardEvent('keydown', {key: 'Enter', bubbles: true}))
	})
}
function bridgeOption() {
	return [...document.querySelectorAll<HTMLElement>('[role="menuitemradio"]')].find((item) =>
		item.textContent?.includes('machines.network-bridge'),
	)!
}

describe('machine network dropdown', () => {
	test('allows automatic Bridge on wired LAN and waits for confirmation before changing the VM selection', async () => {
		let resolve: (bridge: string) => void = () => {}
		const prepare = vi.fn(
			() =>
				new Promise<string>((done) => {
					resolve = done
				}),
		)
		const props = render({onPrepareBridge: prepare})
		await openMenu()
		expect(bridgeOption().getAttribute('aria-disabled')).not.toBe('true')
		await act(async () => bridgeOption().click())
		expect(prepare).toHaveBeenCalledOnce()
		expect(props.onChange).not.toHaveBeenCalled()
		await act(async () => resolve('titan-br0'))
		expect(props.onChange).toHaveBeenCalledWith({mode: 'bridge', bridge: 'titan-br0'})
	})
	test('does not create a second bridge when a LAN bridge already exists', async () => {
		const props = render({bridges: ['br0', 'br1']})
		await openMenu()
		await act(async () => bridgeOption().click())
		expect(props.onPrepareBridge).not.toHaveBeenCalled()
		expect(props.onChange).toHaveBeenCalledWith({mode: 'bridge', bridge: 'br0'})
	})
	test('explains why automatic Bridge cannot be used over WLAN', async () => {
		render({automaticBridge: {available: false, reason: 'wifi'}})
		await openMenu()
		expect(bridgeOption().getAttribute('aria-disabled')).toBe('true')
		expect(document.body.textContent).toContain('machines.network-bridge-wifi')
	})
	test('explains that another network change must finish before automatic bridging', async () => {
		const props = render({automaticBridge: {available: false, reason: 'busy'}})
		await openMenu()
		expect(bridgeOption().getAttribute('aria-disabled')).toBe('true')
		expect(document.body.textContent).toContain('machines-error.machine-bridge-busy')
		expect(props.onPrepareBridge).not.toHaveBeenCalled()
	})
	test('a failed setup keeps the previous VM network selected and displays an error', async () => {
		const props = render({onPrepareBridge: vi.fn(async () => undefined), bridgeError: 'Connection was restored'})
		await openMenu()
		await act(async () => bridgeOption().click())
		expect(props.onChange).not.toHaveBeenCalled()
		expect(container.querySelector('[role="alert"]')?.textContent).toBe('Connection was restored')
	})
	test('disables changes while verifying the NAS connection', () => {
		render({isPreparingBridge: true})
		const trigger = container.querySelector<HTMLButtonElement>('button[aria-label="machines.network"]')!
		expect(trigger.disabled).toBe(true)
		expect(trigger.getAttribute('aria-busy')).toBe('true')
		expect(trigger.textContent).toContain('machines.network-bridge-preparing')
	})
	test('reselecting Bridge preserves the chosen existing interface', async () => {
		const props = render({value: {mode: 'bridge', bridge: 'br1'}, bridges: ['br0', 'br1']})
		await openMenu()
		await act(async () => bridgeOption().click())
		expect(props.onChange).not.toHaveBeenCalled()
		expect(props.onPrepareBridge).not.toHaveBeenCalled()
	})
	test('can recreate a missing previously selected Bridge without switching to another mode first', async () => {
		const props = render({value: {mode: 'bridge', bridge: 'titan-br0'}})
		await openMenu()
		await act(async () => bridgeOption().click())
		expect(props.onPrepareBridge).toHaveBeenCalledOnce()
		expect(props.onChange).toHaveBeenCalledWith({mode: 'bridge', bridge: 'titan-br0'})
	})
})
