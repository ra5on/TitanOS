import {Buffer} from 'node:buffer'
import nodePath from 'node:path'

import {expect} from 'vitest'
import pRetry from 'p-retry'
import pWaitFor from 'p-wait-for'

import {createTestVm} from '../test-utilities/create-test-titand.js'
import type {RestoreStatus} from './backups.js'

export const externalPath = '/External/SanDisk'
export const repositoryPassword = 'test-password'
export const vmDataDirectory = '/home/titan/titan'

type TestVm = Awaited<ReturnType<typeof createTestVm>>

export async function bootWithExternalStorage(titand: TestVm) {
	await titand.vm.addUsbStorage({slot: 1, size: '64G'})
	await titand.vm.powerOn()
	await titand.registerAndLogin()
	await formatUsbStorage(titand)
}

export async function formatUsbStorage(titand: TestVm) {
	const devices = await titand.client.files.externalDevices.query()
	const usbDevice = devices.find((device) => device.transport === 'usb')
	expect(usbDevice).toBeDefined()

	await expect(
		titand.client.files.formatExternalDevice.mutate({
			deviceId: usbDevice!.id,
			filesystem: 'ext4',
			label: 'SanDisk',
		}),
	).resolves.toBe(true)

	await titand.vm.powerOff()
	await titand.vm.powerOn()
	await titand.login()
	await waitForExternalStorage(titand)
	await waitForBackupsKopiaReady(titand)
}

export async function waitForExternalStorage(
	titand: TestVm,
	{path = externalPath, authenticated = true}: {path?: string; authenticated?: boolean} = {},
) {
	const client = authenticated ? titand.client : titand.unauthenticatedClient

	await pRetry(
		async () => {
			const devices = await client.files.externalDevices.query()
			const mountpoints = devices.flatMap((device) => device.partitions.flatMap((partition) => partition.mountpoints))
			expect(mountpoints).toContain(path)
		},
		{retries: 60, factor: 1, minTimeout: 1000, maxTimeout: 1000},
	)
}

export async function waitForBackupsKopiaReady(
	titand: TestVm,
	{path = `${externalPath}/Startup Probe`, authenticated = true}: {path?: string; authenticated?: boolean} = {},
) {
	const client = authenticated ? titand.client : titand.unauthenticatedClient

	await pRetry(
		async () => {
			try {
				await client.backups.connectToExistingRepository.mutate({
					path,
					password: repositoryPassword,
				})
			} catch (error) {
				if (
					error instanceof Error &&
					(error.message.includes('[shutting-down]') || error.message.includes('Invalid path'))
				) {
					throw error
				}
				return
			}

			throw new Error('Unexpectedly connected to startup probe repository')
		},
		{retries: 60, factor: 1, minTimeout: 1000, maxTimeout: 1000},
	)
}

export async function createNetworkBackupShare(titand: TestVm, directoryName = 'Backups') {
	await titand.client.files.createDirectory.mutate({path: `/Home/${directoryName}`})
	await titand.client.files.addShare.mutate({path: `/Home/${directoryName}`})

	const networkSharePath = await connectNetworkBackupShare(titand, directoryName)
	await waitForBackupsKopiaReady(titand, {path: `${networkSharePath}/Startup Probe`})
	return networkSharePath
}

export async function connectNetworkBackupShare(titand: TestVm, directoryName = 'Backups') {
	const password = await titand.client.files.sharePassword.query()

	return pRetry(
		() =>
			titand.client.files.addNetworkShare.mutate({
				host: 'localhost',
				share: `${directoryName} (Titan)`,
				username: 'titan',
				password,
			}),
		{retries: 10, factor: 1, minTimeout: 1000, maxTimeout: 1000},
	)
}

export async function writeDataFile(titand: TestVm, relativePath: string, contents: string) {
	if (relativePath.startsWith('home/')) {
		const query = new URLSearchParams({
			path: `/Home/${relativePath.slice('home/'.length)}`,
			collision: 'replace',
		})
		await titand.api.post(`files/upload?${query}`, {body: contents})
		return
	}

	const encodedContents = Buffer.from(contents).toString('base64')
	const systemPath = `${vmDataDirectory}/${relativePath}`
	const directory = nodePath.posix.dirname(systemPath)

	// The files API intentionally does not expose app-data writes; seed backup
	// fixtures directly in the VM data directory so Kopia sees real files.
	await titand.vm.sshAsRoot(`
set -eu
mkdir -p '${directory}'
printf '%s' '${encodedContents}' | base64 -d > '${systemPath}'
`)
}

export async function latestBackup(titand: TestVm, repositoryId: string) {
	const backups = await titand.client.backups.listBackups.query({repositoryId})
	const backup = backups.at(-1)
	expect(backup).toBeDefined()
	return backup!
}

export async function latestBackupFiles(titand: TestVm, repositoryId: string, path?: string) {
	const backup = await latestBackup(titand, repositoryId)
	return titand.client.backups.listBackupFiles.query({backupId: backup.id, path})
}

export async function expectBackupFiles(
	titand: TestVm,
	repositoryId: string,
	path: string | undefined,
	expectedFile: string,
) {
	await pRetry(
		async () => {
			const files = await latestBackupFiles(titand, repositoryId, path)
			expect(files).toContain(expectedFile)
		},
		{retries: 5, factor: 1, minTimeout: 1000, maxTimeout: 1000},
	)
}

function ignoreRestoreDisconnect(error: unknown) {
	if (!(error instanceof Error)) throw error

	const message = error.message.toLowerCase()
	const expectedDisconnect =
		message.includes('fetch failed') ||
		message.includes('terminated') ||
		message.includes('econnreset') ||
		message.includes('socket hang up') ||
		// The shared test fetch wrapper retries once after a dropped connection.
		// A successful restore revokes sessions before that retry reaches the new
		// titand process, so the retry can correctly receive this response.
		message.includes('invalid token') ||
		(message.includes('command was killed with sigterm') && message.includes('reboot'))
	if (!expectedDisconnect) throw error
}

async function withTimeout<T>(promise: Promise<T>, milliseconds: number) {
	let timeout: ReturnType<typeof setTimeout> | undefined
	try {
		return await Promise.race([
			promise,
			new Promise<never>((_, reject) => {
				timeout = setTimeout(() => reject(new Error('Timed out')), milliseconds)
			}),
		])
	} finally {
		if (timeout) clearTimeout(timeout)
	}
}

async function waitForTitandDisconnect(titand: TestVm) {
	await pWaitFor(
		async () => {
			try {
				await withTimeout(titand.unauthenticatedClient.user.exists.query(), 5000)
				return false
			} catch {
				return true
			}
		},
		{interval: 1000, timeout: 480_000},
	)
}

export async function restoreBackupAndWait({
	titand,
	backupId,
	authenticated = true,
}: {
	titand: TestVm
	backupId: string
	authenticated?: boolean
}) {
	const restoreClient = authenticated ? titand.client : titand.unauthenticatedClient
	let restoreSettled = false
	const restorePromise = restoreClient.backups.restoreBackup
		.mutate({backupId})
		.catch(ignoreRestoreDisconnect)
		.finally(() => {
			restoreSettled = true
		})

	const waitForDisconnectPromise = waitForTitandDisconnect(titand)

	try {
		await Promise.race([restorePromise, waitForDisconnectPromise])
		if (!restoreSettled) await restorePromise
		// The reboot command can return before HTTP drops. Always observe the
		// real VM disconnect before waiting for the restored install to start.
		await waitForDisconnectPromise
	} finally {
		void waitForDisconnectPromise.catch(() => {})
	}
	await titand.waitForStartup({waitForUser: true})
	await titand.login()
}

export function expectRestoreProgressEvents(events: RestoreStatus[], backupId: string) {
	expect(events).toEqual(
		expect.arrayContaining([
			expect.objectContaining({
				backupId,
				running: true,
				progress: 0,
				error: false,
			}),
		]),
	)
	// The final {running: false, progress: 100} completion event is emitted moments
	// before the restore reboot, so its delivery over the dying connection is best
	// effort (the event bus has no replay and the fresh boot does not re-emit it).
	// Completion is instead asserted by the restore-current test via the post-reboot
	// restoreStatus query and restored file contents. The live stream is still
	// expected to report the copy reaching 100%.
	expect(events).toEqual(
		expect.arrayContaining([
			expect.objectContaining({
				running: true,
				progress: 100,
				error: false,
			}),
		]),
	)
}

export async function installBackupIgnoreFixtureApp(titand: TestVm) {
	// Installing an arbitrary local app fixture through the store would add
	// unrelated app-store setup. Seed the minimum app metadata and restart
	// titand so the real apps module reloads the backupIgnore manifest.
	await titand.vm.sshAsRoot(`
set -eu
app_id='vm-backup-ignore'
app_dir='${vmDataDirectory}/app-data/'"$app_id"
mkdir -p "$app_dir/logs" "$app_dir/important-data"
printf 'log content' > "$app_dir/logs/app.log"
printf 'important config' > "$app_dir/important-data/config.json"
cat > "$app_dir/umbrel-app.yml" <<'YAML'
manifestVersion: 1
id: vm-backup-ignore
name: VM Backup Ignore
tagline: VM backup fixture
category: Development
version: '1.0.0'
port: 4000
description: VM backup fixture
website: https://umbrel.com
support: https://umbrel.com
gallery: []
backupIgnore:
  - logs/*
YAML
cat > "$app_dir/settings.yml" <<'YAML'
autoStart: false
YAML
cd /opt/titand
node --input-type=module <<'NODE'
import fs from 'node:fs'
import yaml from 'js-yaml'

const storePath = '/home/titan/titan/titan.yaml'
const store = yaml.load(fs.readFileSync(storePath, 'utf8')) ?? {}
store.apps = Array.from(new Set([...(store.apps ?? []), 'vm-backup-ignore']))
fs.writeFileSync(storePath, yaml.dump(store))
NODE
`)

	await titand.vm.sshAsRoot('systemctl restart titan')
	await titand.waitForStartup({waitForUser: true})
	await titand.login()
	await waitForExternalStorage(titand)
	await waitForBackupsKopiaReady(titand)
}
