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

export async function convertCustomQcow2(
	source: string,
	destination: string,
	sizeGb: number,
	signal: AbortSignal,
	onProgress?: (percent: number) => void,
) {
	if (signal.aborted) throw signal.reason
	// Work on a private snapshot so an uploader cannot change a validated header
	// before conversion. Keep this on disk alongside the staging output, not RAM.
	const scratch = await fsp.mkdtemp(nodePath.join(nodePath.dirname(destination), '.qcow2-import-'))
	const snapshot = nodePath.join(scratch, 'source')
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
		const conversion = execa(
			'/usr/bin/bwrap',
			[
				...qcow2SandboxArguments(snapshot, destination),
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
			['resize', '-f', 'qcow2', destination, `${sizeGb}G`],
			installCommandOptions(signal, MACHINE_INSTALL_SHORT_COMMAND_TIMEOUT_MS),
		)
		onProgress?.(100)
	} catch (error) {
		if (outputCreated) await fsp.rm(destination, {force: true})
		throw error
	} finally {
		await fsp.rm(scratch, {recursive: true, force: true})
	}
}
