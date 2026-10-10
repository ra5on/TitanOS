import {randomBytes} from 'node:crypto'
import {access, mkdir} from 'node:fs/promises'
import nodePath from 'node:path'

import type {Page} from 'playwright'
import {afterAll, beforeAll, describe, expect, test} from 'vitest'

import {createTestVm} from './create-test-titand.js'
import createVmBrowser from './create-vm-browser.js'

// README screenshots of the freshly built image: the real UI served by the
// real backend on the OS being released. A disposable account and sample
// files are created here; the password is random and never leaves this run.
const image = process.env.TITAN_VM_IMAGE
const outputDirectory = process.env.TITAN_SCREENSHOT_DIR
const version = process.env.TITAN_SCREENSHOT_VERSION
if (!image || !outputDirectory || !version) {
	throw new Error('TITAN_VM_IMAGE, TITAN_SCREENSHOT_DIR and TITAN_SCREENSHOT_VERSION are required')
}
await access(image)

const account = {name: 'Titan', password: randomBytes(18).toString('base64url')}
const sampleFiles: Record<string, string> = {
	'/Home/Dokumente/Haushaltsplan 2026.txt': 'Miete, Strom, Internet\n',
	'/Home/Dokumente/Notizen.md': '# Notizen\n\n- Backup prüfen\n',
	'/Home/Downloads/Handbuch.txt': 'TitanOS Handbuch\n',
	'/Home/Willkommen.txt': 'Willkommen bei TitanOS\n',
}

describe('README screenshots', () => {
	let titand: Awaited<ReturnType<typeof createTestVm>>
	let vmBrowser: Awaited<ReturnType<typeof createVmBrowser>>
	let page: Page

	beforeAll(async () => {
		await mkdir(outputDirectory, {recursive: true})
		// A generic PC rather than an emulated appliance board, booting like the image smoke test
		titand = await createTestVm({device: 'nas', bootDisk: 'nvme', image, memory: 4096, cores: 2})
		await titand.vm.powerOn()
		await titand.waitForStartup()
		await titand.unauthenticatedClient.user.register.mutate(account)
		// The cookie-keeping client: uploads need the browser session cookie too
		titand.setAuthToken(await titand.client.user.login.mutate(account))
		for (const [path, contents] of Object.entries(sampleFiles)) {
			await titand.api.post(`files/upload?path=${encodeURIComponent(path)}`, {body: Buffer.from(contents)})
		}

		vmBrowser = await createVmBrowser({forwardPorts: [{hostPort: titand.vm.httpPort, guestPort: 80}]})
		const context = await vmBrowser.browser.newContext({
			viewport: {width: 1280, height: 800},
			locale: 'de-DE',
			timezoneId: 'Europe/Berlin',
			colorScheme: 'dark',
		})
		page = await context.newPage()
		await page.goto('http://127.0.0.1/')
		const password = page.locator('input[type="password"]')
		await password.waitFor({timeout: 120_000})
		await password.fill(account.password)
		await password.press('Enter')
		await page.waitForURL((url) => !url.pathname.startsWith('/login'), {timeout: 120_000})
	})

	afterAll(async () => {
		await vmBrowser?.close()
		await titand?.cleanup()
	})

	async function capture(name: string) {
		// Let wallpaper, icons and fonts settle. Live status polling can keep the
		// network busy, so the idle wait is bounded.
		await page.waitForLoadState('networkidle', {timeout: 15_000}).catch(() => {})
		await page.evaluate('document.fonts.ready')
		await page.waitForTimeout(2500)
		await page.screenshot({path: nodePath.join(outputDirectory!, `${name}-${version}.jpg`), type: 'jpeg', quality: 85})
	}

	test('desktop', async () => {
		await page.goto('http://127.0.0.1/')
		await page.getByRole('heading', {level: 1}).filter({hasText: account.name}).waitFor()
		// Files and Settings open as sheets; only the desktop must be uncovered
		await page.keyboard.press('Escape')
		await page.waitForTimeout(500)
		expect(await page.locator('[role="dialog"]').count()).toBe(0)
		await capture('titanos-desktop')
	})

	test('files', async () => {
		await page.goto('http://127.0.0.1/files/Home')
		for (const folder of ['Dokumente', 'Downloads', 'Fotos', 'Videos']) {
			await page.getByText(folder, {exact: true}).first().waitFor()
		}
		await capture('titanos-files')
	})

	test('settings', async () => {
		await page.goto('http://127.0.0.1/settings')
		await page.getByRole('heading').first().waitFor()
		await capture('titanos-settings')
	})
})
