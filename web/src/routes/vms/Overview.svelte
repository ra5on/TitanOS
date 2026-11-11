<script lang="ts">
	// The tabs "Übersicht", "Hardware" and "Zeitplan" of a VM (PLAN 67; rows
	// instead of cards since PLAN 78-5): groups under a plain heading, every
	// fact a flat row with its name left and its value right. The overview shows the load, the
	// network and a summary of the hardware with "Bearbeiten"; the hardware
	// tab every part; the schedule tab the start and stop times. The CD drive
	// can be changed on both, also while the VM runs.
	import { poll } from '#lib/poll.ts';
	import { api, ApiError, type Hardware, type Volume, type VM } from '#lib/api.ts';
	import Icon from '#lib/components/Icon.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import Sparkline from '#lib/components/Sparkline.svelte';
	import { formatBytes, formatDateTime, formatDuration, formatPercent } from '#lib/format.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import type { SnapshotStatus } from './snapshots.ts';
	import IsoPicker from './IsoPicker.svelte';
	import { WEEK } from '#lib/vm-schedule.ts';

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
		part?: 'overview' | 'hardware' | 'schedule';
	};
	let {
		vm,
		history,
		volumes,
		canStart,
		busy,
		onstart,
		onchanged,
		onsnapshots,
		part = 'overview',
	}: Props = $props();
	const editHref = $derived(`/vms/edit?vm=${encodeURIComponent(vm.name)}`);

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
			try {
				const result = await api.get<{ addresses: string[]; source: '' | 'agent' | 'arp' }>(
					url + '/addresses',
				);
				if (url === base) addresses = result;
			} catch {
				if (url === base) addresses = { addresses: [], source: '' };
			}
		};
		return poll(ask, { interval: 30000 });
	});

	let snapshots = $state<SnapshotStatus | null>(null);
	$effect(() => {
		const url = base;
		snapshots = null;
		const ask = async () => {
			try {
				const result = await api.get<SnapshotStatus>(url + '/snapshots');
				if (url === base) snapshots = result;
			} catch {
				// The group then says nothing about snapshots.
			}
		};
		return poll(ask, { interval: 30000 });
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
	/** PLAN 67-1: Medien › ISO einlegen / auswerfen in the menu bar. */
	export function insertMedium() {
		if (!mediumBusy) chooseMedium();
	}
	export function canEject(): boolean {
		return !!vm.iso && vm.disks.length > 0 && !mediumBusy;
	}
	export function ejectMedium() {
		if (canEject()) void setMedium('');
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
				apiErrorText(err) +
				(err instanceof ApiError && err.code === 'vm_failed' ? ` ${err.message}` : '');
			if (medium) medium.error = text;
			else ejectError = text;
		} finally {
			mediumBusy = false;
		}
	}
</script>

{#snippet editButton()}
	<a class="btn" href={editHref}>{t('vms.ov.edit')}</a>
{/snippet}

{#snippet scheduleSummary()}
	{#if vm.schedule && vm.powerSchedule?.enabled}
		<span
			>{t('vms.sched.nextStart')}: {vm.schedule.nextStart
				? formatDateTime(new Date(vm.schedule.nextStart))
				: '–'}</span
		>
		<span class="muted"
			>{t('vms.sched.nextStop')}: {vm.schedule.nextStop
				? formatDateTime(new Date(vm.schedule.nextStop))
				: '–'}</span
		>
		{#if vm.schedule.waiting}<span class="muted"
				>{t(
					`vms.sched.wait.${vm.schedule.waiting}.${vm.schedule.waitReason ?? 'busy'}` as 'vms.sched.wait.start.busy',
				)}</span
			>{/if}
	{:else}
		<span>{t('vms.sched.off')}</span>
	{/if}
{/snippet}

{#snippet cdRow()}
	<SettingRow title={t('vms.ov.cd')}>
		{#snippet control()}
			{#if vm.iso}
				<span class="mono name" title={vm.iso}>{fileName(vm.iso)}</span>
			{:else}
				<span class="muted name">{t('vms.ov.cdEmpty')}</span>
			{/if}
			<button type="button" class="btn" onclick={chooseMedium} disabled={mediumBusy}
				>{t(vm.iso ? 'vms.ov.cdChange' : 'vms.ov.cdInsert')}</button
			>
			{#if vm.iso && vm.disks.length}<button
					type="button"
					class="btn"
					onclick={() => setMedium('')}
					disabled={mediumBusy}>{t('vms.ov.cdEject')}</button
				>{/if}
		{/snippet}
	</SettingRow>
{/snippet}

{#snippet diskRows()}
	{#each vm.disks as disk, index (index)}
		{@const volume = diskVolume(disk)}
		<SettingRow title="{t('vms.ov.disk')}{vm.disks.length > 1 ? ` ${index + 1}` : ''}">
			{#snippet control()}
				<span class="mono name" title={disk.path || disk.file}
					>{disk.path ? fileName(disk.path) : disk.file}</span
				>
				<span class="muted">{formatBytes(disk.sizeBytes)}</span>
				{#if volume}<span
						class="chip"
						class:warn={usage(volume) >= 0.9}
						title={t('vms.ov.volumeUse', {
							volume: volume.name,
							percent: Math.round(usage(volume) * 100),
						})}>{volume.name} · {Math.round(usage(volume) * 100)} %</span
					>{/if}
			{/snippet}
		</SettingRow>
	{/each}
{/snippet}

<!-- PLAN 78-5: every group is a plain heading above its facts as flat rows
     (name left, value right); the buttons of a group stand beside its heading. -->
<div class="cards sfacts" class:two={part === 'overview'}>
	{#if part === 'overview'}
		<SettingGroup title={t('vms.ov.load')} id="ov-load">
			{#if running}
				<SettingRow title={t('vms.ov.cpus')}>
					{#snippet control()}
						<span class="meter">
							<span class="meter-top"
								><span>{formatPercent(vm.cpuPercent)}</span><span class="muted"
									>{vm.vcpus} vCPU</span
								></span
							>
							<Sparkline values={history} label={t('vms.ov.cpuHistory')} />
						</span>
					{/snippet}
				</SettingRow>
				<SettingRow title={t('vms.ov.memory')}>
					{#snippet control()}
						<span class="meter">
							<span class="meter-top"
								><span>{formatBytes(vm.memoryBytes)}</span><span class="muted"
									>{t('vms.ov.of', { total: formatBytes(vm.memoryMiB * 1024 * 1024) })}</span
								></span
							>
							<span class="bar" aria-hidden="true"
								><span class="metric-level" style:width="{memoryShare}%"></span></span
							>
						</span>
					{/snippet}
				</SettingRow>
				{#if uptime}<SettingRow title={t('vms.ov.uptime')}>
						{#snippet control()}<span>{uptime}</span>{/snippet}
					</SettingRow>{/if}
			{:else}
				<SettingRow title={t('vms.ov.off')}>
					{#snippet control()}
						<button
							type="button"
							class="btn btn-primary"
							onclick={onstart}
							disabled={busy || !canStart}>{t('vms.startVm')}</button
						>
					{/snippet}
				</SettingRow>
			{/if}
		</SettingGroup>
	{/if}

	{#if part === 'overview' || part === 'hardware'}
		<SettingGroup title={t('vms.ov.network')} id="ov-network">
			{#if running}
				<SettingRow title={t('vms.ov.address')}>
					{#snippet control()}
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
					{/snippet}
				</SettingRow>
			{/if}
			<SettingRow title={t('vms.ov.connection')}>
				{#snippet control()}<span
						>{t(vm.network === 'bridge' ? 'vms.ov.bridge' : 'vms.ov.user')}</span
					>{/snippet}
			</SettingRow>
			<SettingRow title="MAC">
				{#snippet control()}<span class="mono">{vm.mac}</span>{/snippet}
			</SettingRow>
		</SettingGroup>
	{/if}

	{#if part === 'overview'}
		<div class="wide">
			<SettingGroup title={t('vms.ov.hardware')} id="ov-hardware" tools={editButton}>
				<SettingRow title={t('vms.ov.system')}>
					{#snippet control()}<span
							>{t(`vms.template.${vm.template}`)} · {vm.firmware === 'bios' ? 'BIOS' : 'UEFI'} · {vm.vcpus}
							vCPU · {formatBytes(vm.memoryMiB * 1024 * 1024)}</span
						>{/snippet}
				</SettingRow>
				{@render diskRows()}
				{@render cdRow()}
				{#if devices.length || vm.folders?.length}
					<SettingRow title={t('vms.ov.devices')}>
						{#snippet control()}<span
								>{[
									...deviceRows.map((row) => deviceName(row.id)),
									...(vm.folders ?? []).map((f) => f.tag),
								].join(', ')}</span
							>{/snippet}
					</SettingRow>
				{/if}
				<SettingRow title={t('vms.sched.title')}>
					{#snippet control()}<span class="column">{@render scheduleSummary()}</span>{/snippet}
				</SettingRow>
				{#if ejectError}<p class="notice-error" role="alert">{ejectError}</p>{/if}
			</SettingGroup>
		</div>

		<div class="wide">
			<SettingGroup title={t('snapshots.title')} id="ov-snapshots">
				{#snippet tools()}
					<button type="button" class="btn" onclick={onsnapshots}>{t('snapshots.manage')}</button>
				{/snippet}
				{#if snapshots}
					{@const latest = snapshots.snapshots
						.filter((s) => !s.incomplete)
						.sort((a, b) => b.created.localeCompare(a.created))[0]}
					<SettingRow title={t('vms.ov.snapshotCount')}>
						{#snippet control()}<span>{snapshots?.snapshots.length}</span>{/snippet}
					</SettingRow>
					<SettingRow title={t('vms.ov.snapshotLatest')}>
						{#snippet control()}<span
								>{latest ? new Date(latest.created).toLocaleString() : '–'}</span
							>{/snippet}
					</SettingRow>
					<SettingRow title={t('vms.ov.snapshotPlan')}>
						{#snippet control()}<span
								>{snapshots?.schedule.enabled
									? t('vms.ov.snapshotPlanOn', { time: snapshots.schedule.time })
									: t('vms.ov.no')}</span
							>{/snippet}
					</SettingRow>
					{#if snapshots.reason}<p class="muted small">
							{t(`snapshots.reason.${snapshots.reason}`)}
						</p>{/if}
				{:else}
					<p class="muted">…</p>
				{/if}
			</SettingGroup>
		</div>
	{/if}

	{#if part === 'hardware'}
		<SettingGroup title={t('vms.ov.hardware')} id="hw-system" tools={editButton}>
			<SettingRow title={t('vms.ov.system')}>
				{#snippet control()}<span>{t(`vms.template.${vm.template}`)}</span>{/snippet}
			</SettingRow>
			<SettingRow title={t('vms.ov.cpus')}>
				{#snippet control()}<span
						>{vm.vcpus} vCPU{#if vm.cpuPin?.length}{' · '}{t('vms.ov.pinned', {
								cpus: vm.cpuPin.join(', '),
							})}{/if}{#if vm.exclusive}{' · '}{t('vms.ov.exclusive')}{/if}</span
					>{/snippet}
			</SettingRow>
			<SettingRow title={t('vms.ov.memory')}>
				{#snippet control()}<span>{formatBytes(vm.memoryMiB * 1024 * 1024)}</span>{/snippet}
			</SettingRow>
			<SettingRow title={t('vms.ov.firmware')}>
				{#snippet control()}<span
						>{[
							vm.firmware === 'bios' ? 'BIOS' : 'UEFI',
							vm.secureBoot ? 'Secure Boot' : '',
							vm.tpm ? 'TPM 2.0' : '',
						]
							.filter(Boolean)
							.join(' · ')}</span
					>{/snippet}
			</SettingRow>
			<SettingRow title={t('vms.autostart')}>
				{#snippet control()}<span>{t(vm.autostart ? 'vms.ov.yes' : 'vms.ov.no')}</span>{/snippet}
			</SettingRow>
		</SettingGroup>

		<SettingGroup title={t('vms.ov.disks')} id="ov-disks">
			{@render diskRows()}
			{@render cdRow()}
			{#if ejectError}<p class="notice-error" role="alert">{ejectError}</p>{/if}
			{#if running}<p class="muted small">{t('vms.ov.cdLive')}</p>{/if}
		</SettingGroup>

		{#if devices.length || vm.folders?.length}
			<SettingGroup title={t('vms.ov.devices')} id="ov-devices">
				{#each deviceRows as row (row.id)}
					<SettingRow title={deviceName(row.id)}>
						{#snippet control()}<span class="mono muted">{row.ids.join(', ')}</span>{/snippet}
					</SettingRow>
				{/each}
				{#each vm.folders ?? [] as folder (folder.tag)}
					<SettingRow title={folder.tag}>
						{#snippet control()}<span class="mono" title={folder.path}>{folder.path}</span
							>{#if folder.readOnly}<span class="chip">{t('vms.ov.readOnly')}</span>{/if}{/snippet}
					</SettingRow>
				{/each}
				{#if gpu}<p class="muted small">{t('vms.ov.gpuScreen')}</p>{/if}
			</SettingGroup>
		{/if}
	{/if}

	{#if part === 'schedule'}
		{@const plan = vm.powerSchedule}
		<SettingGroup title={t('vms.sched.title')} id="ov-schedule" tools={editButton}>
			<SettingRow title={t('vms.ov.state')}>
				{#snippet control()}<span
						>{plan?.enabled ? t('vms.ov.scheduleOn') : t('vms.sched.off')}</span
					>{/snippet}
			</SettingRow>
			{#if plan?.rules.length}
				{#each plan.rules as rule, index (index)}
					<SettingRow title={t(rule.action === 'start' ? 'vms.sched.start' : 'vms.sched.stop')}>
						{#snippet control()}<span class="mono">{rule.time}</span><span class="muted"
								>{WEEK.filter((d) => rule.days.includes(d))
									.map((d) => t(`backup.dayShort.${d}` as 'backup.dayShort.0'))
									.join(', ')}</span
							>{/snippet}
					</SettingRow>
				{/each}
			{:else}
				<p class="muted">{t('vms.sched.none')}</p>
			{/if}
			{#if plan?.enabled}
				<SettingRow title={t('vms.ov.next')}>
					{#snippet control()}<span class="column">{@render scheduleSummary()}</span>{/snippet}
				</SettingRow>
			{/if}
		</SettingGroup>
	{/if}
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
	.cards {
		display: grid;
		grid-template-columns: minmax(0, 1fr);
		gap: var(--space-5) var(--gap, var(--space-4));
		align-items: start;
	}
	.cards.two {
		grid-template-columns: repeat(2, minmax(0, 1fr));
	}
	.cards.two .wide {
		grid-column: 1 / -1;
	}
	@media (max-width: 760px) {
		.cards.two {
			grid-template-columns: minmax(0, 1fr);
		}
	}
	.column {
		display: flex;
		flex-direction: column;
		align-items: flex-end;
		gap: var(--space-1);
	}
	/* CPU and memory: the figures above their graph or bar. */
	.meter {
		display: flex;
		flex-direction: column;
		gap: var(--space-1-5);
		flex: 1 1 12rem;
		min-width: 9rem;
		text-align: start;
	}
	.meter-top {
		display: flex;
		justify-content: space-between;
		gap: var(--space-2-5);
		font-size: var(--text-base);
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
	.mono {
		font-size: var(--text-base);
	}
	.name {
		min-width: 0;
		max-width: 100%;
		overflow-wrap: anywhere;
	}
	.small {
		font-size: var(--text-sm);
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
	@media (max-width: 639px) {
		.column {
			align-items: flex-start;
		}
	}
</style>
