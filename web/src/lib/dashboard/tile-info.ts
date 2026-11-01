import type { StorageStatus, SystemInfo } from '#lib/api.ts';
import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';
import { dockerName } from '#lib/docker-name.ts';
import { vmLabel } from '#lib/vm-name.ts';
import { dockerGroups } from '#lib/docker-layout.ts';
import { formatBytes } from '#lib/format.ts';
import { cpuTemperature, tileTemperatures } from './temperatures.ts';
import { tileSpec, workload, sourceId, type TileId } from './tiles.ts';
import { tileOptions, type TileOptions } from './options.ts';
import type { DashboardData } from './dashboard-data.svelte.ts';

/** What the texts of a tile are made of. */
export type TileContext = {
	options: Record<string, TileOptions>;
	data: DashboardData;
	info: SystemInfo | null | undefined;
	storage: StorageStatus | null | undefined;
	dashboard: Record<string, unknown> | null;
};

/** The title of a tile: its own, the name of its workload or the name of its kind. */
export function tileTitle(id: TileId, { options, data }: Pick<TileContext, 'options' | 'data'>) {
	const o = tileOptions(options, id);
	if (o.title) return o.title;
	const target = workload(id);
	if (target) {
		const vm = target.kind === 'vm' ? data.vms.find((v) => v.name === target.name) : undefined;
		const container =
			target.kind === 'container' ? data.containers.find((v) => v.name === target.name) : undefined;
		return vm ? vmLabel(vm) : container ? dockerName(container, 'distinct') : target.name;
	}
	return t(tileSpec(id).title);
}

/** The short summary shown in the head of a tile. */
export function tileSummary(id: TileId, { options, data, info, storage, dashboard }: TileContext) {
	const kind = sourceId(id);
	if (kind === 'system') return info ? t('dashboard.online') : '–';
	if (kind === 'docker' || kind.startsWith('app:')) {
		if (!data.containersReady || data.dockerError) return '–';
		const cs = kind.startsWith('app:')
			? data.containers.filter((c) => c.app === kind.slice(4))
			: data.containers;
		const gs = dockerGroups(cs);
		return t('dashboard.appSummary', {
			count: gs.length,
			running: gs.filter((g) => g.members.some((c) => c.state === 'running')).length,
		});
	}
	if (kind === 'vms')
		return data.vmsReady && !data.vmError
			? t('dashboard.runningSummary', {
					running: data.vms.filter((v) => v.state === 'running').length,
					count: data.vms.length,
				})
			: '–';
	if (kind === 'cpu') {
		if (!tileOptions(options, id).temperature) return '';
		const temp = cpuTemperature(info?.temperatures, tileOptions(options, id).sensor);
		return temp?.celsius == null ? '' : `${temp.celsius.toFixed(0)} °C`;
	}
	if (kind === 'memory') return info ? formatBytes(info.memory.totalBytes) : '–';
	if (kind === 'volumes')
		return storage
			? t('dashboard.mountedSummary', {
					count: storage.volumes.filter((v) => v.mounted).length,
				})
			: '–';
	if (kind === 'shares')
		return data.sharesReady && !data.sharesError
			? t('dashboard.shareSummary', { count: data.shares.length })
			: '–';
	if (kind === 'disks')
		return storage
			? t('dashboard.deviceSummary', {
					count: storage.disks.filter((d) => d.role !== 'stick').length,
				})
			: '–';
	if (['accelerators', 'igpu', 'nvidia', 'npu'].includes(kind))
		return info
			? t('dashboard.deviceSummary', {
					count: info.accelerators.filter(
						(d) =>
							kind === 'accelerators' ||
							(kind === 'npu'
								? d.kind === 'npu'
								: kind === 'nvidia'
									? d.vendor === 'NVIDIA' && d.kind !== 'npu'
									: d.kind !== 'npu' && d.vendor !== 'NVIDIA'),
					).length,
				})
			: '–';
	if (kind === 'cluster') {
		const page = data.cluster;
		if (!page || data.clusterError) return '–';
		if (!page.cluster.name) return t('tile.clusterNone');
		const o = tileOptions(options, id),
			list = page.nodes.filter((n) => o.items === null || o.items.includes(n.id));
		return t('tile.clusterSummary', {
			online: list.filter((n) => n.online).length,
			count: list.length,
		});
	}
	if (kind === 'netbird') {
		const peers = data.netbirdPeers;
		if (!peers || data.netbirdPeersError) return '–';
		if (!peers.active) return t('netbird.states.inactive');
		const o = tileOptions(options, id),
			list = peers.peers.filter((p) => o.items === null || o.items.includes(p.id));
		return t('netbird.peer.summary', {
			online: list.filter((p) => p.online === true).length,
			count: list.length,
		});
	}
	if (kind === 'tailscale') {
		const devices = data.tailnetDevices;
		if (!devices || data.tailnetDevicesError) return '–';
		if (!devices.active) return t('tailscale.states.inactive');
		const o = tileOptions(options, id),
			list = devices.devices.filter((d) => o.items === null || o.items.includes(d.id));
		return t('tailscale.device.summary', {
			online: list.filter((d) => d.online === true).length,
			count: list.length,
		});
	}
	if (kind === 'temperatures')
		return t('dashboard.sensorSummary', {
			count: tileTemperatures(dashboard, tileOptions(options, id).items, info?.temperatures ?? [])
				.length,
		});
	if (kind === 'network') {
		const o = tileOptions(options, id),
			n = o.interface ? info?.networks.find((n) => n.interface === o.interface) : info?.network;
		return n
			? `${n.interface || '–'}${n.speedMbps > 0 ? ' · ' + n.speedMbps + ' Mbit/s' : ''}`
			: '–';
	}
	return '';
}

/** The entries a tile of this kind can show, for the selection in the inspector. */
export function inspectorItems(
	id: TileId,
	{ data, info, storage }: Pick<TileContext, 'data' | 'info' | 'storage'>,
): { id: string; name: string }[] {
	switch (sourceId(id)) {
		case 'docker':
			return dockerGroups(data.containers).map((g) => ({ id: g.id, name: g.name }));
		case 'vms':
			return data.vms.map((v) => ({ id: v.name, name: vmLabel(v) }));
		case 'shares':
			return data.shares.map((s) => ({ id: s.name, name: s.name }));
		case 'volumes':
			return (storage?.volumes ?? []).map((v) => ({ id: v.name, name: v.name }));
		case 'cluster':
			return (data.cluster?.nodes ?? []).map((n) => ({ id: n.id, name: n.name }));
		case 'netbird':
			return (data.netbirdPeers?.peers ?? []).map((p) => ({
				id: p.id,
				name: p.self ? `${p.name} · ${t('netbird.peer.self')}` : p.name || p.fqdn,
			}));
		case 'tailscale':
			return (data.tailnetDevices?.devices ?? []).map((d) => ({
				id: d.id,
				name: d.self ? `${d.name} · ${t('tailscale.device.self')}` : d.name || d.hostName,
			}));
		case 'temperatures':
			return (info?.temperatures ?? []).map((s) => ({
				id: s.id,
				name: `${s.label} · ${s.chip}`,
			}));
		case 'disks':
			return (storage?.disks ?? [])
				.filter((d) => d.role !== 'stick')
				.map((d) => ({ id: d.identity || d.serial || d.name, name: d.name }));
		default:
			return [];
	}
}

export type WorkloadGroup = { label: MessageKey; choices: { id: TileId; name: string }[] };

/**
 * Stacks, VMs and single containers in their own groups; entries already on
 * the dashboard are marked, adding them again creates an independent copy.
 */
export function workloadGroups(data: DashboardData): WorkloadGroup[] {
	return (
		[
			{
				label: 'docker.stack',
				choices: dockerGroups(data.containers)
					.filter((g) => g.id.startsWith('app:'))
					.map((g) => ({ id: g.id as TileId, name: g.name })),
			},
			{
				label: 'nav.vms',
				choices: data.vms.map((vm) => ({ id: `vm:${vm.name}` as TileId, name: vmLabel(vm) })),
			},
			{
				label: 'tile.container',
				choices: data.containers.map((c) => ({
					id: `container:${c.name}` as TileId,
					name: dockerName(c, 'distinct'),
				})),
			},
		] satisfies WorkloadGroup[]
	).filter((group) => group.choices.length);
}
