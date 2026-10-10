import {z} from 'zod'

// ---------------------------------------------------------------------------
// Snapshots
// ---------------------------------------------------------------------------

// A saved state of a machine's system disk. The data lives inside the qcow2
// image as an internal snapshot; the UEFI variables of that moment are kept
// next to the machine. Shared folders and additional disks are not part of it.
export const machineSnapshotSchema = z.object({
	id: z.string().regex(/^[a-f0-9]{12}$/),
	name: z.string().min(1).max(60),
	createdAt: z.number().int(),
	diskSizeGb: z.number().int().min(1).max(10_000),
})
export type MachineSnapshot = z.infer<typeof machineSnapshotSchema>
export const MAX_MACHINE_SNAPSHOTS = 10

// The snapshot's name inside the qcow2 image
export const snapshotTag = (id: string) => `titan-${id}`

export const snapshotNvramFileName = (id: string) => `${id}.nvram.fd`

// Explicit image options instead of a bare filename: qemu-img then opens the
// disk as qcow2 and never probes what a guest may have written into it.
export function qcow2ImageOptions(path: string) {
	return `driver=qcow2,file.driver=file,file.filename=${path.replaceAll(',', ',,')}`
}

// `qemu-img info --output=json` lists internal snapshots under "snapshots"
export function parseSnapshotTags(info: string) {
	try {
		const snapshots = (JSON.parse(info) as {snapshots?: unknown}).snapshots
		if (!Array.isArray(snapshots)) return []
		return snapshots.flatMap((snapshot) =>
			typeof (snapshot as {name?: unknown})?.name === 'string' ? [(snapshot as {name: string}).name] : [],
		)
	} catch {
		return []
	}
}

// ---------------------------------------------------------------------------
// Autostart
// ---------------------------------------------------------------------------

export const MAX_AUTOSTART_DELAY_SECONDS = 3_600

// When each autostart machine comes up after titanOS is ready: the ones
// without a delay first, the others in the order of their delay.
export function autostartSchedule(
	definitions: {id: string; autostart: boolean; autostartDelaySeconds?: number; createdAt: number}[],
) {
	return definitions
		.filter((definition) => definition.autostart)
		.map(({id, autostartDelaySeconds, createdAt}) => ({
			id,
			createdAt,
			delayMs: Math.min(MAX_AUTOSTART_DELAY_SECONDS, Math.max(0, autostartDelaySeconds ?? 0)) * 1_000,
		}))
		.sort((a, b) => a.delayMs - b.delayMs || a.createdAt - b.createdAt)
		.map(({id, delayMs}) => ({id, delayMs}))
}
