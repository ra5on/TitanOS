// @vitest-environment jsdom

import {act} from 'react'
import {createRoot, type Root} from 'react-dom/client'
import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest'

import {GenericConfirmationDialog} from './generic-confirmation-dialog'

vi.mock('react-i18next', () => ({
	useTranslation: () => ({t: (key: string) => (key === 'yes' ? 'Ja' : key === 'no' ? 'Nein' : key)}),
}))
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
	document.documentElement.removeAttribute('dir')
})

describe('Yes/No confirmation ordering', () => {
	it.each(['ltr', 'rtl'])('keeps Yes physically left, No right and cancellation focused in %s', async (direction) => {
		document.documentElement.dir = direction
		const onResolve = vi.fn()
		const onReject = vi.fn()
		await act(async () =>
			root.render(
				<GenericConfirmationDialog
					isOpen
					options={{
						title: 'Confirm',
						message: 'Question',
						actions: [
							{label: 'Nein', value: 'cancel'},
							{label: 'Ja', value: 'confirm'},
						],
					}}
					onResolve={onResolve}
					onReject={onReject}
				/>,
			),
		)
		const buttons = [...document.querySelectorAll<HTMLButtonElement>('[role=dialog] button')]
		expect(buttons.map((button) => button.textContent)).toEqual(['Ja', 'Nein'])
		expect(buttons[0].parentElement?.dir).toBe('ltr')
		expect(buttons[0].parentElement?.className).toContain('flex-row')
		expect(document.activeElement).toBe(buttons[1])
		await act(async () => buttons[1].click())
		expect(onReject).toHaveBeenCalledWith('cancel')
		expect(onResolve).not.toHaveBeenCalled()
	})

	it('keeps the intended order of non-binary conflict actions', async () => {
		await act(async () =>
			root.render(
				<GenericConfirmationDialog
					isOpen
					options={{
						title: 'Conflict',
						message: 'Question',
						actions: [
							{label: 'Replace', value: 'replace'},
							{label: 'Keep both', value: 'keep'},
							{label: 'Skip', value: 'skip'},
						],
					}}
					onResolve={vi.fn()}
					onReject={vi.fn()}
				/>,
			),
		)
		expect(
			[...document.querySelectorAll<HTMLButtonElement>('[role=dialog] button')].map((button) => button.textContent),
		).toEqual(['Replace', 'Keep both', 'Skip'])
	})
})
