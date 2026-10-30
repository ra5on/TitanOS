<script lang="ts">
	import type { Component } from 'svelte';
	import { tailnetHost } from '#lib/app-links.ts';
	import { workload, sourceId, type StaticTileId, type TileId } from './tiles.ts';
	import type { TileOptions } from './options.ts';
	import type { DashboardData } from './dashboard-data.svelte.ts';
	import UsageTile from './tiles/UsageTile.svelte';
	import AcceleratorTile from './tiles/AcceleratorTile.svelte';
	import WorkloadTile from './tiles/WorkloadTile.svelte';
	import CollectionTile from './tiles/CollectionTile.svelte';
	import SystemTile from './tiles/SystemTile.svelte';
	import NetworkTile from './tiles/NetworkTile.svelte';
	import DisksTile from './tiles/DisksTile.svelte';
	import VolumesTile from './tiles/VolumesTile.svelte';
	import TemperaturesTile from './tiles/TemperaturesTile.svelte';
	import AssistantTile from './tiles/AssistantTile.svelte';
	import TailscaleTile from './tiles/TailscaleTile.svelte';
	import NetbirdTile from './tiles/NetbirdTile.svelte';

	// The content of a dashboard tile, picked by its kind.
	const CONTENT: Partial<Record<StaticTileId, Component<{ options?: TileOptions }>>> = {
		volumes: VolumesTile,
		disks: DisksTile,
		temperatures: TemperaturesTile,
		assistant: AssistantTile,
	};
	let {
		id,
		settings,
		data,
		admin,
		editing,
		saving,
		onoption,
		onpin,
	}: {
		id: TileId;
		settings: TileOptions;
		data: DashboardData;
		admin: boolean;
		editing: boolean;
		saving: boolean;
		onoption: (key: keyof TileOptions, value: unknown) => void;
		onpin: (id: TileId) => void;
	} = $props();
	const source = $derived(sourceId(id));
	const target = $derived(workload(id));
</script>

{#if target?.kind === 'app'}<CollectionTile
		kind="docker"
		kvm={data.kvm}
		{onoption}
		disabled={saving}
		options={settings}
		containers={data.containers.filter((c) => c.app === target.name)}
		ready={data.containersReady}
		error={data.dockerError}
		{admin}
		tailscaleHost={tailnetHost(data.tailnet)}
		{editing}
		onrefresh={data.refresh}
		{onpin}
	/>
{:else if target}<WorkloadTile
		kind={target.kind}
		name={target.name}
		options={settings}
		{editing}
		kvm={data.kvm}
		tailscaleHost={tailnetHost(data.tailnet)}
		vm={data.vms.find((v) => v.name === target.name)}
		container={data.containers.find((c) => c.name === target.name)}
		onrefresh={data.refresh}
		ready={target.kind === 'vm' ? data.vmsReady : data.containersReady}
		error={target.kind === 'vm' ? data.vmError : data.dockerError}
		{admin}
	/>
{:else if source === 'cpu' || source === 'memory'}<UsageTile
		kind={source}
		options={settings}
		ondetails={() => onoption('details', !settings.details)}
	/>
{:else if source === 'network'}<NetworkTile options={settings} />
{:else if source === 'netbird'}<NetbirdTile
		options={settings}
		value={data.netbirdPeers}
		error={data.netbirdPeersError}
	/>
{:else if source === 'tailscale'}<TailscaleTile
		options={settings}
		value={data.tailnetDevices}
		error={data.tailnetDevicesError}
	/>
{:else if source === 'system'}<SystemTile options={settings} />
{:else if source === 'igpu' || source === 'npu' || source === 'nvidia' || source === 'accelerators'}<AcceleratorTile
		kind={source}
		options={settings}
	/>
{:else if source === 'docker' || source === 'vms' || source === 'shares'}<CollectionTile
		kind={source}
		kvm={data.kvm}
		{onoption}
		disabled={saving}
		options={settings}
		containers={data.containers}
		vms={data.vms}
		shares={data.shares}
		ready={source === 'docker'
			? data.containersReady
			: source === 'vms'
				? data.vmsReady
				: data.sharesReady}
		error={source === 'docker'
			? data.dockerError
			: source === 'vms'
				? data.vmError
				: data.sharesError}
		{admin}
		tailscaleHost={tailnetHost(data.tailnet)}
		{editing}
		onrefresh={data.refresh}
		{onpin}
	/>
{:else}{@const Content = CONTENT[source as StaticTileId]}{#if Content}<Content
			options={settings}
		/>{/if}{/if}
