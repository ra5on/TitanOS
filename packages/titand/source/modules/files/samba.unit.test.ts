import fse from 'fs-extra'
import {afterEach, beforeEach, describe, expect, test, vi} from 'vitest'

import Titand from '../../index.js'
import temporaryDirectory from '../utilities/temporary-directory.js'

describe('multi-account Samba policy', () => {
	let directory: ReturnType<typeof temporaryDirectory>
	let titand: Titand

	beforeEach(async () => {
		directory = temporaryDirectory()
		await directory.createRoot()
		titand = new Titand({dataDirectory: await directory.create()})
		await titand.store.set('user', {name: 'Owner', hashedPassword: 'unused'})
		await titand.store.set('members', [
			{
				id: 'Alice',
				name: 'Alice',
				hashedPassword: 'unused',
				language: 'en',
				sambaPassword: 'alice-samba-password',
			},
			{id: 'Bob', name: 'Bob', hashedPassword: 'unused', language: 'en'},
		])

		for (const path of [
			'/Home/Documents',
			'/Users/Alice/Documents',
			'/Users/Alice/Trash/Old',
			'/Users/Bob/Documents',
			'/External/Shared Drive/Alice',
		]) {
			await fse.ensureDir(titand.files.virtualToSystemPathUnsafe(path))
		}
		await titand.store.set('files.memberShares', [
			{path: '/Home/Documents', sharedWith: ['Alice']},
			{path: '/External', sharedWith: ['Alice']},
		])

		vi.spyOn(titand.files.samba, 'applyCredentials').mockResolvedValue(undefined)
		vi.spyOn(titand.files.samba, 'applyShares').mockResolvedValue(undefined as never)
	})

	afterEach(async () => {
		vi.restoreAllMocks()
		await titand.auth.stop()
		await directory.destroyRoot()
	})

	test('migrates legacy shares to the owner and keeps account lists isolated', async () => {
		await titand.store.set('files.shares', [
			{name: 'Legacy', path: '/Home/Documents'},
			{name: 'Alice documents', path: '/Users/Alice/Documents', userId: 'Alice'},
		])

		expect((await titand.files.samba.listShares('0')).map(({path}) => path)).toEqual(['/Home/Documents'])
		expect((await titand.files.samba.listShares('Alice')).map(({path}) => path)).toEqual(['/Users/Alice/Documents'])
		await expect(titand.files.samba.removeShare('/Users/Alice/Documents', '0')).resolves.toBe(false)

		await titand.files.samba.addShare('/Home', '0')
		expect(await titand.store.get('files.shares')).toContainEqual({
			name: 'Legacy',
			path: '/Home/Documents',
			userId: '0',
		})
	})

	test('uses lowercase member ids as client usernames and isolates reserved names', () => {
		expect(titand.files.samba.getShareUsername('0')).toBe('titan')
		expect(titand.files.samba.getShareUsername('Alice')).toBe('alice')
		expect(titand.files.samba.getShareUsername('Alice-2')).toBe('alice-2')
		expect(titand.files.samba.getShareUsername('Titan')).toBe('member-titan')
		expect(titand.files.samba.getShareUsername('Titan-smb-deadbeef')).toBe('member-titan-smb-deadbeef')
	})

	test('gives case-colliding member display names unique identity-derived Home sharenames', async () => {
		const members = (await titand.store.get('members')) ?? []
		await titand.store.set(
			'members',
			members.map((member) => {
				if ('deleted' in member || member.id !== 'Bob') return member
				return {...member, name: 'alice', sambaPassword: 'bob-samba-password'}
			}),
		)

		await titand.files.samba.addShare('/Users/Alice', 'Alice')
		await titand.files.samba.addShare('/Users/Bob', 'Bob')
		const aliceSharename = (await titand.files.samba.listShares('Alice'))[0]!.sharename
		const bobSharename = (await titand.files.samba.listShares('Bob'))[0]!.sharename

		expect(aliceSharename).toBe("Alice's Titan (User Alice)")
		expect(bobSharename).toBe("alice's Titan (User Bob)")
		expect(aliceSharename.toLowerCase()).not.toBe(bobSharename.toLowerCase())

		// Unrelated changes to store order must not rename existing mounts.
		await titand.store.set('files.shares', [
			{name: 'Earlier', path: '/Home/Earlier', userId: '0'},
			...((await titand.store.get('files.shares')) ?? []),
		])
		expect((await titand.files.samba.listShares('Alice'))[0]!.sharename).toBe(aliceSharename)
		expect((await titand.files.samba.listShares('Bob'))[0]!.sharename).toBe(bobSharename)

		await titand.store.set(
			'files.shares',
			((await titand.store.get('files.shares')) ?? []).filter((share) => share.path !== '/Home/Earlier'),
		)
		expect((await titand.files.samba.listShares('Alice'))[0]!.sharename).toBe(aliceSharename)
		expect((await titand.files.samba.listShares('Bob'))[0]!.sharename).toBe(bobSharename)
	})

	test("a member rename cannot change the owner's historic Home sharename", async () => {
		await titand.files.samba.addShare('/Home', '0')
		await titand.files.samba.setMemberAccess('Bob', true)
		await titand.files.samba.addShare('/Users/Bob', 'Bob')
		const ownerSharename = (await titand.files.samba.listShares('0'))[0]!.sharename

		await titand.user.setAccountName('Bob', 'owner')

		expect(ownerSharename).toBe("Owner's Titan")
		expect((await titand.files.samba.listShares('0'))[0]!.sharename).toBe(ownerSharename)
		expect((await titand.files.samba.listShares('Bob'))[0]!.sharename).toBe("owner's Titan (User Bob)")
	})

	test('allows enabled members to share only directories in their own Home', async () => {
		await expect(titand.files.samba.addShare('/Users/Alice', 'Alice')).resolves.toBe('/Users/Alice')
		await expect(titand.files.samba.removeShare('/Users/Alice', 'Alice')).resolves.toBe(true)
		await expect(titand.files.samba.addShare('/Users/Alice/Documents', 'Alice')).resolves.toBe(
			'/Users/Alice/Documents',
		)
		await expect(titand.files.samba.addShare('/Home/Documents', 'Alice')).rejects.toThrow('[operation-not-allowed]')
		await expect(titand.files.samba.addShare('/External/Shared Drive/Alice', 'Alice')).rejects.toThrow(
			'[operation-not-allowed]',
		)
		await expect(titand.files.samba.addShare('/Users/Bob/Documents', 'Alice')).rejects.toThrow(
			'[operation-not-allowed]',
		)
		await expect(titand.files.samba.addShare('/Users/Alice/Trash', 'Alice')).rejects.toThrow('[operation-not-allowed]')
		await expect(titand.files.samba.addShare('/Users/Alice/Trash/Old', 'Alice')).rejects.toThrow(
			'[operation-not-allowed]',
		)

		expect(await titand.store.get('files.shares')).toEqual([
			{name: 'Documents', path: '/Users/Alice/Documents', userId: 'Alice'},
		])
	})

	test('rejects line-oriented config injection and allocates names case-insensitively', async () => {
		await fse.ensureDir(titand.files.virtualToSystemPathUnsafe('/Users/Alice/Reports'))
		await fse.ensureDir(titand.files.virtualToSystemPathUnsafe('/Users/Alice/reports'))
		await fse.ensureDir(titand.files.virtualToSystemPathUnsafe('/Users/Alice/unsafe\nvalid users = root'))
		await fse.ensureDir(titand.files.virtualToSystemPathUnsafe('/Users/Alice/unsafe-%H'))

		await titand.files.samba.addShare('/Users/Alice/Reports', 'Alice')
		await titand.files.samba.addShare('/Users/Alice/reports', 'Alice')
		await expect(titand.files.samba.addShare('/Users/Alice/unsafe\nvalid users = root', 'Alice')).rejects.toThrow(
			'[operation-not-allowed]',
		)
		await expect(titand.files.samba.addShare('/Users/Alice/unsafe-%H', 'Alice')).rejects.toThrow(
			'[operation-not-allowed]',
		)

		expect(await titand.store.get('files.shares')).toEqual([
			{name: 'Reports', path: '/Users/Alice/Reports', userId: 'Alice'},
			{name: 'reports (2)', path: '/Users/Alice/reports', userId: 'Alice'},
		])
	})

	test('enabling generates a stable secret and disabling deletes the secret and every share', async () => {
		await expect(titand.files.samba.getSharePassword('Bob')).rejects.toThrow('[samba-access-disabled]')
		await expect(titand.files.samba.listShares('Bob')).rejects.toThrow('[samba-access-disabled]')

		await titand.files.samba.setMemberAccess('Bob', true)
		const firstPassword = await titand.files.samba.getSharePassword('Bob')
		expect(firstPassword).toMatch(/^[0-9a-f]{32}$/)
		await titand.files.samba.setMemberAccess('Bob', true)
		expect(await titand.files.samba.getSharePassword('Bob')).toBe(firstPassword)

		await titand.files.samba.addShare('/Users/Bob/Documents', 'Bob')
		await titand.files.samba.setMemberAccess('Bob', false)
		await expect(titand.files.samba.getSharePassword('Bob')).rejects.toThrow('[samba-access-disabled]')
		expect(await titand.store.get('files.shares')).toEqual([])
		expect((await titand.user.getMember('Bob'))?.sambaPassword).toBeUndefined()
	})

	test('does not persist a share when access is revoked while the request is in flight', async () => {
		vi.spyOn(titand.files, 'getAllowedOperations').mockImplementationOnce(async () => {
			await titand.store.getWriteLock(async ({get, set}) => {
				const members = (await get('members')) ?? []
				await set(
					'members',
					members.map((member) => {
						if (member.id !== 'Alice' || 'deleted' in member) return member
						const disabledMember = {...member}
						delete disabledMember.sambaPassword
						return disabledMember
					}),
				)
			})
			return ['share']
		})

		await expect(titand.files.samba.addShare('/Users/Alice/Documents', 'Alice')).rejects.toThrow(
			'[samba-access-disabled]',
		)
		expect(await titand.store.get('files.shares')).toBeUndefined()
	})

	test('applies credentials before reloading shares when access changes', async () => {
		const order: string[] = []
		vi.mocked(titand.files.samba.applyCredentials).mockImplementation(async () => {
			order.push('credentials')
		})
		vi.mocked(titand.files.samba.applyShares).mockImplementation(async () => {
			order.push('shares')
			return undefined as never
		})

		await titand.files.samba.setMemberAccess('Bob', true)

		expect(order).toEqual(['credentials', 'shares'])
	})

	test('still removes credentials and config when active-session revocation fails', async () => {
		const disconnect = vi.spyOn(titand.files.samba, 'disconnectUser').mockRejectedValue(new Error('revocation failed'))

		await expect(titand.files.samba.setMemberAccess('Alice', false)).rejects.toThrow('revocation failed')
		expect(disconnect).toHaveBeenCalledWith('Alice')
		expect(titand.files.samba.applyCredentials).toHaveBeenCalledOnce()
		expect(titand.files.samba.applyShares).toHaveBeenCalledOnce()
		expect((await titand.user.getMember('Alice'))?.sambaPassword).toBeUndefined()
	})

	test('still reconciles credentials and config when deletion session revocation fails', async () => {
		const disconnect = vi.spyOn(titand.files.samba, 'disconnectUser').mockRejectedValue(new Error('revocation failed'))

		await expect(titand.files.samba.removeUser('Alice')).rejects.toThrow('revocation failed')
		expect(disconnect).toHaveBeenCalledWith('Alice')
		expect(titand.files.samba.applyCredentials).toHaveBeenCalledOnce()
		expect(titand.files.samba.applyShares).toHaveBeenCalledOnce()
	})

	test('changes only the authenticated account password and rejects disabled members', async () => {
		await titand.files.samba.setSharePassword('Alice', 'alice-new-password')
		expect(await titand.files.samba.getSharePassword('Alice')).toBe('alice-new-password')
		expect((await titand.user.getMember('Bob'))?.sambaPassword).toBeUndefined()
		await expect(titand.files.samba.setSharePassword('Bob', 'bob-new-password')).rejects.toThrow(
			'[samba-access-disabled]',
		)
		await expect(titand.files.samba.setSharePassword('Alice', 'unsafe\npassword')).rejects.toThrow(
			'[invalid-samba-password]',
		)
	})

	test('still disconnects the target account when password reconciliation fails', async () => {
		vi.mocked(titand.files.samba.applyCredentials).mockRejectedValueOnce(new Error('credential repair failed'))
		const disconnect = vi.spyOn(titand.files.samba, 'disconnectUser').mockResolvedValue()

		await expect(titand.files.samba.setSharePassword('Alice', 'alice-new-password')).rejects.toThrow(
			'credential repair failed',
		)
		expect(disconnect).toHaveBeenCalledWith('Alice')
		expect(await titand.files.samba.getSharePassword('Alice')).toBe('alice-new-password')
	})

	test('member removal is idempotent and deletes only that member shares', async () => {
		await titand.store.set('files.shares', [
			{name: 'Owner', path: '/Home/Documents', userId: '0'},
			{name: 'Alice', path: '/Users/Alice/Documents', userId: 'Alice'},
		])

		await expect(titand.files.samba.removeUser('Alice')).resolves.toBe(true)
		await expect(titand.files.samba.removeUser('Alice')).resolves.toBe(false)
		expect(await titand.store.get('files.shares')).toEqual([{name: 'Owner', path: '/Home/Documents', userId: '0'}])
	})
})
