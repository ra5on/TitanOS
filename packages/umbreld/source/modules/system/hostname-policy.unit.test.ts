import {describe, expect, test} from 'vitest'

import {resolveTitanHostname} from './hostname-policy.js'

describe('Titan hostname migration', () => {
	test('replaces only the inherited default', () => {
		expect(resolveTitanHostname(undefined, 'umbrel')).toBe('titan')
		expect(resolveTitanHostname('umbrel', 'umbrel')).toBe('titan')
		expect(resolveTitanHostname(undefined, undefined)).toBe('titan')
	})

	test('preserves configured names across a new image and existing system names', () => {
		expect(resolveTitanHostname('mein-nas', 'titan')).toBe('mein-nas')
		expect(resolveTitanHostname(undefined, 'familien-nas')).toBe('familien-nas')
		expect(resolveTitanHostname('umbrel-lab', 'titan')).toBe('umbrel-lab')
		expect(resolveTitanHostname('titan', 'umbrel')).toBe('titan')
	})
})
