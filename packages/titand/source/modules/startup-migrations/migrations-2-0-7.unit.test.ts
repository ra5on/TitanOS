import fsp from 'node:fs/promises'
import os from 'node:os'
import nodePath from 'node:path'
import BetterSqlite3 from 'better-sqlite3'
import fse from 'fs-extra'
import {afterEach, describe, expect, test, vi} from 'vitest'

// Device detection and legacy data import are not part of this migration
vi.mock('../system/system.js', () => ({detectDevice: vi.fn()}))
vi.mock('../migration/migration.js', () => ({
	findExternalTitanInstall: vi.fn(),
	runPreMigrationChecks: vi.fn(),
	migrateData: vi.fn(),
}))

import FileStore from '../utilities/file-store.js'
import {renamePath, renamePathsDeep} from '../files/home-folders.js'
import Migration from './index.js'

const directories: string[] = []
afterEach(async () => {
	await Promise.all(directories.splice(0).map((path) => fsp.rm(path, {recursive: true, force: true})))
})

async function setup(store: Record<string, unknown>) {
	const dataDirectory = await fsp.mkdtemp(nodePath.join(os.tmpdir(), 'titan-home-folders-'))
	directories.push(dataDirectory)
	const fileStore = new FileStore<any>({filePath: `${dataDirectory}/titan.yaml`})
	await fileStore.overwrite(store)
	const logger = {log: () => {}, error: () => {}}
	const titand = {
		dataDirectory,
		store: fileStore,
		files: {getBaseDirectory: () => `${dataDirectory}/home`},
		logger: {createChildLogger: () => logger},
	}
	return {dataDirectory, store: fileStore, migration: new Migration(titand as any)}
}

describe('TitanOS 2.0.7 startup migrations', () => {
	test('rewrites only paths inside renamed folders', () => {
		const renames = [{from: '/Home/Photos', to: '/Home/Fotos'}]
		expect(renamePath('/Home/Photos', renames)).toBe('/Home/Fotos')
		expect(renamePath('/Home/Photos/Urlaub/a.jpg', renames)).toBe('/Home/Fotos/Urlaub/a.jpg')
		expect(renamePath('/Home/Photoshop', renames)).toBe('/Home/Photoshop')
		const date = new Date(0)
		expect(renamePathsDeep({at: date, list: ['/Home/Photos/x'], ['/Home/Photos']: 1}, renames)).toEqual({
			at: date,
			list: ['/Home/Fotos/x'],
			['/Home/Fotos']: 1,
		})
	})

	test('renames owner and member folders and repoints stored paths and Photos scopes', async () => {
		const {dataDirectory, store, migration} = await setup({
			files: {favorites: ['/Home/Downloads', '/Home/Documents', '/Users/alice/Photos/Urlaub']},
			shares: [{path: '/Home/Photos'}],
		})
		await fse.outputFile(`${dataDirectory}/home/Documents/brief.txt`, 'owner')
		await fse.outputFile(`${dataDirectory}/home/Photos/a.jpg`, 'photo')
		await fse.ensureDir(`${dataDirectory}/home/Downloads`)
		await fse.outputFile(`${dataDirectory}/members/alice/home/Photos/Urlaub/b.jpg`, 'member')
		const database = new BetterSqlite3(`${dataDirectory}/titan.db`)
		database.exec('CREATE TABLE photos_sources (id TEXT PRIMARY KEY, scope_paths TEXT)')
		database.prepare('INSERT INTO photos_sources VALUES (?, ?)').run('s', JSON.stringify(['/Home/Photos/Urlaub']))
		database.close()

		await migration.migrateGermanHomeFolders()

		await expect(fse.readFile(`${dataDirectory}/home/Dokumente/brief.txt`, 'utf8')).resolves.toBe('owner')
		await expect(fse.readFile(`${dataDirectory}/home/Fotos/a.jpg`, 'utf8')).resolves.toBe('photo')
		await expect(fse.readFile(`${dataDirectory}/members/alice/home/Fotos/Urlaub/b.jpg`, 'utf8')).resolves.toBe(
			'member',
		)
		expect(await fse.pathExists(`${dataDirectory}/home/Documents`)).toBe(false)
		expect(await fse.pathExists(`${dataDirectory}/home/Downloads`)).toBe(true)
		expect(await store.get('files.favorites')).toEqual(['/Home/Downloads', '/Home/Dokumente', '/Users/alice/Fotos/Urlaub'])
		expect(await store.get('shares')).toEqual([{path: '/Home/Fotos'}])
		expect(await store.get('migrations.germanHomeFolders')).toBe(true)
		const reopened = new BetterSqlite3(`${dataDirectory}/titan.db`, {readonly: true})
		expect(reopened.prepare('SELECT scope_paths FROM photos_sources').get()).toEqual({
			scope_paths: JSON.stringify(['/Home/Fotos/Urlaub']),
		})
		reopened.close()
	})

	test('never merges into a German folder that already contains files', async () => {
		const {dataDirectory, store, migration} = await setup({files: {favorites: ['/Home/Documents']}})
		await fse.outputFile(`${dataDirectory}/home/Documents/alt.txt`, 'old')
		await fse.outputFile(`${dataDirectory}/home/Dokumente/neu.txt`, 'new')

		await migration.migrateGermanHomeFolders()

		await expect(fse.readFile(`${dataDirectory}/home/Documents/alt.txt`, 'utf8')).resolves.toBe('old')
		await expect(fse.readFile(`${dataDirectory}/home/Dokumente/neu.txt`, 'utf8')).resolves.toBe('new')
		expect(await store.get('files.favorites')).toEqual(['/Home/Documents'])
	})

	test('resets every account to the default desktop transparency only once', async () => {
		const {store, migration} = await setup({
			user: {name: 'Owner', desktopTransparency: 20},
			members: [{id: 'alice', desktopTransparency: 90}, {id: 'bob'}],
		})

		await migration.resetDesktopTransparency()

		expect(await store.get('user')).toEqual({name: 'Owner'})
		expect(await store.get('members')).toEqual([{id: 'alice'}, {id: 'bob'}])
		await store.set('user.desktopTransparency', 40)
		await migration.resetDesktopTransparency()
		expect(await store.get('user.desktopTransparency')).toBe(40)
	})

	test('finishes repointing paths after an interrupted earlier run', async () => {
		const {dataDirectory, store, migration} = await setup({files: {favorites: ['/Home/Photos']}})
		await fse.ensureDir(`${dataDirectory}/home/Fotos`)

		await migration.migrateGermanHomeFolders()

		expect(await store.get('files.favorites')).toEqual(['/Home/Fotos'])
	})
})
