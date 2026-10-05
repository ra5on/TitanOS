import {afterEach, describe, expect, test} from 'vitest'

import {createTestVm} from '../test-utilities/create-test-titand.js'
import {
	hangFinishedPath,
	hangStatePath,
	installPreStartHook,
	rebootAndAssertTitandStarts,
	runSudoScript,
	type TestTitandVm,
} from './custom-hooks.vm-test-helpers.js'

describe('Custom pre-start hook timeout handling', () => {
	let titand: TestTitandVm | undefined

	afterEach(async () => {
		await titand?.cleanup()
		titand = undefined
	})

	test('starts titand on the next boot when a pre-start hook hangs for a long time', async () => {
		// The default pre-start hook timeout is five minutes, so this VM needs a longer boot wait.
		titand = await createTestVm({device: 'titan-home', startupTimeout: 600_000})
		await titand.vm.powerOn()
		await titand.registerAndLogin()

		// Install a hook that starts, then hangs longer than the wrapper allows.
		const hookScript = `#!/bin/sh
set -eu
printf 'started\\n' > '${hangStatePath}'
sleep 600
printf 'finished\\n' > '${hangFinishedPath}'
`
		await installPreStartHook(titand, hookScript)

		// Reboot through the normal boot path and wait for the real hook timeout.
		await rebootAndAssertTitandStarts(titand)

		const hangState = await runSudoScript(titand, `cat '${hangStatePath}'`)
		expect(hangState.trim()).toBe('started')

		const finishedMarker = await runSudoScript(titand, `test ! -e '${hangFinishedPath}' && printf 'missing\\n'`)
		expect(finishedMarker.trim()).toBe('missing')

		const preStartLogs = await runSudoScript(titand, 'journalctl -b -u titan-custom-pre-start.service --no-pager')
		expect(preStartLogs).toContain('timed out after 5m')
	}, 900_000)
})
