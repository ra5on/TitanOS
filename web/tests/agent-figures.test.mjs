import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import { FIGURES, SPHERE, blendSpots, figureSpots } from '../src/lib/agent-figures.ts';

const kinds = Object.keys(FIGURES);

test('every kind of work has a figure that moves and stays inside its box', () => {
	assert.deepEqual(kinds.sort(), ['', 'code', 'files', 'home', 'switch', 'system', 'web', 'write']);
	for (const kind of kinds) {
		let moved = false;
		const first = figureSpots(kind, 0);
		for (let t = 0; t <= 12; t += 0.05) {
			const spots = figureSpots(kind, t);
			assert.equal(spots.length, 3);
			for (const [n, p] of spots.entries()) {
				for (const v of [p.x, p.y, p.z]) assert.ok(Number.isFinite(v), `${kind} ${t}`);
				assert.equal(p.s, undefined, `${kind}: no sphere has a size of its own`);
				// The sphere stays inside the figure (a little air).
				const r = SPHERE;
				assert.ok(Math.abs(p.x) + r <= 1.12, `${kind}: sphere ${n} leaves sideways at ${t}`);
				assert.ok(Math.abs(p.y) + r <= 1.12, `${kind}: sphere ${n} leaves up or down at ${t}`);
				assert.ok(p.z >= -1 && p.z <= 1, `${kind}: depth ${p.z}`);
				if (Math.abs(p.x - first[n].x) + Math.abs(p.y - first[n].y) > 0.2) moved = true;
			}
		}
		assert.ok(moved, `${kind}: the figure stands still`);
	}
});

test('every kind of work shows the same three dots at one pace (owner, 16 Nov 2026)', () => {
	const one = FIGURES[''];
	for (const kind of kinds) {
		assert.equal(FIGURES[kind].figure, one.figure, kind);
		assert.equal(FIGURES[kind].speed, one.speed, kind);
	}
});

test('a change of figure glides: it starts where the spheres stand and ends in the new figure', () => {
	for (const a of kinds)
		for (const b of kinds) {
			const from = figureSpots(a, 3);
			const to = figureSpots(b, 3);
			const at = (spots) => spots.map((p) => [p.x, p.y, p.z].map((v) => Math.round(v * 1e9)));
			assert.deepEqual(at(blendSpots(from, to, 0)), at(from));
			assert.deepEqual(at(blendSpots(from, to, 1)), at(to));
			// No jump on the way: small steps make small moves.
			let before = blendSpots(from, to, 0);
			for (let k = 0.02; k <= 1; k += 0.02) {
				const now = blendSpots(from, to, k);
				for (const n of [0, 1, 2])
					assert.ok(
						Math.abs(now[n].x - before[n].x) + Math.abs(now[n].y - before[n].y) < 0.2,
						`${a} → ${b} jumps at ${k}`,
					);
				before = now;
			}
		}
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
