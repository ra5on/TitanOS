import {expect, it} from 'vitest'

import {
	defaultDockConfig,
	DOCK_ICON_SIZE_MOBILE_MAX,
	DOCK_MAX_ITEMS,
	dockItemKey,
	fitDockIconSize,
	isRemovableDockItem,
} from './dock-dimensions'

it('fits all six default dock actions at common narrow mobile widths and caps growth on larger screens', () => {
	for (const width of [280, 320, 360, 390, 430]) {
		const size = fitDockIconSize(DOCK_ICON_SIZE_MOBILE_MAX, 6, width, 8)
		expect(size * 6 + 5 * 8 + 24).toBeLessThanOrEqual(width - 16)
		expect(size).toBeGreaterThanOrEqual(32)
		expect(size).toBeLessThanOrEqual(48)
	}
	expect(fitDockIconSize(DOCK_ICON_SIZE_MOBILE_MAX, 6, 800, 8)).toBe(48)
})

it('shrinks a full dock to the viewport instead of overflowing, down to a usable floor', () => {
	// A large chosen size is kept while it fits
	expect(fitDockIconSize(72, 6, 1920, 10)).toBe(72)
	const size = fitDockIconSize(72, DOCK_MAX_ITEMS, 1024, 10)
	expect(size).toBeLessThan(72)
	expect(size * DOCK_MAX_ITEMS + (DOCK_MAX_ITEMS - 1) * 10 + 24).toBeLessThanOrEqual(1024 - 16)
	expect(fitDockIconSize(72, DOCK_MAX_ITEMS, 320, 8)).toBe(24)
})

it('starts every account with Settings, and members without Machines', () => {
	const owner = defaultDockConfig(true).items.map(dockItemKey)
	const member = defaultDockConfig(false).items.map(dockItemKey)
	expect(owner).toEqual([
		'system:files',
		'system:photos',
		'system:app-store',
		'system:machines',
		'system:settings',
		'system:live-usage',
	])
	expect(member).not.toContain('system:machines')
	expect(member).toContain('system:settings')
	expect(isRemovableDockItem({type: 'system', id: 'settings'})).toBe(false)
	expect(isRemovableDockItem({type: 'system', id: 'files'})).toBe(true)
	expect(isRemovableDockItem({type: 'app', id: 'settings'})).toBe(true)
})
