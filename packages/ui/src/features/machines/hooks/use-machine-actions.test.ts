import fs from 'node:fs'
import path from 'node:path'
import {describe, expect, test, vi} from 'vitest'

import {getMachinesErrorMessage} from './use-machine-actions'

vi.mock('@/components/ui/toast', () => ({toast: {error: vi.fn()}}))
vi.mock('@/trpc/trpc', () => ({trpcReact: {}}))
vi.mock('@/utils/i18n', async () => {
	const {default: german} = await import('../../../../public/locales/de.json')
	return {t: (key: string) => german[key as keyof typeof german] ?? key}
})

function getBackendErrorCodes(directory: string): Set<string> {
	const codes = new Set<string>()
	for (const entry of fs.readdirSync(directory, {withFileTypes: true})) {
		const file = path.join(directory, entry.name)
		if (entry.isDirectory()) for (const code of getBackendErrorCodes(file)) codes.add(code)
		else if (entry.name.endsWith('.ts') && !entry.name.includes('.test.')) {
			for (const match of fs.readFileSync(file, 'utf8').matchAll(/\[(machine[a-z-]+)\]/g)) codes.add(match[1])
		}
	}
	return codes
}

describe('German VM error messages', () => {
	test('covers every error code returned by the VM backend', () => {
		const directory = path.resolve(import.meta.dirname, '../../../../..', 'umbreld/source/modules/machines')
		const codes = getBackendErrorCodes(directory)
		expect(codes.size).toBeGreaterThan(50)
		for (const code of codes) {
			const message = getMachinesErrorMessage(`[${code}] raw internal diagnostics`)
			expect(message, code).not.toContain('raw internal diagnostics')
			expect(message, code).not.toContain('machines-error.')
		}
	})

	test('shows understandable German feedback for unknown or unstructured failures', () => {
		for (const message of ['[future-machine-error] internals', 'Unhandled native driver failure']) {
			expect(getMachinesErrorMessage(message)).toContain('Die Aktion konnte nicht ausgeführt werden')
		}
	})
})
