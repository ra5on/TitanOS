import {expect, beforeAll, beforeEach, afterAll, afterEach, describe, test} from 'vitest'

import {createTestVm} from '../test-utilities/create-test-titand.js'
import {
	bootWithExternalStorage,
	expectBackupFiles,
	externalPath,
	installBackupIgnoreFixtureApp,
	latestBackupFiles,
	repositoryPassword,
} from './backups.vm-test-helpers.js'

describe.sequential('Backups ignored paths', () => {
	let titand: Awaited<ReturnType<typeof createTestVm>>
	let failed = false
	let repositoryId: string

	beforeAll(async () => {
		titand = await createTestVm({device: 'titan-home'})
		await bootWithExternalStorage(titand)
	})

	afterAll(async () => await titand?.cleanup())

	afterEach(({task}) => {
		if (task.result?.state === 'fail') failed = true
	})

	beforeEach(({skip}) => {
		if (failed) skip()
	})

	test('creates a repository for ignored path coverage', async () => {
		repositoryId = await titand.client.backups.createRepository.mutate({
			path: externalPath,
			password: repositoryPassword,
		})
		expect(repositoryId).toMatch(/[a-f0-9]{8}$/)
	})

	test('respects user ignored paths', async () => {
		await expect(titand.client.backups.backup.mutate({repositoryId})).resolves.toBe(true)
		await expect(latestBackupFiles(titand, repositoryId, undefined)).resolves.toContain('home')

		await expect(titand.client.backups.getIgnoredPaths.query()).resolves.not.toContain('/Home')
		await expect(titand.client.backups.addIgnoredPath.mutate({path: '/Home'})).resolves.toBe(true)
		await expect(titand.client.backups.getIgnoredPaths.query()).resolves.toContain('/Home')
		await expect(titand.client.backups.addIgnoredPath.mutate({path: '/App/foo'})).rejects.toThrow(
			'Path to exclude must be in /Home',
		)

		await expect(titand.client.backups.backup.mutate({repositoryId})).resolves.toBe(true)
		await expect(latestBackupFiles(titand, repositoryId, undefined)).resolves.not.toContain('home')

		await expect(titand.client.backups.removeIgnoredPath.mutate({path: '/Home'})).resolves.toBe(true)
		await expect(titand.client.backups.getIgnoredPaths.query()).resolves.not.toContain('/Home')
		await expect(titand.client.backups.removeIgnoredPath.mutate({path: '/External'})).rejects.toThrow(
			'Path to exclude must be in /Home',
		)

		await expect(titand.client.backups.backup.mutate({repositoryId})).resolves.toBe(true)
		await expectBackupFiles(titand, repositoryId, undefined, 'home')
	})

	test('respects app backupIgnore glob patterns', async () => {
		await installBackupIgnoreFixtureApp(titand)

		await expect(titand.client.backups.backup.mutate({repositoryId})).resolves.toBe(true)
		await expectBackupFiles(titand, repositoryId, '/app-data/vm-backup-ignore', 'logs')
		await expectBackupFiles(titand, repositoryId, '/app-data/vm-backup-ignore', 'important-data')

		const logsDirFiles = await latestBackupFiles(titand, repositoryId, '/app-data/vm-backup-ignore/logs')
		expect(logsDirFiles).not.toContain('app.log')

		const importantDirFiles = await latestBackupFiles(
			titand,
			repositoryId,
			'/app-data/vm-backup-ignore/important-data',
		)
		expect(importantDirFiles).toContain('config.json')
	})
})
