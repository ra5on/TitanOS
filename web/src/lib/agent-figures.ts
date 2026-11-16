// The working figure of the KI-Agent: three shaded spheres (0.100, owner
// 16 Nov 2026: "3 dimensional"). Since 1.01 one motion for every kind of
// work – three dots hopping in a row (see FIGURES).
//
// A figure gives, for a time and a sphere, a place in units of half the
// figure's width (-1 … 1) and a depth (-1 back … 1 front). The component
// (BusyFigure.svelte) draws that; this file is plain arithmetic.
//
// All three spheres are always the same size, in every figure (owner,
// 16 Nov 2026: "dass alle drei Punkte gleich groß sind"): depth shows in
// brightness and in what covers what, not in size. A change of figure
// glides from one motion into the next (blendSpots).
import type { BusyKind } from './agent.ts';

export type Spot = {
	x: number;
	y: number;
	/** -1 behind … 1 in front: decides brightness and what covers what. */
	z: number;
	/** Squash on landing (1 = round). */
	squash?: number;
};
type Figure = (t: number, n: number, speed: number) => Spot;

/** Diameter of every sphere as a share of the figure's width. */
export const SPHERE = 0.24;

/** A row whose spheres hop one after the other. */
const hop: Figure = (t, n, k) => {
	const p = (((t * k * 1.5 - n * 0.22) % 1.4) + 1.4) % 1.4;
	const up = p < 1 ? Math.sin(p * Math.PI) : 0;
	return { x: (n - 1) * 0.62, y: 0.4 - up * 0.75, z: up * 0.4, squash: p < 1 ? 1 : 0.86 };
};

/** Which figure a kind of work shows, how fast, and whether it casts a
 *  shadow. Owner 16 Nov 2026: "nur bei den 3 Punkten bleiben und nichts
 *  anderes noch mit reinholen, dann hätten wir immer eine flüssige
 *  Animation" – every kind shows the same three dots hopping in a row at
 *  one pace; only the word beside them tells what is done. (0.100 to 1.00
 *  had four figures, one per kind.) */
const DOTS = { figure: hop, speed: 1.2, shadow: true };
export const FIGURES: Record<BusyKind, { figure: Figure; speed: number; shadow: boolean }> = {
	'': DOTS,
	system: DOTS,
	web: DOTS,
	home: DOTS,
	files: DOTS,
	code: DOTS,
	write: DOTS,
	switch: DOTS,
};

/** Where the three spheres of a kind stand at time t (seconds). */
export function figureSpots(kind: BusyKind, t: number): Spot[] {
	const { figure, speed } = FIGURES[kind] ?? FIGURES[''];
	return [0, 1, 2].map((n) => figure(t, n, speed));
}

/** Where the spheres stand after `clock` units of motion: the time already
 *  scaled by the figure's speed. The component runs one clock that goes on
 *  across kinds of work, so a new word or a kind with the same figure only
 *  changes the pace and never starts the motion anew (owner 16 Nov 2026:
 *  "wird die immer wieder von vorne neu geladen"). */
export function figureAt(kind: BusyKind, clock: number): Spot[] {
	const { figure } = FIGURES[kind] ?? FIGURES[''];
	return [0, 1, 2].map((n) => figure(clock, n, 1));
}

/** Whether two kinds of work show the same figure (only the pace differs). */
export function sameFigure(a: BusyKind, b: BusyKind): boolean {
	return (FIGURES[a] ?? FIGURES['']).figure === (FIGURES[b] ?? FIGURES['']).figure;
}

/** Eases a change of figure: slow at both ends. */
export function ease(k: number): number {
	const c = Math.min(1, Math.max(0, k));
	return c * c * (3 - 2 * c);
}

/** The spheres on their way from one figure (a) to the next (b), k 0 … 1. */
export function blendSpots(a: Spot[], b: Spot[], k: number): Spot[] {
	const e = ease(k);
	const mix = (x: number, y: number) => x + (y - x) * e;
	return b.map((to, n) => {
		const from = a[n] ?? to;
		return {
			x: mix(from.x, to.x),
			y: mix(from.y, to.y),
			z: mix(from.z, to.z),
			squash: mix(from.squash ?? 1, to.squash ?? 1),
		};
	});
}
