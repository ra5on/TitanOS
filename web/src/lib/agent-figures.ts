// The working figure of the KI-Agent: three flat dots that move in space.
// Since 1.01 one motion for every kind of work; since 1.05 that motion is a
// choreography of several three-dimensional figures that flow into each
// other on one running clock (owner 17 Nov 2026: "Die Punkte drehen sich
// einmal im Kreis, laufen zusammen. Ist doch sehr langweilig … mehr
// visuellen, dreidimensionalen Gedanken … das muss mehr sein").
//
// A figure gives, for a time, the places of the three dots in units of half
// the figure's width (-1 … 1, the middle is 0) and a depth (-1 back … 1
// front). The component (BusyFigure.svelte) draws that; this file is plain
// arithmetic.
//
// Rules that stay (owner 16 Nov 2026): all three dots always have the same
// size and one colour – depth shows only in the motion and in what covers
// what, never in size or brightness; the figure stays inside its box; a new
// word or kind of work never starts it anew; "Animationen: Aus" and less
// motion show one still picture.
import type { BusyKind } from './agent.ts';

export type Spot = {
	x: number;
	y: number;
	/** -1 behind … 1 in front: decides only what covers what. */
	z: number;
};
type Vec = [number, number, number];
/** A phase: the three dots in space at time t (seconds). */
type Phase = (t: number) => Vec[];

/** Diameter of every dot as a share of the figure's width; in half widths
 *  a dot's radius is SPHERE. */
export const SPHERE = 0.24;
/** The farthest a dot's middle goes from the middle of the figure, so the
 *  whole dot stays inside the box. */
export const REACH = 1 - SPHERE;

const TAU = Math.PI * 2;
const THIRD = TAU / 3;

/** Turns a point about the x axis (tipping towards the viewer). */
function rotX([x, y, z]: Vec, a: number): Vec {
	const c = Math.cos(a);
	const s = Math.sin(a);
	return [x, y * c - z * s, y * s + z * c];
}
/** Turns a point about the y axis (a turn seen from above). */
function rotY([x, y, z]: Vec, a: number): Vec {
	const c = Math.cos(a);
	const s = Math.sin(a);
	return [x * c + z * s, y, -x * s + z * c];
}
/** Turns a point in the plane of the screen. */
function rotZ([x, y, z]: Vec, a: number): Vec {
	const c = Math.cos(a);
	const s = Math.sin(a);
	return [x * c - y * s, x * s + y * c, z];
}
const each = (f: (n: number) => Vec): Vec[] => [f(0), f(1), f(2)];

/** A ring that tips back and forth and turns: the dots run round it. */
const ring: Phase = (t) =>
	each((n) => {
		const a = t * 1.9 + n * THIRD;
		const p: Vec = [Math.cos(a) * 0.62, 0, Math.sin(a) * 0.62];
		return rotZ(rotX(p, 1.05 + Math.sin(t * 0.5) * 0.3), t * 0.4);
	});

/** A figure of eight lying in space (Gerono's lemniscate), the dots a
 *  third of the way apart on it; the whole eight sways about the vertical. */
const eight: Phase = (t) =>
	each((n) => {
		const u = t * 1.7 + n * THIRD;
		const p: Vec = [Math.sin(u) * 0.66, Math.sin(2 * u) * 0.3, Math.cos(u) * 0.34];
		return rotX(rotY(p, Math.sin(t * 0.45) * 0.7), 0.3 + Math.sin(t * 0.33) * 0.15);
	});

/** A helix: the dots climb a spiral round the vertical, one above the
 *  other, and the whole spiral rises and sinks. */
const helix: Phase = (t) =>
	each((n) => {
		const a = t * 2.2 + n * 2.1;
		const p: Vec = [
			Math.cos(a) * 0.55,
			(n - 1) * 0.36 + Math.sin(t * 0.8) * 0.14,
			Math.sin(a) * 0.55,
		];
		return rotX(p, 0.2);
	});

/** An equilateral triangle that tumbles: it flips over and over about
 *  its middle line, sways about the vertical and turns in the plane of the
 *  screen. Flipping about the line under its tip keeps the three dots
 *  apart on the screen even when the triangle stands edge-on. */
const tumble: Phase = (t) =>
	each((n) => {
		const a = n * THIRD + Math.PI / 2;
		const p: Vec = [Math.cos(a) * 0.6, Math.sin(a) * 0.6, 0];
		return rotZ(rotY(rotX(p, t * 1.6), Math.sin(t * 0.7) * 0.25), t * 0.5);
	});

/** Together and apart: the dots run in until they touch (three stay three)
 *  and fly out again, turning, and a little in depth. */
const gather: Phase = (t) =>
	each((n) => {
		// 0 together … 1 apart; slow at both ends.
		const k = Math.pow(0.5 - 0.5 * Math.cos(t * 1.5), 1.5);
		const r = (2 * SPHERE) / Math.sqrt(3) + 0.42 * k;
		const a = t * 1.4 + n * THIRD;
		const p: Vec = [Math.cos(a) * r, Math.sin(a) * r, 0];
		return rotY(rotX(p, Math.sin(t * 0.7) * 0.6), Math.cos(t * 0.6) * 0.5);
	});

/** A little planetary system: one dot in the middle, one on a close orbit
 *  seen almost from the side (it passes in front of and behind the middle
 *  one), one on a wide orbit seen more from above, the other way round.
 *  On the screen the wide orbit lies around the close one, so the outer
 *  dot never covers another. */
const planets: Phase = (t) => {
	const spin = t * 0.25;
	const sun: Vec = [Math.sin(t * 0.7) * 0.06, Math.cos(t * 0.9) * 0.05, 0];
	const orbit = (r: number, a: number, tilt: number): Vec =>
		rotZ(rotX([Math.cos(a) * r, 0, Math.sin(a) * r], tilt), spin);
	const inner = orbit(0.45, t * 2.6, 0.75);
	const outer = orbit(0.66, -t * 1.5 + 2, 1.15);
	return [
		[sun[0] + inner[0], sun[1] + inner[1], inner[2]],
		sun,
		[sun[0] + outer[0], sun[1] + outer[1], outer[2]],
	];
};

/** The phases of the choreography; ring first, so the still picture (time
 *  0) is three dots well apart. */
export const PHASES: readonly Phase[] = [ring, eight, helix, tumble, gather, planets];

/** Length of a block of the choreography in seconds: every phase once, in
 *  an order of its own per block, 3.5 to 4.5 s each. */
export const BLOCK = 24;
/** The last seconds of each phase glide into the next one. */
export const BLEND = 1;
const LENGTHS = [3.5, 4.5, 4, 3.5, 4.5, 4];

/** A small random number generator with a seed (mulberry32). */
function random(seed: number): () => number {
	let a = seed >>> 0;
	return () => {
		a = (a + 0x6d2b79f5) >>> 0;
		let r = Math.imul(a ^ (a >>> 15), 1 | a);
		r = (r + Math.imul(r ^ (r >>> 7), 61 | r)) ^ r;
		return ((r ^ (r >>> 14)) >>> 0) / 4294967296;
	};
}

/** The phases of block b in a shuffled order; block 0 begins with the
 *  ring. */
function shuffled(b: number, rnd: () => number): number[] {
	const order = PHASES.map((_, i) => i);
	for (let i = order.length - 1; i > 0; i--) {
		const j = Math.floor(rnd() * (i + 1));
		[order[i], order[j]] = [order[j], order[i]];
	}
	if (b === 0) {
		const at = order.indexOf(0);
		[order[0], order[at]] = [order[at], order[0]];
	}
	return order;
}
/** A well mixed seed per block (the finaliser of MurmurHash3), so blocks
 *  next to each other get unrelated orders. */
function seed(b: number): () => number {
	let h = Math.imul(b + 1, 0x9e3779b1);
	h = Math.imul(h ^ (h >>> 16), 0x85ebca6b);
	h = Math.imul(h ^ (h >>> 13), 0xc2b2ae35);
	return random(h ^ (h >>> 16));
}

/** The order of the phases and their lengths in block b. A block never
 *  begins with the phase the block before ends with. */
function block(b: number): { order: number[]; lengths: number[] } {
	const rnd = seed(b);
	const order = shuffled(b, rnd);
	// Only the first two places change here, so the last place of the
	// block before is its shuffled one.
	if (b !== 0 && order[0] === shuffled(b - 1, seed(b - 1)).at(-1))
		[order[0], order[1]] = [order[1], order[0]];
	const shift = Math.floor(rnd() * LENGTHS.length);
	const lengths = LENGTHS.map((_, i) => LENGTHS[(i + shift) % LENGTHS.length]);
	return { order, lengths };
}

/** Eases a glide: slow at both ends. */
export function ease(k: number): number {
	const c = Math.min(1, Math.max(0, k));
	return c * c * (3 - 2 * c);
}

/** The k-th phase of the choreography (6 per block): which phase and when
 *  it begins. */
function segment(k: number): { phase: number; start: number; length: number } {
	const b = Math.floor(k / PHASES.length);
	const i = k - b * PHASES.length;
	const { order, lengths } = block(b);
	let start = b * BLOCK;
	for (let n = 0; n < i; n++) start += lengths[n];
	return { phase: order[i], start, length: lengths[i] };
}

const PERMS = [
	[0, 1, 2],
	[0, 2, 1],
	[1, 0, 2],
	[1, 2, 0],
	[2, 0, 1],
	[2, 1, 0],
];
/** Which dot of its phase each of the three dots follows in segment k.
 *  At every glide the dots take the places of the next phase that are
 *  nearest to them, so they do not cross each other's paths on the way
 *  (the dots are flat and of one colour: two on top of each other look
 *  like one). It depends on every glide before, so it is worked out once
 *  from the beginning and remembered. */
const roles: number[][] = [[0, 1, 2]];
function rolesOf(k: number): number[] {
	for (let n = roles.length; n <= k; n++) {
		const from = segment(n - 1);
		const to = segment(n);
		const at = to.start - BLEND;
		const before = roles[n - 1];
		const a = PHASES[from.phase](at).map((_, i, all) => all[before[i]]);
		const b = PHASES[to.phase](at);
		let best = PERMS[0];
		let least = Infinity;
		for (const perm of PERMS) {
			let sum = 0;
			for (let i = 0; i < 3; i++) {
				const p = a[i];
				const q = b[perm[i]];
				sum += Math.hypot(p[0] - q[0], p[1] - q[1], p[2] - q[2]);
			}
			if (sum < least) [least, best] = [sum, perm];
		}
		roles.push(best);
	}
	return roles[k];
}

/** Which phase runs at time t, which comes next, and how far the glide
 *  into the next one is (0 … 1). */
export function phaseAt(t: number): { now: number; next: number; glide: number; k: number } {
	let k = Math.max(0, Math.floor(t / BLOCK)) * PHASES.length;
	let seg = segment(k);
	while (t >= seg.start + seg.length) seg = segment(++k);
	return {
		now: seg.phase,
		next: segment(k + 1).phase,
		glide: ease((t - (seg.start + seg.length - BLEND)) / BLEND),
		k,
	};
}

/** The choreography: where the three dots stand at time t (seconds, from
 *  0). Both phases of a glide are taken at the same time t, so the dots
 *  move on without a jump, also from one block to the next. */
export function dance(t: number): Spot[] {
	const { now, next, glide, k } = phaseAt(Math.max(0, t));
	const role = rolesOf(k);
	const all = PHASES[now](t);
	const a = role.map((i) => all[i]);
	let b = a;
	if (glide > 0) {
		const nextAll = PHASES[next](t);
		b = rolesOf(k + 1).map((i) => nextAll[i]);
	}
	return a.map((p, n) => {
		const q = b[n];
		const mix = (i: number) => p[i] + (q[i] - p[i]) * glide;
		return { x: mix(0), y: mix(1), z: Math.max(-1, Math.min(1, mix(2) / REACH)) };
	});
}

/** Which figure a kind of work shows and how fast. Owner 16 Nov 2026:
 *  "nur bei den 3 Punkten bleiben und nichts anderes noch mit reinholen,
 *  dann hätten wir immer eine flüssige Animation" – every kind shows the
 *  same dots at one pace; only the word beside them tells what is done. */
const DOTS = { figure: dance, speed: 1 };
export const FIGURES: Record<BusyKind, { figure: (t: number) => Spot[]; speed: number }> = {
	'': DOTS,
	system: DOTS,
	web: DOTS,
	home: DOTS,
	files: DOTS,
	code: DOTS,
	write: DOTS,
	switch: DOTS,
};

/** Where the three dots of a kind stand at time t (seconds). */
export function figureSpots(kind: BusyKind, t: number): Spot[] {
	const { figure, speed } = FIGURES[kind] ?? FIGURES[''];
	return figure(t * speed);
}

/** Where the dots stand after `clock` units of motion: the time already
 *  scaled by the figure's speed. The component runs one clock that goes on
 *  across kinds of work, so a new word never starts the motion anew (owner
 *  16 Nov 2026: "wird die immer wieder von vorne neu geladen"). */
export function figureAt(kind: BusyKind, clock: number): Spot[] {
	return (FIGURES[kind] ?? FIGURES['']).figure(clock);
}
