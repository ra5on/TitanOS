import type Titand from '../../index.js'

import {OWNER_USER_ID} from '../user/constants.js'

const NON_FILE_TYPES = new Set(['directory', 'symbolic-link', 'socket', 'block-device', 'character-device', 'fifo'])
const MAX_RECENTS = 50

export default class Recents {
	#titand: Titand

	constructor(titand: Titand) {
		this.#titand = titand
	}

	// Get recents
	async get(userId: string = OWNER_USER_ID) {
		if (!this.#titand.files.fileIndex.available) throw new Error('File index is unavailable')
		const homeRoot = userId === OWNER_USER_ID ? '/Home' : `/Users/${userId}`
		const candidates = await this.#titand.files.fileIndex.recentCandidates(homeRoot, MAX_RECENTS, [
			this.#titand.backups.backupDirectoryName,
		])
		const recents = await Promise.allSettled(
			candidates.map(async ({virtualPath}) => {
				const systemPath = await this.#titand.files.virtualToSystemPath(virtualPath, userId)
				return this.#titand.files.status(systemPath, userId)
			}),
		)

		return recents
			.filter(
				(result): result is PromiseFulfilledResult<Awaited<ReturnType<Titand['files']['status']>>> =>
					result.status === 'fulfilled' && !NON_FILE_TYPES.has(result.value.type),
			)
			.map(({value}) => value)
	}
}
