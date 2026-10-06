import {describe, expect, test} from 'vitest'

import {assertManifestVersionCompatible, isManifestVersionCompatible} from './manifest-compatibility.js'

describe('isManifestVersionCompatible()', () => {
	test.each([
		{manifestVersion: '1.0.0', titanVersion: '1.7.3', compatible: true},
		{manifestVersion: '2.0.0', titanVersion: '2.0.0', compatible: true},
		{manifestVersion: '2.0.0', titanVersion: '2.0.0-beta.1', compatible: true},
		{manifestVersion: '2.0.0', titanVersion: '2.0.0-rc.2+build.5', compatible: true},
		{manifestVersion: '2.0.0', titanVersion: '1.9.9', compatible: false},
		{manifestVersion: '2.0.1', titanVersion: '2.0.0-beta.1', compatible: false},
		{manifestVersion: '2.1.0', titanVersion: '2.0.0-beta.1', compatible: false},
		{manifestVersion: 'invalid', titanVersion: '2.0.0', compatible: false},
		{manifestVersion: '2.0.0', titanVersion: 'invalid', compatible: false},
	])('$titanVersion supports $manifestVersion: $compatible', ({manifestVersion, titanVersion, compatible}) => {
		expect(isManifestVersionCompatible(manifestVersion, titanVersion)).toBe(compatible)
	})
})

describe('assertManifestVersionCompatible()', () => {
	test('distinguishes invalid manifest versions from unsupported versions', () => {
		expect(() => assertManifestVersionCompatible('invalid', '2.0.0')).toThrow('App manifest version is invalid')
		expect(() => assertManifestVersionCompatible('2.0.0', '1.9.9')).toThrow('App manifest version not supported')
	})
})
