// What a change of a camera's recording deletes of the recordings that
// exist already (the NAS asks first: nvr_recordings_shrink, docs/NVR.md).
// Frigate applies the new mode and days to what is there about a minute
// after the change: of continuous footage a camera that records on motion
// keeps only the parts with motion, one that records on events only the
// parts with detected objects; fewer days delete what is older. "Off"
// records nothing and keeps everything for its days.
import type { RecordMode } from './types.ts';

/** One thing a change deletes: footage without motion, without detected objects, or older than the new days. */
export type Shrink = 'motion' | 'events' | 'days';

interface Kept {
	mode: RecordMode;
	retainDays: number;
}

const KEEPS: Record<RecordMode, number> = { continuous: 3, off: 3, motion: 2, events: 1 };

/** What going from `was` to `next` deletes of the recordings that exist; empty if nothing. */
export function shrinks(was: Kept | undefined, next: Kept): Shrink[] {
	if (!was) return [];
	const out: Shrink[] = [];
	if (KEEPS[next.mode] < KEEPS[was.mode]) out.push(next.mode === 'events' ? 'events' : 'motion');
	if (next.retainDays < was.retainDays) out.push('days');
	return out;
}
