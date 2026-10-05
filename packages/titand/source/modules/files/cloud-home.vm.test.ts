import {afterAll, afterEach, beforeAll, beforeEach, describe, expect, test} from 'vitest'
import pRetry from 'p-retry'

import {createTestVm} from '../test-utilities/create-test-titand.js'
import {CLOUD_HOME_FREE_SPACE_FLOOR} from './cloud.js'
import type {CloudSyncActivity} from './cloud-types.js'
import {
	createLargeVmCloudFixture,
	createVmCloudFixtureDirectory,
	readVmCloudFixture,
	removeVmCloudFixture,
	startVmCloudWebDav,
	stopVmCloudWebDav,
	waitForSync,
	waitForSyncRemoval,
	VM_CLOUD_WEBDAV_MUTATION_LOG,
	VM_CLOUD_WEBDAV_PASSWORD,
	VM_CLOUD_WEBDAV_TLS_URL,
	VM_CLOUD_WEBDAV_URL,
	VM_CLOUD_WEBDAV_USERNAME,
	writeVmCloudFixture,
} from './cloud.vm-test-helpers.js'

const destination = '/Home/Cloud/Main'
const destinationSystemPath = '/home/titan/titan/home/Cloud/Main'

describe.sequential('Cloud home lifecycle', () => {
	let titand: Awaited<ReturnType<typeof createTestVm>>
	let failed = false
	let capacityMounted = false
	let accountId: string
	let syncId: string
	let lastSuccessfulAt = 0

	const createDirectory = (path: string) => titand.client.files.createDirectory.mutate({path})
	const upload = (path: string, body: string) =>
		titand.api.post(`files/upload?path=${encodeURIComponent(path)}`, {body})
	const read = async (path: string) =>
		(
			await titand.api.get(`files/view?path=${encodeURIComponent(path)}`, {
				responseType: 'text',
			})
		).body
	const listNames = async (path: string) =>
		(await titand.client.files.list.query({path})).files.map(({name}) => name).sort()

	const expectCloudReadOnly = async (operation: Promise<unknown>) => {
		await expect(operation).rejects.toThrow('[cloud-read-only]')
	}

	beforeAll(async () => {
		titand = await createTestVm({device: 'titan-home'})
		await titand.vm.powerOn()
		await titand.registerAndLogin()

		await createVmCloudFixtureDirectory(titand, '/source/empty')
		await createVmCloudFixtureDirectory(titand, '/source/nested')
		await writeVmCloudFixture(titand, '/source/archive.zip', 'not a real archive')
		await writeVmCloudFixture(titand, '/source/hello.txt', 'hello v1')
		await writeVmCloudFixture(titand, '/source/nested/remove-me.txt', 'remove me')
		await writeVmCloudFixture(titand, '/source/.DS_Store', 'remote junk')
		await startVmCloudWebDav(titand, {tls: true})
	})

	afterAll(async () => {
		if (capacityMounted) {
			await titand?.vm.sshAsRoot("umount '/home/titan/titan/home/Cloud/Capacity'").catch(() => {})
		}
		await titand?.cleanup()
	})

	afterEach(({task}) => {
		if (task.result?.state === 'fail') failed = true
	})

	beforeEach(({skip}) => {
		if (failed) skip()
	})

	test('rejects Cloud routes without authentication', async () => {
		const calls = [
			titand.unauthenticatedClient.files.cloud.accounts.query(),
			titand.unauthenticatedClient.files.cloud.syncs.query(),
			titand.unauthenticatedClient.files.cloud.connectWebDav.mutate({
				flavor: 'webdav',
				url: VM_CLOUD_WEBDAV_URL,
				username: VM_CLOUD_WEBDAV_USERNAME,
				password: VM_CLOUD_WEBDAV_PASSWORD,
				tlsMode: 'default',
			}),
			titand.unauthenticatedClient.files.cloud.create.mutate({
				accountId: '11111111-1111-4111-8111-111111111111',
				remote: {path: '/source'},
				destination: {path: '/Home/Cloud/Unauthenticated'},
				mode: 'auto',
			}),
		]
		for (const call of calls) await expect(call).rejects.toThrow('Invalid token')
	})

	test('requires explicit opt-in for a self-signed WebDAV certificate', async () => {
		const connect = (tlsMode: 'default' | 'insecure') =>
			titand.client.files.cloud.connectWebDav.mutate({
				flavor: 'webdav',
				url: VM_CLOUD_WEBDAV_TLS_URL,
				username: VM_CLOUD_WEBDAV_USERNAME,
				password: VM_CLOUD_WEBDAV_PASSWORD,
				tlsMode,
			})

		await expect(connect('default')).rejects.toThrow('[cloud-webdav-untrusted-certificate]')
		const {account} = await connect('insecure')
		await expect(
			titand.client.files.cloud.browse.query({accountId: account.id, remote: {path: '/source'}}),
		).resolves.toMatchObject({
			entries: expect.arrayContaining([{name: 'hello.txt', path: 'hello.txt', type: 'file'}]),
		})
		await titand.client.files.cloud.removeAccount.mutate({accountId: account.id, confirmedSyncIds: []})

		await stopVmCloudWebDav(titand)
		await startVmCloudWebDav(titand)
	})

	test('connects through the public API and imports a real read-only WebDAV tree', async () => {
		const providers = await titand.client.files.cloud.providers.query()
		expect(providers.map(({id}) => id)).toContain('webdav')

		const connected = await titand.client.files.cloud.connectWebDav.mutate({
			flavor: 'webdav',
			url: VM_CLOUD_WEBDAV_URL,
			username: VM_CLOUD_WEBDAV_USERNAME,
			password: VM_CLOUD_WEBDAV_PASSWORD,
			tlsMode: 'default',
		})
		accountId = connected.account.id
		expect(JSON.stringify(await titand.client.files.cloud.accounts.query())).not.toContain(VM_CLOUD_WEBDAV_PASSWORD)

		const sourceListing = await titand.client.files.cloud.browse.query({
			accountId,
			remote: {path: '/source'},
		})
		expect(sourceListing.entries).toEqual(
			expect.arrayContaining([
				{name: 'archive.zip', path: 'archive.zip', type: 'file'},
				{name: 'empty', path: 'empty', type: 'directory'},
				{name: 'nested', path: 'nested', type: 'directory'},
				{name: 'hello.txt', path: 'hello.txt', type: 'file'},
			]),
		)
		expect(sourceListing.entries.map(({name}) => name)).not.toContain('.DS_Store')

		await createDirectory('/Home/Cloud')
		await createDirectory(destination)
		const created = await titand.client.files.cloud.create.mutate({
			accountId,
			remote: {path: '/source'},
			destination: {path: destination},
			mode: 'auto',
		})
		syncId = created.id
		const completed = await waitForSync(
			titand.client,
			syncId,
			(cloud) => cloud.lastSuccessfulAt !== undefined && cloud.status.state === 'idle',
			{timeout: 120_000},
		)
		lastSuccessfulAt = completed.lastSuccessfulAt!

		expect(await listNames(destination)).toEqual(['archive.zip', 'empty', 'hello.txt', 'nested'])
		expect(await listNames(`${destination}/empty`)).toEqual([])
		expect(await read(`${destination}/hello.txt`)).toBe('hello v1')
		expect(await readVmCloudFixture(titand, '/source/hello.txt')).toBe('hello v1')
		expect(await readVmCloudFixture(titand, '/source/.DS_Store')).toBe('remote junk')
	})

	test('rejects nonempty, unwritable, and overlapping destinations while tolerating OS junk', async () => {
		const create = (path: string) =>
			titand.client.files.cloud.create.mutate({
				accountId,
				remote: {path: '/source/empty'},
				destination: {path},
				mode: 'auto',
			})

		await createDirectory('/Home/Cloud/Nonempty')
		await upload('/Home/Cloud/Nonempty/existing.txt', 'existing')
		await expect(create('/Home/Cloud/Nonempty')).rejects.toThrow('[cloud-destination-not-empty]')

		// A destination containing only ignored OS junk counts as empty, and the
		// junk survives the first sync of the empty remote
		const junkPath = '/Home/Cloud/Junk Only'
		const junkSystemPath = '/home/titan/titan/home/Cloud/Junk Only'
		const junkNames = [
			'.DS_Store',
			'.directory',
			'.titan-watcher-health-check',
			'._metadata',
			'Thumbs.db',
			'desktop.ini',
		]
		await createDirectory(junkPath)
		await titand.vm.ssh(
			`cd '${junkSystemPath}' && for name in ${junkNames.join(' ')}; do printf '%s' 'OS metadata' > "$name"; done`,
		)
		const junkSync = await create(junkPath)
		await waitForSync(titand.client, junkSync.id, ({status}) => status.state === 'idle')
		for (const name of junkNames) {
			expect(await titand.vm.ssh(`cat '${junkSystemPath}/${name}'`)).toBe('OS metadata')
		}
		await titand.client.files.cloud.remove.mutate({syncId: junkSync.id})

		await createDirectory('/Home/Cloud/Unwritable')
		await titand.vm.sshAsRoot(
			"chown root:root '/home/titan/titan/home/Cloud/Unwritable' && chmod 700 '/home/titan/titan/home/Cloud/Unwritable'",
		)
		await expect(create('/Home/Cloud/Unwritable')).rejects.toThrow('[cloud-destination-not-writable]')
		await titand.vm.sshAsRoot("chown titan:titan '/home/titan/titan/home/Cloud/Unwritable'")

		// An empty folder inside an existing destination passes the emptiness
		// check and must still be rejected as an overlap
		await titand.vm.ssh(`mkdir '${destinationSystemPath}/overlap-child'`)
		await expect(create(`${destination}/overlap-child`)).rejects.toThrow('[cloud-destination-overlap]')
		await titand.vm.ssh(`rmdir '${destinationSystemPath}/overlap-child'`)
	})

	test('converges remote changes without deleting local data when the provider is offline', async () => {
		await writeVmCloudFixture(titand, '/source/hello.txt', 'hello v2 updated')
		await removeVmCloudFixture(titand, '/source/nested/remove-me.txt')
		await titand.vm.ssh(`
printf '%s' 'local extra' > '${destinationSystemPath}/local-extra.txt'
printf '%s' 'local junk' > '${destinationSystemPath}/.DS_Store'
`)

		await titand.client.files.cloud.run.mutate({syncId})
		const converged = await waitForSync(
			titand.client,
			syncId,
			(cloud) => (cloud.lastSuccessfulAt ?? 0) > lastSuccessfulAt && cloud.status.state === 'idle',
			{timeout: 120_000},
		)
		lastSuccessfulAt = converged.lastSuccessfulAt!
		expect(await read(`${destination}/hello.txt`)).toBe('hello v2 updated')
		await titand.vm.sshAsRoot(`test ! -e '${destinationSystemPath}/local-extra.txt'`)
		await titand.vm.sshAsRoot(`test ! -e '${destinationSystemPath}/nested/remove-me.txt'`)
		expect(await titand.vm.ssh(`cat '${destinationSystemPath}/.DS_Store'`)).toBe('local junk')

		await titand.vm.ssh(`printf '%s' 'retain during outage' > '${destinationSystemPath}/retain.txt'`)
		await stopVmCloudWebDav(titand)
		await titand.client.files.cloud.run.mutate({syncId})
		await waitForSync(
			titand.client,
			syncId,
			({status}) => status.state === 'needs-attention' && status.attention?.kind === 'error',
			{timeout: 120_000},
		)
		expect(await titand.vm.ssh(`cat '${destinationSystemPath}/retain.txt'`)).toBe('retain during outage')

		await startVmCloudWebDav(titand)
		await titand.client.files.cloud.run.mutate({syncId})
		const recovered = await waitForSync(
			titand.client,
			syncId,
			(cloud) => (cloud.lastSuccessfulAt ?? 0) > lastSuccessfulAt && cloud.status.state === 'idle',
			{timeout: 120_000},
		)
		lastSuccessfulAt = recovered.lastSuccessfulAt!
		await titand.vm.sshAsRoot(`test ! -e '${destinationSystemPath}/retain.txt'`)
		expect(await readVmCloudFixture(titand, '/source/hello.txt')).toBe('hello v2 updated')
	})

	test('makes the mirror read-only through Files while allowing copy-out and metadata', async () => {
		await createDirectory('/Home/Outside')
		await upload('/Home/Outside/outside.txt', 'outside')
		const listing = await titand.client.files.list.query({path: destination})
		expect(listing.operations).toEqual(expect.arrayContaining(['copy', 'favorite', 'share']))
		expect(listing.operations).not.toEqual(expect.arrayContaining(['writable', 'move', 'rename', 'trash', 'delete']))

		await expect(createDirectory('/Home/Cloud')).resolves.toEqual({created: false})
		await expect(createDirectory(destination)).resolves.toEqual({created: false})
		await expect(createDirectory(`${destination}/nested`)).resolves.toEqual({created: false})
		await expectCloudReadOnly(createDirectory(`${destination}/new-directory`))
		await expectCloudReadOnly(createDirectory(`${destination}/hello.txt`))
		await expectCloudReadOnly(
			titand.client.files.rename.mutate({path: `${destination}/hello.txt`, newName: 'renamed.txt'}),
		)
		await expectCloudReadOnly(titand.client.files.trash.mutate({path: '/Home/Cloud'}))
		await expectCloudReadOnly(titand.client.files.delete.mutate({path: `${destination}/hello.txt`}))
		await expectCloudReadOnly(
			titand.client.files.copy.mutate({path: '/Home/Outside/outside.txt', toDirectory: destination}),
		)
		await expectCloudReadOnly(
			titand.client.files.move.mutate({path: '/Home/Outside/outside.txt', toDirectory: destination}),
		)
		await expectCloudReadOnly(
			titand.client.files.move.mutate({path: `${destination}/hello.txt`, toDirectory: '/Home/Outside'}),
		)
		await expectCloudReadOnly(titand.client.files.archive.mutate({paths: [`${destination}/hello.txt`]}))
		await expectCloudReadOnly(titand.client.files.unarchive.mutate({path: `${destination}/archive.zip`}))

		const uploadError = await upload(`${destination}/uploaded.txt`, 'blocked').catch((error) => error)
		expect(uploadError.response.statusCode).toBe(400)
		expect(uploadError.response.body).toMatchObject({error: '[cloud-read-only]'})

		await expect(
			titand.client.files.copy.mutate({path: `${destination}/hello.txt`, toDirectory: '/Home/Outside'}),
		).resolves.toBe('/Home/Outside/hello.txt')
		expect(await read('/Home/Outside/hello.txt')).toBe('hello v2 updated')
		await expect(titand.client.files.addFavorite.mutate({path: destination})).resolves.toBe(true)
		await expect(titand.client.files.favorites.query()).resolves.toContain(destination)
		expect(await upload('/Home/Outside/still-writable.txt', 'outside remains writable')).toBeDefined()
	})

	test('blocks natural cleanup and Trash restore scenarios while a folder is a Cloud destination', async () => {
		await createVmCloudFixtureDirectory(titand, '/empty')
		const guardedPath = '/Home/Cloud/Guarded'
		const creation = await createDirectory(guardedPath)
		expect(creation.created).toBe(true)
		if (!creation.created) return

		await upload(`${guardedPath}/restore-me.txt`, 'restore me')
		const trashPath = await titand.client.files.trash.mutate({path: `${guardedPath}/restore-me.txt`})
		const guardedSync = await titand.client.files.cloud.create.mutate({
			accountId,
			remote: {path: '/empty'},
			destination: {path: guardedPath},
			mode: 'auto',
		})
		await waitForSync(titand.client, guardedSync.id, ({status}) => status.state === 'idle')

		await expectCloudReadOnly(
			titand.client.files.cleanupCreatedDirectory.mutate({path: guardedPath, identity: creation.identity}),
		)
		await expectCloudReadOnly(titand.client.files.restore.mutate({path: trashPath}))

		await titand.client.files.cloud.remove.mutate({syncId: guardedSync.id})
		await expect(titand.client.files.restore.mutate({path: trashPath})).resolves.toBe(`${guardedPath}/restore-me.txt`)
	})

	test('persists an explicit pause across an Titand service restart', async () => {
		await writeVmCloudFixture(titand, '/source/paused.txt', 'available after resume')
		await titand.client.files.cloud.pause.mutate({syncId})
		await titand.vm.sshAsRoot('systemctl restart titan')

		const paused = await waitForSync(titand.client, syncId, ({status}) => status.state === 'paused')
		expect(paused.pauseReasons).toEqual({user: true})
		await expect(read(`${destination}/paused.txt`)).rejects.toThrow()

		await titand.client.files.cloud.resume.mutate({syncId})
		await pRetry(async () => expect(await read(`${destination}/paused.txt`)).toBe('available after resume'), {
			retries: 240,
			factor: 1,
			minTimeout: 250,
			maxTimeout: 250,
		})
		const completed = await waitForSync(titand.client, syncId, ({status}) => status.state === 'idle')
		lastSuccessfulAt = completed.lastSuccessfulAt ?? lastSuccessfulAt
	})

	test('removes orphaned account directories at startup while retaining live accounts', async () => {
		const accountsRoot = '/home/titan/titan/cloud/accounts'
		const orphanDirectory = `${accountsRoot}/33333333-3333-4333-8333-333333333333`
		await titand.vm.sshAsRoot(`
set -eu
mkdir -p '${orphanDirectory}'
printf '[cloud]\\ntype = webdav\\n' > '${orphanDirectory}/rclone.conf'
chown -R titan:titan '${orphanDirectory}'
`)
		await titand.vm.sshAsRoot('systemctl restart titan')
		await pRetry(() => titand.vm.sshAsRoot(`test ! -e '${orphanDirectory}'`), {
			retries: 240,
			factor: 1,
			minTimeout: 250,
			maxTimeout: 250,
		})
		await pRetry(
			async () =>
				expect(
					await titand.client.files.cloud.browse.query({
						accountId,
						remote: {path: '/source'},
					}),
				).toMatchObject({
					entries: expect.arrayContaining([{name: 'hello.txt', path: 'hello.txt', type: 'file'}]),
				}),
			{
				retries: 240,
				factor: 1,
				minTimeout: 250,
				maxTimeout: 250,
			},
		)
	})

	test('interrupts a real transfer on shutdown and resumes cleanly after boot', async () => {
		await createLargeVmCloudFixture(titand, '/source/interrupted.bin', 4)
		await startVmCloudWebDav(titand, {stall: {path: '/source/interrupted.bin', afterBytes: 128 * 1024}})
		const progress = titand.subscribeToEvents<CloudSyncActivity[]>('files:cloud-progress')
		await progress.started

		await titand.client.files.cloud.run.mutate({syncId})
		await waitForSync(titand.client, syncId, ({status}) => status.state === 'running')
		await pRetry(
			() =>
				titand.vm.sshAsRoot(
					`find '${destinationSystemPath}' -maxdepth 1 -type f -name 'interrupted.bin.*.partial' -size +0c -print -quit | grep -q .`,
				),
			{retries: 120, factor: 1, minTimeout: 250, maxTimeout: 250},
		)
		await pRetry(
			async () => {
				const snapshots = progress.collected.flat()
				if (!snapshots.some((activity) => activity.syncId === syncId && activity.transferredBytes > 0)) {
					throw new Error('[cloud-progress-not-observed]')
				}
			},
			{retries: 120, factor: 1, minTimeout: 100, maxTimeout: 100},
		)
		expect(JSON.stringify(progress.collected)).not.toContain('/source')
		progress.unsubscribe()

		await titand.vm.powerOff()
		await titand.vm.powerOn()
		await startVmCloudWebDav(titand)
		await titand.client.files.cloud.run.mutate({syncId})
		await pRetry(
			() => titand.vm.sshAsRoot(`test "$(stat -c %s '${destinationSystemPath}/interrupted.bin')" -eq 4194304`),
			{retries: 360, factor: 1, minTimeout: 250, maxTimeout: 250},
		)
		await waitForSync(titand.client, syncId, ({status}) => status.state === 'idle', {timeout: 120_000})
		await titand.vm.sshAsRoot(
			`test -z "$(find '${destinationSystemPath}' -maxdepth 1 -type f -name 'interrupted.bin.*.partial' -print -quit)"`,
		)
		await removeVmCloudFixture(titand, '/source/interrupted.bin')
	})

	test('stops before the home free-space reserve and succeeds after capacity returns', async () => {
		await createVmCloudFixtureDirectory(titand, '/capacity')
		await createLargeVmCloudFixture(titand, '/capacity/blob.bin', 2)
		const capacityPath = '/Home/Cloud/Capacity'
		const capacitySystemPath = '/home/titan/titan/home/Cloud/Capacity'
		await createDirectory(capacityPath)
		const filesystemSize = CLOUD_HOME_FREE_SPACE_FLOOR + 512n * 1024n
		await titand.vm.sshAsRoot(`mount -t tmpfs -o size=${filesystemSize} cloud-capacity '${capacitySystemPath}'`)
		capacityMounted = true

		const capacitySync = await titand.client.files.cloud.create.mutate({
			accountId,
			remote: {path: '/capacity'},
			destination: {path: capacityPath},
			mode: 'auto',
		})
		await waitForSync(
			titand.client,
			capacitySync.id,
			({status}) => status.state === 'needs-attention' && status.attention?.kind === 'error',
			{timeout: 120_000},
		)
		await titand.vm.sshAsRoot(`test ! -e '${capacitySystemPath}/blob.bin'`)

		await titand.vm.sshAsRoot(`umount '${capacitySystemPath}'`)
		capacityMounted = false
		await titand.client.files.cloud.run.mutate({syncId: capacitySync.id})
		await pRetry(() => titand.vm.sshAsRoot(`test "$(stat -c %s '${capacitySystemPath}/blob.bin')" -eq 2097152`), {
			retries: 240,
			factor: 1,
			minTimeout: 250,
			maxTimeout: 250,
		})
		await waitForSync(titand.client, capacitySync.id, ({status}) => status.state === 'idle')
		await titand.client.files.cloud.remove.mutate({syncId: capacitySync.id})
	})

	test('releases one-time imports and retained account data back to normal Files behavior', async () => {
		await createVmCloudFixtureDirectory(titand, '/one-time')
		await writeVmCloudFixture(titand, '/one-time/complete.txt', 'complete')
		const oneTimePath = '/Home/Cloud/One Time'
		await createDirectory(oneTimePath)
		const oneTime = await titand.client.files.cloud.create.mutate({
			accountId,
			remote: {path: '/one-time'},
			destination: {path: oneTimePath},
			mode: 'one-time',
		})
		await waitForSyncRemoval(titand.client, oneTime.id, {timeout: 120_000})
		expect(await read(`${oneTimePath}/complete.txt`)).toBe('complete')
		await expect(createDirectory(`${oneTimePath}/editable`)).resolves.toMatchObject({created: true})

		await expect(titand.client.files.cloud.removeAccount.mutate({accountId, confirmedSyncIds: []})).rejects.toThrow(
			'[cloud-account-removal-confirmation-mismatch]',
		)
		await titand.client.files.cloud.removeAccount.mutate({accountId, confirmedSyncIds: [syncId]})
		expect(await titand.client.files.cloud.accounts.query()).toEqual([])
		expect(await titand.client.files.cloud.syncs.query()).toEqual([])
		expect(await read(`${destination}/hello.txt`)).toBe('hello v2 updated')
		await expect(createDirectory(`${destination}/editable-after-removal`)).resolves.toMatchObject({created: true})
		expect(await readVmCloudFixture(titand, '/source/hello.txt')).toBe('hello v2 updated')
	})

	test('never issued a mutating request to the WebDAV source across the suite', async () => {
		await titand.vm.sshAsRoot(`test ! -s '${VM_CLOUD_WEBDAV_MUTATION_LOG}'`)
	})
})
