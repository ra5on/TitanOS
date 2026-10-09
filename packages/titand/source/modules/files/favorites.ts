import type Titand from '../../index.js'

import type {FileChangeEvent} from './watcher.js'
import {OWNER_USER_ID} from '../user/constants.js'
import {HOME_FOLDERS} from './home-folders.js'
import AsyncBurstCache from '../utilities/async-burst-cache.js'
import AppDirectoryMonitor from './app-directory-monitor.js'

const WATCHER_SNAPSHOT_TTL_MS = 1000

export default class Favorites {
	#titand: Titand
	logger: Titand['logger']
	#removeFileChangeListener?: () => void
	#appDirectories: AppDirectoryMonitor
	#watcherFavorites: AsyncBurstCache<Awaited<ReturnType<Titand['user']['getAllAccountFavorites']>>>

	constructor(titand: Titand) {
		this.#titand = titand
		this.#watcherFavorites = new AsyncBurstCache(
			() => this.#titand.user.getAllAccountFavorites(),
			WATCHER_SNAPSHOT_TTL_MS,
		)
		const {name} = this.constructor
		this.logger = titand.logger.createChildLogger(`files:${name.toLocaleLowerCase()}`)
		this.#appDirectories = new AppDirectoryMonitor({
			listPaths: async () => (await titand.user.getAllAccountFavorites()).flatMap(({favorites}) => favorites ?? []),
			systemPath: (path) => titand.files.virtualToSystemPathUnsafe(path),
			onDelete: (path) => this.removeWithin(path),
			logger: this.logger,
		})
	}

	// Add listener
	async start() {
		this.logger.log('Starting favorites')

		// Attach listener
		this.#removeFileChangeListener = this.#titand.eventBus.on(
			'files:watcher:change',
			this.#handleFileChange.bind(this),
		)
		await this.#appDirectories.start()
	}

	// Get favorites
	async #get(userId: string) {
		const favorites = await this.#titand.user.getAccountFavorites(userId)
		return this.#normalizeFavorites(favorites ?? this.#defaultFavorites(userId))
	}

	// Remove favorites on deletion
	// TODO: It would be nice if we could handle updating favorites when the favorited directory is
	// moved/renamed. It's not trivial because this can happen via something external like an app or SMB
	// and there's no way to tell the difference between a move/rename and a deletion/recreation.
	async #handleFileChange(event: FileChangeEvent) {
		if (event.type !== 'delete') return
		await this.removeWithin(this.#titand.files.systemToVirtualPath(event.path))
	}

	async removeWithin(virtualDeletedPath: string) {
		const accounts = await this.#watcherFavorites.get()
		for (const {userId, favorites: storedFavorites} of accounts) {
			const favorites = this.#normalizeFavorites(storedFavorites ?? this.#defaultFavorites(userId))
			const deletedFavorites = favorites.filter(
				(favorite) => favorite === virtualDeletedPath || favorite.startsWith(`${virtualDeletedPath}/`),
			)
			for (const favorite of deletedFavorites) {
				await this.removeFavorite(favorite, userId).catch((error) =>
					this.logger.error(`Failed to remove deleted favorite ${favorite} for ${userId}`, error),
				)
			}
		}
	}

	// List favorited directories
	async listFavorites(userId: string = OWNER_USER_ID) {
		// Get favorites from the store
		const favorites = await this.#get(userId)

		// Strip out any favorites that aren't existing directories (or no longer
		// resolve, e.g. the directory was replaced with an escaping symlink)
		const mappedFavorites = await Promise.all(
			favorites.map(async (favorite) => {
				const systemPath = await this.#titand.files.virtualToSystemPath(favorite, userId).catch(() => undefined)
				if (!systemPath) return undefined
				const file = await this.#titand.files.status(systemPath).catch(() => undefined)
				if (file?.type !== 'directory') return undefined
				return favorite
			}),
		)
		const filteredFavorites = mappedFavorites.filter((favorite) => favorite !== undefined)

		return filteredFavorites
	}

	// Save a favorite directory
	async addFavorite(virtualPath: string, userId: string = OWNER_USER_ID) {
		virtualPath = this.#titand.files.normalizeVirtualPath(virtualPath)

		// Authorize before inspecting path capabilities so an account cannot use
		// the error shape to probe inaccessible directories.
		await this.#titand.files.virtualToSystemPath(virtualPath, userId)

		// Check operation is allowed
		const allowedOperations = await this.#titand.files.getAllowedOperations(virtualPath, userId)
		if (!allowedOperations.includes('favorite')) throw new Error('[operation-not-allowed]')

		// Save entry in the store
		await this.#titand.user.updateAccountFavorites(userId, (stored) => {
			const favorites = this.#normalizeFavorites(stored ?? this.#defaultFavorites(userId))
			if (favorites.includes(virtualPath)) return undefined
			return [...favorites, virtualPath]
		})
		this.#watcherFavorites.clear()
		this.#appDirectories.forget(virtualPath)
		await this.#appDirectories.refresh()

		return true
	}

	// Remove a favorite directory
	async removeFavorite(virtualPath: string, userId: string = OWNER_USER_ID) {
		virtualPath = this.#titand.files.normalizeVirtualPath(virtualPath)
		let deleted = false
		await this.#titand.user.updateAccountFavorites(userId, (stored) => {
			const favorites = this.#normalizeFavorites(stored ?? this.#defaultFavorites(userId))
			const newFavorites = favorites.filter((favorite) => favorite !== virtualPath)
			deleted = newFavorites.length < favorites.length
			return deleted ? newFavorites : undefined
		})
		this.#watcherFavorites.clear()
		return deleted
	}

	#defaultFavorites(userId: string) {
		const home = userId === OWNER_USER_ID ? '/Home' : `/Users/${userId}`
		return HOME_FOLDERS.map((folder) => `${home}/${folder}`)
	}

	#normalizeFavorites(favorites: string[]) {
		return [...new Set(favorites.map((favorite) => this.#titand.files.normalizeVirtualPath(favorite)))]
	}

	// Remove listener
	async stop() {
		this.logger.log('Stopping favorites')
		this.#removeFileChangeListener?.()
		await this.#appDirectories.stop()
	}
}
