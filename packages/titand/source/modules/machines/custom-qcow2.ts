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

// A user namespace maps only the calling UID. Host root's DAC override does
// not let sandbox setup traverse a different user's private home directory.
// Find an accessible ancestor on the same filesystem; never relax NAS rights
// or stage a potentially large disk on the system partition or a RAM tmpfs.
async function sandboxStageParent(destination: string) {
	const parents: {path: string; dev: number; traversable: boolean}[] = []
	let path = nodePath.dirname(destination)
	for (;;) {
		const stat = await fsp.stat(path)
		parents.push({
			path,
			dev: stat.dev,
			traversable: (stat.uid === process.getuid?.() && (stat.mode & 0o100) !== 0) || (stat.mode & 0o001) !== 0,
		})
		const parent = nodePath.dirname(path)
		if (parent === path) break
		path = parent
	}
	for (let index = 0; index < parents.length; index++) {
		const candidate = parents[index]
		if (candidate.dev !== parents[0].dev || !parents.slice(index).every((parent) => parent.traversable)) continue
		try {
			await fsp.access(candidate.path, fsp.constants.W_OK)
			return candidate.path
		} catch {
			// Try the next accessible ancestor on this data filesystem.
		}
	}
	throw new Error('No private QCOW2 staging location is accessible on the destination filesystem')
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
	const scratch = await fsp.mkdtemp(nodePath.join(await sandboxStageParent(destination), '.qcow2-import-'))
	const snapshot = nodePath.join(scratch, 'source')
	const privateOutput = nodePath.join(scratch, 'disk.qcow2')
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
		const info = await execa(
			'/usr/bin/bwrap',
			[...qcow2SandboxArguments(snapshot), 'info', '-f', 'qcow2', '--output=json', '/work/source'],
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
		const conversion = execa(
			'/usr/bin/bwrap',
			[
				...qcow2SandboxArguments(snapshot, privateOutput),
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
		)
		conversion.stderr?.on('data', (data: Buffer) => {
			for (const match of data.toString().matchAll(/\((\d+(?:\.\d+)?)\/100%\)/g)) onProgress?.(Number(match[1]))
		})
		await conversion
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
		await fsp.rm(scratch, {recursive: true, force: true})
	}
}
