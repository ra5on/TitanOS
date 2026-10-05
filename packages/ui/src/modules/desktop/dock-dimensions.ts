/** Keep all six owner actions inside narrow mobile viewports. */
export function mobileDockIconSize(viewportWidth: number, maximum = 48) {
	// Five 8px gaps, 24px horizontal padding, and a 16px edge margin.
	return Math.min(maximum, Math.max(32, (viewportWidth - 80) / 6))
}
