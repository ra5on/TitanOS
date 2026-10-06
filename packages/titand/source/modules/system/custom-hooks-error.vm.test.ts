import {afterEach, describe, expect, test} from 'vitest'

import {createTestVm} from '../test-utilities/create-test-titand.js'
import {
	errorMarkerPath,
	installPreStartHook,
	rebootAndAssertTitandStarts,
	runSudoScript,
	type TestTitandVm,
} from './custom-hooks.vm-test-helpers.js'

describe('Custom pre-start hook failure handling', () => {
	let titand: TestTitandVm | undefined

	afterEach(async () => {
		await titand?.cleanup()
		titand = undefined
	})

	test('starts titand on the next boot when a pre-start hook exits with an error', async () => {
		// Boot and register a fresh Titan Home so the failed hook runs during a real reboot.
		titand = await createTestVm({device: 'titan-home'})
		await titand.vm.powerOn()
		await titand.registerAndLogin()

		// Install a hook that proves it ran and then fails with a non-zero exit code.
		const hookScript = `#!/bin/sh
set -eu
printf 'failed\\n' > '${errorMarkerPath}'
exit 42
`
		await installPreStartHook(titand, hookScript)

		// The wrapper should absorb the hook failure and still allow titand to start.
		await rebootAndAssertTitandStarts(titand)

		const errorMarker = await runSudoScript(titand, `cat '${errorMarkerPath}'`)
		expect(errorMarker.trim()).toBe('failed')

		const preStartLogs = await runSudoScript(titand, 'journalctl -b -u titan-custom-pre-start.service --no-pager')
		expect(preStartLogs).toContain('exited with status 42')
	}, 600_000)
})
