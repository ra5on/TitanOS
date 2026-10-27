// Bandwidth limit of a backup job, as titand stores it: MiB/s, 0 = none.
// `limit` holds all the time or outside the window; `windowLimit` from
// `windowFrom` to `windowTo` (may pass midnight).
export type Bandwidth = {
	limit: number;
	windowFrom?: string;
	windowTo?: string;
	windowLimit?: number;
};

// The form keeps what was typed; numbers are read when saving.
export type BandwidthForm = {
	on: boolean;
	limit: number | string;
	window: boolean;
	windowFrom: string;
	windowTo: string;
	windowLimit: number | string;
};

export const maxBandwidth = 10000;

export function bandwidthForm(b?: Bandwidth | null): BandwidthForm {
	return {
		on: !!b,
		limit: b?.limit ?? 10,
		window: !!b?.windowFrom,
		windowFrom: b?.windowFrom || '22:00',
		windowTo: b?.windowTo || '06:00',
		windowLimit: b?.windowLimit ?? 0,
	};
}

function mib(value: number | string): number {
	const n = Math.round(Number(value));
	return Number.isFinite(n) && n > 0 ? Math.min(n, maxBandwidth) : 0;
}

// bandwidthPayload returns what goes to the API; undefined is no limit
// (the field is left out, as for jobs that never had one).
export function bandwidthPayload(form: BandwidthForm): Bandwidth | undefined {
	if (!form.on) return undefined;
	const b: Bandwidth = { limit: mib(form.limit) };
	if (form.window && form.windowFrom && form.windowTo) {
		b.windowFrom = form.windowFrom;
		b.windowTo = form.windowTo;
		b.windowLimit = mib(form.windowLimit);
	}
	return b.limit > 0 || (b.windowLimit ?? 0) > 0 ? b : undefined;
}

// bandwidthParts describes a limit for the job list: the limit outside the
// window and, if there is one, the window with its limit. 0 is "none".
export function bandwidthParts(
	b?: Bandwidth | null,
): { limit: number; window?: { from: string; to: string; limit: number } } | null {
	if (!b || (b.limit <= 0 && (b.windowLimit ?? 0) <= 0)) return null;
	return b.windowFrom && b.windowTo
		? { limit: b.limit, window: { from: b.windowFrom, to: b.windowTo, limit: b.windowLimit ?? 0 } }
		: { limit: b.limit };
}
