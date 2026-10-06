import {expect, beforeAll, beforeEach, afterAll, afterEach, test} from 'vitest'

import {createTestVm} from '../test-utilities/create-test-titand.js'

let titand: Awaited<ReturnType<typeof createTestVm>>
let failed = false

beforeAll(async () => {
	titand = await createTestVm({device: 'titan-home'})
	await titand.vm.powerOn()
})

afterAll(async () => {
	await titand.cleanup()
})

// The tests are stateful steps of one scenario, skip the rest after a failure
afterEach(({task}) => {
	if (task.result?.state === 'fail') failed = true
})

beforeEach(({skip}) => {
	if (failed) skip()
})

// The following tests are stateful and must be run in order

test.sequential('enabled() throws invalid error when no user is registered', async () => {
	await expect(titand.client.widget.enabled.query()).rejects.toThrow('Invalid token')
})

test.sequential('enable() throws invalid error when no user is registered', async () => {
	await expect(titand.client.widget.enable.mutate({widgetId: 'titan:storage'})).rejects.toThrow('Invalid token')
})

test.sequential('disable() throws invalid error when no user is registered', async () => {
	await expect(titand.client.widget.disable.mutate({widgetId: 'titan:storage'})).rejects.toThrow('Invalid token')
})

test.sequential('data() throws invalid error when no user is registered', async () => {
	await expect(titand.client.widget.data.query({widgetId: 'titan:storage'})).rejects.toThrow('Invalid token')
})

test.sequential('login', async () => {
	await expect(titand.registerAndLogin()).resolves.toBe(true)
})

// test.sequential('listAll() returns available widgets', async () => {
// 	await expect(titand.client.widget.listAll.query()).resolves.toStrictEqual([
// 		{
// 			id: 'titan:storage',
// 			type: 'stat-with-progress',
// 			refresh: 1000 * 60 * 5,
// 			example: {
// 				title: 'Storage',
// 				value: '256 GB',
// 				progressLabel: '1.75 TB left',
// 				progress: 0.25,
// 			},
// 		},
// 		{
// 			id: 'titan:memory',
// 			type: 'stat-with-progress',
// 			refresh: 1000 * 10,
// 			example: {
// 				title: 'Memory',
// 				value: '5.8 GB',
// 				subValue: '/16GB',
// 				progressLabel: '11.4 GB left',
// 				progress: 0.36,
// 			},
// 		},
// 	])
// })

test.sequential('enabled() returns default widgets', async () => {
	await expect(titand.client.widget.enabled.query()).resolves.toStrictEqual([
		'titan:files-favorites',
		'titan:storage',
		'titan:system-stats',
	])
})

test.sequential('disable() can disable default widgets', async () => {
	await expect(titand.client.widget.disable.mutate({widgetId: 'titan:files-favorites'})).resolves.toStrictEqual(true)
	await expect(titand.client.widget.disable.mutate({widgetId: 'titan:storage'})).resolves.toStrictEqual(true)
	await expect(titand.client.widget.disable.mutate({widgetId: 'titan:system-stats'})).resolves.toStrictEqual(true)
})

test.sequential('enabled() returns no widgets when none are enabled', async () => {
	await expect(titand.client.widget.enabled.query()).resolves.toStrictEqual([])
})

test.sequential('enable() enables a widget', async () => {
	await expect(titand.client.widget.enable.mutate({widgetId: 'titan:storage'})).resolves.toStrictEqual(true)
})

test.sequential('enabled() returns enabled widgets', async () => {
	await expect(titand.client.widget.enabled.query()).resolves.toStrictEqual(['titan:storage'])
})

test.sequential('data() returns live widget data', async () => {
	await expect(titand.client.widget.data.query({widgetId: 'titan:storage'})).resolves.toMatchObject({
		title: 'Storage',
		link: '?dialog=live-usage&live-usage-tab=storage',
		refresh: 30000,
		type: 'text-with-progress',
	})
})

test.sequential('disable() disables a widget', async () => {
	await expect(titand.client.widget.disable.mutate({widgetId: 'titan:storage'})).resolves.toStrictEqual(true)
})

test.sequential('enabled() returns no widgets when they are all disabled', async () => {
	await expect(titand.client.widget.enabled.query()).resolves.toStrictEqual([])
})

// ── Member accounts ────────────────────────────────────────────────────────
// Members get the same defaults at read time without anything being seeded,
// and their widgets are stored per account so they never touch the owner's.

const ownerPassword = 'moneyprintergobrrr'
const memberPassword = 'member-password'
let memberId = ''

const loginAs = async (userId: string, password: string) => {
	const token = await titand.client.user.login.mutate({userId, password})
	titand.setAuthToken(token)
}

test.sequential('owner re-enables a widget before creating a member', async () => {
	await expect(titand.client.widget.enable.mutate({widgetId: 'titan:memory'})).resolves.toStrictEqual(true)
	const member = await titand.client.user.createUser.mutate({name: 'Alice', password: memberPassword})
	memberId = member.userId
	await loginAs(memberId, memberPassword)
})

test.sequential('enabled() returns default widgets for a member without seeding', async () => {
	await expect(titand.client.widget.enabled.query()).resolves.toStrictEqual([
		'titan:files-favorites',
		'titan:storage',
		'titan:system-stats',
	])
})

test.sequential('enable() enforces the widget limit against member defaults', async () => {
	await expect(titand.client.widget.enable.mutate({widgetId: 'titan:memory'})).rejects.toThrow(
		'maximum number of widgets',
	)
})

test.sequential('enable() rejects app widgets for apps not shared with the member', async () => {
	await expect(titand.client.widget.enable.mutate({widgetId: 'transmission:status'})).rejects.toThrow(
		'[widget-not-found]',
	)
})

test.sequential('disable() persists a member change without touching the owner', async () => {
	await expect(titand.client.widget.disable.mutate({widgetId: 'titan:storage'})).resolves.toStrictEqual(true)
	await expect(titand.client.widget.enabled.query()).resolves.toStrictEqual([
		'titan:files-favorites',
		'titan:system-stats',
	])

	await loginAs('0', ownerPassword)
	await expect(titand.client.widget.enabled.query()).resolves.toStrictEqual(['titan:memory'])
})

test.sequential('data() serves titan widgets to members', async () => {
	await loginAs(memberId, memberPassword)
	await expect(titand.client.widget.data.query({widgetId: 'titan:storage'})).resolves.toMatchObject({
		title: 'Storage',
		type: 'text-with-progress',
	})
})

test.sequential('a member can empty their widgets without falling back to defaults', async () => {
	await expect(titand.client.widget.disable.mutate({widgetId: 'titan:files-favorites'})).resolves.toStrictEqual(true)
	await expect(titand.client.widget.disable.mutate({widgetId: 'titan:system-stats'})).resolves.toStrictEqual(true)
	await expect(titand.client.widget.enabled.query()).resolves.toStrictEqual([])
})
