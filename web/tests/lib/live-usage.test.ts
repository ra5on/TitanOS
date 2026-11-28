// §121: Live-Nutzung – CPU of Docker, VMs and system containers on one scale.
import { expect, test } from 'vitest';
import type { DockerContainer, VM } from '../../src/lib/api.ts';
import type { LXCList } from '../../src/lib/lxc.ts';
import { dockerShare, lxcShare, usageRows, vmShare } from '../../src/lib/live-usage.svelte.ts';

const system = (usage: number, threads = 8) => ({
	cpu: { model: 'x', threads, usagePercent: usage },
	memory: { totalBytes: 16e9, usedBytes: 0, freeBytes: 0, availableBytes: 10e9 },
});
const container = (app: string, cpu: number, mem: number, state = 'running') =>
	({
		name: app + '-1',
		app,
		state,
		cpuPercent: cpu,
		memoryBytes: mem,
	}) as unknown as DockerContainer;
const vm = (name: string, cpu: number, vcpus: number) =>
	({ name, state: 'running', cpuPercent: cpu, vcpus, memoryBytes: 2e9 }) as unknown as VM;

test('the three scales become a share of the whole NAS', () => {
	// docker stats: 200 % = two of eight threads busy.
	expect(dockerShare(200, 8)).toBe(25);
	// A VM with 2 vCPUs at 50 % uses one of eight threads.
	expect(vmShare(50, 2, 8)).toBe(12.5);
	// A container limited to 4 CPUs at 50 %, or one without limit.
	expect(lxcShare(50, 4, 8)).toBe(25);
	expect(lxcShare(50, undefined, 8)).toBe(50);
	expect(dockerShare(Number.NaN, 8)).toBe(0);
});

test('apps group their containers; System is the rest and never negative', () => {
	const rows = usageRows(
		system(40),
		[
			container('immich', 80, 1e9),
			container('immich', 40, 5e8),
			container('old', 90, 1e9, 'exited'),
		],
		[vm('ha', 50, 2)],
		[] as LXCList['containers'],
	);
	const immich = rows.find((r) => r.id === 'docker:immich')!;
	expect(immich.count).toBe(2);
	expect(immich.cpu).toBe(15);
	expect(immich.memoryBytes).toBe(1.5e9);
	expect(rows.some((r) => r.name === 'old')).toBe(false);
	const sys = rows.find((r) => r.kind === 'system')!;
	expect(sys.cpu).toBe(12.5);
	// Sorted by CPU: the app first.
	expect(rows[0].id).toBe('docker:immich');

	const busy = usageRows(system(5), [container('a', 800, 0)], [], []);
	expect(busy.find((r) => r.kind === 'system')!.cpu).toBe(0);
});

test('sorting by memory', () => {
	const rows = usageRows(
		system(10),
		[container('a', 10, 3e9), container('b', 70, 1e9)],
		[],
		[],
		'memory',
	);
	// 6 GB in use: 3 + 1 GB in apps, 2 GB for the NAS itself.
	expect(rows.map((r) => r.id)).toEqual(['docker:a', 'system', 'docker:b']);
});
