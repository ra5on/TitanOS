import {afterEach, describe, expect, test} from 'vitest'

import {createTestVm} from '../test-utilities/create-test-titand.js'
import {
	bootMarkerPath,
	hookPath,
	installPreStartHook,
	processStatePath,
	rebootAndAssertTitandStarts,
	runCountPath,
	runSudoScript,
	type TestTitandVm,
} from './custom-hooks.vm-test-helpers.js'

describe('Custom pre-start hook boot order', () => {
	let titand: TestTitandVm | undefined

	afterEach(async () => {
		await titand?.cleanup()
		titand = undefined
	})

	test('runs a persisted pre-start hook on the next boot before titand starts', async () => {
		// Boot and register a fresh Titan Home so the hook lives in persisted user data.
		titand = await createTestVm({device: 'titan-home'})
		await titand.vm.powerOn()
		await titand.registerAndLogin()

		// Install a hook that records execution count and whether titand was already running.
		const hookScript = `#!/bin/sh
set -eu

count=0
if [ -f '${runCountPath}' ]; then
	count="$(cat '${runCountPath}')"
fi
printf '%s\\n' "$((count + 1))" > '${runCountPath}'

if pgrep -f 'titand --data-directory=/home/titan/titan' >/dev/null; then
	printf 'running\\n' > '${processStatePath}'
else
	printf 'not-running\\n' > '${processStatePath}'
fi

printf 'ran\\n' > '${bootMarkerPath}'
`
		await installPreStartHook(titand, hookScript)

		// Reboot through the normal boot path so systemd runs the hook before titand.
		await rebootAndAssertTitandStarts(titand)

		// Confirm the executable hook ran once and before the main titand service.
		const hookMode = await runSudoScript(titand, `stat -c '%a' '${hookPath}'`)
		expect(hookMode.trim()).toBe('755')

		const runCount = await runSudoScript(titand, `cat '${runCountPath}'`)
		expect(runCount.trim()).toBe('1')

		const processState = await runSudoScript(titand, `cat '${processStatePath}'`)
		expect(processState.trim()).toBe('not-running')

		const bootMarker = await runSudoScript(titand, `cat '${bootMarkerPath}'`)
		expect(bootMarker.trim()).toBe('ran')
	}, 600_000)
})
