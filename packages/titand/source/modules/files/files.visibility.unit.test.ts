import {describe, expect, test, vi} from 'vitest'

import Files from './files.js'

import type Titand from '../../index.js'

const createFiles = () =>
	new Files({
		dataDirectory: '/tmp/titand-files-visibility-test',
		logger: {createChildLogger: () => ({})},
		eventBus: {emit: vi.fn()},
	} as unknown as Titand)

describe('Files visibility policy', () => {
	test('keeps the established Files-only hidden names', () => {
		const files = createFiles()

		expect(files.isHidden('.DS_Store')).toBe(true)
		expect(files.isHidden('.directory')).toBe(true)
		expect(files.isHidden('.titan-watcher-health-check')).toBe(true)
		expect(files.isHidden('partial.titan-upload')).toBe(true)
		expect(files.isHidden('claimed.titan-trash')).toBe(true)
	})

	test('does not inherit Cloud-only OS-junk names', () => {
		const files = createFiles()

		expect(files.isHidden('Thumbs.db')).toBe(false)
		expect(files.isHidden('desktop.ini')).toBe(false)
		expect(files.isHidden('._photo.jpg')).toBe(false)
	})
})
