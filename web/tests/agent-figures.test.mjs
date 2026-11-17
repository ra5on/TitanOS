import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import {
	BLEND,
	BLOCK,
	FIGURES,
	PHASES,
	SPHERE,
	dance,
	figureAt,
	figureSpots,
	phaseAt,
} from '../src/lib/agent-figures.ts';

const kinds = Object.keys(FIGURES);
const FRAME = 1 / 60;
const pairs = [
	[0, 1],
	[0, 2],
	[1, 2],
];
const apart = (s) =>
	Math.min(...pairs.map(([a, b]) => Math.hypot(s[a].x - s[b].x, s[a].y - s[b].y)));

/** The times where one phase glides into the next, over a few blocks. */
function borders(blocks) {
	const out = [];
	let before = phaseAt(0);
	for (let t = 0; t < blocks * BLOCK; t += 0.05) {
		const now = phaseAt(t);
		if (now.k !== before.k) out.push(t);
		before = now;
	}
	return out;
}

test('every kind of work shows the same dots at one pace (owner, 16 Nov 2026)', () => {
	assert.deepEqual(kinds.sort(), ['', 'code', 'files', 'home', 'switch', 'system', 'web', 'write']);
	const one = FIGURES[''];
	for (const kind of kinds) {
		assert.equal(FIGURES[kind].figure, one.figure, kind);
		assert.equal(FIGURES[kind].speed, one.speed, kind);
		assert.deepEqual(figureAt(kind, 7.3), figureSpots('', 7.3), kind);
	}
});

test('every phase and every glide stays inside the box, the dots of one size', () => {
	// Each phase alone, and the choreography with its glides, over ten
	// minutes: no dot reaches over the edge of the figure.
	const inside = (spots, what) => {
		assert.equal(spots.length, 3);
		for (const [n, p] of spots.entries()) {
			for (const v of [p.x, p.y, p.z]) assert.ok(Number.isFinite(v), what);
			assert.ok(Math.abs(p.x) + SPHERE <= 1, `${what}: dot ${n} leaves sideways (${p.x})`);
			assert.ok(Math.abs(p.y) + SPHERE <= 1, `${what}: dot ${n} leaves up or down (${p.y})`);
			assert.ok(p.z >= -1 && p.z <= 1, `${what}: depth ${p.z}`);
			// One size for all three: a dot has no size, scale or brightness of its own.
			assert.deepEqual(Object.keys(p).sort(), ['x', 'y', 'z'], what);
		}
	};
	for (const [i, phase] of PHASES.entries())
		for (let t = 0; t < 120; t += 0.05)
			inside(
				phase(t).map(([x, y, z]) => ({ x, y, z: Math.max(-1, Math.min(1, z)) })),
				`phase ${i} at ${t}`,
			);
	for (let t = 0; t < 600; t += 0.05) inside(dance(t), `choreography at ${t.toFixed(2)}`);
});

test('small steps of time make small moves, also where one phase glides into the next', () => {
	// Frame by frame over four blocks, and densely around every border.
	const step = (t) => {
		const a = dance(t);
		const b = dance(t + FRAME);
		for (const n of [0, 1, 2]) {
			const d = Math.hypot(b[n].x - a[n].x, b[n].y - a[n].y, b[n].z - a[n].z);
			assert.ok(d < 0.06, `dot ${n} jumps ${d.toFixed(3)} at ${t.toFixed(3)}`);
		}
	};
	for (let t = 0; t < 4 * BLOCK; t += FRAME) step(t);
	const cuts = borders(6);
	assert.ok(cuts.length >= 30, `phases change (${cuts.length})`);
	for (const cut of cuts) {
		for (const at of [cut - BLEND, cut]) for (let d = -0.05; d <= 0.05; d += 0.001) step(at + d);
		// The glide ends exactly where the next phase is.
		const end = dance(cut + 1e-6);
		const before = dance(cut - 1e-6);
		for (const n of [0, 1, 2])
			assert.ok(
				Math.abs(end[n].x - before[n].x) + Math.abs(end[n].y - before[n].y) < 1e-3,
				`cut at ${cut}`,
			);
	}
	// No kink either: the change of speed from frame to frame stays small.
	for (let t = FRAME; t < 2 * BLOCK; t += FRAME) {
		const [a, b, c] = [dance(t - FRAME), dance(t), dance(t + FRAME)];
		for (const n of [0, 1, 2]) {
			const kink = Math.hypot(a[n].x - 2 * b[n].x + c[n].x, a[n].y - 2 * b[n].y + c[n].y);
			assert.ok(kink < 0.004, `dot ${n} turns sharply at ${t.toFixed(3)} (${kink})`);
		}
	}
});

test('a phase lasts 3 to 5 seconds and glides into the next in about a second', () => {
	assert.ok(BLEND >= 0.8 && BLEND <= 1.2, `glide ${BLEND} s`);
	const cuts = [0, ...borders(8)];
	for (let i = 1; i < cuts.length; i++) {
		const length = cuts[i] - cuts[i - 1];
		assert.ok(
			length >= 3 - 0.06 && length <= 5 + 0.06,
			`phase of ${length.toFixed(2)} s at ${cuts[i]}`,
		);
	}
	// Every phase comes, and never twice in a row.
	const seen = new Set();
	let before = -1;
	for (let t = 0; t < 8 * BLOCK; t += 0.25) {
		const { now } = phaseAt(t);
		seen.add(now);
		if (now !== before) assert.notEqual(now, before);
		before = now;
	}
	assert.equal(seen.size, PHASES.length);
});

test('the choreography does not repeat after a few seconds', () => {
	// Shifted by any time from 1 to 60 s, the dots stand elsewhere: the
	// blocks take their phases in orders of their own, and the phases run
	// at speeds that do not meet.
	const orders = new Set();
	for (let b = 0; b < 6; b++) {
		const order = [];
		for (let t = b * BLOCK; t < (b + 1) * BLOCK; t += 0.25) {
			const { now } = phaseAt(t);
			if (order.at(-1) !== now) order.push(now);
		}
		orders.add(order.join(''));
	}
	assert.ok(orders.size >= 5, `blocks in their own order (${[...orders]})`);
	for (let shift = 1; shift <= 60; shift += 0.5) {
		let sum = 0;
		let count = 0;
		for (let t = 0; t < 2 * BLOCK; t += 0.25) {
			const a = dance(t);
			const b = dance(t + shift);
			for (const n of [0, 1, 2]) sum += Math.hypot(a[n].x - b[n].x, a[n].y - b[n].y);
			count += 3;
		}
		assert.ok(sum / count > 0.15, `repeats after ${shift} s (${(sum / count).toFixed(3)})`);
	}
});

test('three stay three: two dots cover each other only for a moment', () => {
	// The dots are flat and of one colour; two on top of each other look
	// like one. They may pass in front of each other (that shows depth),
	// but not stay together.
	let run = 0;
	let together = 0;
	let frames = 0;
	for (let t = 0; t < 1200; t += FRAME) {
		frames++;
		if (apart(dance(t)) < SPHERE / 2) {
			together++;
			run += FRAME;
			assert.ok(run < 0.45, `two dots stay together at ${t.toFixed(2)}`);
		} else run = 0;
	}
	assert.ok(
		together / frames < 0.03,
		`together ${((100 * together) / frames).toFixed(1)} % of the time`,
	);
	// The still picture (time 0) shows three dots well apart.
	assert.ok(apart(dance(0)) > 2 * SPHERE, 'the still picture');
	// Together means touching: in "together and apart" they come to one
	// diameter, not closer.
	const gather = PHASES[4];
	let least = Infinity;
	for (let t = 0; t < 30; t += 0.01) {
		const s = gather(t);
		least = Math.min(
			least,
			...pairs.map(([a, b]) => Math.hypot(...[0, 1, 2].map((i) => s[a][i] - s[b][i]))),
		);
	}
	assert.ok(Math.abs(least - 2 * SPHERE) < 0.01, `they touch (${least})`);
});

test('the words of every kind exist in German and English, numbered without a gap', () => {
	const view = readFileSync(
		new URL('../src/lib/components/agent/chat/LiveLine.svelte', import.meta.url),
		'utf8',
	);
	const block = /const WORDS = \{([^}]*)\}/.exec(view)?.[1] ?? '';
	const counts = Object.fromEntries(
		[...block.matchAll(/(''|\w+): (\d+)/g)].map((m) => [m[1] === "''" ? '' : m[1], Number(m[2])]),
	);
	assert.deepEqual(Object.keys(counts).sort(), kinds.sort());
	for (const lang of ['de', 'en']) {
		const texts = JSON.parse(
			readFileSync(new URL(`../src/lib/i18n/${lang}.json`, import.meta.url), 'utf8'),
		);
		for (const [kind, count] of Object.entries({ ...counts, long: 6, night: 2 })) {
			const key = (n) => `agent.busy.${kind ? kind + '.' : ''}${n}`;
			for (let n = 1; n <= count; n++) assert.ok(texts[key(n)], `${lang}: ${key(n)} is missing`);
			assert.equal(texts[key(count + 1)], undefined, `${lang}: ${key(count + 1)} is never shown`);
		}
	}
});
