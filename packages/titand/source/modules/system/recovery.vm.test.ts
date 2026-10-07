import {createReadStream} from 'node:fs'
import {access, readFile, stat, writeFile} from 'node:fs/promises'
import https from 'node:https'
import path from 'node:path'
import {setTimeout as delay} from 'node:timers/promises'
import {afterAll, afterEach, beforeAll, beforeEach, describe, expect, test} from 'vitest'
import pRetry from 'p-retry'
import pWaitFor from 'p-wait-for'

import {createTestVm} from '../test-utilities/create-test-titand.js'

// This is a private, separately signed versioned baseline of the current code.
// It proves real Rugix update/rollback behaviour, not an old release's migrations.
const fixtureDirectory = process.env.TITAN_RECOVERY_FIXTURE
if (!fixtureDirectory) throw new Error('TITAN_RECOVERY_FIXTURE must identify the isolated signed release fixture')
const fixture = JSON.parse(await readFile(path.join(fixtureDirectory, 'fixture.json'), 'utf8')) as {
	baselineVersion: string
	candidateVersion: string
	baselineImage: string
	sourceCommit: string
	privateBaseline: true
	legacyMigrationTested: false
}
await access(fixture.baselineImage)
if (!(await stat(fixture.baselineImage)).isFile()) throw new Error('The recovery baseline must be a real IMG')

type Vm = Awaited<ReturnType<typeof createTestVm>>
let titand: Vm
let server: https.Server
let serverPort: number
let failed = false
const marker = 'titan-recovery-data-marker'
const successfulRequests = new Set<string>()
const evidence: Record<string, unknown> = {
	status: 'not-run',
	baselineVersion: fixture.baselineVersion,
	candidateVersion: fixture.candidateVersion,
	sourceCommit: fixture.sourceCommit,
	privateBaseline: fixture.privateBaseline,
	legacyMigrationTested: false,
	checks: [],
}
function record(check: string) {
	;(evidence.checks as string[]).push(check)
}

async function guestVersion() {
	return (await titand.unauthenticatedClient.system.version.query()).version
}

async function waitForVersion(version: string) {
	await pWaitFor(
		async () => {
			try {
				return (await guestVersion()) === version
			} catch {
				return false
			}
		},
		{interval: 1_000, timeout: 600_000},
	)
	await titand.login()
	await pRetry(
		async () => {
			const state = await titand.client.system.recoveryStatus.query()
			expect(state.current).toMatchObject({version, confirmed: true})
			return state
		},
		{retries: 120, minTimeout: 1_000, maxTimeout: 1_000},
	)
}

async function assertMarker() {
	const result = await titand.client.files.list.query({path: '/Home'})
	expect(result.files.some((file) => file.name === marker)).toBe(true)
}

async function nativeBoot() {
	return JSON.parse(await titand.vm.sshAsRoot('rugix-ctrl system info --json')).boot as {
		activeGroup: 'a' | 'b'
		defaultGroup: 'a' | 'b'
	}
}

// Instrument transport only inside disposable test guests. All original URL,
// metadata, signature, checksum and native install checks still run. The normal
// installed updater has no test-feed option and no relaxed TLS/URL policy.
async function connectPrivateFeed() {
	const certificate = await readFile(path.join(fixtureDirectory!, 'https-cert.pem'), 'utf8')
	const wrapper = `#!/usr/bin/env python3
import importlib.util, ssl, sys, urllib.parse, urllib.request
spec=importlib.util.spec_from_file_location('titan_production_updater','/usr/libexec/titan-system-update.production.py')
product=importlib.util.module_from_spec(spec); spec.loader.exec_module(product)
def response(url):
 value=urllib.parse.urlsplit(url)
 if value.scheme!='https' or value.username or value.password or value.port not in (None,443) or value.hostname not in ('api.github.com','github.com'):
  raise product.UpdateError('Invalid private test transport address')
 target='https://10.0.2.2:${serverPort}/'+value.hostname+value.path+('?' + value.query if value.query else '')
 return urllib.request.urlopen(target,context=ssl.create_default_context(cafile='/run/titan-recovery-test-ca.crt'),timeout=60)
product.response=response
sys.exit(product.main())
`
	await titand.vm
		.sshAsRoot(`test -f /usr/libexec/titan-system-update.production.py || cp /usr/libexec/titan-system-update.py /usr/libexec/titan-system-update.production.py
printf '%s' '${Buffer.from(certificate).toString('base64')}' | base64 -d > /run/titan-recovery-test-ca.crt
printf '%s' '${Buffer.from(wrapper).toString('base64')}' | base64 -d > /usr/libexec/titan-system-update.py
chmod 755 /usr/libexec/titan-system-update.py`)
}

async function updateThroughUiApi() {
	await connectPrivateFeed()
	const latest = await titand.client.system.checkUpdate.query()
	expect(latest.version).toBe(fixture.candidateVersion)
	const result = await titand.client.system.update.mutate().catch((error: unknown) => {
		// A successful OS reboot can close the mutation's connection before its
		// final response. An application or verification error must still fail.
		if (!(error instanceof Error) || !/fetch failed|socket|ECONN|other side closed/i.test(error.message)) throw error
		return undefined
	})
	if (result !== undefined) expect(result).toBe(true)
	await waitForVersion(fixture.candidateVersion)
}

async function rollbackThroughUiApi(version: string) {
	const state = await titand.client.system.recoveryStatus.query()
	const previous = state.previous.find((item) => item.version === version)
	expect(previous).toBeDefined()
	expect(await titand.client.system.rollback.mutate({selection: previous!.selection})).toBe(true)
	await waitForVersion(version)
}

describe.sequential('Signed TitanOS system update and recovery on a real UEFI guest', () => {
	beforeAll(async () => {
		server = https.createServer(
			{
				key: await readFile(path.join(fixtureDirectory!, 'https-key.pem')),
				cert: await readFile(path.join(fixtureDirectory!, 'https-cert.pem')),
			},
			async (request, response) => {
				try {
					const url = new URL(request.url!, 'https://private.invalid')
					let file: string | undefined
					if (url.pathname === '/api.github.com/repos/ra5on/TitanOS/releases') {
						const latest = JSON.parse(await readFile(path.join(fixtureDirectory!, 'latest.json'), 'utf8'))
						response.setHeader('content-type', 'application/json')
						response.end(JSON.stringify([latest]))
						return
					}
					const tag = url.pathname.match(
						/^\/api\.github\.com\/repos\/ra5on\/TitanOS\/releases\/tags\/(v[0-9]+\.[0-9]+\.[0-9]+)$/,
					)
					if (tag) file = path.join(fixtureDirectory!, tag[1], 'github.json')
					const asset = url.pathname.match(
						/^\/github\.com\/ra5on\/TitanOS\/releases\/download\/(v[0-9]+\.[0-9]+\.[0-9]+)\/([A-Za-z0-9][A-Za-z0-9._-]{0,180})$/,
					)
					if (asset && !asset[2].endsWith('.pem')) file = path.join(fixtureDirectory!, asset[1], asset[2])
					if (!file) {
						response.writeHead(404)
						response.end()
						return
					}
					const info = await stat(file)
					response.setHeader('content-length', info.size)
					successfulRequests.add(url.pathname)
					createReadStream(file)
						.on('error', () => response.destroy())
						.pipe(response)
				} catch {
					response.writeHead(500)
					response.end()
				}
			},
		)
		await new Promise<void>((resolve) => server.listen(0, '0.0.0.0', resolve))
		const address = server.address()
		if (!address || typeof address === 'string') throw new Error('Private HTTPS fixture did not listen')
		serverPort = address.port
		titand = await createTestVm({device: 'titan-home', image: fixture.baselineImage, memory: 4096, cores: 2})
		await titand.vm.powerOn()
		await titand.registerAndLogin()
		await waitForVersion(fixture.baselineVersion)
	})
	beforeEach(({skip}) => {
		if (failed) skip()
	})
	afterEach(async ({task}) => {
		if (task.result?.state === 'fail') {
			failed = true
			console.error(titand?.vm.bootConsoleOutput)
			console.error(await titand?.vm.sshAsRoot('journalctl -u titan --no-pager -n 160').catch(String))
		}
	})
	afterAll(async () => {
		evidence.status = failed ? 'failed' : 'passed'
		evidence.httpsRequests = [...successfulRequests].sort()
		const report = process.env.TITAN_RECOVERY_EVIDENCE
		if (report) await writeFile(report, JSON.stringify(evidence, null, 2) + '\n')
		await titand?.cleanup()
		await new Promise<void>(
			(resolve, reject) => server?.close((error) => (error ? reject(error) : resolve())) ?? resolve(),
		)
	})

	test('fresh installation has no previous slot and denies unauthenticated recovery', async () => {
		expect(await titand.client.system.recoveryStatus.query()).toMatchObject({previous: []})
		await expect(titand.unauthenticatedClient.system.recoveryStatus.query()).rejects.toMatchObject({
			data: {code: 'UNAUTHORIZED'},
		})
		await expect(
			titand.unauthenticatedClient.system.rollback.mutate({selection: 'a'.repeat(64)}),
		).rejects.toMatchObject({data: {code: 'UNAUTHORIZED'}})
		expect(titand.vm.bootConsoleOutput).toContain('TITAN_BOOT_MENU automatic=2 previous=')
		await titand.client.files.createDirectory.mutate({path: `/Home/${marker}`})
		record('fresh-no-rollback-and-authentication')
	})

	test('installs the separately signed candidate through the product update API and commits its healthy boot', async () => {
		await updateThroughUiApi()
		const boot = await nativeBoot()
		expect(boot.activeGroup).toBe('b')
		expect(boot.defaultGroup).toBe('b')
		const state = await titand.client.system.recoveryStatus.query()
		expect(state.previous).toEqual([expect.objectContaining({version: fixture.baselineVersion, slot: 'a'})])
		await assertMarker()
		record('signed-update-api-reboot-confirmed')
		record('data-and-user-retained-after-update')
	})

	test('selects the previous version with actual console keys in the five-second boot menu and preserves data', async () => {
		await titand.vm.powerOff()
		await titand.vm.powerOn({
			onBootConsole: async (console) => {
				await console.waitForText('TITAN_BOOT_MENU automatic=3 previous=2')
				await delay(350)
				await console.sendKey('down')
				await delay(150)
				await console.sendKey('ret')
			},
		})
		await waitForVersion(fixture.baselineVersion)
		expect(titand.vm.bootConsoleOutput).toContain('TITAN_BOOT_SELECTED part=2 trial=true')
		const boot = await nativeBoot()
		expect(boot.activeGroup).toBe('a')
		expect(boot.defaultGroup).toBe('a')
		await assertMarker()
		record('boot-menu-previous-real-keyboard-selection')
		record('manual-recovery-health-commit-data-retained')
	})

	test('automatically boots the confirmed recovered version without console input', async () => {
		await titand.vm.powerOff()
		await titand.vm.powerOn()
		await waitForVersion(fixture.baselineVersion)
		expect(titand.vm.bootConsoleOutput).toContain('TITAN_BOOT_MENU automatic=2 previous=3')
		expect(titand.vm.bootConsoleOutput).toContain('TITAN_BOOT_SELECTED part=2 trial=false')
		await assertMarker()
		record('automatic-confirmed-boot-without-input')
	})

	test('restores the other confirmed system through the authenticated rollback API', async () => {
		await rollbackThroughUiApi(fixture.candidateVersion)
		const boot = await nativeBoot()
		expect(boot.activeGroup).toBe('b')
		expect(boot.defaultGroup).toBe('b')
		await assertMarker()
		record('rollback-api-reboot-confirmed-data-retained')
	})

	test('rejects a stale rollback selection without rebooting or changing the running slot', async () => {
		const before = await nativeBoot()
		await expect(titand.client.system.rollback.mutate({selection: '0'.repeat(64)})).rejects.toMatchObject({
			data: {code: 'CONFLICT'},
		})
		expect(await nativeBoot()).toEqual(before)
		expect(await guestVersion()).toBe(fixture.candidateVersion)
		record('stale-selection-does-not-reboot')
	})

	test('automatically falls back after a deliberately damaged unconfirmed update and keeps user files', async () => {
		await rollbackThroughUiApi(fixture.baselineVersion)
		await connectPrivateFeed()
		// Fault injection needs the real install helper's native "set" phase so
		// the target can be damaged before restarting. The same verified helper
		// is used by the public update API exercised above; no fake slot state.
		await titand.vm.sshAsRoot(
			`python3 /usr/libexec/titan-system-update.py install --current-version ${fixture.baselineVersion} --version ${fixture.candidateVersion} --channel stable`,
		)
		await titand.vm.sshAsRoot(`set -e
boot_device=$(rugix-ctrl utils resolve-partition 3 | jq -er '.device')
mkdir -p /run/titan-recovery-fault
mount "$boot_device" /run/titan-recovery-fault
printf 'echo "TITAN_TEST_DAMAGED_TRIAL"\n' > /run/titan-recovery-fault/grub.cfg
sync
umount /run/titan-recovery-fault`)
		await titand.vm.powerOff()
		await titand.vm.powerOn()
		await waitForVersion(fixture.baselineVersion)
		expect(titand.vm.bootConsoleOutput).toContain('TITAN_TEST_DAMAGED_TRIAL')
		expect(titand.vm.bootConsoleOutput).toContain('TITAN_BOOT_SELECTED part=3 trial=true')
		expect(titand.vm.bootConsoleOutput).toContain('TITAN_BOOT_SELECTED part=2 trial=false')
		const boot = await nativeBoot()
		expect(boot.activeGroup).toBe('a')
		expect(boot.defaultGroup).toBe('a')
		expect((await titand.client.system.recoveryStatus.query()).previous).toEqual([])
		await assertMarker()
		record('failed-unconfirmed-update-automatic-fallback')
		record('failed-slot-not-offered-and-data-retained')
	})
})
