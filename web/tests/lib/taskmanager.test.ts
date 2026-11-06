import { expect, test } from 'vitest';
import de from '#lib/i18n/features/taskmanager.de.json';
import en from '#lib/i18n/features/taskmanager.en.json';
import {
	memberDisk,
	pushHistory,
	scaleToPeak,
	shortCommand,
	sortProcesses,
	volumeRates,
	type ProcessRow,
} from '#lib/taskmanager.ts';

const row = (
	pid: number,
	name: string,
	cpu: number,
	mem: number,
	extra: Partial<ProcessRow> = {},
): ProcessRow => ({
	pid,
	ppid: 1,
	name,
	command: `/usr/bin/${name}`,
	user: 'root',
	state: 'S',
	cpuPercent: cpu,
	memoryBytes: mem,
	start: pid * 10,
	owner: { kind: '' },
	...extra,
});
const rows = [
	row(10, 'smbd', 2, 50),
	row(20, 'Immich', 30, 900, { owner: { kind: 'docker', id: 'ab', name: 'immich_server' } }),
	row(30, 'bash', 0, 10, { user: 'alex' }),
	row(40, 'qemu', 30, 4000),
];

test('sorts by CPU, memory and name', () => {
	expect(sortProcesses(rows, 'cpu').map((p) => p.pid)).toEqual([40, 20, 10, 30]);
	expect(sortProcesses(rows, 'memory').map((p) => p.pid)).toEqual([40, 20, 10, 30]);
	expect(sortProcesses(rows, 'name').map((p) => p.name)).toEqual([
		'bash',
		'Immich',
		'qemu',
		'smbd',
	]);
	expect(rows.map((p) => p.pid)).toEqual([10, 20, 30, 40]);
});

test('searches name, command, user, owner and pid', () => {
	expect(sortProcesses(rows, 'cpu', 'IMMICH_S').map((p) => p.pid)).toEqual([20]);
	expect(sortProcesses(rows, 'cpu', 'alex').map((p) => p.pid)).toEqual([30]);
	expect(sortProcesses(rows, 'cpu', ' 40 ').map((p) => p.pid)).toEqual([40]);
	expect(sortProcesses(rows, 'cpu', 'usr/bin/smb').map((p) => p.pid)).toEqual([10]);
	expect(sortProcesses(rows, 'cpu', 'nichts')).toEqual([]);
});

test('histories and sparkline scaling', () => {
	let h: number[] = [];
	for (let i = 0; i < 50; i++) h = pushHistory(h, i, 40);
	expect(h).toHaveLength(40);
	expect(h.at(-1)).toBe(49);
	expect(pushHistory([], Number.NaN)).toEqual([0]);
	expect(scaleToPeak([0, 5, 10])).toEqual([0, 50, 100]);
	expect(scaleToPeak([0, 0])).toEqual([0, 0]);
});

test('commands are shortened', () => {
	expect(shortCommand('a b')).toBe('a b');
	const long = shortCommand('x'.repeat(200), 20);
	expect(long).toHaveLength(20);
	expect(long.endsWith('…')).toBe(true);
});

test('volume throughput sums member disks', () => {
	const io = [
		{ name: 'sda', readBytesPerSecond: 100, writeBytesPerSecond: 10, busyPercent: 1 },
		{ name: 'sdb', readBytesPerSecond: 50, writeBytesPerSecond: 10, busyPercent: 1 },
		{ name: 'nvme0n1', readBytesPerSecond: 7, writeBytesPerSecond: 3, busyPercent: 1 },
	];
	expect(memberDisk('/dev/sda1', ['sda', 'sdb'])).toBe('sda');
	expect(memberDisk('nvme0n1p2', ['nvme0n1', 'nvme0n10'])).toBe('nvme0n1');
	expect(memberDisk('sdz', ['sda'])).toBe('');
	expect(
		volumeRates(
			[
				{
					name: 'Data',
					members: [
						{ disk: 'sda', state: 'active' },
						{ disk: 'sdb1', state: 'active' },
					],
				},
				{ name: 'Fast', members: [{ disk: 'nvme0n1p1', state: 'active' }] },
				{ name: 'Gone', members: [{ disk: 'sdq', state: 'missing' }] },
			] as never,
			io,
		),
	).toEqual([
		{ name: 'Data', read: 150, write: 20, known: true },
		{ name: 'Fast', read: 7, write: 3, known: true },
		{ name: 'Gone', read: 0, write: 0, known: false },
	]);
});

test('both languages have the same texts', () => {
	expect(Object.keys(en).sort()).toEqual(Object.keys(de).sort());
	for (const key of Object.keys(de)) expect(key.startsWith('taskmanager.')).toBe(true);
});
