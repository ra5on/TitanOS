import {describe, expect, test} from 'vitest'

import {dockConfigSchema} from './dock.js'

const settings = {type: 'system', id: 'settings'} as const

describe('dock configuration', () => {
	test('accepts system apps, apps, machines and shortcuts in any order', () => {
		const dock = {
			items: [
				{type: 'app', id: 'nextcloud'},
				settings,
				{type: 'machine', id: 'home-assistant'},
				{type: 'shortcut', id: 'https://example.com/'},
				{type: 'system', id: 'files'},
			],
			iconSize: 64,
		}
		expect(dockConfigSchema.parse(dock)).toEqual(dock)
	})

	test('always keeps Settings, which is where the dock is restored', () => {
		expect(dockConfigSchema.safeParse({items: [{type: 'system', id: 'files'}], iconSize: 50}).success).toBe(false)
		// An app that happens to be called settings does not count
		expect(dockConfigSchema.safeParse({items: [{type: 'app', id: 'settings'}], iconSize: 50}).success).toBe(false)
	})

	test('rejects duplicates, unknown entries, too many entries and sizes outside the slider', () => {
		const invalid = [
			{items: [settings, settings], iconSize: 50},
			{items: [settings, {type: 'system', id: 'terminal'}], iconSize: 50},
			{items: [settings, {type: 'link', id: 'x'}], iconSize: 50},
			{items: [settings, ...Array.from({length: 16}, (_, index) => ({type: 'app', id: `app-${index}`}))], iconSize: 50},
			{items: [settings], iconSize: 35},
			{items: [settings], iconSize: 73},
			{items: [settings], iconSize: 50.5},
			{items: [settings], iconSize: 50, extra: true},
		]
		for (const dock of invalid) expect(dockConfigSchema.safeParse(dock).success, JSON.stringify(dock)).toBe(false)
		expect(
			dockConfigSchema.safeParse({
				items: [settings, ...Array.from({length: 15}, (_, index) => ({type: 'app', id: `app-${index}`}))],
				iconSize: 72,
			}).success,
		).toBe(true)
	})
})
