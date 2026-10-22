/** Height of the VM console in CSS pixels, as dragged at its corner. */
export const VNC_HEIGHT = { min: 240, max: 2000, default: 620 };
/** Width of the VM console; without one it is as wide as its column. */
export const VNC_WIDTH = { min: 320, max: 4000 };

export function vncHeight(value: unknown): number {
	const height = value && typeof value === 'object' ? (value as Record<string, unknown>).height : undefined;
	return typeof height === 'number' && Number.isFinite(height) ? Math.round(Math.max(VNC_HEIGHT.min, Math.min(VNC_HEIGHT.max, height))) : VNC_HEIGHT.default;
}

/** The stored width, or null for the full width of the column. */
export function vncWidth(value: unknown): number | null {
	const width = value && typeof value === 'object' ? (value as Record<string, unknown>).width : undefined;
	return typeof width === 'number' && Number.isFinite(width) ? Math.round(Math.max(VNC_WIDTH.min, Math.min(VNC_WIDTH.max, width))) : null;
}

/**
 * The height the console gets on the page: what was dragged, but never more
 * than fits between its top and the bottom of the window, so that the whole
 * picture is in view without scrolling the page.
 */
export function fitHeight(stored: number, room: number): number {
	if (!Number.isFinite(room) || room <= 0) return stored;
	return Math.max(VNC_HEIGHT.min, Math.min(stored, Math.floor(room)));
}
