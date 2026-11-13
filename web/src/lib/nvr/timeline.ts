// PLAN 79-7: the arithmetic of the recordings' time line. Recordings are
// short segments with gaps between them; the player plays the segments of a
// window one after the other, so a position in the video is not a clock
// time. These functions translate between the two and prepare the segments
// for drawing. Times are Unix seconds.
import type { Segment } from './types.ts';

export const DAY = 86400;

/** The start of a calendar day ("2026-11-12") in this browser's time zone. */
export function dayStart(day: string): number {
	const [y, m, d] = day.split('-').map(Number);
	return Math.floor(new Date(y, (m || 1) - 1, d || 1).getTime() / 1000);
}

/** The calendar day of a time, as the date field wants it. */
export function dayOf(seconds: number): string {
	const at = new Date(seconds * 1000);
	const two = (n: number) => String(n).padStart(2, '0');
	return `${at.getFullYear()}-${two(at.getMonth() + 1)}-${two(at.getDate())}`;
}

export type Run = { start: number; end: number; kind: 'plain' | 'motion' | 'objects' };

const kindOf = (s: Segment): Run['kind'] =>
	s.objects > 0 ? 'objects' : s.motion > 0 ? 'motion' : 'plain';

/**
 * Neighbouring segments of the same kind as one run (a day has thousands of
 * segments, the line needs a few hundred pieces at most). A pause of up to
 * `join` seconds does not split a run.
 */
export function runs(segments: Segment[], join = 2): Run[] {
	const out: Run[] = [];
	for (const s of [...segments].sort((a, b) => a.start - b.start)) {
		const last = out[out.length - 1];
		const kind = kindOf(s);
		if (last && last.kind === kind && s.start - last.end <= join)
			last.end = Math.max(last.end, s.end);
		else out.push({ start: s.start, end: s.end, kind });
	}
	return out;
}

/** The stretches without a recording between `from` and `to` (longer than `min` seconds). */
export function gaps(segments: Segment[], from: number, to: number, min = 5): Run[] {
	const out: Run[] = [];
	let at = from;
	for (const s of [...segments].sort((a, b) => a.start - b.start)) {
		if (s.start - at > min) out.push({ start: at, end: Math.min(s.start, to), kind: 'plain' });
		at = Math.max(at, s.end);
		if (at >= to) break;
	}
	if (to - at > min) out.push({ start: at, end: to, kind: 'plain' });
	return out;
}

/** The parts of the segments that lie inside a window. */
export function clip(segments: Segment[], from: number, to: number): Segment[] {
	return segments
		.filter((s) => s.end > from && s.start < to)
		.map((s) => ({ ...s, start: Math.max(s.start, from), end: Math.min(s.end, to) }))
		.sort((a, b) => a.start - b.start);
}

/** The clock time of a position in the video of a window. */
export function mediaToWall(
	segments: Segment[],
	from: number,
	to: number,
	position: number,
): number {
	let left = position;
	const parts = clip(segments, from, to);
	for (const s of parts) {
		const length = s.end - s.start;
		if (left <= length) return s.start + left;
		left -= length;
	}
	return parts.length ? parts[parts.length - 1].end : from;
}

/**
 * The position in the video of a window for a clock time; a time inside a
 * gap gives the start of the next recording.
 */
export function wallToMedia(segments: Segment[], from: number, to: number, wall: number): number {
	let position = 0;
	for (const s of clip(segments, from, to)) {
		if (wall < s.start) return position;
		if (wall <= s.end) return position + (wall - s.start);
		position += s.end - s.start;
	}
	return position;
}

/** Whether a recording covers this time. */
export function recordedAt(segments: Segment[], wall: number): boolean {
	return segments.some((s) => wall >= s.start && wall < s.end);
}

/** The start of the first recording at or after a time; null when none follows. */
export function nextRecorded(segments: Segment[], wall: number): number | null {
	let best: number | null = null;
	for (const s of segments) {
		if (s.end <= wall) continue;
		const at = Math.max(s.start, wall);
		if (best === null || at < best) best = at;
	}
	return best;
}

/** The window the player loads around a time: the clock hour it lies in. */
export function playWindow(
	wall: number,
	dayFrom: number,
	limit: number,
): { from: number; to: number } {
	const from = dayFrom + Math.floor((wall - dayFrom) / 3600) * 3600;
	return { from, to: Math.min(from + 3600, limit) };
}
