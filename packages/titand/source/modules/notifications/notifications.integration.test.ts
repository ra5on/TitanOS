import {expect, beforeAll, afterAll, test} from 'vitest'

import createTestTitand from '../test-utilities/create-test-titand.js'

let titand: Awaited<ReturnType<typeof createTestTitand>>

beforeAll(async () => {
	titand = await createTestTitand()
})

afterAll(async () => {
	await titand.cleanup()
})

// The following tests are stateful and must be run in order

// We sleep to allow time for fs events to be triggered and handled by the titand filewatcher

test.sequential('notifications.get() throws invalid error without auth token', async () => {
	await expect(titand.client.notifications.get.query()).rejects.toThrow('Invalid token')
})

test.sequential('login', async () => {
	await expect(titand.registerAndLogin()).resolves.toBe(true)
})

test.sequential('notifications.get() lists only the onboarding notification on a fresh install', async () => {
	await expect(titand.client.notifications.get.query()).resolves.toMatchObject(['onboarding-complete'])
})

test.sequential('notifications.clear(notification) clears the onboarding notification', async () => {
	await titand.client.notifications.clear.mutate('onboarding-complete')
	await expect(titand.client.notifications.get.query()).resolves.toMatchObject([])
})

test.sequential('notifications.add(notification) adds a notification', async () => {
	await titand.instance.notifications.add('test notification')
	await expect(titand.client.notifications.get.query()).resolves.toMatchObject(['test notification'])
})

test.sequential('notifications.clear(notification) clears a notification', async () => {
	await expect(titand.client.notifications.get.query()).resolves.toMatchObject(['test notification'])
	await titand.client.notifications.clear.mutate('test notification')
	await expect(titand.client.notifications.get.query()).resolves.toMatchObject([])
})

test.sequential('notifications.add(notification) moves duplicate notifications to front', async () => {
	// Add numbered notifications
	await titand.instance.notifications.add('notification-1')
	await titand.instance.notifications.add('notification-2')
	await titand.instance.notifications.add('notification-3')

	// Now add the first again to move it to the front
	await titand.instance.notifications.add('notification-1')

	await expect(titand.client.notifications.get.query()).resolves.toMatchObject([
		'notification-1',
		'notification-3',
		'notification-2',
	])
})

test.sequential('account-scoped notifications are private and independently clearable', async () => {
	const memberPassword = 'member-password'
	const member = await titand.client.user.createUser.mutate({name: 'Alice', password: memberPassword})
	await titand.instance.notifications.add('device-notification')
	await titand.instance.notifications.addForAccount('0', 'owner-cloud-notification')
	await titand.instance.notifications.addForAccount(member.userId, 'member-cloud-notification')

	const ownerNotifications = await titand.client.notifications.get.query()
	expect(ownerNotifications).toEqual(expect.arrayContaining(['device-notification', 'owner-cloud-notification']))
	expect(ownerNotifications).not.toContain('member-cloud-notification')

	const memberToken = await titand.client.user.login.mutate({
		userId: member.userId,
		password: memberPassword,
	})
	titand.setAuthToken(memberToken)
	// A new member starts with their own scoped onboarding notification
	await expect(titand.client.notifications.get.query()).resolves.toEqual([
		'member-cloud-notification',
		'onboarding-complete',
	])
	await expect(titand.client.notifications.clear.mutate('member-cloud-notification')).resolves.toBe(true)
	await expect(titand.client.notifications.clear.mutate('onboarding-complete')).resolves.toBe(true)
	await expect(titand.client.notifications.get.query()).resolves.toEqual([])

	const ownerToken = await titand.client.user.login.mutate({
		userId: '0',
		password: 'moneyprintergobrrr',
	})
	titand.setAuthToken(ownerToken)
	const ownerNotificationsAfterMemberClear = await titand.client.notifications.get.query()
	expect(ownerNotificationsAfterMemberClear).toEqual(
		expect.arrayContaining(['device-notification', 'owner-cloud-notification']),
	)
	expect(await titand.instance.notifications.get()).not.toEqual(
		expect.arrayContaining([expect.stringContaining('member-cloud-notification')]),
	)
})
