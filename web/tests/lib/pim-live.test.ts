// 1.13: the live stream of Kontakte and Kalender (lib/pim/live.ts).
import { expect, test } from 'vitest';
import { parsePimBlock, pimPauseFor } from '#lib/pim/live.ts';

test('a change names kind, collection and generation', () => {
	expect(
		parsePimBlock(
			'event: pim\ndata: {"account":"alex","kind":"contacts","collection":"kontakte","generation":"ab-12"}',
		),
	).toEqual({
		type: 'pim',
		account: 'alex',
		kind: 'contacts',
		collection: 'kontakte',
		generation: 'ab-12',
	});
	expect(
		parsePimBlock(
			'event: pim\ndata: {"account":"alex","kind":"calendar","collection":"arbeit","generation":""}',
		),
	).toEqual({
		type: 'pim',
		account: 'alex',
		kind: 'calendar',
		collection: 'arbeit',
		generation: '',
	});
});

test('ready carries the generations, resync none', () => {
	expect(
		parsePimBlock(
			'event: ready\ndata: {"account":"alex","generations":{"contacts/kontakte":"0-0","x":5}}',
		),
	).toEqual({ type: 'ready', account: 'alex', generations: { 'contacts/kontakte': '0-0' } });
	expect(parsePimBlock('event: resync\ndata: {"account":"alex"}')).toEqual({ type: 'resync' });
});

test('anything else is no event', () => {
	expect(parsePimBlock(': keep-alive')).toBeNull();
	expect(
		parsePimBlock('event: pim\ndata: {"account":"alex","kind":"mail","collection":"x"}'),
	).toBeNull();
	expect(parsePimBlock('event: pim\ndata: {"kind":"contacts","collection":"x"}')).toBeNull();
	expect(parsePimBlock('event: pim\ndata: not json')).toBeNull();
	expect(parsePimBlock('event: other\ndata: {"account":"alex"}')).toBeNull();
});

test('the pause grows to 30 s', () => {
	expect(pimPauseFor(0)).toBe(1000);
	expect(pimPauseFor(3)).toBe(8000);
	expect(pimPauseFor(10)).toBe(30_000);
});
