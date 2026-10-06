import {setTimeout as delay} from 'node:timers/promises'
import {default as SMB2} from '@tryjsky/v9u-smb2'
import {afterAll, afterEach, beforeAll, beforeEach, describe, expect, test} from 'vitest'

import {createTestVm} from '../test-utilities/create-test-titand.js'

const ownerDashboardPassword = 'moneyprintergobrrr'
const memberDashboardPassword = 'member-password'
const ownerSambaPassword = 'owner-samba-password'
const sambaDenialCodes = new Set(['STATUS_ACCESS_DENIED', 'STATUS_LOGON_FAILURE', 'STATUS_BAD_NETWORK_NAME'])

describe.sequential('account-scoped Samba access', () => {
	let titand: Awaited<ReturnType<typeof createTestVm>>
	let failed = false
	let aliceId: string
	let bobId: string
	let aliceSambaUsername: string
	let bobSambaUsername: string
	let aliceSambaPassword: string
	let bobSambaPassword: string
	let aliceSystemUsername: string
	let bobSystemUsername: string
	let ownerSharename: string
	let aliceSharename: string
	let bobSharename: string

	const loginAs = async (userId: string, password: string) => {
		const token = await titand.client.user.login.mutate({userId, password})
		titand.setAuthToken(token)
	}

	const createSmbClient = (username: string, password: string, share: string) =>
		new (SMB2 as any)({
			share: `\\\\127.0.0.1\\${share}`,
			port: titand.vm.getHostPort(445),
			username,
			password,
			autoCloseTimeout: 0,
		})
	const disconnectSmbClient = (client: ReturnType<typeof createSmbClient>) => {
		// v9u-smb2's disconnect is a no-op when session setup succeeds but tree
		// connect fails. Destroy the underlying socket as well so expected denials
		// cannot leak partial sessions into later assertions.
		client.disconnect()
		client.socket.destroy()
	}
	const withSmbTimeout = async <T>(operation: Promise<T>, label: string, timeoutMs = 10_000) => {
		let timeout: NodeJS.Timeout
		try {
			return await Promise.race([
				operation,
				new Promise<never>((_resolve, reject) => {
					timeout = setTimeout(() => reject(new Error(`[smb-timeout] ${label}`)), timeoutMs)
					timeout.unref()
				}),
			])
		} finally {
			clearTimeout(timeout!)
		}
	}

	const expectSmbDenied = async (username: string, password: string, share: string) => {
		const client = createSmbClient(username, password, share)
		try {
			let denied = false
			try {
				await withSmbTimeout(client.exists('private.txt'), `authenticate ${username} to ${share}`)
			} catch (error) {
				if ((error as Error).message.startsWith('[smb-timeout]')) throw error
				expect(sambaDenialCodes).toContain((error as NodeJS.ErrnoException).code)
				denied = true
			}
			expect(denied).toBe(true)
		} finally {
			disconnectSmbClient(client)
		}
	}

	const expectSmbReadable = async (
		username: string,
		password: string,
		share: string,
		file: string,
		contents: string,
	) => {
		let lastError: unknown
		for (let attempt = 1; attempt <= 3; attempt++) {
			const client = createSmbClient(username, password, share)
			try {
				const result = await withSmbTimeout(
					client.readFile(file, {encoding: 'utf8'}),
					`read ${share}/${file} as ${username} (attempt ${attempt})`,
				)
				expect(result).toBe(contents)
				return
			} catch (error) {
				lastError = error
			} finally {
				disconnectSmbClient(client)
			}
			if (attempt < 3) await delay(1000)
		}
		throw lastError
	}
	const writeSmbFile = async (username: string, password: string, share: string, file: string, contents: string) => {
		let lastError: unknown
		for (let attempt = 1; attempt <= 3; attempt++) {
			const client = createSmbClient(username, password, share)
			try {
				await withSmbTimeout(
					client.writeFile(file, contents, {encoding: 'utf8', flags: 'w'}),
					`write ${share}/${file} as ${username} (attempt ${attempt})`,
				)
				return
			} catch (error) {
				lastError = error
				if ((error as NodeJS.ErrnoException).code !== 'STATUS_PENDING') throw error
				// STATUS_PENDING is a valid intermediate SMB response. v9u-smb2
				// incorrectly rejects it instead of waiting for the final response,
				// so give Samba time to finish before checking the result through a
				// fresh connection. This is observed under loaded x64 CI runners.
				await delay(500)
			} finally {
				disconnectSmbClient(client)
			}

			try {
				await expectSmbReadable(username, password, share, file, contents)
				return
			} catch (error) {
				lastError = error
			}
		}
		throw lastError
	}

	const mappedSystemUsername = async (clientUsername: string) =>
		(await titand.vm.sshAsRoot(`awk -F ' = ' '$2 == "${clientUsername}" { print $1 }' /etc/samba/username.map`)).trim()
	const activeSmbProcessId = async (systemUsername: string) =>
		(
			await titand.vm.sshAsRoot(
				`export SMB_USER='${systemUsername}'
				for attempt in $(seq 1 100); do
					process_id=$(smbstatus -b | awk '$2 == ENVIRON["SMB_USER"] { print $1; exit }')
					if [ -n "$process_id" ]; then echo "$process_id"; exit 0; fi
					sleep 0.1
				done`,
			)
		).trim()
	const expectSmbProcessStopped = async (processId: string) => {
		expect(processId).toMatch(/^\d+$/)
		await expect(
			titand.vm.sshAsRoot(`
				for attempt in $(seq 1 100); do
					if ! kill -0 '${processId}' 2>/dev/null; then echo stopped; exit 0; fi
					sleep 0.1
				done
				echo running
			`),
		).resolves.toContain('stopped')
	}

	beforeAll(async () => {
		titand = await createTestVm({device: 'titan-home', forwardPorts: [{guestPort: 445}]})
		await titand.vm.powerOn()
		await titand.registerAndLogin()
	})

	afterAll(async () => await titand?.cleanup())

	afterEach(({task}) => {
		if (task.result?.state === 'fail') failed = true
	})

	beforeEach(({skip}) => {
		if (failed) skip()
	})

	test('owner rotates the main Samba password and creates a private share', async () => {
		const originalPassword = await titand.client.files.sharePassword.query()
		await titand.client.files.setSharePassword.mutate({password: ownerSambaPassword})
		expect(await titand.client.files.sharePassword.query()).toBe(ownerSambaPassword)
		await expect(
			titand.vm.sshAsRoot(`stat -c '%a' '${titand.vm.dataDirectory}/secrets/share-password'`),
		).resolves.toContain('600')

		await titand.client.files.createDirectory.mutate({path: '/Home/Owner private'})
		await titand.client.files.addShare.mutate({path: '/Home/Owner private'})
		ownerSharename = (await titand.client.files.shares.query()).find(
			(share) => share.path === '/Home/Owner private',
		)!.sharename

		await writeSmbFile('titan', ownerSambaPassword, ownerSharename, 'private.txt', 'owner data')
		await expectSmbReadable('titan', ownerSambaPassword, ownerSharename, 'private.txt', 'owner data')
		await expectSmbDenied('titan', originalPassword, ownerSharename)
	})

	test('owner enables separate Samba credentials for two members without seeing their passwords', async () => {
		// This deliberately produces member id `Titan`, which must not collide
		// case-insensitively with the owner's historic `titan` Samba username.
		const alice = await titand.client.user.createUser.mutate({name: 'Titan', password: memberDashboardPassword})
		const bob = await titand.client.user.createUser.mutate({name: 'Bob', password: memberDashboardPassword})
		aliceId = alice.userId
		bobId = bob.userId
		expect(aliceId).toBe('Titan')
		aliceSambaUsername = 'member-titan'
		bobSambaUsername = 'bob'

		await titand.client.files.setMemberSambaAccess.mutate({userId: aliceId, enabled: true})
		await titand.client.files.setMemberSambaAccess.mutate({userId: bobId, enabled: true})
		expect(await titand.client.files.memberSambaAccess.query()).toEqual([
			{userId: aliceId, enabled: true, username: aliceSambaUsername},
			{userId: bobId, enabled: true, username: bobSambaUsername},
		])

		aliceSystemUsername = await mappedSystemUsername(aliceSambaUsername)
		bobSystemUsername = await mappedSystemUsername(bobSambaUsername)
		expect(aliceSystemUsername).toMatch(/^titan-smb-[a-f0-9]{20}$/)
		expect(bobSystemUsername).toMatch(/^titan-smb-[a-f0-9]{20}$/)
		expect(bobSystemUsername).not.toBe(aliceSystemUsername)
		await expect(titand.vm.sshAsRoot(`getent passwd '${aliceSystemUsername}'`)).resolves.toContain('/usr/sbin/nologin')
		await expect(titand.vm.sshAsRoot(`getent passwd '${bobSystemUsername}'`)).resolves.toContain('/usr/sbin/nologin')
		await expect(titand.vm.sshAsRoot("stat -c '%a' /etc/samba/username.map")).resolves.toContain('600')
	})

	test('members can use Samba RPCs only for directories in their own Home', async () => {
		await loginAs(aliceId, memberDashboardPassword)
		const alice = await titand.client.user.get.query()
		expect(alice).toMatchObject({sambaEnabled: true, sambaUsername: aliceSambaUsername})
		aliceSambaPassword = await titand.client.files.sharePassword.query()
		expect(aliceSambaPassword).toMatch(/^[a-f0-9]{32}$/)

		await titand.client.files.createDirectory.mutate({path: `/Users/${aliceId}/Alice private`})
		await titand.client.files.addShare.mutate({path: `/Users/${aliceId}/Alice private`})
		aliceSharename = (await titand.client.files.shares.query()).find(
			(share) => share.path === `/Users/${aliceId}/Alice private`,
		)!.sharename
		await expect(titand.client.files.addShare.mutate({path: '/Home/Owner private'})).rejects.toThrow(
			'[operation-not-allowed]',
		)
		await expect(titand.client.files.addShare.mutate({path: `/Users/${bobId}`})).rejects.toThrow(
			'[operation-not-allowed]',
		)
		await expect(titand.client.files.addShare.mutate({path: `/Users/${aliceId}/Trash`})).rejects.toThrow(
			'[operation-not-allowed]',
		)

		await writeSmbFile(aliceSambaUsername, aliceSambaPassword, aliceSharename, 'private.txt', 'alice data')

		await loginAs(bobId, memberDashboardPassword)
		bobSambaPassword = await titand.client.files.sharePassword.query()
		await titand.client.files.createDirectory.mutate({path: `/Users/${bobId}/Bob private`})
		await titand.client.files.addShare.mutate({path: `/Users/${bobId}/Bob private`})
		bobSharename = (await titand.client.files.shares.query()).find(
			(share) => share.path === `/Users/${bobId}/Bob private`,
		)!.sharename
		await writeSmbFile(bobSambaUsername, bobSambaPassword, bobSharename, 'private.txt', 'bob data')
	})

	test('Samba credentials cannot cross account boundaries', async () => {
		await expectSmbReadable('titan', ownerSambaPassword, ownerSharename, 'private.txt', 'owner data')
		await expectSmbReadable(aliceSambaUsername, aliceSambaPassword, aliceSharename, 'private.txt', 'alice data')
		await expectSmbReadable(bobSambaUsername, bobSambaPassword, bobSharename, 'private.txt', 'bob data')
		await expectSmbDenied('titan', ownerSambaPassword, aliceSharename)
		await expectSmbDenied(aliceSambaUsername, aliceSambaPassword, ownerSharename)
		await expectSmbDenied(aliceSambaUsername, aliceSambaPassword, bobSharename)
		await expectSmbDenied(bobSambaUsername, bobSambaPassword, aliceSharename)

		const config = await titand.vm.sshAsRoot('cat /etc/samba/smb.conf')
		expect(config).toContain(`valid users = ${aliceSystemUsername}`)
		expect(config).toContain(`valid users = ${bobSystemUsername}`)
		expect(config).toContain('valid users = titan')
		await expect(
			titand.vm.sshAsRoot('testparm -s /etc/samba/smb.conf >/dev/null 2>&1 && echo valid'),
		).resolves.toContain('valid')
	})

	test('member password rotation keeps the immutable username and invalidates the old password', async () => {
		await loginAs(aliceId, memberDashboardPassword)
		await titand.client.user.set.mutate({name: 'Renamed Alice'})
		expect(await titand.client.user.get.query()).toMatchObject({sambaUsername: aliceSambaUsername})

		const oldPassword = aliceSambaPassword
		aliceSambaPassword = 'alice-rotated-password'
		await titand.client.files.setSharePassword.mutate({password: aliceSambaPassword})
		await expectSmbReadable(aliceSambaUsername, aliceSambaPassword, aliceSharename, 'private.txt', 'alice data')
		await expectSmbDenied(aliceSambaUsername, oldPassword, aliceSharename)
	})

	test('credentials and account-owned shares survive a reboot', async () => {
		await loginAs('0', ownerDashboardPassword)
		await titand.vm.powerOff()
		await titand.vm.powerOn()
		await loginAs('0', ownerDashboardPassword)

		await expectSmbReadable('titan', ownerSambaPassword, ownerSharename, 'private.txt', 'owner data')
		await expectSmbReadable(aliceSambaUsername, aliceSambaPassword, aliceSharename, 'private.txt', 'alice data')
		await expectSmbReadable(bobSambaUsername, bobSambaPassword, bobSharename, 'private.txt', 'bob data')
		expect(await mappedSystemUsername(aliceSambaUsername)).toBe(aliceSystemUsername)
		expect(await mappedSystemUsername(bobSambaUsername)).toBe(bobSystemUsername)
	})

	test('disabling a member revokes an active session and deletes its credential and shares', async () => {
		const activeAliceClient = createSmbClient(aliceSambaUsername, aliceSambaPassword, aliceSharename)
		expect(
			await withSmbTimeout(
				activeAliceClient.readFile('private.txt', {encoding: 'utf8'}),
				'open active Alice session before revocation',
			),
		).toBe('alice data')
		const activeAliceProcessId = await activeSmbProcessId(aliceSystemUsername)

		await loginAs('0', ownerDashboardPassword)
		await titand.client.files.setMemberSambaAccess.mutate({userId: aliceId, enabled: false})
		expect((await titand.client.files.memberSambaAccess.query()).find(({userId}) => userId === aliceId)).toEqual({
			userId: aliceId,
			enabled: false,
			username: aliceSambaUsername,
		})
		await expectSmbProcessStopped(activeAliceProcessId)
		disconnectSmbClient(activeAliceClient)
		await expectSmbDenied(aliceSambaUsername, aliceSambaPassword, aliceSharename)

		const cleanup = await titand.vm.sshAsRoot(`
			if getent passwd '${aliceSystemUsername}' >/dev/null; then echo system-present; else echo system-absent; fi
			if pdbedit -L | cut -d: -f1 | grep -Fx '${aliceSystemUsername}' >/dev/null; then echo passdb-present; else echo passdb-absent; fi
			if grep -F ' = ${aliceSambaUsername}' /etc/samba/username.map >/dev/null; then echo map-present; else echo map-absent; fi
		`)
		expect(cleanup).toContain('system-absent')
		expect(cleanup).toContain('passdb-absent')
		expect(cleanup).toContain('map-absent')

		await loginAs(aliceId, memberDashboardPassword)
		await expect(titand.client.files.sharePassword.query()).rejects.toThrow('[samba-access-disabled]')
		await expect(titand.client.files.shares.query()).rejects.toThrow('[samba-access-disabled]')
	})

	test('re-enabling creates a new password without restoring removed shares', async () => {
		await loginAs('0', ownerDashboardPassword)
		await titand.client.files.setMemberSambaAccess.mutate({userId: aliceId, enabled: true})

		await loginAs(aliceId, memberDashboardPassword)
		const replacementPassword = await titand.client.files.sharePassword.query()
		expect(replacementPassword).not.toBe(aliceSambaPassword)
		expect(await titand.client.files.shares.query()).toEqual([])
		await expectSmbDenied(aliceSambaUsername, replacementPassword, aliceSharename)

		aliceSambaPassword = replacementPassword
		await titand.client.files.addShare.mutate({path: `/Users/${aliceId}/Alice private`})
		aliceSharename = (await titand.client.files.shares.query())[0]!.sharename
		await expectSmbReadable(aliceSambaUsername, aliceSambaPassword, aliceSharename, 'private.txt', 'alice data')
	})

	test('deleting a member removes active access, passdb identity, share config, and dashboard login', async () => {
		const activeBobClient = createSmbClient(bobSambaUsername, bobSambaPassword, bobSharename)
		expect(
			await withSmbTimeout(
				activeBobClient.readFile('private.txt', {encoding: 'utf8'}),
				'open active Bob session before deletion',
			),
		).toBe('bob data')
		const activeBobProcessId = await activeSmbProcessId(bobSystemUsername)

		await loginAs('0', ownerDashboardPassword)
		await titand.client.user.deleteUser.mutate({userId: bobId})
		await expectSmbProcessStopped(activeBobProcessId)
		disconnectSmbClient(activeBobClient)
		await expectSmbDenied(bobSambaUsername, bobSambaPassword, bobSharename)
		await expect(
			titand.unauthenticatedClient.user.login.mutate({userId: bobId, password: memberDashboardPassword}),
		).rejects.toThrow('Incorrect password')

		const cleanup = await titand.vm.sshAsRoot(`
			if getent passwd '${bobSystemUsername}' >/dev/null; then echo system-present; else echo system-absent; fi
			if pdbedit -L | cut -d: -f1 | grep -Fx '${bobSystemUsername}' >/dev/null; then echo passdb-present; else echo passdb-absent; fi
			if grep -F ' = ${bobSambaUsername}' /etc/samba/username.map >/dev/null; then echo map-present; else echo map-absent; fi
			if grep -F '${bobSharename}' /etc/samba/smb.conf >/dev/null; then echo share-present; else echo share-absent; fi
		`)
		expect(cleanup).toContain('system-absent')
		expect(cleanup).toContain('passdb-absent')
		expect(cleanup).toContain('map-absent')
		expect(cleanup).toContain('share-absent')
	})

	test('deleted member access does not return after another reboot', async () => {
		await titand.vm.powerOff()
		await titand.vm.powerOn()
		await loginAs('0', ownerDashboardPassword)

		await expectSmbReadable('titan', ownerSambaPassword, ownerSharename, 'private.txt', 'owner data')
		await expectSmbReadable(aliceSambaUsername, aliceSambaPassword, aliceSharename, 'private.txt', 'alice data')
		await expectSmbDenied(bobSambaUsername, bobSambaPassword, bobSharename)
		const config = await titand.vm.sshAsRoot('cat /etc/samba/smb.conf')
		expect(config).not.toContain(bobSystemUsername)
		expect(config).not.toContain(bobSharename)
	})
})
