import fsp from 'node:fs/promises'
import os from 'node:os'
import nodePath from 'node:path'
import {execa} from 'execa'
import {afterEach, describe, expect, test} from 'vitest'
import {convertCustomQcow2, qcow2SandboxArguments, validateQcow2Header} from './custom-qcow2.js'

const directories: string[] = []
afterEach(async () => {
	await Promise.all(directories.splice(0).map((path) => fsp.rm(path, {recursive: true, force: true})))
})
async function fixture() {
	const directory = await fsp.mkdtemp(nodePath.join(os.tmpdir(), 'titan-qcow-test-'))
	directories.push(directory)
	return {
		directory,
		source: nodePath.join(directory, 'source.qcow2'),
		destination: nodePath.join(directory, 'destination.qcow2'),
	}
}
function header() {
	const buffer = Buffer.alloc(104)
	buffer.writeUInt32BE(0x514649fb, 0)
	buffer.writeUInt32BE(3, 4)
	return buffer
}
describe('Custom QCOW2 import security', () => {
	test('rejects backing files, external data, encryption and non-QCOW2 headers before parsing', () => {
		const backing = header()
		backing.writeBigUInt64BE(4096n, 8)
		expect(() => validateQcow2Header(backing)).toThrow('backing-chain')
		const data = header()
		data.writeBigUInt64BE(4n, 72)
		expect(() => validateQcow2Header(data)).toThrow('external-data')
		const encrypted = header()
		encrypted.writeUInt32BE(1, 32)
		expect(() => validateQcow2Header(encrypted)).toThrow('image-invalid')
		expect(() => validateQcow2Header(Buffer.alloc(20))).toThrow('image-invalid')
		expect(() => validateQcow2Header(header())).not.toThrow()
	})
	test('sandbox exposes only immutable runtime files, its private source and output; no network or host data', () => {
		const args = qcow2SandboxArguments('/private/source', '/private/output')
		expect(args).toContain('--unshare-all')
		expect(args).toContain('--clearenv')
		expect(args).toContain('--new-session')
		expect(args).not.toContain('/etc')
		expect(args).not.toContain('/home')
		expect(args).not.toContain('/run')
		expect(args.filter((arg) => arg === '--bind')).toHaveLength(1)
	})
	test('converts a real standalone disk and preserves its bytes without backing links', async () => {
		const {directory, source, destination} = await fixture()
		const raw = nodePath.join(directory, 'source.img')
		await fsp.writeFile(raw, Buffer.alloc(65536, 0x54))
		await execa('qemu-img', ['convert', '-f', 'raw', '-O', 'qcow2', raw, source])
		const original = await fsp.readFile(source)
		await convertCustomQcow2(source, destination, 1, new AbortController().signal)
		const info = JSON.parse((await execa('qemu-img', ['info', '--output=json', destination])).stdout)
		expect(info.format).toBe('qcow2')
		expect(info['backing-filename']).toBeUndefined()
		expect(info['virtual-size']).toBe(1024 ** 3)
		const exported = nodePath.join(directory, 'export.img')
		await execa('qemu-img', ['convert', '-f', 'qcow2', '-O', 'raw', destination, exported])
		const handle = await fsp.open(exported, 'r')
		try {
			const bytes = Buffer.alloc(65536)
			await handle.read(bytes, 0, bytes.length, 0)
			expect(bytes.equals(Buffer.alloc(65536, 0x54))).toBe(true)
		} finally {
			await handle.close()
		}
		expect((await fsp.readFile(source)).equals(original)).toBe(true)
		expect((await fsp.readdir(directory)).some((name) => name.startsWith('.qcow2-import-'))).toBe(false)
	})
	test('uses Debian tools even when PATH contains another bwrap or qemu-img', async () => {
		const {directory, source, destination} = await fixture()
		await execa('qemu-img', ['create', '-f', 'qcow2', source, '1M'])
		const bin = nodePath.join(directory, 'bin')
		await fsp.mkdir(bin)
		for (const name of ['bwrap', 'qemu-img']) {
			await fsp.writeFile(nodePath.join(bin, name), '#!/bin/sh\nexit 99\n', {mode: 0o755})
		}
		const previous = process.env.PATH
		process.env.PATH = `${bin}:${previous ?? ''}`
		try {
			await expect(convertCustomQcow2(source, destination, 1, new AbortController().signal)).resolves.toBeUndefined()
		} finally {
			process.env.PATH = previous
		}
	})
	test('accepts an image exactly as large as the selected QEMU target size', async () => {
		const {source, destination} = await fixture()
		await execa('qemu-img', ['create', '-f', 'qcow2', source, '1G'])
		await expect(convertCustomQcow2(source, destination, 1, new AbortController().signal)).resolves.toBeUndefined()
	})
	test.runIf(process.getuid?.() === 0)(
		'imports through a private home owned by a different host UID without relaxing its permissions',
		async () => {
			const {directory, source, destination} = await fixture()
			await execa('/usr/bin/qemu-img', ['create', '-f', 'qcow2', source, '1M'])
			await fsp.chown(directory, 65534, 65534)
			await fsp.chmod(directory, 0o700)
			await expect(
				execa('/usr/bin/bwrap', [...qcow2SandboxArguments(source), 'info', '-f', 'qcow2', '/work/source']),
			).rejects.toThrow('Permission denied')
			await expect(convertCustomQcow2(source, destination, 1, new AbortController().signal)).resolves.toBeUndefined()
			const stat = await fsp.stat(directory)
			expect(stat.uid).toBe(65534)
			expect(stat.mode & 0o777).toBe(0o700)
			expect(
				JSON.parse((await execa('/usr/bin/qemu-img', ['info', '--output=json', destination])).stdout)['virtual-size'],
			).toBe(1024 ** 3)
		},
	)
	test('rejects a real backing chain and leaves no output or private copy', async () => {
		const {directory, source, destination} = await fixture()
		const backing = nodePath.join(directory, 'secret.img')
		await fsp.writeFile(backing, 'private host data')
		await execa('qemu-img', ['create', '-f', 'qcow2', '-F', 'raw', '-b', backing, source])
		await expect(convertCustomQcow2(source, destination, 1, new AbortController().signal)).rejects.toThrow(
			'backing-chain',
		)
		expect(await fsp.readFile(backing, 'utf8')).toBe('private host data')
		expect((await fsp.readdir(directory)).sort()).toEqual(['secret.img', 'source.qcow2'])
	})
	test('rejects too-small target disks and cancellation without leaving imported data', async () => {
		const {directory, source, destination} = await fixture()
		await execa('qemu-img', ['create', '-f', 'qcow2', source, '2G'])
		await expect(convertCustomQcow2(source, destination, 1, new AbortController().signal)).rejects.toThrow(
			'disk-too-small',
		)
		const controller = new AbortController()
		controller.abort(new Error('cancelled'))
		await expect(convertCustomQcow2(source, destination, 3, controller.signal)).rejects.toThrow('cancelled')
		expect(await fsp.readdir(directory)).toEqual(['source.qcow2'])
	})
})
