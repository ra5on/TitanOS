// PLAN 88: areas drawn into a camera's picture – zones (named; events and
// messages can be limited to them) and masks (left out of the motion or
// object detection). Points are relative to the picture, 0 to 1, so they
// hold when the camera's resolution changes; the NAS checks them again
// (backend/internal/nvr/areas.go) and writes them into Frigate's config.
import type { AreaPoint, CameraMask, CameraMasks, CameraZone } from './types.ts';

export type AreaKind = 'zone' | 'motion' | 'objects';
export const AREA_KINDS: AreaKind[] = ['zone', 'motion', 'objects'];

/** The NAS's limits (areas.go). */
export const MAX_ZONES = 10;
export const MAX_MASKS = 10;
export const MIN_POINTS = 3;
export const MAX_POINTS = 32;

/** A coordinate as the NAS keeps it: 0 to 1, three places. */
export function coordinate(v: number): number {
	return Math.round(Math.min(1, Math.max(0, v)) * 1000) / 1000;
}

export function point(x: number, y: number): AreaPoint {
	return [coordinate(x), coordinate(y)];
}

/** The polygons of one kind. */
export function areasOf(kind: AreaKind, zones: CameraZone[], masks: CameraMasks) {
	return kind === 'zone' ? zones : masks[kind];
}

/** How many areas of a kind may be drawn still. */
export function roomFor(kind: AreaKind, zones: CameraZone[], masks: CameraMasks): boolean {
	return areasOf(kind, zones, masks).length < (kind === 'zone' ? MAX_ZONES : MAX_MASKS);
}

/** The middle of a polygon (average of its points; for its label). */
export function centre(points: AreaPoint[]): AreaPoint {
	if (points.length === 0) return [0.5, 0.5];
	const x = points.reduce((sum, p) => sum + p[0], 0) / points.length;
	const y = points.reduce((sum, p) => sum + p[1], 0) / points.length;
	return [x, y];
}

/** The area of a polygon as a share of the picture (shoelace). */
export function polygonArea(points: AreaPoint[]): number {
	let sum = 0;
	for (let i = 0; i < points.length; i++) {
		const [x1, y1] = points[i];
		const [x2, y2] = points[(i + 1) % points.length];
		sum += x1 * y2 - x2 * y1;
	}
	return Math.abs(sum) / 2;
}

/** Whether a polygon is one the NAS takes: enough points and an area, not a line. */
export function closable(points: AreaPoint[]): boolean {
	return points.length >= MIN_POINTS && polygonArea(points) >= 0.0001;
}

const UMLAUTS: Record<string, string> = { ä: 'ae', ö: 'oe', ü: 'ue', ß: 'ss' };

/**
 * A zone's ID from its name, like the NAS makes it ("Tor & Weg" →
 * "tor_weg"); a number is added until it is free. The page makes it, so
 * that messages can be limited to a zone before it is saved.
 */
export function zoneId(name: string, taken: string[]): string {
	let base = '';
	for (const ch of name.toLowerCase()) {
		if (/[a-z0-9]/.test(ch)) base += ch;
		else if (UMLAUTS[ch]) base += UMLAUTS[ch];
		else if (base && !base.endsWith('_')) base += '_';
	}
	base = base.replace(/^_+|_+$/g, '');
	if (!/^[a-z]/.test(base)) base = `zone_${base}`.replace(/_+$/, '');
	base = base.slice(0, 24).replace(/_+$/, '');
	let id = base;
	for (let n = 2; taken.includes(id) || /_(sub|twoway)$/.test(id); n++) id = `${base}_${n}`;
	return id;
}

/** Points moved by an arrow key: a step of the picture, kept inside it. */
export function nudge(p: AreaPoint, key: string, step: number): AreaPoint | null {
	const [x, y] = p;
	switch (key) {
		case 'ArrowLeft':
			return point(x - step, y);
		case 'ArrowRight':
			return point(x + step, y);
		case 'ArrowUp':
			return point(x, y - step);
		case 'ArrowDown':
			return point(x, y + step);
	}
	return null;
}

/** A copy of the masks with one kind's list replaced. */
export function withMasks(
	masks: CameraMasks,
	kind: 'motion' | 'objects',
	list: CameraMask[],
): CameraMasks {
	return { ...masks, [kind]: list };
}
