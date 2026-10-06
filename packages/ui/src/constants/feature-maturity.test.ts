import {describe, expect, it} from 'vitest'

import {getFeatureMaturity, parseFeatureMaturity} from './feature-maturity'

describe('feature maturity', () => {
	it('treats every unspecified feature as stable', () => {
		for (const feature of ['files', 'photos', 'machines', 'app-store', 'mcp'] as const) {
			expect(getFeatureMaturity(feature)).toBe('stable')
		}
	})

	it('accepts explicit preview configuration independently for each feature', () => {
		expect(parseFeatureMaturity('{"mcp":"alpha","machines":"beta","files":"stable"}')).toEqual({
			mcp: 'alpha',
			machines: 'beta',
			files: 'stable',
		})
	})

	it('ignores malformed configuration, unknown feature IDs, and unsupported values', () => {
		for (const input of [undefined, '', 'broken', 'null', '[]', 'true']) expect(parseFeatureMaturity(input)).toEqual({})
		expect(parseFeatureMaturity('{"mcp":"experimental","unknown":"beta","files":"beta"}')).toEqual({files: 'beta'})
	})
})
