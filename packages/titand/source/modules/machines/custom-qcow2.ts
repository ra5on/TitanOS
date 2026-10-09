import fsp from 'node:fs/promises'
import nodePath from 'node:path'
import {execa} from 'execa'
import {installCommandOptions, MACHINE_INSTALL_SHORT_COMMAND_TIMEOUT_MS} from './install-command.js'

// Only the private copy and output are exposed. No host /etc, /home, /run,
// network, credentials, host devices or other machine directories are mounted.
export function qcow2SandboxArguments(source: string, destination?: string) {
	return [
		'--unshare-all',
		'--die-with-parent',
		'--new-session',
		'--cap-drop',
		'ALL',
		'--clearenv',
		'--ro-bind',
		'/usr',
		'/usr',
		'--symlink',
		'usr/lib',
		'/lib',
		'--symlink',
		'usr/lib64',
		'/lib64',
		'--proc',
		'/proc',
		'--dev',
		'/dev',
		'--tmpfs',
		'/tmp',
		'--dir',
		'/work',
		'--ro-bind',
		source,
		'/work/source',
		...(destination ? ['--bind', destination, '/work/destination'] : []),
		'--chdir',
		'/work',
		'--',
		'/usr/bin/qemu-img',
	]
}

export function validateQcow2Header(header: Buffer) {
	if (header.length < 104 || header.readUInt32BE(0) !== 0x514649fb || ![2, 3].includes(header.readUInt32BE(4)))
		throw new Error('[machine-image-invalid]')
	if (header.readBigUInt64BE(8) !== 0n || header.readUInt32BE(16) !== 0)
		throw new Error('[machine-image-backing-chain-not-supported]')
	if (header.readUInt32BE(32) !== 0) throw new Error('[machine-image-invalid]') // encrypted images need secrets
	if (header.readUInt32BE(4) === 3 && (header.readBigUInt64BE(72) & 4n) !== 0n)
		throw new Error('[machine-image-external-data-not-supported]')
}

// A user namespace maps only the calling UID, so host root's DAC override does
// not let sandbox setup traverse a NAS home or data directory owned by another
// UID. Bind the private scratch directory under root-owned /run instead: the
// data stays on the destination filesystem and NAS rights stay untouched.
// Without mount rights (unprivileged development runs) use the path directly.
async function sandboxView(scratch: string) {
	let view: string
	try {
		view = await fsp.mkdtemp('/run/titan-qcow2-import-')
	} catch {
		return {path: scratch, release: async () => {}}
	}
	try {
		await execa('/usr/bin/mount', ['--bind', scratch, view])
	} catch {
		await fsp.rmdir(view).catch(() => {})
		return {path: scratch, release: async () => {}}
	}
	// Keep the temporary view out of other mount namespaces
	await execa('/usr/bin/mount', ['--make-private', view]).catch(() => {})
	return {
		path: view,
		release: async () => {
			await execa('/usr/bin/umount', [view])
			await fsp.rmdir(view)
		},
	}
}

// A qemu-img failure without a Titan error code is an import failure. The raw
// tool output stays in the daemon log.
async function sandboxedQemuImg(
	args: string[],
	options: ReturnType<typeof installCommandOptions>,
	onOutput?: (data: Buffer) => void,
) {
	try {
		const command = execa('/usr/bin/bwrap', args, options)
		if (onOutput) {
			// qemu-img prints progress on stdout; older builds used stderr
			command.stdout?.on('data', onOutput)
			command.stderr?.on('data', onOutput)
		}
		return await command
	} catch (error) {
		if (options.signal.aborted) throw options.signal.reason
		throw new Error(`[machine-image-import-failed] ${error instanceof Error ? error.message : String(error)}`)
	}
}

export async function convertCustomQcow2(
	source: string,
	destination: string,
	sizeGb: number,
	signal: AbortSignal,
	onProgress?: (percent: number) => void,
) {
	if (signal.aborted) throw signal.reason
	// Work on a private snapshot so an uploader cannot change a validated header
	// before conversion. Keep source and output on the destination filesystem.
	const scratch = await fsp.mkdtemp(nodePath.join(nodePath.dirname(destination), '.qcow2-import-'))
	const snapshot = nodePath.join(scratch, 'source')
	const privateOutput = nodePath.join(scratch, 'disk.qcow2')
	let view: Awaited<ReturnType<typeof sandboxView>> | undefined
	let outputCreated = false
	try {
		await fsp.copyFile(source, snapshot)
		await fsp.chmod(snapshot, 0o400)
		if (signal.aborted) throw signal.reason
		const handle = await fsp.open(snapshot, 'r')
		try {
			const header = Buffer.alloc(104)
			const {bytesRead} = await handle.read(header, 0, header.length, 0)
			validateQcow2Header(header.subarray(0, bytesRead))
		} finally {
			await handle.close()
		}
		view = await sandboxView(scratch)
		const sandboxSource = nodePath.join(view.path, 'source')
		const sandboxOutput = nodePath.join(view.path, 'disk.qcow2')
		const info = await sandboxedQemuImg(
			[...qcow2SandboxArguments(sandboxSource), 'info', '-f', 'qcow2', '--output=json', '/work/source'],
			installCommandOptions(signal, MACHINE_INSTALL_SHORT_COMMAND_TIMEOUT_MS),
		)
		const details = JSON.parse(info.stdout) as {'virtual-size': number; 'backing-filename'?: string}
		if (details['backing-filename']) throw new Error('[machine-image-backing-chain-not-supported]')
		if (!Number.isSafeInteger(details['virtual-size']) || details['virtual-size'] <= 0)
			throw new Error('[machine-image-invalid]')
		if (details['virtual-size'] > sizeGb * 1024 ** 3) throw new Error('[machine-disk-too-small]')
		await fsp.writeFile(destination, '', {flag: 'wx', mode: 0o600})
		outputCreated = true
		await fsp.writeFile(privateOutput, '', {flag: 'wx', mode: 0o600})
		await sandboxedQemuImg(
			[
				...qcow2SandboxArguments(sandboxSource, sandboxOutput),
				'convert',
				'-p',
				'-f',
				'qcow2',
				'-O',
				'qcow2',
				'/work/source',
				'/work/destination',
			],
			installCommandOptions(signal),
			(data) => {
				for (const match of data.toString().matchAll(/\((\d+(?:\.\d+)?)\/100%\)/g)) onProgress?.(Number(match[1]))
			},
		)
		await execa(
			'/usr/bin/qemu-img',
			['resize', '-f', 'qcow2', privateOutput, `${sizeGb}G`],
			installCommandOptions(signal, MACHINE_INSTALL_SHORT_COMMAND_TIMEOUT_MS),
		)
		if (signal.aborted) throw signal.reason
		await fsp.rename(privateOutput, destination)
		onProgress?.(100)
	} catch (error) {
		if (outputCreated) await fsp.rm(destination, {force: true})
		throw error
	} finally {
		try {
			await view?.release()
		} finally {
			await fsp.rm(scratch, {recursive: true, force: true})
		}
	}
}
