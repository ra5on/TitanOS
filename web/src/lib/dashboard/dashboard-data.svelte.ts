import {
	api,
	type VM,
	type VMStatus,
	type DockerContainer,
	type DockerStatus,
	type Share,
} from '#lib/api.ts';
import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
import { poll } from '#lib/poll.ts';
import type { TailnetStatus } from '#lib/app-links.ts';
import type { TailnetDevices } from './tailscale.ts';
import type { NetbirdPeers } from './netbird.ts';
import type { ClusterPage } from '#lib/cluster.svelte.ts';

/** Which data the dashboard needs right now; each getter is read inside an effect. */
export type DashboardWatch = {
	/** VMs, Docker and the Tailscale status for links. */
	workloads: () => boolean;
	tailnet: () => boolean;
	netbird: () => boolean;
	cluster: () => boolean;
	shares: () => boolean;
};

/** Workloads, shares and VPN devices shown by the dashboard tiles. */
export class DashboardData {
	containers = $state<DockerContainer[]>([]);
	vms = $state<VM[]>([]);
	kvm = $state(false);
	shares = $state<Share[]>([]);
	tailnet = $state<TailnetStatus | null>(null);
	tailnetDevices = $state<TailnetDevices | null>(null);
	tailnetDevicesError = $state('');
	netbirdPeers = $state<NetbirdPeers | null>(null);
	netbirdPeersError = $state('');
	cluster = $state<ClusterPage | null>(null);
	clusterError = $state('');
	containersReady = $state(false);
	vmsReady = $state(false);
	sharesReady = $state(false);
	dockerError = $state('');
	vmError = $state('');
	sharesError = $state('');
	/** Counting it up asks every running poll again right away. */
	refreshSerial = $state(0);

	refresh = () => {
		this.refreshSerial++;
	};
}

/**
 * Creates the dashboard data and polls the NAS while `watch` asks for it.
 * Call it while a component initialises: the polls run in its effects.
 */
export function dashboardData(watch: DashboardWatch): DashboardData {
	const data = new DashboardData();
	$effect(() => {
		if (!watch.workloads()) return;
		void data.refreshSerial;
		return poll(
			async (signal) => {
				const [machines, docker, tailscale] = await Promise.allSettled([
					api.get<VMStatus>('/vms', signal),
					api.get<DockerStatus>('/docker', signal),
					api.get<TailnetStatus>('/plugins/tailscale', signal),
				]);
				if (signal.aborted) return;
				if (machines.status === 'fulfilled') {
					data.vms = machines.value.vms;
					data.kvm = machines.value.kvm;
					data.vmsReady = true;
					data.vmError = '';
				} else data.vmError = apiErrorText(machines.reason);
				if (docker.status === 'fulfilled') {
					data.containers = docker.value.containers;
					data.containersReady = true;
					data.dockerError =
						docker.value.settings.enabled && !docker.value.running
							? t('dashboard.engineUnavailable')
							: '';
				} else data.dockerError = apiErrorText(docker.reason);
				data.tailnet = tailscale.status === 'fulfilled' ? tailscale.value : null;
			},
			{ interval: 5000 },
		);
	});
	/** Polls one address while `watching` is true and hands each answer or error to `take`. */
	function follow<T>(
		watching: () => boolean,
		path: string,
		take: (value: T | null, error: string) => void,
	) {
		$effect(() => {
			if (!watching()) return;
			void data.refreshSerial;
			return poll(
				async (signal) => {
					try {
						const value = await api.get<T>(path, signal);
						if (!signal.aborted) take(value, '');
					} catch (e) {
						if (!signal.aborted) take(null, apiErrorText(e));
					}
				},
				{ interval: 15000 },
			);
		});
	}
	// Tailnet devices and NetBird peers only while such a tile is shown or being
	// edited; the server answers from a short RAM cache.
	follow<TailnetDevices>(watch.tailnet, '/plugins/tailscale/devices', (value, problem) => {
		if (value) data.tailnetDevices = value;
		data.tailnetDevicesError = problem;
	});
	follow<NetbirdPeers>(watch.netbird, '/plugins/netbird/peers', (value, problem) => {
		if (value) data.netbirdPeers = value;
		data.netbirdPeersError = problem;
	});
	// The nodes of the cluster, always those of this NAS (a local path).
	follow<ClusterPage>(watch.cluster, '/cluster', (value, problem) => {
		if (value) data.cluster = value;
		data.clusterError = problem;
	});
	follow<Share[]>(watch.shares, '/shares', (value, problem) => {
		if (value) {
			data.shares = value;
			data.sharesReady = true;
		}
		data.sharesError = problem;
	});
	return data;
}
