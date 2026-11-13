// PLAN 83: the choices of a camera's condition on Home Assistant.
import { describe, expect, it } from 'vitest';
import {
	conditionActive,
	conditionOn,
	entityGroups,
	firstStates,
	newRule,
	stateChoices,
	type HAEntity,
} from '../../src/lib/nvr/ha-condition.ts';

const entities: HAEntity[] = [
	{ id: 'sensor.temperatur', name: 'Temperatur', state: '21.5', domain: 'sensor' },
	{ id: 'person.alex', name: 'Alex', state: 'home', domain: 'person' },
	{
		id: 'alarm_control_panel.home',
		name: 'Alarmanlage',
		state: 'disarmed',
		domain: 'alarm_control_panel',
	},
];

describe('condition on Home Assistant', () => {
	it('starts with the alarm system, armed and triggered', () => {
		const rule = newRule('main', entities);
		expect(rule.entity).toBe('alarm_control_panel.home');
		expect(rule.states).toContain('armed_away');
		expect(rule.states).toContain('triggered');
		expect(rule.states).not.toContain('disarmed');
		expect(newRule('main', [])).toEqual({ connection: 'main', entity: '', states: [] });
	});
	it('offers the known states, the chosen ones and the current one', () => {
		expect(stateChoices('person.alex', [], 'Arbeit')).toEqual(['home', 'not_home', 'Arbeit']);
		expect(stateChoices('sensor.x', ['hoch'], 'unavailable')).toEqual(['hoch']);
		expect(firstStates('person.alex', 'home')).toEqual(['not_home']);
		expect(firstStates('input_select.modus', 'Nacht')).toEqual(['Nacht']);
		expect(firstStates('switch.x', 'unavailable')).toEqual(['on']);
	});
	it('lists suitable domains first and searches names and IDs', () => {
		expect(entityGroups(entities, '', '').map(([d]) => d)).toEqual([
			'alarm_control_panel',
			'person',
			'sensor',
		]);
		expect(entityGroups(entities, 'ALEX', '').flatMap(([, l]) => l.map((e) => e.id))).toEqual([
			'person.alex',
		]);
		expect(
			entityGroups(entities, 'temp', 'person.alex').flatMap(([, l]) => l.map((e) => e.id)),
		).toEqual(['person.alex', 'sensor.temperatur']);
	});
	it('is switched off as no rules, which removes it on the NAS', () => {
		expect(conditionOn(false, 'main', entities)).toEqual({ mode: 'all', rules: [] });
		expect(conditionActive(conditionOn(false, 'main', entities))).toBe(false);
		expect(conditionActive(conditionOn(true, 'main', entities))).toBe(true);
		expect(conditionActive(null)).toBe(false);
	});
});
