// @vitest-environment jsdom
import {act} from 'react'
import {createRoot, type Root} from 'react-dom/client'
import {afterEach, beforeEach, describe, expect, test, vi} from 'vitest'

import type {Machine} from '@/features/machines/types'

import {SnapshotsSection} from './machine-device-settings'

const actions = vi.hoisted(() => ({
	createSnapshot: vi.fn(async () => ({})),
	revertSnapshot: vi.fn(async () => true),
	deleteSnapshot: vi.fn(async () => true),
}))

vi.mock('@/utils/i18n', () => ({t: (key: string) => key}))
vi.mock('react-i18next', () => ({useTranslation: () => ({i18n: {language: 'de'}})}))
vi.mock('@/trpc/trpc', () => ({trpcReact: {}}))
vi.mock('@/features/machines/hooks/use-machine-actions', () => ({useMachineActions: () => actions}))
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
	vi.clearAllMocks()
})

const snapshot = {id: '0123456789ab', name: 'Vor dem Update', createdAt: Date.UTC(2026, 9, 10, 18), diskSizeGb: 32}
function render(machine: Partial<Machine> = {}, disabled = false) {
	const props = {id: 'home-assistant', name: 'Home Assistant', state: 'stopped', snapshots: [snapshot], ...machine}
	act(() => root.render(<SnapshotsSection machine={props as Machine} disabled={disabled} />))
}
const button = (label: string) =>
	[...document.querySelectorAll<HTMLButtonElement>('button')].find(
		(candidate) => candidate.textContent?.includes(label) || candidate.getAttribute('aria-label') === label,
	)!
// The confirmation dialog renders outside the section, in a portal
const dialogButton = (label: string) =>
	[...document.querySelectorAll<HTMLButtonElement>('button')].find(
		(candidate) => !container.contains(candidate) && candidate.textContent?.trim() === label,
	)!
const nameInput = () => container.querySelector<HTMLInputElement>('input')!
async function type(value: string) {
	const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!
	await act(async () => {
		setter.call(nameInput(), value)
		nameInput().dispatchEvent(new Event('input', {bubbles: true}))
	})
}

describe('machine snapshots section', () => {
	test('lists snapshots and creates one with the typed name', async () => {
		render()
		expect(container.textContent).toContain('Vor dem Update')
		expect(container.textContent).not.toContain('machines.snapshots-stop-note')

		await type('  Vor dem Kernel-Update ')
		await act(async () => button('machines.snapshot-create').click())
		expect(actions.createSnapshot).toHaveBeenCalledWith({id: 'home-assistant', name: 'Vor dem Kernel-Update'})
		expect(nameInput().value).toBe('')
	})

	test('names a snapshot after the current time when no name is given', async () => {
		render({snapshots: undefined})
		expect(container.textContent).toContain('machines.snapshots-empty')
		await act(async () => button('machines.snapshot-create').click())
		expect(actions.createSnapshot).toHaveBeenCalledWith({id: 'home-assistant', name: 'machines.snapshot-default-name'})
	})

	test('only works while the machine is shut down', () => {
		render({state: 'running'})
		expect(container.textContent).toContain('machines.snapshots-stop-note')
		expect(button('machines.snapshot-create').disabled).toBe(true)
		expect(button('machines.snapshot-revert').disabled).toBe(true)
		expect(nameInput().disabled).toBe(true)
	})

	test('stops offering new snapshots at the limit', () => {
		render({snapshots: Array.from({length: 10}, (_, index) => ({...snapshot, id: `${index}`.padStart(12, '0')}))})
		expect(container.textContent).toContain('machines.snapshots-full')
		expect(button('machines.snapshot-create').disabled).toBe(true)
		expect(button('machines.snapshot-revert').disabled).toBe(false)
	})

	test('asks before going back to a snapshot or deleting it', async () => {
		render()
		await act(async () => button('machines.snapshot-revert').click())
		expect(document.body.textContent).toContain('machines.snapshot-revert-title')
		expect(actions.revertSnapshot).not.toHaveBeenCalled()
		await act(async () => dialogButton('machines.snapshot-revert').click())
		expect(actions.revertSnapshot).toHaveBeenCalledWith({id: 'home-assistant', snapshotId: snapshot.id})
		expect(actions.deleteSnapshot).not.toHaveBeenCalled()

		await act(async () => button('machines.snapshot-delete-label').click())
		expect(document.body.textContent).toContain('machines.snapshot-delete-title')
		await act(async () => dialogButton('machines.snapshot-delete-confirm').click())
		expect(actions.deleteSnapshot).toHaveBeenCalledWith({id: 'home-assistant', snapshotId: snapshot.id})
	})
})
