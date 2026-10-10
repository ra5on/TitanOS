import {describe, expect, test} from 'vitest'

import {
	autostartSchedule,
	machineSnapshotSchema,
	parseSnapshotTags,
	qcow2ImageOptions,
	snapshotNvramFileName,
	snapshotTag,
} from './machine-snapshots.js'

describe('machine snapshots', () => {
	test('names a snapshot inside the image and its saved firmware variables', () => {
		expect(snapshotTag('0123456789ab')).toBe('titan-0123456789ab')
		expect(snapshotNvramFileName('0123456789ab')).toBe('0123456789ab.nvram.fd')
	})

	test('opens the disk as qcow2 and escapes commas in its path', () => {
		expect(qcow2ImageOptions('/data/machines/ha/disk.qcow2')).toBe(
			'driver=qcow2,file.driver=file,file.filename=/data/machines/ha/disk.qcow2',
		)
		expect(qcow2ImageOptions('/mnt/My Disk, old/vm.qcow2')).toBe(
			'driver=qcow2,file.driver=file,file.filename=/mnt/My Disk,, old/vm.qcow2',
		)
	})

	test('reads the snapshot names qemu-img reports', () => {
		const info = JSON.stringify({
			'virtual-size': 1_073_741_824,
			snapshots: [
				{id: '1', name: 'titan-0123456789ab', 'vm-state-size': 0},
				{id: '2', name: 'made-by-hand'},
				{id: '3'},
			],
		})
		expect(parseSnapshotTags(info)).toEqual(['titan-0123456789ab', 'made-by-hand'])
		expect(parseSnapshotTags(JSON.stringify({'virtual-size': 1}))).toEqual([])
		expect(parseSnapshotTags('not json')).toEqual([])
	})

	test('accepts only well-formed snapshot records', () => {
		const snapshot = {id: '0123456789ab', name: 'Before the update', createdAt: 1, diskSizeGb: 32}
		expect(machineSnapshotSchema.parse(snapshot)).toEqual(snapshot)
		expect(machineSnapshotSchema.safeParse({...snapshot, id: '../escape'}).success).toBe(false)
		expect(machineSnapshotSchema.safeParse({...snapshot, name: ''}).success).toBe(false)
	})
})

describe('autostart schedule', () => {
	test('starts machines without a delay first and the others by their delay', () => {
		expect(
			autostartSchedule([
				{id: 'late', autostart: true, autostartDelaySeconds: 120, createdAt: 1},
				{id: 'off', autostart: false, autostartDelaySeconds: 0, createdAt: 2},
				{id: 'second', autostart: true, createdAt: 4},
				{id: 'first', autostart: true, autostartDelaySeconds: 0, createdAt: 3},
				{id: 'soon', autostart: true, autostartDelaySeconds: 30, createdAt: 5},
			]),
		).toEqual([
			{id: 'first', delayMs: 0},
			{id: 'second', delayMs: 0},
			{id: 'soon', delayMs: 30_000},
			{id: 'late', delayMs: 120_000},
		])
	})

	test('never waits longer than an hour', () => {
		expect(autostartSchedule([{id: 'a', autostart: true, autostartDelaySeconds: 99_999, createdAt: 1}])).toEqual([
			{id: 'a', delayMs: 3_600_000},
		])
	})
})
