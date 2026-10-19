/** Percentage of the available console column, independent of monitor size. */
export function vncWidth(value: unknown): number {
 const width = value && typeof value === 'object' ? (value as Record<string, unknown>).width : undefined;
 return typeof width === 'number' && Number.isFinite(width) ? Math.round(Math.max(40, Math.min(100, width))) : 75;
}
