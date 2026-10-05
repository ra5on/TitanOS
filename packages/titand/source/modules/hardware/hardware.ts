import type Titand from '../../index.js'

import InternalStorage from './internal-storage.js'
import Raid from './raid.js'
import TitanPro from './titan-pro.js'
import Thunderbolt from './thunderbolt.js'

export default class Hardware {
	#titand: Titand
	logger: Titand['logger']
	internalStorage: InternalStorage
	raid: Raid
	titanPro: TitanPro
	thunderbolt: Thunderbolt

	constructor(titand: Titand) {
		this.#titand = titand
		const {name} = this.constructor
		this.logger = titand.logger.createChildLogger(name.toLowerCase())

		this.internalStorage = new InternalStorage(titand)
		this.raid = new Raid(titand)
		this.titanPro = new TitanPro(titand)
		this.thunderbolt = new Thunderbolt(titand)
	}

	async start() {
		this.logger.log('Starting hardware')

		// Start submodules
		await Promise.all([
			this.internalStorage.start().catch((error) => this.logger.error('Failed to start internal storage', error)),
			this.raid.start().catch((error) => this.logger.error('Failed to start RAID', error)),
			this.titanPro.start().catch((error) => this.logger.error('Failed to start Titan Pro', error)),
			this.thunderbolt.start().catch((error) => this.logger.error('Failed to start Thunderbolt monitor', error)),
		])
	}

	async stop() {
		this.logger.log('Stopping hardware')

		// Stop submodules
		await Promise.all([
			this.internalStorage.stop().catch((error) => this.logger.error('Failed to stop internal storage', error)),
			this.raid.stop().catch((error) => this.logger.error('Failed to stop RAID', error)),
			this.titanPro.stop().catch((error) => this.logger.error('Failed to stop Titan Pro', error)),
			this.thunderbolt.stop().catch((error) => this.logger.error('Failed to stop Thunderbolt monitor', error)),
		])
	}
}
