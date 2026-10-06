import {utimes} from 'node:fs/promises'

import fse from 'fs-extra'
import {afterEach, beforeEach, expect, test} from 'vitest'

import createTestTitand from '../test-utilities/create-test-titand.js'

let titand: Awaited<ReturnType<typeof createTestTitand>>

beforeEach(async () => {
	titand = await createTestTitand()
	await titand.registerAndLogin()
})

afterEach(async () => {
	await titand.cleanup()
})

async function reconcileHome(reason: string) {
	await titand.instance.files.fileIndex.reconcileRoot('/Home', reason)
}

async function recentNames() {
	return (await titand.client.files.recents.query()).map(({name}) => name)
}

test('recents() throws invalid error without auth token', async () => {
	await expect(titand.unauthenticatedClient.files.recents.query()).rejects.toThrow('Invalid token')
})

test('recents() returns files by indexed modification time rather than event order', async () => {
	const directory = `${titand.instance.dataDirectory}/home/recents-order-test`
	const newest = `${directory}/newest.txt`
	const middle = `${directory}/middle.txt`
	const oldest = `${directory}/oldest.txt`
	await Promise.all([
		fse.outputFile(newest, 'newest'),
		fse.outputFile(middle, 'middle'),
		fse.outputFile(oldest, 'oldest'),
	])
	await Promise.all([
		utimes(newest, new Date('2025-01-03T00:00:00Z'), new Date('2025-01-03T00:00:00Z')),
		utimes(middle, new Date('2025-01-02T00:00:00Z'), new Date('2025-01-02T00:00:00Z')),
		utimes(oldest, new Date('2025-01-01T00:00:00Z'), new Date('2025-01-01T00:00:00Z')),
	])
	await reconcileHome('recents-order-test')

	await expect(recentNames()).resolves.toStrictEqual(['newest.txt', 'middle.txt', 'oldest.txt'])
})

test('recents() updates when indexed file modification times change', async () => {
	const directory = `${titand.instance.dataDirectory}/home/recents-update-test`
	const changed = `${directory}/changed.txt`
	const initiallyNewest = `${directory}/initially-newest.txt`
	await Promise.all([fse.outputFile(changed, 'old'), fse.outputFile(initiallyNewest, 'new')])
	await Promise.all([
		utimes(changed, new Date('2025-01-01T00:00:00Z'), new Date('2025-01-01T00:00:00Z')),
		utimes(initiallyNewest, new Date('2025-01-02T00:00:00Z'), new Date('2025-01-02T00:00:00Z')),
	])
	await reconcileHome('recents-update-initial')
	await expect(recentNames()).resolves.toStrictEqual(['initially-newest.txt', 'changed.txt'])

	await fse.writeFile(changed, 'updated')
	const updated = new Date('2025-01-03T00:00:00Z')
	await utimes(changed, updated, updated)
	await titand.instance.files.fileIndex.reconcilePath(changed)

	await expect(recentNames()).resolves.toStrictEqual(['changed.txt', 'initially-newest.txt'])
})

test('recents() removes files deleted from the index', async () => {
	const directory = `${titand.instance.dataDirectory}/home/recents-delete-test`
	const removed = `${directory}/removed.txt`
	const kept = `${directory}/kept.txt`
	await Promise.all([fse.outputFile(removed, 'remove'), fse.outputFile(kept, 'keep')])
	await reconcileHome('recents-delete-initial')
	await expect(recentNames()).resolves.toEqual(expect.arrayContaining(['removed.txt', 'kept.txt']))

	await fse.remove(removed)
	await titand.instance.files.fileIndex.reconcilePath(removed)

	await expect(recentNames()).resolves.toStrictEqual(['kept.txt'])
})

test('recents() returns only visible regular files from Home', async () => {
	const home = `${titand.instance.dataDirectory}/home`
	const appData = `${titand.instance.dataDirectory}/app-data`
	await Promise.all([
		fse.outputFile(`${home}/visible.txt`, 'visible'),
		fse.outputFile(`${home}/.DS_Store`, 'hidden'),
		fse.ensureDir(`${home}/directory`),
		fse.outputFile(`${appData}/app-file.txt`, 'app'),
	])
	await Promise.all([
		reconcileHome('recents-visible-files'),
		titand.instance.files.fileIndex.reconcileRoot('/Apps', 'recents-visible-files'),
	])

	await expect(recentNames()).resolves.toStrictEqual(['visible.txt'])
})

test('recents() excludes files inside Titan backup directories at any depth', async () => {
	const home = `${titand.instance.dataDirectory}/home`
	const backupName = titand.instance.backups.backupDirectoryName
	await Promise.all([
		fse.outputFile(`${home}/visible.txt`, 'visible'),
		fse.outputFile(`${home}/${backupName}/root-backup.txt`, 'backup'),
		fse.outputFile(`${home}/nested/${backupName}/nested-backup.txt`, 'nested backup'),
		fse.outputFile(`${home}/ordinary/${backupName}`, 'ordinary file'),
	])
	await reconcileHome('recents-backup-filter')

	const names = await recentNames()
	expect(names).toHaveLength(2)
	expect(names).toEqual(expect.arrayContaining(['visible.txt', backupName]))
})

test('recents() excludes files moved to Trash', async () => {
	const directory = `${titand.instance.dataDirectory}/home/recents-trash-test`
	const trashed = `${directory}/trashed.txt`
	const kept = `${directory}/kept.txt`
	await Promise.all([fse.outputFile(trashed, 'trash'), fse.outputFile(kept, 'keep')])
	await reconcileHome('recents-trash-initial')
	await expect(recentNames()).resolves.toEqual(expect.arrayContaining(['trashed.txt', 'kept.txt']))

	await titand.client.files.trash.mutate({path: '/Home/recents-trash-test/trashed.txt'})
	await Promise.all([
		reconcileHome('recents-trash-home'),
		titand.instance.files.fileIndex.reconcileRoot('/Trash', 'recents-trash-trash'),
	])

	await expect(recentNames()).resolves.toStrictEqual(['kept.txt'])
})

test('recents() survives restart through the durable index without writing a recents snapshot', async () => {
	const path = `${titand.instance.dataDirectory}/home/persist.txt`
	await fse.writeFile(path, 'persist')
	await reconcileHome('recents-restart')
	await expect(recentNames()).resolves.toStrictEqual(['persist.txt'])
	await expect((titand.instance.store as any).get('files.recents')).resolves.toBeUndefined()

	await titand.instance.stop()
	await titand.instance.start()

	await expect(recentNames()).resolves.toStrictEqual(['persist.txt'])
	await expect((titand.instance.store as any).get('files.recents')).resolves.toBeUndefined()
})

test('recents() returns at most the 50 most recently modified files', async () => {
	const directory = `${titand.instance.dataDirectory}/home/recents-limit-test`
	await fse.ensureDir(directory)
	const epoch = Date.parse('2025-01-01T00:00:00Z')
	for (let number = 1; number <= 51; number++) {
		const path = `${directory}/file${number}.txt`
		await fse.writeFile(path, '')
		const modified = new Date(epoch + number * 1000)
		await utimes(path, modified, modified)
	}
	await reconcileHome('recents-limit')

	const expected = Array.from({length: 50}, (_, index) => `file${51 - index}.txt`)
	await expect(recentNames()).resolves.toStrictEqual(expected)
})
