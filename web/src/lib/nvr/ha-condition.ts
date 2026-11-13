// PLAN 83: helpers of a camera's condition on Home Assistant – which
// states an entity offers, which ones are chosen at first, the order of
// the entities in the choice.
import type { HACondition, HARule } from './types.ts';

/** An entity as GET /homeassistant/entities lists it. */
export type HAEntity = { id: string; name: string; area?: string; state: string; domain: string };

export const MAX_RULES = 10;

/** The states Home Assistant knows for a domain, in a sensible order. */
const KNOWN: Record<string, string[]> = {
	alarm_control_panel: [
		'disarmed',
		'armed_home',
		'armed_away',
		'armed_night',
		'armed_vacation',
		'armed_custom_bypass',
		'arming',
		'pending',
		'triggered',
	],
	person: ['home', 'not_home'],
	device_tracker: ['home', 'not_home'],
	input_boolean: ['on', 'off'],
	binary_sensor: ['on', 'off'],
	switch: ['on', 'off'],
	light: ['on', 'off'],
	fan: ['on', 'off'],
	automation: ['on', 'off'],
	group: ['on', 'off', 'home', 'not_home'],
	lock: ['locked', 'unlocked'],
	cover: ['open', 'closed'],
	sun: ['above_horizon', 'below_horizon'],
};

/** Domains that suit a condition come first in the choice. */
export const DOMAIN_ORDER = [
	'alarm_control_panel',
	'person',
	'input_boolean',
	'binary_sensor',
	'device_tracker',
	'input_select',
	'switch',
	'lock',
	'cover',
	'sun',
	'group',
];

export const domainOf = (entity: string) => entity.split('.')[0] ?? '';

/** The states to offer: the known ones, the chosen ones and the current one. */
export function stateChoices(entity: string, chosen: string[], current?: string): string[] {
	const list = [...(KNOWN[domainOf(entity)] ?? [])];
	for (const s of [...chosen, current ?? '']) {
		if (s && s !== 'unavailable' && s !== 'unknown' && !list.includes(s)) list.push(s);
	}
	return list;
}

/** The states chosen for an entity just picked: an alarm while armed, else its current state. */
export function firstStates(entity: string, current?: string): string[] {
	if (domainOf(entity) === 'alarm_control_panel')
		return [
			'armed_home',
			'armed_away',
			'armed_night',
			'armed_vacation',
			'armed_custom_bypass',
			'triggered',
		];
	if (domainOf(entity) === 'person' || domainOf(entity) === 'device_tracker') return ['not_home'];
	if (current && current !== 'unavailable' && current !== 'unknown') return [current];
	const known = KNOWN[domainOf(entity)];
	return known ? [known[0]] : [];
}

/** Entities matching a search, grouped by domain in DOMAIN_ORDER, then A–Z. */
export function entityGroups(
	entities: HAEntity[],
	search: string,
	keep: string,
): [string, HAEntity[]][] {
	const q = search.trim().toLowerCase();
	const shown = entities.filter(
		(e) =>
			e.id === keep || !q || e.id.toLowerCase().includes(q) || e.name.toLowerCase().includes(q),
	);
	const groups = new Map<string, HAEntity[]>();
	for (const e of shown) groups.set(e.domain, [...(groups.get(e.domain) ?? []), e]);
	const rank = (d: string) => {
		const i = DOMAIN_ORDER.indexOf(d);
		return i < 0 ? DOMAIN_ORDER.length : i;
	};
	return [...groups.entries()]
		.sort(([a], [b]) => rank(a) - rank(b) || a.localeCompare(b))
		.map(([d, list]) => [
			d,
			list.sort((a, b) => a.name.localeCompare(b.name) || a.id.localeCompare(b.id)),
		]);
}

/** A new rule: the first alarm system if there is one. */
export function newRule(connection: string, entities: HAEntity[]): HARule {
	const alarm = entities.find((e) => e.domain === 'alarm_control_panel');
	return alarm
		? { connection, entity: alarm.id, states: firstStates(alarm.id, alarm.state) }
		: { connection, entity: '', states: [] };
}

/** The condition switched on or off; off is sent as no rules, which removes it. */
export function conditionOn(on: boolean, connection: string, entities: HAEntity[]): HACondition {
	return on ? { mode: 'all', rules: [newRule(connection, entities)] } : { mode: 'all', rules: [] };
}

export const conditionActive = (c: HACondition | null | undefined) => !!c && c.rules.length > 0;
