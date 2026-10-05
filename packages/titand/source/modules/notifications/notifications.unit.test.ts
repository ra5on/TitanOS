import {afterEach, beforeEach, describe, expect, test} from 'vitest'

import Titand from '../../index.js'
import temporaryDirectory from '../utilities/temporary-directory.js'

describe('account-scoped notifications', () => {
	let directory: ReturnType<typeof temporaryDirectory>
	let titand: Titand

	beforeEach(async () => {
		directory = temporaryDirectory()
		await directory.createRoot()
		titand = new Titand({dataDirectory: await directory.create()})
		await titand.store.set('notifications', [])
	})

	afterEach(async () => {
		await titand.auth.stop()
		await directory.destroyRoot()
	})

	test('shows device notifications only to the owner and Cloud notifications only to their account', async () => {
		await titand.notifications.add('device-notification')
		await titand.notifications.addForAccount('0', 'owner-cloud-notification')
		await titand.notifications.addForAccount('Alice', 'member-cloud-notification')

		expect(await titand.notifications.getForAccount('0')).toEqual(['owner-cloud-notification', 'device-notification'])
		expect(await titand.notifications.getForAccount('Alice')).toEqual(['member-cloud-notification'])
		expect(await titand.notifications.getForAccount('Bob')).toEqual([])
	})

	test('clears one account without touching another account or device notifications', async () => {
		await titand.notifications.add('device-notification')
		await titand.notifications.addForAccount('0', 'cloud-auth:owner-account')
		await titand.notifications.addForAccount('Alice', 'cloud-auth:member-account')

		await titand.notifications.clearAccount('Alice')

		expect(await titand.notifications.getForAccount('Alice')).toEqual([])
		expect(await titand.notifications.getForAccount('0')).toEqual(['cloud-auth:owner-account', 'device-notification'])
	})

	test('lets only the owner clear a visible device notification', async () => {
		await titand.notifications.add('device-notification')
		await titand.notifications.addForAccount('Alice', 'device-notification')

		await titand.notifications.clearVisibleForAccount('Alice', 'device-notification')
		expect(await titand.notifications.get()).toEqual(['device-notification'])

		await titand.notifications.clearVisibleForAccount('0', 'device-notification')
		expect(await titand.notifications.get()).toEqual([])
	})
})
