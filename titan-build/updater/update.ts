import {$} from 'execa'
import type {ProgressStatus} from '../apps/schema.js'
import type Titand from '../../index.js'

type UpdateStatus = ProgressStatus
type Release = {version: string; name: string; releaseNotes: string}
const helper = '/usr/libexec/titan-system-update.py'
const cached = new Map<string, {expires: number; release: Release}>()
const pending = new Map<string, Promise<Release>>()
let updateStatus: UpdateStatus
resetUpdateStatus()

function resetUpdateStatus() {
	updateStatus = {running: false, progress: 0, description: '', error: false}
}

function setUpdateStatus(properties: Partial<UpdateStatus>) {
	updateStatus = {...updateStatus, ...properties}
}

export function getUpdateStatus() {
	return updateStatus
}

export async function getLatestRelease(titand: Titand): Promise<Release> {
	const channel = 'stable'
	const key = `${titand.version}:${channel}`
	const previous = cached.get(key)
	if (previous && previous.expires > Date.now()) return previous.release
	const inFlight = pending.get(key)
	if (inFlight) return inFlight
	const request = (async () => {
		const {stdout} = await $`/usr/bin/python3 ${helper} check --current-version ${titand.version} --channel ${channel}`
		const release = JSON.parse(stdout) as Release
		if (typeof release.version !== 'string' || typeof release.name !== 'string' || typeof release.releaseNotes !== 'string') {
			throw new Error('Ungültige Antwort des Titan-Updaters')
		}
		// Cache verified checks; concurrent callers share the same request.
		// Installation independently checks metadata and signatures again.
		cached.set(key, {expires: Date.now() + 5 * 60 * 1000, release})
		return release
	})()
	pending.set(key, request)
	try {
		return await request
	} finally {
		pending.delete(key)
	}
}

export async function performUpdate(titand: Titand) {
	if (updateStatus.running) throw new Error('Ein Systemupdate läuft bereits')
	setUpdateStatus({running: true, progress: 0, description: 'Titan-Update prüfen…', error: false})
	try {
		const channel = 'stable'
		const release = await getLatestRelease(titand)
		if (release.version === titand.version) throw new Error('Kein neueres Titan-Systemupdate verfügbar')
		const process = $`/usr/bin/python3 ${helper} install --current-version ${titand.version} --channel ${channel} --version ${release.version}`
		const buffers = {stdout: '', stderr: ''}
		function handleOutput(stream: 'stdout' | 'stderr', chunk: Buffer) {
			buffers[stream] += chunk.toString()
			if (buffers[stream].length > 65536) buffers[stream] = ''
			let newline: number
			while ((newline = buffers[stream].indexOf('\n')) !== -1) {
				const line = buffers[stream].slice(0, newline)
				buffers[stream] = buffers[stream].slice(newline + 1)
				try {
					if (line.startsWith('titan-update: ')) {
						const status = JSON.parse(line.slice('titan-update: '.length)) as Partial<UpdateStatus>
						if (typeof status.description === 'string') setUpdateStatus({description: status.description})
						if (typeof status.error === 'string') setUpdateStatus({error: status.error})
						if (typeof status.progress === 'number' && Number.isFinite(status.progress)) {
							setUpdateStatus({progress: Math.min(100, Math.max(0, status.progress))})
						}
					} else {
						const status = JSON.parse(line) as {event: string; progress: number}
						if (status.event === 'UpdateProgress' && Number.isFinite(status.progress)) {
							setUpdateStatus({progress: Math.min(99, 40 + Math.max(0, status.progress) * 0.59)})
						}
					}
				} catch {
					// Normal Rugix diagnostic output is not a progress event.
				}
			}
		}
		process.stdout?.on('data', (chunk: Buffer) => handleOutput('stdout', chunk))
		process.stderr?.on('data', (chunk: Buffer) => handleOutput('stderr', chunk))
		await process
		cached.clear()
	} catch (error) {
		const message = updateStatus.error || (error instanceof Error ? error.message : 'Titan-Systemupdate fehlgeschlagen')
		resetUpdateStatus()
		setUpdateStatus({error: message})
		titand.logger.error('Titan system update failed', error)
		return false
	}
	setUpdateStatus({running: false, progress: 100, description: 'Neustart…'})
	return true
}
