// The working figure of the KI-Agent (0.100, owner 16 Nov 2026: "kann man
// das noch mehr machen und 3 dimensional?"): three spheres that move in
// space. Each kind of work has the figure whose motion fits it; the owner
// liked all four of the draft, so every one has its place.
//
// A figure gives, for a time and a sphere, a place in units of half the
// figure's width (-1 … 1), a depth (-1 back … 1 front) and a size. The
// component (BusyFigure.svelte) draws that; this file is plain arithmetic.
import type { BusyKind } from './agent.ts';

export type Spot = {
	x: number;
	y: number;
	/** -1 behind … 1 in front: decides size, brightness and what covers what. */
	z: number;
	/** Size of the sphere, 1 = 42 % of the figure's width. */
	s: number;
	/** Squash on landing (1 = round). */
	squash?: number;
};
type Figure = (t: number, n: number, speed: number) => Spot;

const THIRD = (2 * Math.PI) / 3;

/** Three spheres on a tilted ring. */
const orbit: Figure = (t, n, k) => {
	const a = t * k * 2.2 + n * THIRD;
	return { x: Math.cos(a) * 0.7, y: Math.sin(a) * 0.26, z: Math.sin(a), s: 0.62 };
};
/** A core two spheres circle on two rings: the agent's own sign. */
const atom: Figure = (t, n, k) => {
	if (n === 0) return { x: 0, y: 0, z: 0, s: 0.78 + Math.sin(t * 2.4) * 0.06 };
	const a = t * k * (n === 1 ? 2.6 : -1.9) + n;
	const tilt = n === 1 ? 0.6 : -0.6;
	const ex = Math.cos(a) * 0.78;
	const ey = Math.sin(a) * 0.28;
	return {
		x: ex * Math.cos(tilt) - ey * Math.sin(tilt),
		y: ex * Math.sin(tilt) + ey * Math.cos(tilt),
		z: Math.sin(a) * 0.9,
		s: 0.42,
	};
};
/** The spheres fly in an eight, like being juggled. */
const juggle: Figure = (t, n, k) => {
	const a = t * k * 2.4 + n * THIRD;
	return {
		x: Math.sin(a) * 0.72,
		y: -Math.abs(Math.cos(a)) * 0.62 + 0.3,
		z: Math.cos(a) * 0.8,
		s: 0.56,
	};
};
/** A row whose spheres hop one after the other. */
const hop: Figure = (t, n, k) => {
	const p = (((t * k * 1.5 - n * 0.22) % 1.4) + 1.4) % 1.4;
	const up = p < 1 ? Math.sin(p * Math.PI) : 0;
	return { x: (n - 1) * 0.62, y: 0.4 - up * 0.75, z: up * 0.4, s: 0.56, squash: p < 1 ? 1 : 0.86 };
};

/** Which figure a kind of work shows, how fast, and whether it casts a shadow. */
export const FIGURES: Record<BusyKind, { figure: Figure; speed: number; shadow: boolean }> = {
	'': { figure: atom, speed: 1, shadow: false },
	system: { figure: atom, speed: 1.6, shadow: false },
	web: { figure: orbit, speed: 1.5, shadow: true },
	home: { figure: orbit, speed: 0.8, shadow: true },
	files: { figure: juggle, speed: 0.8, shadow: true },
	code: { figure: juggle, speed: 1.7, shadow: true },
	write: { figure: hop, speed: 1.2, shadow: true },
	switch: { figure: hop, speed: 2, shadow: true },
};

/** Where the three spheres of a kind stand at time t (seconds). */
export function figureSpots(kind: BusyKind, t: number): Spot[] {
	const { figure, speed } = FIGURES[kind] ?? FIGURES[''];
	return [0, 1, 2].map((n) => figure(t, n, speed));
}
