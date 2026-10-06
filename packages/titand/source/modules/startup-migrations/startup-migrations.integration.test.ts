import {expect, beforeEach, afterEach, test} from 'vitest'
import {createTRPCProxyClient, httpBatchLink} from '@trpc/client'
import yaml from 'js-yaml'
import fse from 'fs-extra'
import pWaitFor from 'p-wait-for'

import createTestTitand from '../test-utilities/create-test-titand.js'
import type {AppRouter} from '../server/trpc/index.js'

async function readYaml(path: string) {
	return yaml.load(await fse.readFile(path, 'utf8'))
}

type ReportedSystemVersion = {
	version: string
	name: string
	previousVersion?: string
}

function createUnauthenticatedClient(port: number) {
	return createTRPCProxyClient<AppRouter>({
		links: [
			httpBatchLink({
				// titand binds IPv4 loopback by default and localhost can resolve to ::1 first
				url: `http://127.0.0.1:${port}/trpc`,
			}),
		],
	})
}

async function waitForReportedSystemVersion() {
	let reportedVersion: ReportedSystemVersion | undefined
	await pWaitFor(
		async () => {
			const port = titand.instance.server.port
			if (!port) return false

			try {
				reportedVersion = await createUnauthenticatedClient(port).system.version.query()
				return true
			} catch {
				return false
			}
		},
		{interval: 100, timeout: 5000},
	)

	if (!reportedVersion) throw new Error('Timed out waiting for system.version')
	return reportedVersion
}

// Fresh non-running titand instance for each test
let titand: Awaited<ReturnType<typeof createTestTitand>>
beforeEach(async () => (titand = await createTestTitand({autoStart: false})))
afterEach(() => titand.cleanup())

test('legacy downloads directory is migrated', async () => {
	const dataDirectory = titand.instance.dataDirectory
	const legacyDownloadsDirectory = `${dataDirectory}/data/storage/downloads`
	const legacyDownloadsFile = `${legacyDownloadsDirectory}/bitcoin.pdf`
	const newDownloadsDirectory = `${dataDirectory}/home/Downloads`
	const newDownloadsFile = `${newDownloadsDirectory}/bitcoin.pdf`

	// Create legacy downloads data
	await fse.ensureDir(legacyDownloadsDirectory)
	await fse.writeFile(legacyDownloadsFile, 'Bitcoin: A Peer-to-Peer Electronic Cash System')

	// Ensure files exist at legacy path and not new path
	await expect(fse.pathExists(legacyDownloadsDirectory)).resolves.toBe(true)
	await expect(fse.pathExists(legacyDownloadsFile)).resolves.toBe(true)
	await expect(fse.pathExists(newDownloadsDirectory)).resolves.toBe(false)
	await expect(fse.pathExists(newDownloadsFile)).resolves.toBe(false)

	// Start titand
	await titand.instance.start()

	// Ensure files are migrated to new path
	await expect(fse.pathExists(legacyDownloadsDirectory)).resolves.toBe(false)
	await expect(fse.pathExists(legacyDownloadsFile)).resolves.toBe(false)
	await expect(fse.pathExists(newDownloadsDirectory)).resolves.toBe(true)
	await expect(fse.pathExists(newDownloadsFile)).resolves.toBe(true)
})

test('Back That Mac Up app port is migrated from 445 to 1445', async () => {
	const {dataDirectory} = titand.instance
	const appComposeFile = `${dataDirectory}/app-data/back-that-mac-up/docker-compose.yml`

	// Create app directory structure
	await fse.ensureFile(appComposeFile)

	// Create docker-compose.yml with old port mapping
	const oldComposeContent = {
		version: '3.7',
		services: {
			timemachine: {
				ports: ['445:445'],
				random: 'property',
			},
			random: 'property',
		},
	}
	await fse.writeFile(appComposeFile, yaml.dump(oldComposeContent))

	// Mark app as installed in store
	await titand.instance.store.set('apps', ['back-that-mac-up'])

	// Check the docker-compose.yml has the expected value
	await expect(readYaml(appComposeFile)).resolves.toMatchObject(oldComposeContent)

	// Start titand
	await titand.instance.start()

	// Check if the docker-compose.yml has been updated with the new port mapping
	// and all other values are the same
	await expect(readYaml(appComposeFile)).resolves.toMatchObject({
		version: '3.7',
		services: {
			timemachine: {
				ports: ['1445:445'],
				random: 'property',
			},
			random: 'property',
		},
	})

	// Verify notification was created
	const notifications = await titand.instance.notifications.get()
	expect(notifications.includes('migrated-back-that-mac-up')).toBe(true)
})

test('first run writes version without adding a notification', async () => {
	// Ensure no version is set on first run
	const versionBefore = await titand.instance.store.get('version')
	expect(versionBefore).toBeUndefined()

	// Start titand
	await titand.instance.start()

	// Verify version is written to store
	const versionAfter = await titand.instance.store.get('version')
	expect(versionAfter).toBe(titand.instance.version)
	await expect(titand.instance.store.get('previousVersion')).resolves.toBeUndefined()

	// Verify no notification was created (first run)
	const notifications = await titand.instance.notifications.get()
	expect(notifications.includes('titanos-updated')).toBe(false)
})

test('OS update adds a notification', async () => {
	const oldVersion = '1.4.2'

	// Set an old version in the store
	await titand.instance.store.set('version', oldVersion)

	// Verify old version is set
	const versionBefore = await titand.instance.store.get('version')
	expect(versionBefore).toBe(oldVersion)

	// Start titand
	await titand.instance.start()

	// Verify version is updated to current version
	const versionAfter = await titand.instance.store.get('version')
	expect(versionAfter).toBe(titand.instance.version)
	expect(versionAfter).not.toBe(oldVersion)
	await expect(titand.instance.store.get('previousVersion')).resolves.toBe(oldVersion)

	// Verify notification was created
	const notifications = await titand.instance.notifications.get()
	expect(notifications.includes('titanos-updated')).toBe(true)
})

test('restarting with same version does not add a notification', async () => {
	const currentVersion = titand.instance.version

	// Start titand
	await titand.instance.start()

	// Verify version is written after first start
	const versionAfterFirstStart = await titand.instance.store.get('version')
	expect(versionAfterFirstStart).toBe(currentVersion)

	// Stop titand
	await titand.instance.stop()

	// Restart titand with the same version
	await titand.instance.start()

	// Verify version remains the same
	const versionAfterRestart = await titand.instance.store.get('version')
	expect(versionAfterRestart).toBe(currentVersion)

	// Verify no notification was created
	const notifications = await titand.instance.notifications.get()
	expect(notifications.includes('titanos-updated')).toBe(false)
})

test('OS update after a previous boot reports current and previous versions', async () => {
	const oldVersion = '1.4.2'
	const currentVersion = titand.instance.version

	// Start titand with the current version
	await titand.instance.start()

	// Verify the current version is written after first start
	await expect(titand.instance.store.get('version')).resolves.toBe(currentVersion)

	// Stop titand before simulating an update from an older version
	await titand.instance.stop()

	// Overwrite the stored version to simulate updating from an old booted OS
	await titand.instance.store.set('version', oldVersion)

	// Start titand again with the current version
	await titand.instance.start()

	// Verify the store and version route report the current version and where we came from
	await expect(titand.instance.store.get('version')).resolves.toBe(currentVersion)
	await expect(titand.instance.store.get('previousVersion')).resolves.toBe(oldVersion)

	const reportedVersion = await waitForReportedSystemVersion()
	expect(reportedVersion.version).toBe(currentVersion)
	expect(reportedVersion.previousVersion).toBe(oldVersion)

	// Verify notification was created
	const notifications = await titand.instance.notifications.get()
	expect(notifications.includes('titanos-updated')).toBe(true)
})
