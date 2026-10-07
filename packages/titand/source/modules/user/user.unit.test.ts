import nodePath from 'node:path'

import fse from 'fs-extra'
import {afterEach, beforeEach, describe, expect, test, vi} from 'vitest'

import Titand from '../../index.js'
import temporaryDirectory from '../utilities/temporary-directory.js'
import * as totp from '../utilities/totp.js'

const accountAvatarDirectory = (titand: Titand, userId: string) =>
	nodePath.join(titand.dataDirectory, 'avatars', userId)
const accountAvatarPath = (titand: Titand, userId: string, hash: string) =>
	nodePath.join(accountAvatarDirectory(titand, userId), `${hash}.webp`)

describe('member lifecycle', () => {
	let directory: ReturnType<typeof temporaryDirectory>
	let dataDirectory: string
	let titand: Titand

	beforeEach(async () => {
		directory = temporaryDirectory()
		await directory.createRoot()
		dataDirectory = await directory.create()
		titand = new Titand({dataDirectory})
		await titand.store.set('user', {name: 'Owner', hashedPassword: 'unused'})
		vi.spyOn(titand.files, 'createMemberDirectories').mockResolvedValue()
		vi.spyOn(titand.files, 'deleteMemberDirectories').mockResolvedValue()
		vi.spyOn(titand.files.cloud, 'removeUser').mockResolvedValue()
		vi.spyOn(titand.files.samba, 'removeUser').mockResolvedValue(false)
		vi.spyOn(titand.files.memberShares, 'removeUserFromShares').mockResolvedValue()
		vi.spyOn(titand.apps, 'removeUserFromMemberShares').mockResolvedValue()
		vi.spyOn(titand.auth, 'revokeAllForAccount').mockResolvedValue(0)
		vi.spyOn(titand.photos, 'deleteAccount').mockResolvedValue()
		vi.spyOn(titand.hardware.raid, 'hasConfigStore').mockResolvedValue(false)
	})

	afterEach(async () => {
		vi.restoreAllMocks()
		await titand.auth.stop()
		await directory.destroyRoot()
	})

	test('persists desktop transparency independently for owner and members across reloads', async () => {
		const member = await titand.user.createUser('Appearance member', 'passwordpassword')
		expect((await titand.user.get())?.desktopTransparency).toBeUndefined()
		expect((await titand.user.getMember(member.userId))?.desktopTransparency).toBeUndefined()
		await titand.user.setAccountDesktopTransparency('0', 0)
		await titand.user.setAccountDesktopTransparency(member.userId, 100)
		const reloaded = new Titand({dataDirectory})
		expect((await reloaded.user.get())?.desktopTransparency).toBe(0)
		expect((await reloaded.user.getMember(member.userId))?.desktopTransparency).toBe(100)
		await titand.user.setAccountDesktopTransparency(member.userId, 75)
		expect((await titand.user.get())?.desktopTransparency).toBe(0)
		expect((await titand.user.getMember(member.userId))?.desktopTransparency).toBe(75)
	})

	test('permanently reserves a deleted member id and retries interrupted cleanup', async () => {
		const first = await titand.user.createUser('Alice', 'passwordpassword')
		expect(first.userId).toBe('Alice')

		vi.mocked(titand.auth.revokeAllForAccount).mockRejectedValueOnce(new Error('simulated cleanup failure'))
		await expect(titand.user.deleteUser(first.userId)).rejects.toThrow('simulated cleanup failure')

		// The account disappears immediately even though cleanup did not finish.
		expect(await titand.user.getMember(first.userId)).toBeUndefined()
		expect(await titand.user.listMembers()).toEqual([])
		expect(await titand.user.listDeletedMemberIds()).toEqual(['Alice'])
		expect(await titand.store.get('members')).toEqual([{id: 'Alice', deleted: true}])
		expect(titand.files.cloud.removeUser).toHaveBeenCalledWith('Alice')
		expect(titand.files.samba.removeUser).toHaveBeenCalledWith('Alice')
		expect(titand.files.deleteMemberDirectories).not.toHaveBeenCalled()

		// Reusing the display name creates a new security identity.
		const replacement = await titand.user.createUser('Alice', 'passwordpassword')
		expect(replacement.userId).toBe('Alice-2')

		// Retrying the original deletion resumes its idempotent cleanup.
		await expect(titand.user.deleteUser(first.userId)).resolves.toBe(true)
		expect(await titand.store.get('members')).toContainEqual({
			id: 'Alice',
			deleted: true,
			cleanupComplete: true,
		})
		expect(await titand.user.listDeletedMemberIds()).toEqual(['Alice'])
		expect(titand.files.cloud.removeUser).toHaveBeenCalledWith('Alice')
		expect(titand.photos.deleteAccount).toHaveBeenCalledWith('Alice')
		expect(titand.files.deleteMemberDirectories).toHaveBeenCalledWith('Alice')
		expect(titand.files.memberShares.removeUserFromShares).toHaveBeenCalledWith('Alice')
		expect(titand.apps.removeUserFromMemberShares).toHaveBeenCalledWith('Alice')
		expect(vi.mocked(titand.files.cloud.removeUser).mock.invocationCallOrder[0]).toBeLessThan(
			vi.mocked(titand.photos.deleteAccount).mock.invocationCallOrder[0],
		)
		expect(vi.mocked(titand.photos.deleteAccount).mock.invocationCallOrder[0]).toBeLessThan(
			vi.mocked(titand.files.deleteMemberDirectories).mock.invocationCallOrder[0],
		)
	})

	test('resumes pending member cleanup after restart', async () => {
		const member = await titand.user.createUser('Grace', 'passwordpassword')
		vi.mocked(titand.auth.revokeAllForAccount).mockRejectedValueOnce(new Error('simulated process interruption'))
		await expect(titand.user.deleteUser(member.userId)).rejects.toThrow('simulated process interruption')

		vi.restoreAllMocks()
		const restarted = new Titand({dataDirectory})
		vi.spyOn(restarted.files, 'deleteMemberDirectories').mockResolvedValue()
		vi.spyOn(restarted.files.cloud, 'removeUser').mockResolvedValue()
		vi.spyOn(restarted.files.samba, 'removeUser').mockResolvedValue(false)
		vi.spyOn(restarted.files.memberShares, 'removeUserFromShares').mockResolvedValue()
		vi.spyOn(restarted.apps, 'removeUserFromMemberShares').mockResolvedValue()
		vi.spyOn(restarted.auth, 'revokeAllForAccount').mockResolvedValue(0)
		vi.spyOn(restarted.photos, 'deleteAccount').mockResolvedValue()

		await restarted.user.finishPendingDeletions()

		expect(restarted.auth.revokeAllForAccount).toHaveBeenCalledWith('Grace')
		expect(restarted.files.cloud.removeUser).toHaveBeenCalledWith('Grace')
		expect(restarted.files.samba.removeUser).toHaveBeenCalledWith('Grace')
		expect(restarted.photos.deleteAccount).toHaveBeenCalledWith('Grace')
		expect(restarted.files.deleteMemberDirectories).toHaveBeenCalledWith('Grace')
		expect(await restarted.store.get('members')).toEqual([{id: 'Grace', deleted: true, cleanupComplete: true}])
	})

	test('keeps member files until retryable Cloud cleanup succeeds', async () => {
		const member = await titand.user.createUser('Alice', 'passwordpassword')
		vi.mocked(titand.files.cloud.removeUser).mockRejectedValueOnce(new Error('cloud cleanup unavailable'))

		await expect(titand.user.deleteUser(member.userId)).rejects.toThrow('cloud cleanup unavailable')

		expect(titand.files.cloud.removeUser).toHaveBeenCalledWith(member.userId)
		expect(titand.files.deleteMemberDirectories).not.toHaveBeenCalled()
		expect(await titand.store.get('members')).toEqual([{id: member.userId, deleted: true}])
	})

	test('keeps member files until retryable Samba cleanup succeeds', async () => {
		const member = await titand.user.createUser('Alice', 'passwordpassword')
		vi.mocked(titand.files.samba.removeUser).mockRejectedValueOnce(new Error('samba cleanup unavailable'))

		await expect(titand.user.deleteUser(member.userId)).rejects.toThrow('samba cleanup unavailable')
		expect(titand.files.samba.removeUser).toHaveBeenCalledWith(member.userId)
		expect(titand.files.deleteMemberDirectories).not.toHaveBeenCalled()
		expect(await titand.store.get('members')).toEqual([{id: member.userId, deleted: true}])

		await expect(titand.user.finishPendingDeletions()).resolves.toBeUndefined()
		expect(titand.files.samba.removeUser).toHaveBeenCalledTimes(2)
		expect(titand.files.deleteMemberDirectories).toHaveBeenCalledWith(member.userId)
	})

	test('retries member deletion when Photos cleanup fails', async () => {
		const member = await titand.user.createUser('Alice', 'passwordpassword')
		vi.mocked(titand.photos.deleteAccount).mockRejectedValueOnce(new Error('photos cleanup unavailable'))

		await expect(titand.user.deleteUser(member.userId)).rejects.toThrow('photos cleanup unavailable')

		expect(titand.photos.deleteAccount).toHaveBeenCalledWith(member.userId)
		expect(titand.files.deleteMemberDirectories).not.toHaveBeenCalled()
		expect(await titand.store.get('members')).toEqual([{id: member.userId, deleted: true}])

		await expect(titand.user.deleteUser(member.userId)).resolves.toBe(true)
		expect(titand.photos.deleteAccount).toHaveBeenCalledTimes(2)
		expect(titand.files.deleteMemberDirectories).toHaveBeenCalledWith(member.userId)
	})

	test('removes a deleted member avatar directory and retries an interrupted avatar cleanup', async () => {
		const member = await titand.user.createUser('Avatar owner', 'passwordpassword')
		const hash = 'a'.repeat(64)
		const avatarDirectory = accountAvatarDirectory(titand, member.userId)
		await titand.user.setAccountAvatarHash(member.userId, hash)
		await fse.outputFile(accountAvatarPath(titand, member.userId, hash), 'avatar')

		const originalRemove = fse.remove.bind(fse)
		const removeSpy = vi.spyOn(fse, 'remove').mockImplementation(async (path) => {
			if (path === avatarDirectory) throw new Error('simulated avatar cleanup failure')
			return originalRemove(path)
		})
		await expect(titand.user.deleteUser(member.userId)).rejects.toThrow('simulated avatar cleanup failure')
		expect(await fse.pathExists(avatarDirectory)).toBe(true)
		expect(await titand.store.get('members')).toEqual([{id: member.userId, deleted: true}])
		expect(titand.files.memberShares.removeUserFromShares).toHaveBeenCalledWith(member.userId)
		expect(titand.apps.removeUserFromMemberShares).toHaveBeenCalledWith(member.userId)

		removeSpy.mockRestore()
		await expect(titand.user.finishPendingDeletions()).resolves.toBeUndefined()
		expect(await fse.pathExists(avatarDirectory)).toBe(false)
		expect(await titand.store.get('members')).toEqual([{id: member.userId, deleted: true, cleanupComplete: true}])
	})

	test('does not rewrite member metadata when an existing account already has no avatar', async () => {
		const member = await titand.user.createUser('No avatar', 'passwordpassword')
		const originalWriteLock = titand.store.getWriteLock.bind(titand.store)
		let memberWrites = 0
		vi.spyOn(titand.store, 'getWriteLock').mockImplementation((job) =>
			originalWriteLock(async (methods) =>
				job({
					...methods,
					set: (async (property: string, value: unknown) => {
						if (property === 'members') memberWrites += 1
						return (methods.set as (property: string, value: unknown) => Promise<boolean>)(property, value)
					}) as typeof methods.set,
				}),
			),
		)

		await expect(titand.user.removeAccountAvatarHash(member.userId)).resolves.toBeUndefined()
		expect(memberWrites).toBe(0)
		await expect(titand.user.removeAccountAvatarHash('Missing')).rejects.toThrow('User not found')
		expect(memberWrites).toBe(0)
	})

	test('serializes display-name uniqueness checks with account renames', async () => {
		const first = await titand.user.createUser('Alice', 'passwordpassword')
		const second = await titand.user.createUser('Bob', 'passwordpassword')

		const results = await Promise.allSettled([
			titand.user.setAccountName(first.userId, 'Shared name'),
			titand.user.setAccountName(second.userId, 'Shared name'),
		])

		expect(results.filter((result) => result.status === 'fulfilled')).toHaveLength(1)
		expect(results.filter((result) => result.status === 'rejected')).toHaveLength(1)
		expect((await titand.user.listMembers()).filter((member) => member.name === 'Shared name')).toHaveLength(1)
	})

	test("reapplies Samba shares when the owner's display name changes", async () => {
		let nameWhenSharesApplied: string | undefined
		const applyShares = vi.spyOn(titand.files.samba, 'applyShares').mockImplementation(async () => {
			nameWhenSharesApplied = (await titand.user.get())?.name
			return undefined
		})

		await expect(titand.user.setAccountName('0', 'Renamed owner')).resolves.toBe(true)

		expect(await titand.user.get()).toMatchObject({name: 'Renamed owner'})
		expect(nameWhenSharesApplied).toBe('Renamed owner')
		expect(applyShares).toHaveBeenCalledOnce()
	})

	test("reapplies Samba shares when an enabled member's display name changes", async () => {
		const member = await titand.user.createUser('Alice', 'passwordpassword')
		const records = (await titand.store.get('members')) ?? []
		await titand.store.set(
			'members',
			records.map((record) =>
				record.id === member.userId && !('deleted' in record)
					? {...record, sambaPassword: 'member-samba-password'}
					: record,
			),
		)
		const applyShares = vi.spyOn(titand.files.samba, 'applyShares').mockResolvedValue(undefined)

		await expect(titand.user.setAccountName(member.userId, 'Renamed Alice')).resolves.toBe(true)
		expect((await titand.user.getMember(member.userId))?.name).toBe('Renamed Alice')
		expect(applyShares).toHaveBeenCalledOnce()
	})

	test('attempts the Samba refresh when the RAID mirror fails after an owner rename', async () => {
		vi.mocked(titand.hardware.raid.hasConfigStore).mockResolvedValue(true)
		vi.spyOn(titand.hardware.raid.configStore, 'set').mockRejectedValueOnce(new Error('RAID mirror unavailable'))
		const applyShares = vi.spyOn(titand.files.samba, 'applyShares').mockResolvedValue(undefined)

		await expect(titand.user.setAccountName('0', 'Renamed owner')).rejects.toThrow('RAID mirror unavailable')

		expect(await titand.user.get()).toMatchObject({name: 'Renamed owner'})
		expect(applyShares).toHaveBeenCalledOnce()
	})

	test("copies the owner's language when creating a member, then keeps both preferences independent", async () => {
		await titand.user.setAccountLanguage('0', 'fr')
		const member = await titand.user.createUser('Alice', 'passwordpassword')
		await expect(titand.user.getAccountLanguage(member.userId)).resolves.toBe('fr')
		await expect(titand.user.listAccounts()).resolves.toEqual([
			{userId: '0', name: 'Owner', wallpaper: undefined, language: 'fr'},
			{userId: member.userId, name: 'Alice', wallpaper: undefined, language: 'fr'},
		])

		await titand.user.setAccountLanguage('0', 'es')
		await expect(titand.user.getAccountLanguage(member.userId)).resolves.toBe('fr')

		await titand.user.setAccountLanguage(member.userId, 'de')
		await expect(titand.user.getAccountLanguage(member.userId)).resolves.toBe('de')
		await expect(titand.user.getAccountLanguage('0')).resolves.toBe('es')
		await expect(titand.user.listAccounts()).resolves.toEqual([
			{userId: '0', name: 'Owner', wallpaper: undefined, language: 'es'},
			{userId: member.userId, name: 'Alice', wallpaper: undefined, language: 'de'},
		])
	})

	test('does not issue a session from password and MFA state verified before an account reset', async () => {
		await titand.auth.start()
		const member = await titand.user.createUser('Satoshi', 'old-password')
		const totpUri = totp.generateUri('Titan', 'titan.local')
		await titand.user.enable2faForAccount(member.userId, totpUri)

		const staleValidation = await titand.user.validateAccountLogin(
			member.userId,
			'old-password',
			totp.generateToken(totpUri),
		)
		expect(staleValidation.valid).toBe(true)
		if (!staleValidation.valid) throw new Error('Expected valid login')

		await titand.user.resetMemberPassword(member.userId, 'new-password')

		await expect(
			titand.auth.createSession({
				accountId: member.userId,
				expectedSessionIssuanceRevision: staleValidation.sessionIssuanceRevision,
			}),
		).rejects.toThrow('Login credentials changed')
		await expect(titand.user.validateAccountLogin(member.userId, 'old-password')).resolves.toEqual({
			valid: false,
			reason: 'incorrect-password',
		})

		const currentValidation = await titand.user.validateAccountLogin(member.userId, 'new-password')
		expect(currentValidation.valid).toBe(true)
		if (!currentValidation.valid) throw new Error('Expected valid login')
		await expect(
			titand.auth.createSession({
				accountId: member.userId,
				expectedSessionIssuanceRevision: currentValidation.sessionIssuanceRevision,
			}),
		).resolves.toBeDefined()
	})
})
