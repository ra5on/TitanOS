/** Height of the VM console in CSS pixels, as dragged at its corner. */
export const VNC_HEIGHT = { min: 240, max: 2000, default: 620 };

export function vncHeight(value: unknown): number {
	const height = value && typeof value === 'object' ? (value as Record<string, unknown>).height : undefined;
	return typeof height === 'number' && Number.isFinite(height) ? Math.round(Math.max(VNC_HEIGHT.min, Math.min(VNC_HEIGHT.max, height))) : VNC_HEIGHT.default;
}
