import type Titand from '../../index.js'
import {OWNER_USER_ID} from '../user/constants.js'

export default class Search {
	#titand: Titand

	constructor(titand: Titand) {
		this.#titand = titand
	}

	// No background tasks
	async start() {}
	async stop() {}

	async search(query: string, maxResults = 250, userId: string = OWNER_USER_ID) {
		const homeRoot = userId === OWNER_USER_ID ? '/Home' : `/Users/${userId}`
		if (!this.#titand.files.fileIndex.available) throw new Error('File index is unavailable')
		return this.#searchIndex(query, maxResults, homeRoot, userId)
	}

	async #searchIndex(query: string, maxResults: number, homeRoot: string, userId: string) {
		const results = await this.#titand.files.fileIndex.searchCandidates(homeRoot, query, maxResults)
		// TODO: Broad searches are currently slow because every result is revalidated
		// with filesystem path and status lookups. In a follow-up, make the index
		// guarantee that its results are safe to return without these per-result reads.
		const fileReads = await Promise.allSettled(
			results.map(async ({virtualPath}) => {
				// The database is derived state, never an authorization oracle. Resolve
				// every result through the live Files boundary before reading status.
				const systemPath = await this.#titand.files.virtualToSystemPath(virtualPath, userId)
				return this.#titand.files.status(systemPath, userId)
			}),
		)
		return fileReads.filter((result) => result.status === 'fulfilled').map((result) => result.value)
	}
}
