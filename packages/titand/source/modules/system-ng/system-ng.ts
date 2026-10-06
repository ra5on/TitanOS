import type Titand from '../../index.js'

import Device from './device.js'
import Advertisement from './advertisement.js'

// Replacement for the legacy system module. Will be renamed to "system" once the old module is fully migrated.
export default class SystemNg {
	#titand: Titand
	logger: Titand['logger']
	device: Device
	advertisement: Advertisement

	constructor(titand: Titand) {
		this.#titand = titand
		const {name} = this.constructor
		this.logger = titand.logger.createChildLogger(name.toLowerCase())

		this.device = new Device(titand)
		this.advertisement = new Advertisement(titand)
	}

	async start() {
		this.logger.log('Starting system-ng')

		// Start submodules
		await this.device.start().catch((error) => this.logger.error('Failed to start device', error))
		await this.advertisement.start().catch((error) => this.logger.error('Failed to start advertisement', error))
	}

	async stop() {
		this.logger.log('Stopping system-ng')

		// Stop submodules
		await this.advertisement.stop().catch((error) => this.logger.error('Failed to stop advertisement', error))
		await this.device.stop().catch((error) => this.logger.error('Failed to stop device', error))
	}
}
