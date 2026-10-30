/** Unknown totals stay indeterminate. Never fabricate completion percentages. */
export function taskPercent(done: number, total: number): number | undefined {
	if (!Number.isFinite(done) || !Number.isFinite(total) || total <= 0) return undefined;
	return Math.min(100, Math.max(0, (done / total) * 100));
}
