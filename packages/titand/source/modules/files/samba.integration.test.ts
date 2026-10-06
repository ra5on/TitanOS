import {expect, beforeEach, afterEach, describe, test, vi} from 'vitest'

import fse from 'fs-extra'
import {delay} from 'es-toolkit'
import {default as SMB2} from '@tryjsky/v9u-smb2'
import tcpPortUsed from 'tcp-port-used'
import {$} from 'execa'

import createTestTitand from '../test-utilities/create-test-titand.js'

let titand: Awaited<ReturnType<typeof createTestTitand>>

// Create a new titand instance for each test
beforeEach(async () => (titand = await createTestTitand({autoLogin: true})))
afterEach(async () => await titand.cleanup())

describe('shares()', () => {
	test('throws invalid error without auth token', async () => {
		await expect(titand.unauthenticatedClient.files.shares.query()).rejects.toThrow('Invalid token')
	})

	test('returns empty array on first start', async () => {
		const shares = await titand.client.files.shares.query()
		expect(shares).toStrictEqual([])
	})

	test('marks deleted directories as unavailable', async () => {
		// Create test directories
		const testDirectory1 = `${titand.instance.dataDirectory}/home/samba-existing-test1`
		const testDirectory2 = `${titand.instance.dataDirectory}/home/samba-existing-test2`
		await fse.mkdir(testDirectory1)
		await fse.mkdir(testDirectory2)

		// Add both directories to shares
		await titand.client.files.addShare.mutate({
			path: '/Home/samba-existing-test1',
		})
		await titand.client.files.addShare.mutate({
			path: '/Home/samba-existing-test2',
		})

		// Delete one directory
		await fse.remove(testDirectory1)

		// Verify both shares are returned but the deleted one is marked unavailable
		const shares = await titand.client.files.shares.query()
		const share1 = shares.find((s) => s.path === '/Home/samba-existing-test1')
		const share2 = shares.find((s) => s.path === '/Home/samba-existing-test2')
		expect(share1?.available).toBe(false)
		expect(share2?.available).toBe(true)
	})

	test('returns proper client-facing sharename for non /Home shares', async () => {
		// Create test directory
		const dir = `${titand.instance.dataDirectory}/home/samba-clientname-test`
		await fse.mkdir(dir)

		// Add to shares
		await titand.client.files.addShare.mutate({path: '/Home/samba-clientname-test'})

		// Verify sharename is returned
		const shares = await titand.client.files.shares.query()
		const entry = shares.find((s) => s.path === '/Home/samba-clientname-test') as any
		expect(entry?.sharename).toBe('samba-clientname-test (Titan)')
	})

	test('returns sharename for Home share', async () => {
		// Add home directory to shares
		await titand.client.files.addShare.mutate({path: '/Home'})

		// Verify sharename matches username's Titan
		const shares = await titand.client.files.shares.query()
		const entry = shares.find((s) => s.path === '/Home') as any
		expect(entry?.sharename).toBe("satoshi's Titan")
	})
})

describe('#handleFileChange()', () => {
	test('automatically removes shares when directory is deleted', async () => {
		// Create test directories
		const testDirectoryToDelete = `${titand.instance.dataDirectory}/home/samba-auto-remove-test`
		const testDirectoryToKeep = `${titand.instance.dataDirectory}/home/samba-keep-test`
		await fse.mkdir(testDirectoryToDelete)
		await fse.mkdir(testDirectoryToKeep)

		// Wait for the creation fs events to fire
		await delay(100)

		// Add both directories to shares
		await titand.client.files.addShare.mutate({path: '/Home/samba-auto-remove-test'})
		await titand.client.files.addShare.mutate({path: '/Home/samba-keep-test'})

		// Verify directories are in shares
		let shares = await titand.client.files.shares.query()
		const paths = shares.map((share) => share.path)
		expect(paths).toContain('/Home/samba-auto-remove-test')
		expect(paths).toContain('/Home/samba-keep-test')

		// Delete one directory
		await fse.remove(testDirectoryToDelete)

		// Watchman/Parcel batch events and store writes are asynchronous. Wait for
		// persisted cleanup instead of assuming both finish within 100 ms.
		await vi.waitFor(
			async () => {
				const storedShares = await titand.instance.store.get('files.shares')
				const storedPaths = storedShares.map((share) => share.path)
				expect(storedPaths).not.toContain('/Home/samba-auto-remove-test')
				expect(storedPaths).toContain('/Home/samba-keep-test')
			},
			{timeout: 5000},
		)
	})

	test('automatically removes shares when directory is renamed', async () => {
		// Create test directory
		const originalDirectory = `${titand.instance.dataDirectory}/home/original-directory`
		const renamedDirectory = `${titand.instance.dataDirectory}/home/renamed-directory`
		await fse.mkdir(originalDirectory)

		// Wait for the creation fs events to fire
		await delay(100)

		// Add directory to shares
		await titand.client.files.addShare.mutate({path: '/Home/original-directory'})

		// Verify directory is in shares
		let shares = await titand.client.files.shares.query()
		const paths = shares.map((share) => share.path)
		expect(paths).toContain('/Home/original-directory')

		// Rename the directory (this causes a delete event for the original path)
		await fse.rename(originalDirectory, renamedDirectory)

		// Verify original path is removed from the store
		await vi.waitFor(
			async () => {
				const storedShares = await titand.instance.store.get('files.shares')
				expect(storedShares.map((share) => share.path)).not.toContain('/Home/original-directory')
			},
			{timeout: 5000},
		)
	})

	test('automatically removes child shares when parent directory is deleted', async () => {
		// Create test directories
		const parentDirectory = `${titand.instance.dataDirectory}/home/parent-directory`
		const childDirectory = `${parentDirectory}/child-directory`
		await fse.mkdir(parentDirectory)
		await fse.mkdir(childDirectory)

		// Wait for the creation fs events to fire
		await delay(100)

		// Add child directory to shares
		await titand.client.files.addShare.mutate({path: '/Home/parent-directory/child-directory'})

		// Verify directories are in shares
		let shares = await titand.client.files.shares.query()
		const paths = shares.map((share) => share.path)
		expect(paths).toContain('/Home/parent-directory/child-directory')

		// Delete parent directory (which also removes the child)
		await fse.remove(parentDirectory)

		// Verify deleted directory is removed from the store
		await vi.waitFor(
			async () => {
				const storedShares = await titand.instance.store.get('files.shares')
				expect(storedShares.map((share) => share.path)).not.toContain('/Home/parent-directory/child-directory')
			},
			{timeout: 5000},
		)
	})
})

describe('addShare()', () => {
	test('throws invalid error without auth token', async () => {
		await expect(titand.unauthenticatedClient.files.addShare.mutate({path: '/Home/Documents'})).rejects.toThrow(
			'Invalid token',
		)
	})

	test('throws on non-directory paths', async () => {
		// Create test file
		const testDirectory = `${titand.instance.dataDirectory}/home/samba-test`
		await fse.mkdir(testDirectory)
		await fse.writeFile(`${testDirectory}/file.txt`, 'test content')

		// Attempt to share a file
		await expect(titand.client.files.addShare.mutate({path: '/Home/samba-test/file.txt'})).rejects.toThrow(
			'[operation-not-allowed]',
		)
	})

	test('throws on unshareable app paths', async () => {
		// Create test directory
		const testDirectory = `${titand.instance.dataDirectory}/app-data/bitcoin`
		await fse.mkdir(testDirectory)

		// Attempt to share the directory
		await expect(titand.client.files.addShare.mutate({path: '/Apps/bitcoin'})).rejects.toThrow(
			'[operation-not-allowed]',
		)
	})

	test('successfully adds external drive mount point to shares', async () => {
		// Create test directory (simulating an external drive mount point)
		const testDirectory = `${titand.instance.dataDirectory}/external/My Portable SSD`
		await fse.ensureDir(testDirectory)

		// Sharing external drive mount points should succeed
		const result = await titand.client.files.addShare.mutate({path: '/External/My Portable SSD'})
		expect(result).toBe('/External/My Portable SSD')

		// Verify it's in the shares list
		const shares = await titand.client.files.shares.query()
		const paths = shares.map((share) => share.path)
		expect(paths).toContain('/External/My Portable SSD')
	})

	test('successfully adds external drive subdirectory to shares', async () => {
		// Create test directory (simulating an external drive mount point)
		const testDirectory = `${titand.instance.dataDirectory}/external/My Portable SSD`
		await fse.ensureDir(testDirectory)

		// Create subdirectory
		const subDirectory = `${testDirectory}/sub-directory`
		await fse.ensureDir(subDirectory)

		// Sharing a subdirectory should succeed
		const result = await titand.client.files.addShare.mutate({
			path: '/External/My Portable SSD/sub-directory',
		})
		expect(result).toBe('/External/My Portable SSD/sub-directory')

		// Verify it's in the shares list
		const shares = await titand.client.files.shares.query()
		const paths = shares.map((share) => share.path)
		expect(paths).toContain('/External/My Portable SSD/sub-directory')
	})

	test('throws on directory traversal attempt', async () => {
		await expect(titand.client.files.addShare.mutate({path: '/Home/../../../../etc/share-dir'})).rejects.toThrow(
			'[operation-not-allowed]',
		)
	})

	test('throws on symlink traversal attempt', async () => {
		// Create a symlink to the root directory
		await fse.ensureDir(`${titand.instance.dataDirectory}/home`)
		await fse.symlink('/', `${titand.instance.dataDirectory}/home/symlink-to-root`)

		// Attempt to share directory through symlink. The path resolves outside its
		// base so it's rejected before anything is persisted.
		await expect(titand.client.files.addShare.mutate({path: '/Home/symlink-to-root/etc'})).rejects.toThrow(
			'[escapes-base]',
		)
	})

	test('throws on relative paths', async () => {
		await Promise.all(
			['', ' ', '.', '..', 'Home', 'Home/shared-dir', 'Home/../shared-dir'].map((path) =>
				expect(titand.client.files.addShare.mutate({path})).rejects.toThrow('[operation-not-allowed]'),
			),
		)
	})

	test('throws on invalid base directory', async () => {
		await expect(titand.client.files.addShare.mutate({path: '/Invalid/test-share'})).rejects.toThrow(
			'[operation-not-allowed]',
		)
	})

	test('successfully adds a directory to shares', async () => {
		// Create test directory
		const testDirectory = `${titand.instance.dataDirectory}/home/samba-test`
		await fse.mkdir(testDirectory)

		// Add directory to shares
		const result = await titand.client.files.addShare.mutate({path: '/Home/samba-test'})

		expect(result).toBe('/Home/samba-test')

		// Verify directory is in shares
		const shares = await titand.client.files.shares.query()
		const paths = shares.map((share) => share.path)
		expect(paths).toContain('/Home/samba-test')
	})

	test('successfully adds home directory to shares', async () => {
		// Add home directory to shares
		const result = await titand.client.files.addShare.mutate({path: '/Home'})
		expect(result).toBe('/Home')

		// Verify directory is in shares
		const shares = await titand.client.files.shares.query()
		const paths = shares.map((share) => share.path)
		expect(paths).toContain('/Home')
	})

	test('throws error on duplicate shares', async () => {
		// Create test directory
		const testDirectory = `${titand.instance.dataDirectory}/home/samba-duplicate-test`
		await fse.mkdir(testDirectory)

		// Add directory to shares
		await titand.client.files.addShare.mutate({
			path: '/Home/samba-duplicate-test',
		})

		// Try to add again and expect failure
		await expect(titand.client.files.addShare.mutate({path: '/Home/samba-duplicate-test'})).rejects.toThrow(
			'[share-already-exists]',
		)
	})

	test('auto-generates unique share names when basename conflicts', async () => {
		// Create test directories with same basename but different paths
		const testDirectory1 = `${titand.instance.dataDirectory}/home/folder1/same-name`
		const testDirectory2 = `${titand.instance.dataDirectory}/home/folder2/same-name`
		await fse.mkdir(testDirectory1, {recursive: true})
		await fse.mkdir(testDirectory2, {recursive: true})

		// Add both directories to shares
		await titand.client.files.addShare.mutate({path: '/Home/folder1/same-name'})
		await titand.client.files.addShare.mutate({path: '/Home/folder2/same-name'})

		// Verify both directories are added with unique names
		const shares = await titand.client.files.shares.query()
		const names = shares.map((share) => share.name)

		// Verify we have two distinct share names
		expect(names).toMatchObject(['same-name', 'same-name (2)'])
	})
})

describe('removeShare()', () => {
	test('successfully removes a directory from shares', async () => {
		// Create test directory
		const testDirectory = `${titand.instance.dataDirectory}/home/samba-remove-test`
		await fse.mkdir(testDirectory)

		// Add directory to shares
		await titand.client.files.addShare.mutate({path: '/Home/samba-remove-test'})

		// Remove from shares
		const result = await titand.client.files.removeShare.mutate({path: '/Home/samba-remove-test'})

		expect(result).toBe(true)

		// Verify directory is not in shares
		const shares = await titand.client.files.shares.query()
		const paths = shares.map((share) => share.path)
		expect(paths).not.toContain('/Home/samba-remove-test')
	})

	test('returns false when removing non-existent share', async () => {
		const result = await titand.client.files.removeShare.mutate({path: '/Home/non-existent-share'})

		expect(result).toBe(false)
	})
})

describe('sharePassword()', () => {
	test('throws invalid error without auth token', async () => {
		await expect(titand.unauthenticatedClient.files.sharePassword.query()).rejects.toThrow('Invalid token')
	})

	test('generates a 128-bit hex string on first run', async () => {
		const sharePassword = await titand.client.files.sharePassword.query()

		// Check it's a 128-bit hex string (32 hex characters = 128 bits)
		expect(sharePassword.length).toBe(32)
		expect(/^[0-9a-f]{32}$/.test(sharePassword)).toBe(true)
	})

	test('always returns the same password', async () => {
		const sharePassword1 = await titand.client.files.sharePassword.query()
		const sharePassword2 = await titand.client.files.sharePassword.query()

		// Verify it's consistently the same password
		expect(sharePassword1).toBe(sharePassword2)
	})
})

describe('samba', () => {
	async function createSmbClient(share: string) {
		const password = await titand.client.files.sharePassword.query()
		return new (SMB2 as any)({
			share: `\\\\localhost\\${share}`,
			username: 'titan',
			password,
		})
	}

	test('port is only listening where shares are active', async () => {
		const smbPort = 445

		// Check if port is open
		await expect(tcpPortUsed.check(smbPort, 'localhost')).resolves.toBe(false)

		// Add home directory to shares
		await expect(titand.client.files.addShare.mutate({path: '/Home'})).resolves.toBe('/Home')

		// Check if port is open
		await expect(tcpPortUsed.check(smbPort, 'localhost')).resolves.toBe(true)

		// Remove share
		await expect(titand.client.files.removeShare.mutate({path: '/Home'})).resolves.toBe(true)

		// Check if port is closed again
		await expect(tcpPortUsed.check(smbPort, 'localhost')).resolves.toBe(false)
	})

	test('share name has (Titan appended)', async () => {
		// Add home directory to shares
		await expect(titand.client.files.addShare.mutate({path: '/Home/Documents'})).resolves.toBe('/Home/Documents')

		// create an SMB2 instance
		const smb = await createSmbClient('Documents (Titan)')

		// Test connection
		await expect(smb.exists('non-existent-file.txt')).resolves.toBe(false)
	})

	test('/Home share is called "username\'s Titan"', async () => {
		// Add home directory to shares
		await expect(titand.client.files.addShare.mutate({path: '/Home'})).resolves.toBe('/Home')

		// create an SMB2 instance
		const smb = await createSmbClient("satoshi's Titan")

		// Test connection
		await expect(smb.exists('non-existent-file.txt')).resolves.toBe(false)
	})

	test("renames the /Home share when the owner's name changes", async () => {
		await titand.client.files.addShare.mutate({path: '/Home'})
		const oldHomeClient = await createSmbClient("satoshi's Titan")
		await expect(oldHomeClient.exists('non-existent-file.txt')).resolves.toBe(false)
		// Samba deliberately preserves active sessions across config reloads. Disconnect
		// first so the final assertion proves a genuinely new client cannot use the old name.
		oldHomeClient.disconnect()
		await expect
			.poll(async () => (await $`smbstatus -b -u titan`).stdout.includes('titan'), {
				interval: 100,
				timeout: 10_000,
			})
			.toBe(false)

		await titand.client.user.set.mutate({name: 'Hal'})

		const homeShare = (await titand.client.files.shares.query()).find((share) => share.path === '/Home')
		expect(homeShare?.sharename).toBe("Hal's Titan")
		await expect((await createSmbClient("Hal's Titan")).exists('non-existent-file.txt')).resolves.toBe(false)
		await expect
			.poll(
				async () => {
					const oldHomeClient = await createSmbClient("satoshi's Titan")
					try {
						await oldHomeClient.exists('non-existent-file.txt')
						return 'share is still available'
					} catch (error) {
						return error instanceof Error ? error.message : String(error)
					} finally {
						oldHomeClient.disconnect()
					}
				},
				{interval: 100, timeout: 10_000},
			)
			.toContain('STATUS_BAD_NETWORK_NAME')
	})

	// This test can be a little flaky (seemingly due to pure js samba client) so retry on failure
	test('client can interact with share', {retry: 5}, async () => {
		// Add home directory to shares
		await expect(titand.client.files.addShare.mutate({path: '/Home'})).resolves.toBe('/Home')

		// create an SMB2 instance
		const smb = await createSmbClient("satoshi's Titan")

		// Test connection
		await expect(smb.exists('non-existent-file.txt')).resolves.toBe(false)

		// Test write
		await expect(smb.writeFile('file.txt', 'hello world', {encoding: 'utf8'})).resolves.toBe(undefined)

		// Test file exists on filesystem
		await expect(fse.exists(`${titand.instance.dataDirectory}/home/file.txt`)).resolves.toBe(true)

		// Test read
		await expect(smb.readFile('file.txt', {encoding: 'utf8'})).resolves.toBe('hello world')

		// Remove the share
		await expect(titand.client.files.removeShare.mutate({path: '/Home'})).resolves.toBe(true)

		// Test read no longer works
		// For some reason the first read after close hangs and the second throws so we do a dummy read
		// first that hangs
		smb.readFile('file.txt', {encoding: 'utf8'})
		// And then a second read that throws
		await expect(smb.readFile('file.txt', {encoding: 'utf8'})).rejects.toThrow('write EPIPE')
	})

	test('reloads config when shares are updated', async () => {
		const smbHome = await createSmbClient("satoshi's Titan")
		let smbDocuments = await createSmbClient('Documents (Titan)')

		// Add home directory to shares and test it works
		await titand.client.files.addShare.mutate({path: '/Home'})
		await expect(smbHome.exists('non-existent-file.txt')).resolves.toBe(false)
		await expect(smbDocuments.exists('non-existent-file.txt')).rejects.toThrow('STATUS_BAD_NETWORK_NAME')

		// Add documents share and test it works
		// We need to recreate the client because for some reason it can't be used after the above error
		smbDocuments = await createSmbClient('Documents (Titan)')
		await titand.client.files.addShare.mutate({path: '/Home/Documents'})
		await expect(smbHome.exists('non-existent-file.txt')).resolves.toBe(false)
		await expect(smbDocuments.exists('non-existent-file.txt')).resolves.toBe(false)
	})

	test("doesn't allow escaping shared directories via path traversal", async () => {
		// Create test directory file
		const testFile = `${titand.instance.dataDirectory}/home/path-traversal-test/test/file.txt`
		await fse.ensureFile(testFile)

		// Create a sensitive file outside the share
		const sensitiveFile = `${titand.instance.dataDirectory}/secrets/sensitive.txt`
		await fse.ensureFile(sensitiveFile)

		// Add test directory to shares
		await titand.client.files.addShare.mutate({path: '/Home/path-traversal-test'})

		// Connect to share
		const smb = await createSmbClient('path-traversal-test (Titan)')

		// Test ..\\ syntax works
		await expect(smb.readFile('test\\..\\test\\file.txt', {encoding: 'utf8'})).resolves.toBe('')

		// Test traversal outside of share fails
		await expect(smb.readFile('..\\..\\secrets\\sensitive.txt', {encoding: 'utf8'})).rejects.toThrow(
			'STATUS_OBJECT_PATH_SYNTAX_BAD',
		)
	})

	test("doesn't allow escaping shared directories via symlinks", async () => {
		// Create a sensitive file outside of files root
		const sensitiveFile = `${titand.instance.dataDirectory}/secrets/sensitive.txt`
		await fse.ensureFile(sensitiveFile)
		await fse.writeFile(sensitiveFile, 'sensitive data')

		// Create test directory with symlink to sensitive file and a normal file
		const testDirectory = `${titand.instance.dataDirectory}/home/symlink-traversal-test`
		await fse.ensureDir(testDirectory)
		await fse.symlink(sensitiveFile, `${testDirectory}/symlink-to-sensitive`)
		await fse.ensureFile(`${testDirectory}/normal-file`)

		// Add test directory to shares
		await titand.client.files.addShare.mutate({path: '/Home/symlink-traversal-test'})

		// Connect to share
		const smb = await createSmbClient('symlink-traversal-test (Titan)')

		// Test samba lists the normal file but not the symlink
		await expect(smb.readFile('normal-file', {encoding: 'utf8'})).resolves.toBe('')
		await expect(smb.readFile('symlink-to-sensitive', {encoding: 'utf8'})).rejects.toThrow(
			'STATUS_OBJECT_NAME_NOT_FOUND',
		)
	})
})

describe('wsdd2', () => {
	test('runs only while samba runs', async () => {
		// Check wsdd2 is not running
		await expect($`systemctl is-active wsdd2`).rejects.toThrow('inactive')

		// Add home directory to shares
		await expect(titand.client.files.addShare.mutate({path: '/Home'})).resolves.toBe('/Home')

		// Check wsdd2 is running
		await expect($`systemctl is-active wsdd2`).resolves.toMatchObject({stdout: 'active'})

		// Remove share
		await expect(titand.client.files.removeShare.mutate({path: '/Home'})).resolves.toBe(true)

		// Check wsdd2 is not running
		await expect($`systemctl is-active wsdd2`).rejects.toThrow('inactive')
	})
})
