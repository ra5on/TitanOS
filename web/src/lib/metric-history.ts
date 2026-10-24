/** Stable time slots keep new samples from stretching the entire history. */
export function historySlots(
	values: readonly (number | null)[],
	capacity: number,
): (number | null)[] {
	const count = Math.max(2, Math.min(180, Math.round(capacity)));
	const tail = values
		.slice(-count)
		.map((v) => (v !== null && Number.isFinite(v) ? Math.max(0, v) : null));
	return [...Array<null>(count - tail.length).fill(null), ...tail];
}
export function historyPath(values: readonly (number | null)[], peak = 100, height = 64): string {
	let connected = false;
	const bottom = height - 3,
		range = height - 7;
	return values
		.map((v, i) => {
			if (v === null || !Number.isFinite(v)) {
				connected = false;
				return '';
			}
			const command = connected ? 'L' : 'M';
			connected = true;
			return `${command}${((i * 240) / Math.max(1, values.length - 1)).toFixed(2)},${(bottom - Math.max(0, Math.min(1, v / Math.max(1, peak))) * range).toFixed(2)}`;
		})
		.join(' ');
}
/** An unavailable endpoint stays a gap throughout the visual transition. */
export function historyFrame(
	from: readonly (number | null)[],
	to: readonly (number | null)[],
	progress: number,
): (number | null)[] {
	return to.map((v, i) =>
		v === null
			? null
			: from[i] === null || from[i] === undefined
				? v
				: from[i]! + (v - from[i]!) * progress,
	);
}
