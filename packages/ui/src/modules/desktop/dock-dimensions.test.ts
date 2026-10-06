import {expect, it} from 'vitest'

import {mobileDockIconSize} from './dock-dimensions'

it('fits all six dock actions at common narrow mobile widths and caps growth on larger screens', () => {
	for (const width of [280, 320, 360, 390, 430]) {
		const size = mobileDockIconSize(width)
		expect(size * 6 + 5 * 8 + 24).toBeLessThanOrEqual(width - 16)
		expect(size).toBeGreaterThanOrEqual(32)
		expect(size).toBeLessThanOrEqual(48)
	}
	expect(mobileDockIconSize(800)).toBe(48)
})
