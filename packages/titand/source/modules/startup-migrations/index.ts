import nodePath from 'node:path'
import BetterSqlite3 from 'better-sqlite3'
import fse from 'fs-extra'
import {z} from 'zod'
import yaml from 'js-yaml'
import semver from 'semver'

import type Titand from '../../index.js'

import {detectDevice} from '../system/system.js'
import {findExternalTitanInstall, runPreMigrationChecks, migrateData} from '../migration/migration.js'
import {LEGACY_HOME_FOLDERS, renamePathsDeep, type PathRename} from '../files/home-folders.js'

async function readYaml(path: string) {
	return yaml.load(await fse.readFile(path, 'utf8'))
}

async function writeYaml(path: string, data: any) {
	return fse.writeFile(path, yaml.dump(data))
}

class Migration {
	titand: Titand
	logger: Titand['logger']

	constructor(titand: Titand) {
		this.titand = titand
		const {name} = this.constructor
		this.logger = titand.logger.createChildLogger(name.toLowerCase())
	}

	// One off migration for legacy custom Linux install users
	async migrateLegacyLinuxData() {
		const {deviceId} = await detectDevice()

		// Only run this on unknown devices AKA not a Home or a Pi
		if (deviceId !== 'unknown') return

		// Don't do anything if a user has already been registered
		if (await this.titand.user.exists()) return

		this.logger.log(
			'Unkown device booting for the first time, checking if we need to migrate legacy Linux install data...',
		)

		const externalTitanInstall = await findExternalTitanInstall()
		if (!externalTitanInstall) {
			this.logger.log('No legacy Linux install found, skipping migration')
			return
		}

		this.logger.log('Legacy Linux install found, migrating data...')

		const currentInstall = this.titand.dataDirectory
		await runPreMigrationChecks(currentInstall, externalTitanInstall as string, this.titand, false)
		await this.titand.server.start()
		await migrateData(currentInstall, externalTitanInstall as string, this.titand)
		this.logger.log('Migration complete!')
	}

	async activateImportedDataDirectory() {
		const importData = `${this.titand.dataDirectory}/import`
		const importDataExists = await fse.exists(importData)
		if (!importDataExists) return
		this.logger.log('Found Titan data to import, activating...')
		// We have to move the import dir parrallel to the data dir and then overwrte.
		// This is because fse.move doesn't work if the source is a subdirectory of the destination.
		// This is fine to do on Titan Home because all of /home is on the large data partition.
		// On Rasperry Pi the data partition is small on the SD card and only the data dir on the
		// large external USB storage. We don't currently support data import on Pi so it's ok for now
		// but we'll need to handle this if we want to support it in the future.
		const temporaryData = `${this.titand.dataDirectory}-import-temp`
		await fse.move(importData, temporaryData, {overwrite: true})
		await fse.move(temporaryData, this.titand.dataDirectory, {overwrite: true})
	}

	async migrateLegacyData() {
		// Check for a legacy <1.0 Titan data directory
		const userJsonPath = `${this.titand.dataDirectory}/db/user.json`
		const userJsonExists = await fse.exists(userJsonPath)
		if (!userJsonExists) return
		this.logger.log('Found legacy Titan data, migrating...')

		// Validate the data
		const legacyDataSchema = z.object({
			name: z.string(),
			password: z.string(),
			installedApps: z.array(z.string()).optional(),
			repos: z.array(z.string()),
			remoteTorAccess: z.boolean().optional(),
			otpUri: z.string().optional(),
		})
		const legacyDataJson = await fse.readJson(userJsonPath)
		const legacyData = legacyDataSchema.parse(legacyDataJson)

		// Migrate data
		await this.titand.user.setName(legacyData.name)
		await this.titand.user.setHashedPassword(legacyData.password)
		if (legacyData.otpUri) await this.titand.user.enable2fa(legacyData.otpUri)
		await this.titand.store.set('appRepositories', legacyData.repos)
		if (legacyData.installedApps) await this.titand.store.set('apps', legacyData.installedApps)
		if (legacyData.remoteTorAccess) await this.titand.store.set('torEnabled', legacyData.remoteTorAccess)

		// Showcase widgets for migrating users
		await this.titand.store.set('widgets', ['titan:memory', 'titan:system-stats', 'titan:storage'])

		// Ensure we have app repositories pulled otherwise there will be a race condition where
		// if an app gets started before the repo has completed it's initial pull on startup we'll
		// get the error `App with ID <appId> not found in any repository `
		await this.titand.appStore.update()

		// Mark the legacy file as migrated
		await fse.move(userJsonPath, `${userJsonPath}.migrated`)

		// Move the .env file so env vars don't get preserved
		const envPath = `${this.titand.dataDirectory}/.env`
		await fse.move(envPath, `${envPath}.migrated`)
		this.logger.log('Migration successful')
	}

	async migrateBackThatMacUpPort() {
		// Check if the Back That Mac Up app is installed
		const isBackThatMacUpInstalled = ((await this.titand.store.get('apps')) || []).includes('back-that-mac-up')
		if (!isBackThatMacUpInstalled) return

		// Check if app has already been migrated
		const composePath = `${this.titand.dataDirectory}/app-data/back-that-mac-up/docker-compose.yml`
		const newSambaPortMapping = '1445:445'
		const compose = (await readYaml(composePath)) as any
		if (compose.services.timemachine.ports[0] === newSambaPortMapping) return
		this.logger.log('Old Back That Mac Up app found, migrating...')

		// Update the docker-compose.yml file to use the new samba port mapping
		// to avoid collisions with titanOS Samba port
		compose.services.timemachine.ports = [newSambaPortMapping]
		await writeYaml(composePath, compose)
		this.logger.log('Back That Mac Up app migrated')

		// Add notification
		await this.titand.notifications.add('migrated-back-that-mac-up')
	}

	async migrateDownloadsDirectory() {
		const legacyDownloadsPath = `${this.titand.dataDirectory}/data/storage/downloads`
		const newDownloadsPath = `${this.titand.files.getBaseDirectory('/Home')}/Downloads`
		const legacyDownloadsPathExists = await fse.exists(legacyDownloadsPath)
		const newDownloadsPathHasData =
			(await fse.exists(newDownloadsPath)) && (await fse.readdir(newDownloadsPath)).length > 0
		if (!legacyDownloadsPathExists || newDownloadsPathHasData) return
		this.logger.log('Found legacy Downloads directory, migrating...')
		await fse.ensureDir(newDownloadsPath)
		await fse.move(legacyDownloadsPath, newDownloadsPath, {overwrite: true})
		this.logger.log('Downloads directory migrated')
	}

	// TitanOS 2.0.7 names the default home folders in German. Rename existing
	// Documents/Photos folders once and repoint stored paths (favorites, shares,
	// app folder access, Photos scopes). File index entries are rebuilt by the
	// startup reconcile; Photos albums are keyed by content hash.
	async migrateGermanHomeFolders() {
		if (await this.titand.store.get('migrations.germanHomeFolders')) return
		const homes = [{virtualRoot: '/Home', systemRoot: this.titand.files.getBaseDirectory('/Home')}]
		const membersDirectory = `${this.titand.dataDirectory}/members`
		if (await fse.pathExists(membersDirectory)) {
			for (const slug of await fse.readdir(membersDirectory)) {
				const systemRoot = nodePath.join(membersDirectory, slug, 'home')
				if (await fse.pathExists(systemRoot)) homes.push({virtualRoot: `/Users/${slug}`, systemRoot})
			}
		}

		const renames: PathRename[] = []
		for (const {virtualRoot, systemRoot} of homes) {
			for (const [legacyName, name] of Object.entries(LEGACY_HOME_FOLDERS)) {
				const legacy = nodePath.join(systemRoot, legacyName)
				const target = nodePath.join(systemRoot, name)
				const rename = [
					{from: `${virtualRoot}/${legacyName}`, to: `${virtualRoot}/${name}`},
					{from: legacy, to: target},
				]
				const legacyStats = await fse.lstat(legacy).catch(() => undefined)
				if (!legacyStats?.isDirectory()) {
					// Renamed by an interrupted earlier run: still repoint stored paths
					if (await fse.pathExists(target)) renames.push(...rename)
					continue
				}
				if (await fse.pathExists(target)) {
					// Only replace a target the user has not filled yet
					if ((await fse.readdir(target)).length > 0) {
						this.logger.log(`Keeping '${legacy}': '${target}' already contains files`)
						continue
					}
					await fse.rmdir(target)
				}
				await fse.rename(legacy, target)
				renames.push(...rename)
				this.logger.log(`Renamed '${legacy}' to '${target}'`)
			}
		}
		if (renames.length > 0) await this.#repointRenamedHomeFolders(renames)
		await this.titand.store.set('migrations.germanHomeFolders', true)
	}

	async #repointRenamedHomeFolders(renames: PathRename[]) {
		await this.titand.store.update((store) => {
			const renamed = renamePathsDeep(store, renames) as typeof store
			for (const key of Object.keys(store)) delete (store as Record<string, unknown>)[key]
			Object.assign(store, renamed)
		})

		const titanDatabasePath = `${this.titand.dataDirectory}/titan.db`
		if (!(await fse.pathExists(titanDatabasePath))) return
		const database = new BetterSqlite3(titanDatabasePath, {timeout: 5000})
		try {
			const table = database
				.prepare("SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = 'photos_sources'")
				.get()
			if (!table) return
			const sources = database
				.prepare('SELECT id, scope_paths FROM photos_sources WHERE scope_paths IS NOT NULL')
				.all() as {id: string; scope_paths: string}[]
			const update = database.prepare('UPDATE photos_sources SET scope_paths = ? WHERE id = ?')
			database.transaction(() => {
				for (const source of sources) {
					const paths = JSON.parse(source.scope_paths) as unknown
					const renamed = JSON.stringify(renamePathsDeep(paths, renames))
					if (renamed !== source.scope_paths) update.run(renamed, source.id)
				}
			})()
		} finally {
			database.close()
		}
	}

	// TitanOS 2.0.7 starts every account at the default desktop transparency
	// (75 %) once. Accounts can change it again afterwards.
	async resetDesktopTransparency() {
		await this.titand.store.update((store) => {
			if (store.migrations?.desktopTransparencyReset) return
			if (store.user) delete store.user.desktopTransparency
			for (const member of store.members ?? []) if (!('deleted' in member)) delete member.desktopTransparency
			store.migrations = {...store.migrations, desktopTransparencyReset: true}
		})
	}

	async start() {
		this.logger.log('Checking if any migrations are needed...')

		// Ensure data directory exists
		await fse.ensureDir(this.titand.dataDirectory)

		// Check for a data directory to import
		try {
			await this.activateImportedDataDirectory()
		} catch (error) {
			this.logger.error(`Failed to activate imported Titan data`, error)
		}

		// Check for a legacy <1.0 Titan data directory and migrate to 1.0 format if found
		try {
			await this.migrateLegacyData()
		} catch (error) {
			this.logger.error(`Failed to migrate legacy data`, error)
		}

		// Check for first boot of an unknown device and migrate legacy Linux install data if it exists
		try {
			await this.migrateLegacyLinuxData()
		} catch (error) {
			this.logger.error(`Failed to migrate legacy Linux data`, error)
		}

		// Check for the Back That Mac Up app and migrate it if it exists
		try {
			await this.migrateBackThatMacUpPort()
		} catch (error) {
			this.logger.error(`Failed to migrate Back That Mac Up app`, error)
		}

		// Migrate Downloads directory to Home/Downloads
		try {
			await this.migrateDownloadsDirectory()
		} catch (error) {
			this.logger.error(`Failed to migrate Downloads directory`, error)
		}

		// Rename the default home folders to their German names
		try {
			await this.migrateGermanHomeFolders()
		} catch (error) {
			this.logger.error(`Failed to migrate home folder names`, error)
		}

		// Start every account at the default desktop transparency once
		try {
			await this.resetDesktopTransparency()
		} catch (error) {
			this.logger.error(`Failed to reset desktop transparency`, error)
		}

		// Write the current version to signal what version we've migrated up to.
		// This also serves as a read/write permission check on the first run.
		const previousVersion = await this.titand.store.get('version')
		if (previousVersion && previousVersion !== this.titand.version) {
			await this.titand.store.set('previousVersion', previousVersion)
		} else if (!previousVersion) {
			await this.titand.store.delete('previousVersion')
		}
		await this.titand.store.set('version', this.titand.version)

		// Add notification if version changed
		if (previousVersion && previousVersion !== this.titand.version) {
			await this.titand.notifications.add('titanos-updated').catch(() => {})

			// Include 2.0 prereleases when welcoming users upgrading without any installed apps.
			const upgradingToTitanos2 = semver.major(previousVersion) < 2 && semver.major(this.titand.version) >= 2
			const installedApps = (await this.titand.store.get('apps')) || []
			if (upgradingToTitanos2 && installedApps.length === 0) {
				await this.titand.notifications.add('onboarding-complete').catch(() => {})
			}
		}

		this.logger.log('Migrations complete')
		return {reboot: false}
	}
}

export default Migration
