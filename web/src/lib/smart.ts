// SMART overview: the shapes of /api/v1/storage/smart and the rules the page
// shows them by. The backend judges the state (thresholds in docs/SMART.md);
// nothing here invents values or re-judges them.

export type SmartState = 'ok' | 'warning' | 'failed' | 'unavailable' | 'unknown';
export type SmartWarning =
	| 'smartFailed'
	| 'attributeNow'
	| 'attributePast'
	| 'reallocated'
	| 'pending'
	| 'uncorrectable'
	| 'crc'
	| 'temperature'
	| 'nvmeCritical'
	| 'wear'
	| 'mediaErrors'
	| 'spare';

export type SmartAttribute = {
	id: number;
	name: string;
	value: number;
	worst: number;
	threshold: number;
	raw: string;
	flags?: string;
	prefailure: boolean;
	whenFailed?: string;
	warn?: boolean;
};

export type SmartValues = {
	protocol: string;
	modelFamily?: string;
	firmware?: string;
	passed: boolean;
	temperatureC: number | null;
	powerOnHours: number | null;
	powerCycles: number | null;
	rotationRate: number;
	reallocatedSectors: number | null;
	pendingSectors: number | null;
	uncorrectableSectors: number | null;
	crcErrors: number | null;
	nvme?: {
		criticalWarning: number;
		percentageUsed: number;
		availableSpare: number;
		availableSpareThreshold: number;
		mediaErrors: number;
		errorLogEntries: number;
		unsafeShutdowns: number;
		dataUnitsRead: number;
		dataUnitsWritten: number;
	};
	attributes?: SmartAttribute[];
};

export type SmartReport = {
	disk: string;
	path: string;
	model: string;
	serial: string;
	kind: 'hdd' | 'ssd' | 'nvme' | 'usb';
	role: string;
	volume?: string;
	state: SmartState;
	warnings: SmartWarning[];
	sleeping: boolean;
	checkedAt?: string;
	readAt?: string;
	values: SmartValues | null;
};

/** The colour of a state badge; status is never shown by colour alone. */
export function smartTone(state: SmartState): 'ok' | 'warn' | 'error' | '' {
	switch (state) {
		case 'ok':
			return 'ok';
		case 'warning':
			return 'warn';
		case 'failed':
			return 'error';
	}
	return '';
}

const rank: Record<SmartState, number> = {
	failed: 0,
	warning: 1,
	ok: 2,
	unknown: 3,
	unavailable: 4,
};

/** Problems first, then by device name in natural order (sda, sdb, sdaa …). */
export function sortReports(reports: SmartReport[]): SmartReport[] {
	return reports.toSorted(
		(a, b) =>
			(rank[a.state] ?? 9) - (rank[b.state] ?? 9) ||
			a.disk.localeCompare(b.disk, 'en', { numeric: true }),
	);
}

/** Counts per badge for the summary line; a sleeping disk counts as sleeping too. */
export function smartCounts(reports: SmartReport[]) {
	const counts = { ok: 0, warning: 0, failed: 0, sleeping: 0, other: 0 };
	for (const report of reports) {
		if (report.sleeping) counts.sleeping++;
		if (report.state === 'ok' || report.state === 'warning' || report.state === 'failed')
			counts[report.state]++;
		else counts.other++;
	}
	return counts;
}

/** Damaged-sector counters added up; null when the disk reports none of them. */
export function sectorProblems(values: SmartValues | null): number | null {
	if (!values) return null;
	const known = [
		values.reallocatedSectors,
		values.pendingSectors,
		values.uncorrectableSectors,
	].filter((value): value is number => typeof value === 'number');
	return known.length ? known.reduce((sum, value) => sum + value, 0) : null;
}

/** Whether an answer of the list route has the expected shape. */
export function isSmartList(value: unknown): value is { disks: SmartReport[] } {
	if (!value || typeof value !== 'object') return false;
	const disks = (value as { disks?: unknown }).disks;
	return (
		Array.isArray(disks) &&
		disks.every(
			(d) =>
				!!d &&
				typeof d === 'object' &&
				typeof (d as SmartReport).disk === 'string' &&
				typeof (d as SmartReport).state === 'string' &&
				Array.isArray((d as SmartReport).warnings),
		)
	);
}

/** A disk name as the API accepts it in a path (sda, nvme0n1, …). */
export function validDiskName(name: string): boolean {
	return /^[A-Za-z0-9][A-Za-z0-9_-]{0,31}$/.test(name);
}
