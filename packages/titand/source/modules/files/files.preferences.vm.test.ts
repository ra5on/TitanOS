import {expect, beforeAll, afterAll, beforeEach, describe, test} from 'vitest'

import {createTestVm} from '../test-utilities/create-test-titand.js'

let titand: Awaited<ReturnType<typeof createTestVm>>

const defaultPreferences = {
	view: 'list',
	sortBy: 'name',
	sortOrder: 'ascending',
} as const

beforeAll(async () => {
	titand = await createTestVm({device: 'titan-home'})
	await titand.vm.powerOn()
	await titand.registerAndLogin()
})

afterAll(async () => {
	await titand.cleanup()
})

describe('viewPreferences()', () => {
	test('throws invalid error without auth token', async () => {
		await expect(titand.unauthenticatedClient.files.viewPreferences.query()).rejects.toThrow('Invalid token')
	})

	// This runs before anything mutates preferences so it sees pristine state
	test('returns default view preferences on first start', async () => {
		const viewPreferences = await titand.client.files.viewPreferences.query()
		expect(viewPreferences).toStrictEqual(defaultPreferences)
	})
})

describe('updateViewPreferences()', () => {
	// Reset to default state through the API before each test
	beforeEach(async () => {
		await titand.client.files.updateViewPreferences.mutate(defaultPreferences)
	})

	test('throws invalid error without auth token', async () => {
		await expect(
			titand.unauthenticatedClient.files.updateViewPreferences.mutate({
				view: 'icons',
			}),
		).rejects.toThrow('Invalid token')
	})

	test('successfully updates view property', async () => {
		const updatedPreferences = await titand.client.files.updateViewPreferences.mutate({
			view: 'icons',
		})

		expect(updatedPreferences).toStrictEqual({
			view: 'icons',
			sortBy: 'name',
			sortOrder: 'ascending',
		})

		// Verify the update round-trips through the query
		await expect(titand.client.files.viewPreferences.query()).resolves.toStrictEqual({
			view: 'icons',
			sortBy: 'name',
			sortOrder: 'ascending',
		})
	})

	test('successfully updates sortBy property', async () => {
		const updatedPreferences = await titand.client.files.updateViewPreferences.mutate({
			sortBy: 'modified',
		})

		expect(updatedPreferences).toStrictEqual({
			view: 'list',
			sortBy: 'modified',
			sortOrder: 'ascending',
		})

		// Verify the update round-trips through the query
		await expect(titand.client.files.viewPreferences.query()).resolves.toStrictEqual({
			view: 'list',
			sortBy: 'modified',
			sortOrder: 'ascending',
		})
	})

	test('successfully updates sortOrder property', async () => {
		const updatedPreferences = await titand.client.files.updateViewPreferences.mutate({
			sortOrder: 'descending',
		})

		expect(updatedPreferences).toStrictEqual({
			view: 'list',
			sortBy: 'name',
			sortOrder: 'descending',
		})

		// Verify the update round-trips through the query
		await expect(titand.client.files.viewPreferences.query()).resolves.toStrictEqual({
			view: 'list',
			sortBy: 'name',
			sortOrder: 'descending',
		})
	})

	test('successfully updates multiple properties in a single call', async () => {
		const updatedPreferences = await titand.client.files.updateViewPreferences.mutate({
			view: 'icons',
			sortBy: 'size',
			sortOrder: 'descending',
		})

		expect(updatedPreferences).toStrictEqual({
			view: 'icons',
			sortBy: 'size',
			sortOrder: 'descending',
		})

		// Verify the update round-trips through the query
		await expect(titand.client.files.viewPreferences.query()).resolves.toStrictEqual({
			view: 'icons',
			sortBy: 'size',
			sortOrder: 'descending',
		})
	})

	test('preserves existing properties when updating partial preferences', async () => {
		// Set initial non-default preferences
		await titand.client.files.updateViewPreferences.mutate({
			view: 'icons',
			sortBy: 'modified',
			sortOrder: 'descending',
		})

		// Update just one property
		const updatedPreferences = await titand.client.files.updateViewPreferences.mutate({
			sortBy: 'size',
		})

		// Check that only the specified property was updated
		expect(updatedPreferences).toStrictEqual({
			view: 'icons',
			sortBy: 'size',
			sortOrder: 'descending',
		})
	})

	test('handles sequential updates correctly', async () => {
		// Update step 1
		await titand.client.files.updateViewPreferences.mutate({
			view: 'icons',
		})

		// Update step 2
		await titand.client.files.updateViewPreferences.mutate({
			sortBy: 'modified',
		})

		// Update step 3
		const finalPreferences = await titand.client.files.updateViewPreferences.mutate({
			sortOrder: 'descending',
		})

		// Check final state
		expect(finalPreferences).toStrictEqual({
			view: 'icons',
			sortBy: 'modified',
			sortOrder: 'descending',
		})
	})
})

describe('persistence', () => {
	test('view preferences persist across a reboot', async () => {
		// Set non-default preferences
		await titand.client.files.updateViewPreferences.mutate({
			view: 'icons',
			sortBy: 'modified',
			sortOrder: 'descending',
		})

		// Power cycle the VM
		await titand.vm.powerOff()
		await titand.vm.powerOn()
		await titand.login()

		// Preferences survived the reboot
		await expect(titand.client.files.viewPreferences.query()).resolves.toStrictEqual({
			view: 'icons',
			sortBy: 'modified',
			sortOrder: 'descending',
		})
	})
})
