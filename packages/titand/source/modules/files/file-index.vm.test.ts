import {afterAll, beforeAll, describe, expect, test} from 'vitest'
import pRetry from 'p-retry'

import {createTestVm} from '../test-utilities/create-test-titand.js'

let titand: Awaited<ReturnType<typeof createTestVm>>

const suiteRoot = '/Home/file-index-vm'

beforeAll(async () => {
	titand = await createTestVm({device: 'titan-home'})
	await titand.vm.powerOn()
	await titand.registerAndLogin()
	await titand.client.files.createDirectory.mutate({path: suiteRoot})

	// Wait for the real initial index pass so the tests below exercise the
	// durable searchable index as well as direct mutation updates.
	await pRetry(
		() => titand.vm.sshAsRoot(`journalctl -u titan --no-pager | grep -F "Reconciled '/Home'" >/dev/null`),
		{retries: 120, factor: 1, minTimeout: 500, maxTimeout: 500},
	)
})

afterAll(async () => {
	await titand.cleanup()
})

async function upload(path: string, content = path) {
	await titand.api.post(`files/upload?path=${encodeURIComponent(path)}`, {body: content})
}

async function searchPaths(query: string) {
	return (await titand.client.files.search.query({query, maxResults: 1000})).map(({path}) => path)
}

async function expectEventually(path: string, present: boolean) {
	const name = path.split('/').at(-1)!
	await pRetry(
		async () => {
			const paths = await searchPaths(name)
			expect(paths.includes(path)).toBe(present)
		},
		{retries: 120, factor: 1, minTimeout: 250, maxTimeout: 250},
	)
}

describe('durable file index', () => {
	test('stores derived state outside watched roots and converges direct mutation hints', async () => {
		await expect(titand.vm.sshAsRoot(`test -f '${titand.vm.dataDirectory}/file-index/index.db'`)).resolves.toBe('')
		await expect(titand.vm.sshAsRoot(`test -f '${titand.vm.dataDirectory}/titan.db'`)).resolves.toBe('')
		await expect(
			titand.vm.sshAsRoot(`test ! -e '${titand.vm.dataDirectory}/file-index/index.sqlite3'`),
		).resolves.toBe('')
		await expect(titand.vm.sshAsRoot(`test ! -e '${titand.vm.dataDirectory}/photos/photos.sqlite3'`)).resolves.toBe(
			'',
		)

		const original = `${suiteRoot}/direct-original-7d53f0.txt`
		const renamed = `${suiteRoot}/direct-renamed-7d53f0.txt`
		await upload(original)
		await expectEventually(original, true)

		await expect(
			titand.client.files.rename.mutate({path: original, newName: 'direct-renamed-7d53f0.txt'}),
		).resolves.toBe(renamed)
		await expectEventually(original, false)
		await expectEventually(renamed, true)

		const trashed = await titand.client.files.trash.mutate({path: renamed})
		await expectEventually(renamed, false)
		await expect(titand.client.files.restore.mutate({path: trashed, collision: 'error'})).resolves.toBe(renamed)
		await expectEventually(renamed, true)

		const reTrashed = await titand.client.files.trash.mutate({path: renamed})
		await expect(titand.client.files.delete.mutate({path: reTrashed})).resolves.toBe(true)
		await expectEventually(renamed, false)
	})

	test('converges a burst of out-of-band creates, renames, and deletes', async () => {
		const systemRoot = `${titand.vm.dataDirectory}/home/file-index-vm/watch-burst`
		await titand.vm.sshAsRoot(`
			mkdir -p '${systemRoot}'
			for i in $(seq -w 1 500); do printf '%s' "$i" > '${systemRoot}/watch-burst-'"$i"'-91a6.txt'; done
			mv '${systemRoot}/watch-burst-500-91a6.txt' '${systemRoot}/watch-renamed-500-91a6.txt'
			rm '${systemRoot}/watch-burst-250-91a6.txt'
		`)

		await expectEventually(`${suiteRoot}/watch-burst/watch-burst-001-91a6.txt`, true)
		await expectEventually(`${suiteRoot}/watch-burst/watch-renamed-500-91a6.txt`, true)
		await expectEventually(`${suiteRoot}/watch-burst/watch-burst-250-91a6.txt`, false)
		await expectEventually(`${suiteRoot}/watch-burst/watch-burst-500-91a6.txt`, false)
	})

	test('keeps titand responsive while the worker performs repeated full-index searches', async () => {
		const systemRoot = `${titand.vm.dataDirectory}/home/file-index-vm/worker-load`
		await titand.vm.sshAsRoot(`
			mkdir -p '${systemRoot}'
			for i in $(seq -w 1 20000); do : > '${systemRoot}/worker-load-'$i'-5f31.txt'; done
		`)
		await expectEventually(`${suiteRoot}/worker-load/worker-load-20000-5f31.txt`, true)

		let searchesSettled = false
		const searches = Promise.all(
			Array.from({length: 16}, () =>
				titand.client.files.search.query({query: 'worker-load-probe-5f31', maxResults: 1}),
			),
		).finally(() => {
			searchesSettled = true
		})
		await new Promise((resolve) => setTimeout(resolve, 50))

		const probeStartedAt = Date.now()
		await expect(titand.unauthenticatedClient.user.exists.query()).resolves.toBe(true)
		expect(Date.now() - probeStartedAt).toBeLessThan(2000)
		expect(searchesSettled).toBe(false)
		await searches
	})

	test('indexes symlinks without traversing them and excludes hidden temporary files', async () => {
		const homeSystemRoot = `${titand.vm.dataDirectory}/home/file-index-vm`
		const outsideRoot = '/home/titan/file-index-vm-outside'
		await titand.vm.sshAsRoot(`
			mkdir -p '${homeSystemRoot}/symlink-target-4c82'
			printf visible > '${homeSystemRoot}/symlink-target-4c82/target-secret-4c82.txt'
			ln -s '${homeSystemRoot}/symlink-target-4c82' '${homeSystemRoot}/directory-link-4c82'
			mkdir -p '${outsideRoot}'
			printf secret > '${outsideRoot}/outside-secret-4c82.txt'
			ln -s '${outsideRoot}' '${homeSystemRoot}/outside-link-4c82'
			printf partial > '${homeSystemRoot}/.partial-4c82.txt.titan-upload'
		`)

		await expectEventually(`${suiteRoot}/directory-link-4c82`, true)
		await expectEventually(`${suiteRoot}/directory-link-4c82/target-secret-4c82.txt`, false)
		await expectEventually(`${suiteRoot}/symlink-target-4c82/target-secret-4c82.txt`, true)
		await expectEventually(`${suiteRoot}/outside-link-4c82`, false)
		expect(await searchPaths('partial-4c82')).not.toContain(`${suiteRoot}/.partial-4c82.txt.titan-upload`)
	})

	test('repairs missed events after service downtime and survives a hard power cut', async () => {
		const removedWhileStopped = `${suiteRoot}/removed-while-stopped-c193.txt`
		const createdWhileStopped = `${suiteRoot}/created-while-stopped-c193.txt`
		await upload(removedWhileStopped)
		await expectEventually(removedWhileStopped, true)

		await titand.vm.sshAsRoot('systemctl stop titan')
		await titand.vm.sshAsRoot(`
			rm '${titand.vm.dataDirectory}/home/file-index-vm/removed-while-stopped-c193.txt'
			printf offline > '${titand.vm.dataDirectory}/home/file-index-vm/created-while-stopped-c193.txt'
		`)
		await titand.vm.sshAsRoot('systemctl start titan')
		await titand.waitForStartup({waitForUser: true})
		await titand.login()
		await expectEventually(removedWhileStopped, false)
		await expectEventually(createdWhileStopped, true)

		await titand.vm.forcePowerOff()
		await titand.vm.powerOn()
		await titand.login()
		await expectEventually(createdWhileStopped, true)
	})
})
