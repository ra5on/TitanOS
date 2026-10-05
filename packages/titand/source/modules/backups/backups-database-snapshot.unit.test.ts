import nodePath from 'node:path'

import BetterSqlite3 from 'better-sqlite3'
import fse from 'fs-extra'
import {afterEach, expect, test, vi} from 'vitest'

import type Titand from '../../index.js'
import temporaryDirectory from '../utilities/temporary-directory.js'
import Backups, {TITAN_DATABASE_BACKUP_DIRECTORY} from './backups.js'

const temporary = temporaryDirectory()

afterEach(async () => temporary.destroyRoot())

async function fixture(createTitanDatabaseBackup = vi.fn()) {
	const dataDirectory = await temporary.create()
	const backups = new Backups({
		dataDirectory,
		logger: {createChildLogger: () => ({log: vi.fn(), verbose: vi.fn(), error: vi.fn()})},
		files: {
			getBaseDirectory: () => nodePath.join(dataDirectory, 'backups'),
			fileIndex: {createTitanDatabaseBackup},
			thumbnails: {thumbnailDirectory: nodePath.join(dataDirectory, 'thumbnails')},
			virtualToSystemPathUnsafe: (virtualPath: string) =>
				nodePath.join(dataDirectory, virtualPath.slice(1).toLowerCase()),
		},
		apps: {instances: []},
		store: {get: async () => undefined},
	} as unknown as Titand)
	return {backups, dataDirectory}
}

test('prepares one validated database file without SQLite sidecars', async () => {
	const createTitanDatabaseBackup = vi.fn(async (databasePath: string) => {
		await createProbeDatabase(databasePath, 'snapshot state')
		await Promise.all([
			fse.writeFile(`${databasePath}-wal`, ''),
			fse.writeFile(`${databasePath}-shm`, ''),
			fse.writeFile(`${databasePath}-journal`, ''),
		])
	})
	const {backups, dataDirectory} = await fixture(createTitanDatabaseBackup)
	const backupPath = nodePath.join(dataDirectory, TITAN_DATABASE_BACKUP_DIRECTORY, 'titan.db')

	await backups.prepareTitanDatabaseBackup()

	expect(readProbe(backupPath)).toStrictEqual({value: 'snapshot state'})
	await expect(fse.pathExists(`${backupPath}-wal`)).resolves.toBe(false)
	await expect(fse.pathExists(`${backupPath}-shm`)).resolves.toBe(false)
	await expect(fse.pathExists(`${backupPath}-journal`)).resolves.toBe(false)
})

test('excludes every live SQLite file while including the standalone backup directory', async () => {
	const {backups, dataDirectory} = await fixture()

	await backups.createIgnoreFile()

	const ignore = await fse.readFile(nodePath.join(dataDirectory, '.kopiaignore'), 'utf8')
	expect(ignore).toMatch(/^\/titan\.db$/m)
	expect(ignore).toMatch(/^\/titan\.db-wal$/m)
	expect(ignore).toMatch(/^\/titan\.db-shm$/m)
	expect(ignore).toMatch(/^\/titan\.db-journal$/m)
	expect(ignore).not.toContain(`/${TITAN_DATABASE_BACKUP_DIRECTORY}`)
})

async function createProbeDatabase(databasePath: string, value: string) {
	await fse.ensureDir(nodePath.dirname(databasePath))
	const database = new BetterSqlite3(databasePath)
	database.exec('CREATE TABLE backup_probe(value TEXT NOT NULL)')
	database.prepare('INSERT INTO backup_probe(value) VALUES (?)').run(value)
	database.close()
}

function readProbe(databasePath: string) {
	const database = new BetterSqlite3(databasePath, {readonly: true})
	try {
		expect(database.pragma('quick_check', {simple: true})).toBe('ok')
		return database.prepare('SELECT value FROM backup_probe').get()
	} finally {
		database.close()
	}
}

test('promotes a validated standalone database and removes stale live sidecars', async () => {
	const {backups, dataDirectory} = await fixture()
	const databasePath = nodePath.join(dataDirectory, 'titan.db')
	const backupDirectory = nodePath.join(dataDirectory, TITAN_DATABASE_BACKUP_DIRECTORY)
	const backupPath = nodePath.join(backupDirectory, 'titan.db')
	await createProbeDatabase(databasePath, 'live state')
	await createProbeDatabase(backupPath, 'snapshot state')
	await Promise.all([
		fse.writeFile(`${databasePath}-wal`, 'stale wal'),
		fse.writeFile(`${databasePath}-shm`, 'stale shm'),
		fse.writeFile(`${databasePath}-journal`, 'stale journal'),
	])

	await expect(backups.promoteTitanDatabaseBackup(dataDirectory)).resolves.toBe(true)

	expect(readProbe(databasePath)).toStrictEqual({value: 'snapshot state'})
	await expect(fse.pathExists(backupDirectory)).resolves.toBe(false)
	await expect(fse.pathExists(`${databasePath}-wal`)).resolves.toBe(false)
	await expect(fse.pathExists(`${databasePath}-shm`)).resolves.toBe(false)
	await expect(fse.pathExists(`${databasePath}-journal`)).resolves.toBe(false)
})

test('rejects an invalid staged database without replacing existing state', async () => {
	const {backups, dataDirectory} = await fixture()
	const databasePath = nodePath.join(dataDirectory, 'titan.db')
	const backupPath = nodePath.join(dataDirectory, TITAN_DATABASE_BACKUP_DIRECTORY, 'titan.db')
	await createProbeDatabase(databasePath, 'live state')
	await fse.outputFile(backupPath, 'not a database')

	await expect(backups.promoteTitanDatabaseBackup(dataDirectory)).rejects.toThrow()

	expect(readProbe(databasePath)).toStrictEqual({value: 'live state'})
	await expect(fse.readFile(backupPath, 'utf8')).resolves.toBe('not a database')
})

test('leaves legacy backups without a staged database unchanged', async () => {
	const {backups, dataDirectory} = await fixture()
	const databasePath = nodePath.join(dataDirectory, 'titan.db')
	await createProbeDatabase(databasePath, 'legacy snapshot state')

	await expect(backups.promoteTitanDatabaseBackup(dataDirectory)).resolves.toBe(false)

	expect(readProbe(databasePath)).toStrictEqual({value: 'legacy snapshot state'})
})
