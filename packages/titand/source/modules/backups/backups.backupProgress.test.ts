import {expect, beforeAll, afterAll, test, describe} from 'vitest'

import fse from 'fs-extra'
import {delay} from 'es-toolkit'

import createTestTitand from '../test-utilities/create-test-titand.js'
import {TITAN_DATABASE_BACKUP_DIRECTORY} from './backups.js'

let titand: Awaited<ReturnType<typeof createTestTitand>>

beforeAll(async () => {
	titand = await createTestTitand()
	await titand.registerAndLogin()
})

afterAll(async () => {
	await titand.cleanup()
})

describe(`backupProgress()`, () => {
	test('includes durable Titan state while excluding disposable indexes and generated artifacts', async () => {
		await titand.instance.backups.createIgnoreFile()
		const ignore = await fse.readFile(`${titand.instance.dataDirectory}/.kopiaignore`, 'utf8')

		expect(ignore).toContain('file-index')
		expect(ignore).toContain('thumbnails')
		expect(ignore).toMatch(/^\/titan\.db$/m)
		expect(ignore).toMatch(/^\/titan\.db-wal$/m)
		expect(ignore).toMatch(/^\/titan\.db-shm$/m)
		expect(ignore).not.toContain(`/${TITAN_DATABASE_BACKUP_DIRECTORY}`)
		expect(ignore).toContain('machines/*/operations')
		expect(ignore).toContain('machines/*/media')
	})

	test('throws invalid error without auth token', async () => {
		await expect(titand.unauthenticatedClient.backups.backupProgress.query()).rejects.toThrow('Invalid token')
	})

	test('returns an empty array if no backups are in progress', async () => {
		await expect(titand.client.backups.backupProgress.query()).resolves.toMatchObject([])
	})

	test('returns backup progress during backup operation', async () => {
		// Create fake usb drive
		const backupPath = `${titand.instance.dataDirectory}/external/SanDisk`
		await fse.mkdir(backupPath, {recursive: false})

		// Create some test data to backup
		const testDataPath = `${titand.instance.dataDirectory}/home/backup-progress-test.txt`
		await fse.mkdir(`${titand.instance.dataDirectory}/home`, {recursive: true})
		await fse.writeFile(testDataPath, 'test content for backup progress')

		const repositoryId = await titand.client.backups.createRepository.mutate({
			path: '/External/SanDisk',
			password: 'test-password',
		})

		// Listen for all backup progress events
		const collectedEvents: any[] = []
		const removeListener = titand.instance.eventBus.on(
			'backups:backup-progress',
			(progress) => void collectedEvents.push(JSON.parse(JSON.stringify(progress))),
		)

		// Test we start with no backups in progress
		await expect(titand.client.backups.backupProgress.query()).resolves.toMatchObject([])

		// Start the backup
		const backupPromise = titand.client.backups.backup.mutate({repositoryId})

		// Wait for the backup to complete
		const result = await backupPromise
		expect(result).toBe(true)

		// Test we end with no backups in progress
		await expect(titand.client.backups.backupProgress.query()).resolves.toMatchObject([])

		// Test all the events we collected
		expect(collectedEvents.length).toBeGreaterThanOrEqual(2)
		expect(collectedEvents.at(0)).toMatchObject([
			{
				repositoryId,
				percent: 0,
			},
		])
		expect(collectedEvents.at(-1)).toMatchObject([])

		// Clean up
		removeListener()
		await fse.remove(backupPath)
	})

	// Skip this for now, come back to this when we test backup events heavily
	test.skip('handles multiple concurrent backups', async () => {
		// Create two test repositories
		const backupPath1 = `${titand.instance.dataDirectory}/external/USB-1`
		const backupPath2 = `${titand.instance.dataDirectory}/external/USB-2`
		await fse.mkdir(backupPath1, {recursive: true})
		await fse.mkdir(backupPath2, {recursive: true})

		// Create test data
		const testDataPath = `${titand.instance.dataDirectory}/home/concurrent-backup-test.txt`
		await fse.writeFile(testDataPath, 'test content for concurrent backups')

		const repositoryId1 = await titand.client.backups.createRepository.mutate({
			path: '/External/USB-1',
			password: 'password1',
		})
		const repositoryId2 = await titand.client.backups.createRepository.mutate({
			path: '/External/USB-2',
			password: 'password2',
		})

		// Start both backups concurrently
		const backup1Promise = titand.client.backups.backup.mutate({repositoryId: repositoryId1})
		const backup2Promise = titand.client.backups.backup.mutate({repositoryId: repositoryId2})

		// Wait for both to start
		await delay(100)

		// Test we have two backup operations in progress
		const progressInProgress = await titand.client.backups.backupProgress.query()
		expect(progressInProgress).toHaveLength(2)

		const repo1Progress = progressInProgress.find((p) => p.repositoryId === repositoryId1)
		const repo2Progress = progressInProgress.find((p) => p.repositoryId === repositoryId2)

		expect(repo1Progress).toMatchObject({
			repositoryId: repositoryId1,
			percent: expect.any(Number),
		})
		expect(repo2Progress).toMatchObject({
			repositoryId: repositoryId2,
			percent: expect.any(Number),
		})

		// Wait for both backups to complete
		await Promise.all([backup1Promise, backup2Promise])

		// Test we end with no backups in progress
		await expect(titand.client.backups.backupProgress.query()).resolves.toMatchObject([])

		// Clean up
		await fse.remove(backupPath1)
		await fse.remove(backupPath2)
	})
})
