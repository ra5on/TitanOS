<script lang="ts">
	// The tab "Overview" of a VM: what it is made of and what it does, as
	// tiles. The CD drive can be changed here, also while the VM runs.
	import { api, ApiError, type Hardware, type Volume, type VM } from '#lib/api.ts';
	import Icon from '#lib/components/Icon.svelte';
	import Sparkline from '#lib/components/Sparkline.svelte';
	import { formatBytes, formatDateTime, formatDuration, formatPercent } from '#lib/format.ts';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';
	import type { SnapshotStatus } from './snapshots.ts';
	import IsoPicker from './IsoPicker.svelte';

	type Props = {
		vm: VM;
		/** The last CPU loads of the VM, oldest first. */
		history: number[];
		volumes: Volume[];
		canStart: boolean;
		busy: boolean;
		onstart: () => void;
		onchanged: () => void;
		onsnapshots: () => void;
	};
	let { vm, history, volumes, canStart, busy, onstart, onchanged, onsnapshots }: Props = $props();

	const running = $derived(vm.state === 'running');
	const base = $derived(`/vms/${encodeURIComponent(vm.name)}`);

	// Addresses: from the guest agent, or from the neighbour table for a VM
	// on the bridge. Asked when the tab opens and every 30 seconds.
	let addresses = $state<{ addresses: string[]; source: '' | 'agent' | 'arp' } | null>(null);
	$effect(() => {
		const url = base;
		addresses = null;
		if (!running) return;
		const ask = async () => {
			if (document.hidden) return;
			try {
				const result = await api.get<{ addresses: string[]; source: '' | 'agent' | 'arp' }>(
					url + '/addresses',
				);
				if (url === base) addresses = result;
			} catch {
				if (url === base) addresses = { addresses: [], source: '' };
			}
		};
		ask();
		const timer = setInterval(ask, 30000);
		return () => clearInterval(timer);
	});

	let snapshots = $state<SnapshotStatus | null>(null);
	$effect(() => {
		const url = base;
		snapshots = null;
		const ask = async () => {
			if (document.hidden) return;
			try {
				const result = await api.get<SnapshotStatus>(url + '/snapshots');
				if (url === base) snapshots = result;
			} catch {
				// The tile then says nothing about snapshots.
			}
		};
		ask();
		const timer = setInterval(ask, 30000);
		return () => clearInterval(timer);
	});

	// Names of the devices handed to the VM; the configuration only has addresses.
	let hardware = $state<Hardware['hardware'] | null>(null);
	const devices = $derived([...(vm.pci ?? []), ...(vm.usb ?? [])]);
	// Further functions of a card (its sound, …) are listed with the card.
	const companionOf = (id: string) =>
		hardware?.pci.find(
			(device) => device.companions.includes(id) && vm.pci?.includes(device.address),
		);
	const deviceRows = $derived(
		devices
			.filter((id) => !companionOf(id))
			.map((id) => ({
				id,
				ids: [
					id,
					...(hardware?.pci
						.find((device) => device.address === id)
						?.companions.filter((c) => vm.pci?.includes(c)) ?? []),
				],
			})),
	);
	$effect(() => {
		if (!devices.length || hardware) return;
		api
			.get<Hardware>('/hardware')
			.then((result) => (hardware = result.hardware))
			.catch(() => {});
	});
	function deviceName(id: string): string {
		const pci = hardware?.pci.find((device) => device.address === id);
		if (pci) return `${t(`hardware.kind.${pci.kind}`)} · ${pci.vendorName || pci.vendor}`;
		return hardware?.usb.find((device) => device.id === id)?.name || id;
	}
	const gpu = $derived(
		(vm.pci ?? []).some(
			(id) => hardware?.pci.find((device) => device.address === id)?.kind === 'gpu',
		),
	);

	const fileName = (path: string) => path.split('/').at(-1) ?? path;
	const volumeOf = (path: string) =>
		volumes.find((volume) => path.startsWith(`${volume.mountPoint || `/mnt/${volume.name}`}/`));
	const usage = (volume: Volume | undefined) =>
		volume && volume.sizeBytes > 0 ? volume.usedBytes / volume.sizeBytes : 0;
	const diskVolume = (disk: VM['disks'][number]) =>
		volumes.find((volume) => volume.name === disk.volume) ??
		(disk.path ? volumeOf(disk.path) : undefined);

	const memoryShare = $derived(
		vm.memoryMiB > 0 ? Math.min(100, (vm.memoryBytes / (vm.memoryMiB * 1024 * 1024)) * 100) : 0,
	);
	const uptime = $derived.by(() => {
		const since = new Date(vm.since).getTime();
		return running && since > 0 ? formatDuration(Math.max(0, (Date.now() - since) / 1000)) : '';
	});

	let copied = $state('');
	async function copy(text: string) {
		try {
			await navigator.clipboard.writeText(text);
			copied = text;
			setTimeout(() => {
				if (copied === text) copied = '';
			}, 2000);
		} catch {
			copied = '';
		}
	}

	// Changing the medium in the CD drive.
	let medium = $state<{ error: string } | null>(null);
	let mediumBusy = $state(false);
	let ejectError = $state('');
	function chooseMedium() {
		medium = { error: '' };
	}
	async function setMedium(path: string) {
		mediumBusy = true;
		ejectError = '';
		try {
			await api.put(`${base}/medium`, { path });
			medium = null;
			onchanged();
		} catch (err) {
			const text =
				errorText(err instanceof ApiError ? err.code : 'internal') +
				(err instanceof ApiError && err.code === 'vm_failed' ? ` ${err.message}` : '');
			if (medium) medium.error = text;
			else ejectError = text;
		} finally {
			mediumBusy = false;
		}
	}
</script>

<div class="tiles">
	<section class="tile card" aria-labelledby="ov-load">
		<h2 class="tile-title" id="ov-load">{t('vms.ov.load')}</h2>
		{#if running}
			<div class="meter">
				<span class="key">{t('ui.cpu')}</span>
				<Sparkline values={history} label={t('vms.ov.cpuHistory')} />
				<span class="mono value">{formatPercent(vm.cpuPercent)}</span>
			</div>
			<div class="meter">
				<span class="key">{t('ui.ram')}</span>
				<div class="bar" aria-hidden="true">
					<span class="metric-level" style:width="{memoryShare}%"></span>
				</div>
				<span class="mono value"
					>{formatBytes(vm.memoryBytes)} / {formatBytes(vm.memoryMiB * 1024 * 1024)}</span
				>
			</div>
			{#if uptime}<p class="muted small">{t('vms.since', { time: uptime })}</p>{/if}
		{:else}
			<p class="muted">{t('vms.ov.off')}</p>
			<div>
				<button type="button" class="btn btn-primary" onclick={onstart} disabled={busy || !canStart}
					>{t('vms.startVm')}</button
				>
			</div>
		{/if}
	</section>

	<section class="tile card" aria-labelledby="ov-hardware">
		<h2 class="tile-title" id="ov-hardware">{t('vms.ov.hardware')}</h2>
		<dl>
			<dt>{t('vms.ov.system')}</dt>
			<dd>{t(`vms.template.${vm.template}`)}</dd>
			<dt>{t('vms.ov.cpus')}</dt>
			<dd class="mono">
				{vm.vcpus} vCPU{#if vm.cpuPin?.length}{' · '}{t('vms.ov.pinned', {
						cpus: vm.cpuPin.join(', '),
					})}{/if}{#if vm.exclusive}{' · '}{t('vms.ov.exclusive')}{/if}
			</dd>
			<dt>{t('vms.ov.memory')}</dt>
			<dd class="mono">{formatBytes(vm.memoryMiB * 1024 * 1024)}</dd>
			<dt>{t('vms.ov.firmware')}</dt>
			<dd>
				{[
					vm.firmware === 'bios' ? 'BIOS' : 'UEFI',
					vm.secureBoot ? 'Secure Boot' : '',
					vm.tpm ? 'TPM 2.0' : '',
				]
					.filter(Boolean)
					.join(' · ')}
			</dd>
			<dt>{t('vms.autostart')}</dt>
			<dd>{t(vm.autostart ? 'vms.ov.yes' : 'vms.ov.no')}</dd>
			<dt>{t('vms.sched.title')}</dt>
			<dd>
				{#if vm.schedule}
					{t('vms.sched.nextStart')}: {vm.schedule.nextStart
						? formatDateTime(new Date(vm.schedule.nextStart))
						: '–'}<br />
					{t('vms.sched.nextStop')}: {vm.schedule.nextStop
						? formatDateTime(new Date(vm.schedule.nextStop))
						: '–'}
					{#if vm.schedule.waiting}<br /><span class="muted"
							>{t(
								`vms.sched.wait.${vm.schedule.waiting}.${vm.schedule.waitReason ?? 'busy'}` as 'vms.sched.wait.start.busy',
							)}</span
						>{/if}
				{:else}
					{t('vms.sched.off')}
				{/if}
			</dd>
		</dl>
	</section>

	<section class="tile card" aria-labelledby="ov-disks">
		<h2 class="tile-title" id="ov-disks">{t('vms.ov.disks')}</h2>
		<ul class="rows">
			{#each vm.disks as disk, index (index)}
				{@const volume = diskVolume(disk)}
				<li>
					<Icon name="disk" size={16} />
					<span class="mono name" title={disk.path || disk.file}
						>{disk.path ? fileName(disk.path) : disk.file}</span
					>
					<span class="mono muted">{formatBytes(disk.sizeBytes)}</span>
					{#if volume}<span
							class="chip"
							class:warn={usage(volume) >= 0.9}
							title={t('vms.ov.volumeUse', {
								volume: volume.name,
								percent: Math.round(usage(volume) * 100),
							})}>{volume.name} · {Math.round(usage(volume) * 100)} %</span
						>{/if}
				</li>
			{/each}
			<li>
				<Icon name="disc" size={16} />
				{#if vm.iso}
					<span class="mono name" title={vm.iso}>{fileName(vm.iso)}</span>
				{:else}
					<span class="muted name">{t('vms.ov.cdEmpty')}</span>
				{/if}
				<span class="row-actions">
					<button type="button" class="btn small" onclick={chooseMedium} disabled={mediumBusy}
						>{t(vm.iso ? 'vms.ov.cdChange' : 'vms.ov.cdInsert')}</button
					>
					{#if vm.iso && vm.disks.length}<button
							type="button"
							class="btn small"
							onclick={() => setMedium('')}
							disabled={mediumBusy}>{t('vms.ov.cdEject')}</button
						>{/if}
				</span>
			</li>
		</ul>
		{#if ejectError}<p class="notice-error" role="alert">{ejectError}</p>{/if}
		{#if running}<p class="muted small">{t('vms.ov.cdLive')}</p>{/if}
	</section>

	<section class="tile card" aria-labelledby="ov-network">
		<h2 class="tile-title" id="ov-network">{t('vms.ov.network')}</h2>
		<dl>
			<dt>{t('vms.ov.connection')}</dt>
			<dd>{t(vm.network === 'bridge' ? 'vms.ov.bridge' : 'vms.ov.user')}</dd>
			<dt>MAC</dt>
			<dd class="mono">{vm.mac}</dd>
			{#if running}
				<dt>{t('vms.ov.address')}</dt>
				<dd>
					{#if addresses === null}
						<span class="muted">…</span>
					{:else if addresses.addresses.length}
						{#each addresses.addresses as address (address)}
							<span class="address">
								<span class="mono">{address}</span>
								<button
									type="button"
									class="icon"
									aria-label={t('vms.ov.copyAddress', { address })}
									title={t('vms.ov.copyAddress', { address })}
									onclick={() => copy(address)}
									><Icon name={copied === address ? 'check' : 'copy'} size={14} /></button
								>
							</span>
						{/each}
						<span class="muted small"
							>{t(addresses.source === 'agent' ? 'vms.ov.fromAgent' : 'vms.ov.fromArp')}</span
						>
					{:else}
						<span class="muted"
							>{t(vm.network === 'bridge' ? 'vms.ov.noAddress' : 'vms.ov.noAddressUser')}</span
						>
					{/if}
				</dd>
			{/if}
		</dl>
	</section>

	{#if devices.length || vm.folders?.length}
		<section class="tile card" aria-labelledby="ov-devices">
			<h2 class="tile-title" id="ov-devices">{t('vms.ov.devices')}</h2>
			<ul class="rows">
				{#each deviceRows as row (row.id)}
					<li>
						<Icon name="monitor" size={16} /><span class="name">{deviceName(row.id)}</span><span
							class="mono muted">{row.ids.join(', ')}</span
						>
					</li>
				{/each}
				{#each vm.folders ?? [] as folder (folder.tag)}
					<li>
						<Icon name="folder" size={16} /><span class="mono name" title={folder.path}
							>{folder.tag} ← {folder.path}</span
						>{#if folder.readOnly}<span class="chip">{t('vms.ov.readOnly')}</span>{/if}
					</li>
				{/each}
			</ul>
			{#if gpu}<p class="muted small">{t('vms.ov.gpuScreen')}</p>{/if}
		</section>
	{/if}

	<section class="tile card snapshot-card" aria-labelledby="ov-snapshots">
		<h2 class="tile-title" id="ov-snapshots">{t('snapshots.title')}</h2>
		{#if snapshots}
			{@const latest = snapshots.snapshots
				.filter((s) => !s.incomplete)
				.sort((a, b) => b.created.localeCompare(a.created))[0]}
			<dl>
				<dt>{t('vms.ov.snapshotCount')}</dt>
				<dd>{snapshots.snapshots.length}</dd>
				<dt>{t('vms.ov.snapshotLatest')}</dt>
				<dd>{latest ? new Date(latest.created).toLocaleString() : '–'}</dd>
				<dt>{t('vms.ov.snapshotPlan')}</dt>
				<dd>
					{snapshots.schedule.enabled
						? t('vms.ov.snapshotPlanOn', { time: snapshots.schedule.time })
						: t('vms.ov.no')}
				</dd>
			</dl>
			{#if snapshots.reason}<p class="muted small">
					{t(`snapshots.reason.${snapshots.reason}`)}
				</p>{/if}
		{:else}
			<p class="muted">…</p>
		{/if}
		<div>
			<button type="button" class="btn small" onclick={onsnapshots}>{t('snapshots.manage')}</button>
		</div>
	</section>
</div>

{#if medium}
	<IsoPicker
		title={t('vms.ov.cdTitle')}
		text={t(running ? 'vms.ov.cdTextLive' : 'vms.ov.cdText')}
		initial={vm.iso}
		submit={t('vms.ov.cdSubmit')}
		busy={mediumBusy}
		error={medium.error}
		onpick={(path) => {
			if (path !== vm.iso) void setMedium(path);
			else medium = null;
		}}
		onclose={() => (medium = null)}
	/>
{/if}

<style>
	.tiles {
		display: grid;
		grid-template-columns: repeat(2, minmax(0, 1fr));
		gap: 16px;
	}
	.card {
		display: flex;
		flex-direction: column;
		gap: 14px;
		min-width: 0;
		padding: 18px 20px;
		min-height: 180px;
	}
	.snapshot-card {
		grid-column: 1 / -1;
		min-height: 0;
		display: grid;
		grid-template-columns: minmax(0, 1fr) auto;
		align-items: center;
		gap: 12px 24px;
	}
	.snapshot-card > h2,
	.snapshot-card > p {
		grid-column: 1 / -1;
	}
	.snapshot-card > dl {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 12px;
	}
	.snapshot-card > dl > dd {
		margin-right: 24px;
	}
	@media (max-width: 1100px) {
		.tiles {
			grid-template-columns: 1fr;
		}
		.snapshot-card {
			display: flex;
			align-items: stretch;
		}
		.snapshot-card > dl {
			display: grid;
		}
		.snapshot-card > dl > dd {
			margin-right: 0;
		}
	}
	.meter {
		display: grid;
		grid-template-columns: 2.5rem minmax(0, 1fr) auto;
		align-items: center;
		gap: 10px;
		font-size: 0.875rem;
	}
	.value {
		font-size: 0.8125rem;
	}
	.bar {
		height: 6px;
		background: var(--track);
		border-radius: var(--radius-bar);
		overflow: hidden;
	}
	.bar span {
		display: block;
		height: 100%;
		background: var(--accent);
	}
	dl {
		display: grid;
		grid-template-columns: auto minmax(0, 1fr);
		gap: 6px 14px;
		margin: 0;
		font-size: 0.875rem;
	}
	dt {
		color: var(--muted);
	}
	dd {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 4px 8px;
		margin: 0;
		min-width: 0;
		overflow-wrap: anywhere;
	}
	.mono {
		font-size: 0.8125rem;
	}
	.rows {
		display: flex;
		flex-direction: column;
		gap: 8px;
		margin: 0;
		padding: 0;
		list-style: none;
		font-size: 0.875rem;
	}
	.rows li {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 6px 10px;
		min-width: 0;
	}
	.rows .name {
		flex: 1 1 8rem;
		min-width: 0;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.row-actions {
		display: flex;
		gap: 6px;
	}
	.small {
		font-size: 0.75rem;
	}
	.btn.small {
		height: 2rem;
		padding: 0 10px;
		font-size: 0.8125rem;
	}
	p {
		margin: 0;
	}
	.address {
		display: inline-flex;
		align-items: center;
		gap: 2px;
	}
	.icon {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		width: 1.75rem;
		height: 1.75rem;
		padding: 0;
		background: transparent;
		border: 0;
		border-radius: var(--radius-ctl);
		color: var(--muted);
		cursor: pointer;
	}
	.icon:hover {
		background: var(--chip);
		color: var(--text);
	}
</style>
