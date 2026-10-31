// SMART history of a disk (backend/internal/smarthistory): one sample per
// day on a data volume. Here only what the page shows from it.

export type SmartSample = {
	day: string;
	readAt: string;
	temperatureC?: number;
	powerOnHours?: number;
	reallocatedSectors?: number;
	pendingSectors?: number;
	uncorrectableSectors?: number;
	crcErrors?: number;
	mediaErrors?: number;
	percentageUsed?: number;
	availableSpare?: number;
};

export type SmartHistory = { disk: string; volume: string; samples: SmartSample[] };

/** The counters and levels the history shows, in this order. */
export const HISTORY_FIELDS = [
	'reallocatedSectors',
	'pendingSectors',
	'uncorrectableSectors',
	'crcErrors',
	'mediaErrors',
	'percentageUsed',
	'temperatureC',
] as const;
export type HistoryField = (typeof HISTORY_FIELDS)[number];

/** Rising is bad for all but the temperature, which goes up and down. */
const COUNTERS = new Set<HistoryField>([
	'reallocatedSectors',
	'pendingSectors',
	'uncorrectableSectors',
	'crcErrors',
	'mediaErrors',
	'percentageUsed',
]);

export type HistoryRow = {
	field: HistoryField;
	first: number;
	firstDay: string;
	last: number;
	lastDay: string;
	/** For counters: the first day the value was higher than before. */
	roseOn?: string;
	min: number;
	max: number;
};

export function isSmartHistory(value: unknown): value is SmartHistory {
	const h = value as SmartHistory;
	return (
		!!h &&
		typeof h === 'object' &&
		typeof h.disk === 'string' &&
		typeof h.volume === 'string' &&
		Array.isArray(h.samples) &&
		h.samples.every((s) => !!s && typeof s.day === 'string')
	);
}

/** One row per value the disk reports, oldest to newest sample. */
export function historyRows(samples: SmartSample[]): HistoryRow[] {
	const rows: HistoryRow[] = [];
	for (const field of HISTORY_FIELDS) {
		let row: HistoryRow | undefined;
		for (const s of samples) {
			const value = s[field];
			if (typeof value !== 'number') continue;
			if (!row) {
				row = {
					field,
					first: value,
					firstDay: s.day,
					last: value,
					lastDay: s.day,
					min: value,
					max: value,
				};
				continue;
			}
			if (COUNTERS.has(field) && value > row.last && !row.roseOn) row.roseOn = s.day;
			row.last = value;
			row.lastDay = s.day;
			row.min = Math.min(row.min, value);
			row.max = Math.max(row.max, value);
		}
		if (row) rows.push(row);
	}
	return rows;
}

/** Whether a counter went up within the history. */
export function rose(row: HistoryRow): boolean {
	return COUNTERS.has(row.field) && row.last > row.first;
}

/** Points of the temperature line in a box of width × height. */
export function temperaturePoints(
	samples: SmartSample[],
	width: number,
	height: number,
): { points: { x: number; y: number; day: string; value: number }[]; min: number; max: number } {
	const known = samples.filter((s) => typeof s.temperatureC === 'number');
	if (known.length === 0) return { points: [], min: 0, max: 0 };
	const values = known.map((s) => s.temperatureC as number);
	let min = Math.min(...values),
		max = Math.max(...values);
	if (max - min < 10) {
		const middle = (max + min) / 2;
		min = Math.floor(middle - 5);
		max = Math.ceil(middle + 5);
	}
	const step = known.length > 1 ? width / (known.length - 1) : 0;
	return {
		points: known.map((s, i) => ({
			x: known.length > 1 ? i * step : width / 2,
			y: height - ((s.temperatureC as number) - min) * (height / (max - min)),
			day: s.day,
			value: s.temperatureC as number,
		})),
		min,
		max,
	};
}
