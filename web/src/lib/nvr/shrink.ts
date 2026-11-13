// What a change of a camera's recording deletes of the recordings that
// exist already (the NAS asks first: nvr_recordings_shrink, docs/NVR.md).
// Frigate applies the new mode and days to what is there about a minute
// after the change: of continuous footage a camera that records on motion
// keeps only the parts with motion, one that records on events only the
// parts with detected objects; fewer days delete what is older. "Off"
// records nothing and keeps everything for its days. Since PLAN 88 the
// days are two: recordings without an event (continuous, motion) and the
// recordings of events; a camera of 0.80 has retainDays for both.
import type { RecordMode } from './types.ts';

/**
 * One thing a change deletes: footage without motion, without detected
 * objects, recordings older than the new record days, events older than
 * the new event days.
 */
export type Shrink = 'motion' | 'events' | 'days' | 'eventDays';

export interface Kept {
	mode: RecordMode;
	retainDays: number;
	recordDays?: number;
	eventDays?: number;
}

const KEEPS: Record<RecordMode, number> = { continuous: 3, off: 3, motion: 2, events: 1 };

/** The record days (continuous and motion) of a camera; 0.80: retainDays. */
export const recordDaysOf = (k: Kept) => k.recordDays || k.retainDays;
/** The event days of a camera; 0.80: retainDays. */
export const eventDaysOf = (k: Kept) => k.eventDays || k.retainDays;
/** Whether the mode keeps recordings without an event (events keeps only events). */
export const usesRecordDays = (mode: RecordMode) => mode !== 'events';

/** What going from `was` to `next` deletes of the recordings that exist; empty if nothing. */
export function shrinks(was: Kept | undefined, next: Kept): Shrink[] {
	if (!was) return [];
	const out: Shrink[] = [];
	if (KEEPS[next.mode] < KEEPS[was.mode]) out.push(next.mode === 'events' ? 'events' : 'motion');
	if (
		usesRecordDays(was.mode) &&
		usesRecordDays(next.mode) &&
		recordDaysOf(next) < recordDaysOf(was)
	)
		out.push('days');
	if (eventDaysOf(next) < eventDaysOf(was)) out.push('eventDays');
	return out;
}
