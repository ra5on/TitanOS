<script lang="ts">
	// The tabs "Übersicht", "Hardware" and "Zeitplan" of a VM (PLAN 67): cards
	// with rows, label left and value right. The overview shows the load, the
	// network and a summary of the hardware with "Bearbeiten"; the hardware
	// tab every part; the schedule tab the start and stop times. The CD drive
	// can be changed on both, also while the VM runs.
	import { poll } from '#lib/poll.ts';
	import { api, ApiError, type Hardware, type Volume, type VM } from '#lib/api.ts';
	import Icon from '#lib/components/Icon.svelte';
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
				// The tile then says nothing about snapshots.
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
	<div class="kv">
		<span class="k">{t('vms.ov.cd')}</span>
		<span class="v">
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
		</span>
	</div>
{/snippet}

{#snippet diskRows()}
	{#each vm.disks as disk, index (index)}
		{@const volume = diskVolume(disk)}
		<div class="kv">
			<span class="k">{t('vms.ov.disk')}{vm.disks.length > 1 ? ` ${index + 1}` : ''}</span>
			<span class="v">
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
			</span>
		</div>
	{/each}
{/snippet}

<div class="cards" class:two={part === 'overview'}>
	{#if part === 'overview'}
		<section class="tile ui-card" aria-labelledby="ov-load">
			<div class="card-head">
				<h2 id="ov-load">{t('vms.ov.load')}</h2>
				{#if !running}<div class="card-tools">
						<button
							type="button"
							class="btn btn-primary"
							onclick={onstart}
							disabled={busy || !canStart}>{t('vms.startVm')}</button
						>
					</div>{/if}
			</div>
			{#if running}
				<div class="kv">
					<span class="k">{t('vms.ov.cpus')}</span>
					<span class="v meter">
						<span class="meter-top"
							><span>{formatPercent(vm.cpuPercent)}</span><span class="muted">{vm.vcpus} vCPU</span
							></span
						>
						<Sparkline values={history} label={t('vms.ov.cpuHistory')} />
					</span>
				</div>
				<div class="kv">
					<span class="k">{t('vms.ov.memory')}</span>
					<span class="v meter">
						<span class="meter-top"
							><span>{formatBytes(vm.memoryBytes)}</span><span class="muted"
								>{t('vms.ov.of', { total: formatBytes(vm.memoryMiB * 1024 * 1024) })}</span
							></span
						>
						<span class="bar" aria-hidden="true"
							><span class="metric-level" style:width="{memoryShare}%"></span></span
						>
					</span>
				</div>
				{#if uptime}<div class="kv">
						<span class="k">{t('vms.ov.uptime')}</span><span class="v">{uptime}</span>
					</div>{/if}
			{:else}
				<p class="card-text muted">{t('vms.ov.off')}</p>
			{/if}
		</section>
	{/if}

	{#if part === 'overview' || part === 'hardware'}
		<section class="tile ui-card" aria-labelledby="ov-network">
			<div class="card-head"><h2 id="ov-network">{t('vms.ov.network')}</h2></div>
			{#if running}
				<div class="kv">
					<span class="k">{t('vms.ov.address')}</span>
					<span class="v">
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
					</span>
				</div>
			{/if}
			<div class="kv">
				<span class="k">{t('vms.ov.connection')}</span>
				<span class="v">{t(vm.network === 'bridge' ? 'vms.ov.bridge' : 'vms.ov.user')}</span>
			</div>
			<div class="kv">
				<span class="k">MAC</span><span class="v mono">{vm.mac}</span>
			</div>
		</section>
	{/if}

	{#if part === 'overview'}
		<section class="tile ui-card wide" aria-labelledby="ov-hardware">
			<div class="card-head">
				<h2 id="ov-hardware">{t('vms.ov.hardware')}</h2>
				<div class="card-tools">{@render editButton()}</div>
			</div>
			<div class="kv">
				<span class="k">{t('vms.ov.system')}</span>
				<span class="v"
					>{t(`vms.template.${vm.template}`)} · {vm.firmware === 'bios' ? 'BIOS' : 'UEFI'} · {vm.vcpus}
					vCPU · {formatBytes(vm.memoryMiB * 1024 * 1024)}</span
				>
			</div>
			{@render diskRows()}
			{@render cdRow()}
			{#if devices.length || vm.folders?.length}
				<div class="kv">
					<span class="k">{t('vms.ov.devices')}</span>
					<span class="v"
						>{[
							...deviceRows.map((row) => deviceName(row.id)),
							...(vm.folders ?? []).map((f) => f.tag),
						].join(', ')}</span
					>
				</div>
			{/if}
			<div class="kv">
				<span class="k">{t('vms.sched.title')}</span>
				<span class="v">{@render scheduleSummary()}</span>
			</div>
			{#if ejectError}<p class="notice-error inside" role="alert">{ejectError}</p>{/if}
		</section>

		<section class="tile ui-card wide" aria-labelledby="ov-snapshots">
			<div class="card-head">
				<h2 id="ov-snapshots">{t('snapshots.title')}</h2>
				<div class="card-tools">
					<button type="button" class="btn" onclick={onsnapshots}>{t('snapshots.manage')}</button>
				</div>
			</div>
			{#if snapshots}
				{@const latest = snapshots.snapshots
					.filter((s) => !s.incomplete)
					.sort((a, b) => b.created.localeCompare(a.created))[0]}
				<div class="kv">
					<span class="k">{t('vms.ov.snapshotCount')}</span>
					<span class="v">{snapshots.snapshots.length}</span>
				</div>
				<div class="kv">
					<span class="k">{t('vms.ov.snapshotLatest')}</span>
					<span class="v">{latest ? new Date(latest.created).toLocaleString() : '–'}</span>
				</div>
				<div class="kv">
					<span class="k">{t('vms.ov.snapshotPlan')}</span>
					<span class="v"
						>{snapshots.schedule.enabled
							? t('vms.ov.snapshotPlanOn', { time: snapshots.schedule.time })
							: t('vms.ov.no')}</span
					>
				</div>
				{#if snapshots.reason}<p class="card-text muted small">
						{t(`snapshots.reason.${snapshots.reason}`)}
					</p>{/if}
			{:else}
				<p class="card-text muted">…</p>
			{/if}
		</section>
	{/if}

	{#if part === 'hardware'}
		<section class="tile ui-card" aria-labelledby="hw-system">
			<div class="card-head">
				<h2 id="hw-system">{t('vms.ov.hardware')}</h2>
				<div class="card-tools">{@render editButton()}</div>
			</div>
			<div class="kv">
				<span class="k">{t('vms.ov.system')}</span><span class="v"
					>{t(`vms.template.${vm.template}`)}</span
				>
			</div>
			<div class="kv">
				<span class="k">{t('vms.ov.cpus')}</span>
				<span class="v"
					>{vm.vcpus} vCPU{#if vm.cpuPin?.length}{' · '}{t('vms.ov.pinned', {
							cpus: vm.cpuPin.join(', '),
						})}{/if}{#if vm.exclusive}{' · '}{t('vms.ov.exclusive')}{/if}</span
				>
			</div>
			<div class="kv">
				<span class="k">{t('vms.ov.memory')}</span>
				<span class="v">{formatBytes(vm.memoryMiB * 1024 * 1024)}</span>
			</div>
			<div class="kv">
				<span class="k">{t('vms.ov.firmware')}</span>
				<span class="v"
					>{[
						vm.firmware === 'bios' ? 'BIOS' : 'UEFI',
						vm.secureBoot ? 'Secure Boot' : '',
						vm.tpm ? 'TPM 2.0' : '',
					]
						.filter(Boolean)
						.join(' · ')}</span
				>
			</div>
			<div class="kv">
				<span class="k">{t('vms.autostart')}</span>
				<span class="v">{t(vm.autostart ? 'vms.ov.yes' : 'vms.ov.no')}</span>
			</div>
		</section>

		<section class="tile ui-card" aria-labelledby="ov-disks">
			<div class="card-head"><h2 id="ov-disks">{t('vms.ov.disks')}</h2></div>
			{@render diskRows()}
			{@render cdRow()}
			{#if ejectError}<p class="notice-error inside" role="alert">{ejectError}</p>{/if}
			{#if running}<p class="card-text muted small">{t('vms.ov.cdLive')}</p>{/if}
		</section>

		{#if devices.length || vm.folders?.length}
			<section class="tile ui-card" aria-labelledby="ov-devices">
				<div class="card-head"><h2 id="ov-devices">{t('vms.ov.devices')}</h2></div>
				{#each deviceRows as row (row.id)}
					<div class="kv">
						<span class="k">{deviceName(row.id)}</span>
						<span class="v mono muted">{row.ids.join(', ')}</span>
					</div>
				{/each}
				{#each vm.folders ?? [] as folder (folder.tag)}
					<div class="kv">
						<span class="k mono">{folder.tag}</span>
						<span class="v"
							><span class="mono" title={folder.path}>{folder.path}</span>{#if folder.readOnly}<span
									class="chip">{t('vms.ov.readOnly')}</span
								>{/if}</span
						>
					</div>
				{/each}
				{#if gpu}<p class="card-text muted small">{t('vms.ov.gpuScreen')}</p>{/if}
			</section>
		{/if}
	{/if}

	{#if part === 'schedule'}
		{@const plan = vm.powerSchedule}
		<section class="tile ui-card" aria-labelledby="ov-schedule">
			<div class="card-head">
				<h2 id="ov-schedule">{t('vms.sched.title')}</h2>
				<div class="card-tools">{@render editButton()}</div>
			</div>
			<div class="kv">
				<span class="k">{t('vms.ov.state')}</span>
				<span class="v">{plan?.enabled ? t('vms.ov.scheduleOn') : t('vms.sched.off')}</span>
			</div>
			{#if plan?.rules.length}
				{#each plan.rules as rule, index (index)}
					<div class="kv">
						<span class="k"
							>{t(rule.action === 'start' ? 'vms.sched.start' : 'vms.sched.stop')}</span
						>
						<span class="v"
							><span class="mono">{rule.time}</span><span class="muted"
								>{WEEK.filter((d) => rule.days.includes(d))
									.map((d) => t(`backup.dayShort.${d}` as 'backup.dayShort.0'))
									.join(', ')}</span
							></span
						>
					</div>
				{/each}
			{:else}
				<p class="card-text muted">{t('vms.sched.none')}</p>
			{/if}
			{#if plan?.enabled}
				<div class="kv">
					<span class="k">{t('vms.ov.next')}</span>
					<span class="v column">{@render scheduleSummary()}</span>
				</div>
			{/if}
		</section>
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
		gap: var(--gap, 16px);
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
	/* A row of a card: label left, value right. */
	.kv {
		display: grid;
		grid-template-columns: minmax(7rem, 34%) minmax(0, 1fr);
		align-items: center;
		gap: 4px 16px;
		padding: 11px 16px;
		border-top: 1px solid var(--panel-border, var(--border));
		font-size: var(--text-md);
		min-width: 0;
	}
	.card-head + .kv {
		border-top: 0;
	}
	.k {
		color: var(--text);
		min-width: 0;
		overflow-wrap: anywhere;
	}
	.v {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 4px 10px;
		min-width: 0;
		overflow-wrap: anywhere;
	}
	.v.column {
		flex-direction: column;
		align-items: flex-start;
	}
	.meter {
		flex-direction: column;
		align-items: stretch;
		gap: 6px;
	}
	.meter-top {
		display: flex;
		justify-content: space-between;
		gap: 10px;
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
	.card-text {
		margin: 0;
		padding: 12px 16px;
	}
	.card-head + .card-text {
		padding-top: 14px;
	}
	.kv + .card-text {
		border-top: 1px solid var(--panel-border, var(--border));
	}
	.inside {
		margin: 0 16px 12px;
	}
	.mono {
		font-size: var(--text-base);
	}
	.name {
		min-width: 0;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
		max-width: 100%;
	}
	.row-actions {
		display: flex;
		gap: 6px;
		margin-left: auto;
	}
	.small {
		font-size: var(--text-sm);
	}
	.btn.small {
		min-height: 30px;
		height: 30px;
		padding: 0 10px;
		font-size: var(--text-base);
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
	@media (max-width: 479px) {
		.kv {
			grid-template-columns: minmax(0, 1fr);
		}
	}
</style>
