import {randomBytes} from 'node:crypto'
import {access, mkdir} from 'node:fs/promises'
import nodePath from 'node:path'

import type {Page} from 'playwright'
import {afterAll, beforeAll, describe, expect, test} from 'vitest'
import pRetry from 'p-retry'

import {createTestVm} from './create-test-titand.js'
import createVmBrowser from './create-vm-browser.js'

// The dock edit mode in a real browser against the real backend of the image
// being released: context menu, remove, add, size and persistence.
const image = process.env.TITAN_VM_IMAGE
if (!image) throw new Error('TITAN_VM_IMAGE must identify the freshly built image')
await access(image)
const screenshotDirectory = process.env.TITAN_SCREENSHOT_DIR

const account = {name: 'Titan', password: randomBytes(18).toString('base64url')}

describe('Dock edit mode on the released OS', () => {
	let titand: Awaited<ReturnType<typeof createTestVm>>
	let vmBrowser: Awaited<ReturnType<typeof createVmBrowser>>
	let page: Page

	const dockLabels = () =>
		page.locator('[data-dock]:not([data-dock-editing]) a').evaluateAll((links) => links.map((link) => link.getAttribute('aria-label')))
	const savedDock = async () => (await titand.client.user.get.query()).dock

	beforeAll(async () => {
		titand = await createTestVm({device: 'nas', bootDisk: 'nvme', image, memory: 4096, cores: 2})
		await titand.vm.powerOn()
		await titand.waitForStartup()
		await titand.unauthenticatedClient.user.register.mutate(account)
		titand.setAuthToken(await titand.client.user.login.mutate(account))

		vmBrowser = await createVmBrowser({forwardPorts: [{hostPort: titand.vm.httpPort, guestPort: 80}]})
		const context = await vmBrowser.browser.newContext({viewport: {width: 1280, height: 800}, locale: 'de-DE'})
		page = await context.newPage()
		await page.goto('http://127.0.0.1/')
		const password = page.locator('input[type="password"]')
		await password.waitFor({timeout: 120_000})
		await password.fill(account.password)
		await password.press('Enter')
		await page.waitForURL((url) => !url.pathname.startsWith('/login'), {timeout: 120_000})
		await page.locator('[data-dock] a').first().waitFor()
	})

	afterAll(async () => {
		await vmBrowser?.close()
		await titand?.cleanup()
	})

	test('removes, re-adds and resizes dock entries from the context menu and keeps them after a reload', async () => {
		// An untouched account has the default dock and nothing saved
		const initial = await dockLabels()
		expect(initial).toHaveLength(6)
		expect(await savedDock()).toBeUndefined()

		await page.locator('[data-dock]').click({button: 'right', position: {x: 4, y: 4}})
		await page.getByRole('menuitem', {name: 'Dock bearbeiten'}).click()
		const editor = page.locator('[data-dock-editing]')
		await editor.waitFor()
		expect(await editor.locator('[data-dock-item]').count()).toBe(6)
		// Settings is where the dock is restored: it has no remove button
		expect(await editor.locator('[data-dock-item="system:settings"] button').count()).toBe(0)
		if (screenshotDirectory) {
			await mkdir(screenshotDirectory, {recursive: true})
			await page.waitForTimeout(500)
			await page.screenshot({path: nodePath.join(screenshotDirectory, 'titanos-dock-edit.jpg'), type: 'jpeg', quality: 85})
		}

		await editor.locator('[data-dock-item="system:files"] button').click()
		await expect.poll(() => editor.locator('[data-dock-item]').count()).toBe(5)

		// The removed entry is offered again and returns at the end
		await page.getByRole('button', {name: 'Hinzufügen'}).click()
		await page.getByRole('menuitem', {name: initial[0]!, exact: true}).click()
		await expect.poll(() => editor.locator('[data-dock-item]').count()).toBe(6)

		await page.getByRole('slider').focus()
		await page.keyboard.press('ArrowRight')
		await page.getByRole('button', {name: 'Fertig'}).click()
		await page.locator('[data-dock]:not([data-dock-editing]) a').first().waitFor()

		const expected = [...initial.slice(1), initial[0]]
		expect(await dockLabels()).toEqual(expected)
		await pRetry(
			async () => {
				const dock = await savedDock()
				expect(dock?.iconSize).toBe(52)
				expect(dock?.items.at(-1)).toEqual({type: 'system', id: 'files'})
				expect(dock?.items).toHaveLength(6)
			},
			{retries: 10, minTimeout: 500, maxTimeout: 500},
		)

		await page.reload()
		await page.locator('[data-dock] a').first().waitFor()
		expect(await dockLabels()).toEqual(expected)

		// The backend refuses a dock without Settings
		await expect(
			titand.client.user.set.mutate({dock: {items: [{type: 'system', id: 'files'}], iconSize: 50}}),
		).rejects.toThrow()
	})
})
