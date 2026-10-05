import nodePath from 'node:path'

import {afterAll, afterEach, beforeAll, beforeEach, describe, expect, test} from 'vitest'

import {createTestVm} from '../test-utilities/create-test-titand.js'
import {
	createVmCloudFixtureDirectory,
	startVmCloudWebDav,
	waitForSync,
	VM_CLOUD_WEBDAV_PASSWORD,
	VM_CLOUD_WEBDAV_URL,
	VM_CLOUD_WEBDAV_USERNAME,
	writeVmCloudFixture,
} from './cloud.vm-test-helpers.js'

const ownerPassword = 'moneyprintergobrrr'
const memberPassword = 'member-password'
const ownerDestination = '/Home/Cloud/Owner'

describe.sequential('Cloud account isolation', () => {
	let titand: Awaited<ReturnType<typeof createTestVm>>
	let failed = false
	let ownerAccountId: string
	let ownerSyncId: string
	let memberAccountId: string
	let memberSyncId: string
	let memberUserId: string
	let memberDestination: string

	const loginAs = async (userId: string, password: string) => {
		const token = await titand.client.user.login.mutate({userId, password})
		titand.setAuthToken(token)
	}

	const connectWebDav = () =>
		titand.client.files.cloud.connectWebDav.mutate({
			flavor: 'webdav',
			url: VM_CLOUD_WEBDAV_URL,
			username: VM_CLOUD_WEBDAV_USERNAME,
			password: VM_CLOUD_WEBDAV_PASSWORD,
			tlsMode: 'default',
		})

	const upload = (path: string, body: string) =>
		titand.api.post(`files/upload?path=${encodeURIComponent(path)}`, {body})

	const rejectionMessage = async (operation: () => Promise<unknown>) => {
		let rejection: unknown
		try {
			await operation()
		} catch (error) {
			rejection = error
		}
		expect(rejection).toBeInstanceOf(Error)
		return (rejection as Error).message
	}

	const expectIndistinguishablePrivateRejection = async (
		cloudOperation: () => Promise<unknown>,
		nonCloudOperation: () => Promise<unknown>,
	) => {
		const cloudMessage = await rejectionMessage(cloudOperation)
		const nonCloudMessage = await rejectionMessage(nonCloudOperation)
		expect(cloudMessage).not.toContain('[cloud-read-only]')
		// Authorization errors echo the path the caller supplied, so compare
		// their externally meaningful response code rather than those inputs.
		expect(cloudMessage.match(/^\[[^\]]+\]/)?.[0]).toBe(nonCloudMessage.match(/^\[[^\]]+\]/)?.[0])
	}

	const expectOtherDestinationPrivate = async ({
		destination,
		nonCloudDestination,
		importedFile,
		ownSource,
	}: {
		destination: string
		nonCloudDestination: string
		importedFile: string
		ownSource: string
	}) => {
		const nonCloudFile = `${nonCloudDestination}/private.txt`
		const ownDirectory = nodePath.posix.dirname(ownSource)
		const identity = {device: 0, inode: 0, birthtimeMs: 0}
		const expectPrivatePair = (cloudOperation: () => Promise<unknown>, nonCloudOperation: () => Promise<unknown>) =>
			expectIndistinguishablePrivateRejection(cloudOperation, nonCloudOperation)

		await expectPrivatePair(
			() => titand.client.files.createDirectory.mutate({path: `${destination}/new-directory`}),
			() => titand.client.files.createDirectory.mutate({path: `${nonCloudDestination}/new-directory`}),
		)
		await expectPrivatePair(
			() => titand.client.files.cleanupCreatedDirectory.mutate({path: destination, identity}),
			() => titand.client.files.cleanupCreatedDirectory.mutate({path: nonCloudDestination, identity}),
		)
		await expectPrivatePair(
			() => titand.client.files.copy.mutate({path: ownSource, toDirectory: destination}),
			() => titand.client.files.copy.mutate({path: ownSource, toDirectory: nonCloudDestination}),
		)
		await expectPrivatePair(
			() => titand.client.files.move.mutate({path: ownSource, toDirectory: destination}),
			() => titand.client.files.move.mutate({path: ownSource, toDirectory: nonCloudDestination}),
		)
		await expectPrivatePair(
			() => titand.client.files.copy.mutate({path: importedFile, toDirectory: ownDirectory}),
			() => titand.client.files.copy.mutate({path: nonCloudFile, toDirectory: ownDirectory}),
		)
		await expectPrivatePair(
			() => titand.client.files.move.mutate({path: importedFile, toDirectory: ownDirectory}),
			() => titand.client.files.move.mutate({path: nonCloudFile, toDirectory: ownDirectory}),
		)
		await expectPrivatePair(
			() => titand.client.files.rename.mutate({path: importedFile, newName: 'renamed.txt'}),
			() => titand.client.files.rename.mutate({path: nonCloudFile, newName: 'renamed.txt'}),
		)
		await expectPrivatePair(
			() => titand.client.files.trash.mutate({path: importedFile}),
			() => titand.client.files.trash.mutate({path: nonCloudFile}),
		)
		await expectPrivatePair(
			() => titand.client.files.delete.mutate({path: importedFile}),
			() => titand.client.files.delete.mutate({path: nonCloudFile}),
		)
		await expectPrivatePair(
			() => titand.client.files.deleteMany.mutate({paths: [importedFile]}),
			() => titand.client.files.deleteMany.mutate({paths: [nonCloudFile]}),
		)
		await expectPrivatePair(
			() => titand.client.files.archive.mutate({paths: [importedFile]}),
			() => titand.client.files.archive.mutate({paths: [nonCloudFile]}),
		)
		await expectPrivatePair(
			() => titand.client.files.unarchive.mutate({path: importedFile}),
			() => titand.client.files.unarchive.mutate({path: nonCloudFile}),
		)
		await expectPrivatePair(
			() => titand.client.files.pathOperations.query({path: destination}),
			() => titand.client.files.pathOperations.query({path: nonCloudDestination}),
		)

		const cloudUploadError = await upload(`${destination}/uploaded.txt`, 'blocked').catch((error) => error)
		const nonCloudUploadError = await upload(`${nonCloudDestination}/uploaded.txt`, 'blocked').catch((error) => error)
		expect(cloudUploadError.response.statusCode).toBe(nonCloudUploadError.response.statusCode)
		expect(cloudUploadError.response.body).toEqual(nonCloudUploadError.response.body)
		expect(cloudUploadError.response.body).toMatchObject({error: 'invalid path'})
	}

	beforeAll(async () => {
		titand = await createTestVm({device: 'titan-home'})
		await titand.vm.powerOn()
		await titand.registerAndLogin()
		await createVmCloudFixtureDirectory(titand, '/owner')
		await createVmCloudFixtureDirectory(titand, '/member')
		await writeVmCloudFixture(titand, '/owner/owner.txt', 'owner cloud data')
		await writeVmCloudFixture(titand, '/member/member.txt', 'member cloud data')
		await startVmCloudWebDav(titand)
	})

	afterAll(async () => await titand?.cleanup())

	afterEach(({task}) => {
		if (task.result?.state === 'fail') failed = true
	})

	beforeEach(({skip}) => {
		if (failed) skip()
	})

	test('creates independent Cloud accounts and imports for the owner and a member', async () => {
		const ownerAccount = await connectWebDav()
		ownerAccountId = ownerAccount.account.id
		await titand.client.files.createDirectory.mutate({path: '/Home/Cloud'})
		await titand.client.files.createDirectory.mutate({path: ownerDestination})
		const ownerSync = await titand.client.files.cloud.create.mutate({
			accountId: ownerAccountId,
			remote: {path: '/owner'},
			destination: {path: ownerDestination},
			mode: 'auto',
		})
		ownerSyncId = ownerSync.id
		await waitForSync(titand.client, ownerSyncId, ({status}) => status.state === 'idle')

		const member = await titand.client.user.createUser.mutate({name: 'Alice', password: memberPassword})
		memberUserId = member.userId
		memberDestination = `/Users/${memberUserId}/Cloud`
		await loginAs(memberUserId, memberPassword)

		const memberAccount = await connectWebDav()
		memberAccountId = memberAccount.account.id
		await titand.client.files.createDirectory.mutate({path: memberDestination})
		const memberSync = await titand.client.files.cloud.create.mutate({
			accountId: memberAccountId,
			remote: {path: '/member'},
			destination: {path: memberDestination},
			mode: 'auto',
		})
		memberSyncId = memberSync.id
		await waitForSync(titand.client, memberSyncId, ({status}) => status.state === 'idle')

		expect((await titand.client.files.cloud.accounts.query()).map(({id}) => id)).toEqual([memberAccountId])
		expect((await titand.client.files.cloud.syncs.query()).map(({id}) => id)).toEqual([memberSyncId])
		expect((await titand.client.files.list.query({path: memberDestination})).files.map(({name}) => name)).toEqual([
			'member.txt',
		])
		await expect(titand.client.files.cloud.pause.mutate({syncId: ownerSyncId})).rejects.toThrow('[cloud-not-found]')
		await expect(titand.client.files.cloud.locations.query({accountId: ownerAccountId})).rejects.toThrow(
			'[cloud-account-not-found]',
		)
	})

	test("does not reveal the owner's Cloud destinations through member Files mutations", async () => {
		const memberSource = `/Users/${memberUserId}/outside.txt`
		await upload(memberSource, 'member-owned source')
		await expectOtherDestinationPrivate({
			destination: ownerDestination,
			nonCloudDestination: '/Home/Private',
			importedFile: `${ownerDestination}/owner.txt`,
			ownSource: memberSource,
		})
		expect(
			(await titand.client.files.list.query({path: `/Users/${memberUserId}`})).files.map(({name}) => name),
		).toEqual(expect.arrayContaining(['Cloud', 'outside.txt']))
	})

	test("does not reveal the member's Cloud destinations through owner Files mutations", async () => {
		await loginAs('0', ownerPassword)
		expect((await titand.client.files.cloud.accounts.query()).map(({id}) => id)).toEqual([ownerAccountId])
		expect((await titand.client.files.cloud.syncs.query()).map(({id}) => id)).toEqual([ownerSyncId])
		await expect(titand.client.files.cloud.pause.mutate({syncId: memberSyncId})).rejects.toThrow('[cloud-not-found]')

		await titand.client.files.createDirectory.mutate({path: '/Home/Outside'})
		const ownerSource = '/Home/Outside/owner-source.txt'
		await upload(ownerSource, 'owner source')
		await expectOtherDestinationPrivate({
			destination: memberDestination,
			nonCloudDestination: `/Users/${memberUserId}/Private`,
			importedFile: `${memberDestination}/member.txt`,
			ownSource: ownerSource,
		})
		expect((await titand.client.files.list.query({path: ownerDestination})).files.map(({name}) => name)).toEqual([
			'owner.txt',
		])
	})

	test('each account can remove only its own Cloud state', async () => {
		await loginAs(memberUserId, memberPassword)
		await titand.client.files.cloud.removeAccount.mutate({
			accountId: memberAccountId,
			confirmedSyncIds: [memberSyncId],
		})
		expect(await titand.client.files.cloud.accounts.query()).toEqual([])
		await expect(
			titand.client.files.createDirectory.mutate({path: `${memberDestination}/editable-after-removal`}),
		).resolves.toMatchObject({created: true})

		await loginAs('0', ownerPassword)
		expect((await titand.client.files.cloud.accounts.query()).map(({id}) => id)).toEqual([ownerAccountId])
		await titand.client.files.cloud.removeAccount.mutate({
			accountId: ownerAccountId,
			confirmedSyncIds: [ownerSyncId],
		})
		await titand.client.user.deleteUser.mutate({userId: memberUserId})
		expect(await titand.client.files.cloud.accounts.query()).toEqual([])
		expect(await titand.client.files.cloud.syncs.query()).toEqual([])
	})
})
