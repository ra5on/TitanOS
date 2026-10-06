import {Buffer} from 'node:buffer'
import {expect} from 'vitest'
import pRetry from 'p-retry'

import type {createTestVm} from '../test-utilities/create-test-titand.js'

export type TestTitandVm = Awaited<ReturnType<typeof createTestVm>>

export const titanDataDirectory = '/home/titan/titan'
export const hooksDirectory = `${titanDataDirectory}/custom-hooks`
export const hookPath = `${hooksDirectory}/pre-start`
export const bootMarkerPath = '/run/titan-custom-pre-start-vm-test'
export const runCountPath = `${hooksDirectory}/pre-start-run-count`
export const processStatePath = `${hooksDirectory}/pre-start-titan-process-state`
export const errorMarkerPath = `${hooksDirectory}/pre-start-error-marker`
export const hangStatePath = `${hooksDirectory}/pre-start-hang-state`
export const hangFinishedPath = `${hooksDirectory}/pre-start-hang-finished`

const setupMarker = 'pre-start-hook-setup-complete'
const defaultSystemPassword = 'titan'
const userSystemPassword = 'moneyprintergobrrr'
const sudoPasswords = [userSystemPassword, defaultSystemPassword]
const sudoPasswordByVm = new WeakMap<TestTitandVm, string>()
const hookStatePaths = [
	bootMarkerPath,
	runCountPath,
	processStatePath,
	errorMarkerPath,
	hangStatePath,
	hangFinishedPath,
]

function isSudoAuthenticationFailure(output: string) {
	return (
		output.includes('Sorry, try again') ||
		output.includes('sudo: no password was provided') ||
		output.includes('sudo: a password is required') ||
		output.includes('incorrect password attempt')
	)
}

export async function runSudoScript(titand: TestTitandVm, script: string) {
	const encodedScript = Buffer.from(`set -eu\n${script}`).toString('base64')

	let lastOutput = ''
	return await pRetry(
		async () => {
			const cachedPassword = sudoPasswordByVm.get(titand)
			const passwords = cachedPassword
				? [cachedPassword, ...sudoPasswords.filter((password) => password !== cachedPassword)]
				: sudoPasswords

			for (const password of passwords) {
				const output = await titand.vm.ssh(
					`printf '%s\\n' '${password}' | sudo -k -S -p '' sh -c "printf '%s' '${encodedScript}' | base64 -d | sh" 2>&1`,
				)

				if (!isSudoAuthenticationFailure(output)) {
					sudoPasswordByVm.set(titand, password)
					return output
				}

				lastOutput = output
				sudoPasswordByVm.delete(titand)
			}

			throw new Error(`Could not authenticate sudo in VM:\n${lastOutput}`)
		},
		{retries: 20, minTimeout: 500, maxTimeout: 500},
	)
}

export async function installPreStartHook(titand: TestTitandVm, hookScript: string) {
	const encodedHookScript = Buffer.from(hookScript).toString('base64')
	const markerPaths = hookStatePaths.map((path) => `'${path}'`).join(' ')

	const setupOutput = await runSudoScript(
		titand,
		`
mkdir -p '${hooksDirectory}'
printf '%s' '${encodedHookScript}' | base64 -d > '${hookPath}'
chmod +x '${hookPath}'
rm -f ${markerPaths}
rm -rf /etc/systemd/system/titan-custom-pre-start.service.d
systemctl daemon-reload
test -x '${hookPath}'
# Flush the hook to disk so it survives the upcoming reboot even if the
# power cycle ends up being a hard stop instead of a clean shutdown.
sync
echo '${setupMarker}'
`,
	)

	expect(setupOutput).toContain(setupMarker)
	const hookExecutable = await titand.vm.ssh(`test -x '${hookPath}' && echo executable || echo missing`)
	expect(hookExecutable.trim()).toBe('executable')
}

export async function assertTitandStartedAfterPreStartHook(titand: TestTitandVm) {
	const titandState = await titand.vm.ssh('systemctl is-active titan || true')
	expect(titandState.trim()).toBe('active')

	const preStartResult = await titand.vm.ssh('systemctl show -p Result --value titan-custom-pre-start.service')
	expect(preStartResult.trim()).toBe('success')
}

export async function rebootAndAssertTitandStarts(titand: TestTitandVm) {
	await titand.vm.powerOff()
	await titand.vm.powerOn()
	await titand.login()

	await assertTitandStartedAfterPreStartHook(titand)
}
