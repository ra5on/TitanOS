<script lang="ts">
	import { onMount, untrack } from 'svelte';
	import { page } from '$app/state';
	import { beforeNavigate } from '$app/navigation';
	import Dropdown from '#lib/components/Dropdown.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import { storageNavigation } from '#lib/storage-view.ts';
	import { revealTarget } from '#lib/reveal-target.ts';
	import {
		api,
		ApiError,
		type Disk,
		type DiskSleepStatus,
		type DockerStatus,
		type Share,
		type StorageStatus,
		type Volume,
		type VMStatus,
	} from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import { formatDiskBytes, formatPercent } from '#lib/format.ts';
	import { apiErrorText, errorText, i18n, t } from '#lib/i18n/index.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import { refreshStorage, storage } from '#lib/storage.svelte.ts';
	import AddDiskDialog from './AddDiskDialog.svelte';
	import ProfileDialog from './ProfileDialog.svelte';
	import { takesDisk } from './btrfs.ts';
	import CreateVolumeDialog from './CreateVolumeDialog.svelte';
	import DeleteVolumeDialog from './DeleteVolumeDialog.svelte';
	import DiskSleepDialog from './DiskSleepDialog.svelte';
	import {
		diskSleep as diskSleepOf,
		sleepDuration,
		sleepOrigin,
		volumeSleep,
	} from './sleep-view.ts';
	import { diskStatus, diskType, smartText } from './labels.ts';
	import VolumeCard from './VolumeCard.svelte';

	const canAdminister = $derived(
		session.data?.user?.role === 'owner' || session.data?.user?.role === 'admin',
	);
	const disks = $derived(storage.status?.disks ?? []);
	const volumes = $derived(storage.status?.volumes ?? []);
	const free = $derived(disks.filter((disk) => disk.role === 'free' && !disk.testing));

	let dialog = $state<
		| { kind: 'create' }
		| { kind: 'add'; volume: string; disk?: Disk }
		| { kind: 'delete'; volume: string }
		| { kind: 'remove'; volume: string; disk: string }
		| { kind: 'profile'; volume: string }
		| null
	>(null);
	let busy = $state(false);
	let error = $state<{ text: string; detail: string } | null>(null);
	const diskSleep = $derived(storage.status?.diskSleep);

	const navigation = storageNavigation(session.data?.user?.name ?? '');
	let tab = $state(navigation.tab),
		query = $state(navigation.query),
		filter = $state(navigation.filter),
		volumeFilter = $state(navigation.volume),
		sort = $state(navigation.sort);
	let pendingDisk = $state('');
	let selected = $state(navigation.selected),
		selectedDisk = $state(navigation.disk),
		sleepOpen = $state(false),
		sleepFocus = $state<{ kind: 'volume' | 'disk'; key: string } | undefined>();
	const detail = $derived(volumes.find((v) => v.name === selected));
	const diskDetail = $derived(
		disks.find((d) => (d.identity || d.serial || d.name) === selectedDisk),
	);
	const visibleVolumes = $derived(
		volumes
			.filter(
				(v) =>
					`${v.name} ${v.level} ${v.filesystem}`
						.toLocaleLowerCase()
						.includes(query.toLocaleLowerCase()) &&
					(filter !== 'problems' || !['ok', 'syncing'].includes(v.state)),
			)
			.toSorted((a, b) =>
				sort === 'size' ? b.sizeBytes - a.sizeBytes : a.name.localeCompare(b.name, i18n.locale),
			),
	);
	const visibleDisks = $derived(
		disks
			.filter(
				(d) =>
					`${d.name} ${d.model} ${d.serial}`
						.toLocaleLowerCase()
						.includes(query.toLocaleLowerCase()) &&
					(!volumeFilter || d.volume === volumeFilter) &&
					(filter === 'all' ||
						(filter === 'free' && d.role === 'free') ||
						(filter === 'assigned' && !!d.volume) ||
						(filter === 'problems' && (d.memberState === 'faulty' || d.smart?.healthy === false)) ||
						d.kind === filter),
			)
			.toSorted((a, b) =>
				sort === 'size'
					? b.sizeBytes - a.sizeBytes
					: sort === 'temperature'
						? (b.smart?.temperatureC ?? -1) - (a.smart?.temperatureC ?? -1)
						: a.name.localeCompare(b.name, i18n.locale, { numeric: true }),
			),
	);
	beforeNavigate(() =>
		Object.assign(navigation, {
			tab,
			query,
			filter,
			volume: volumeFilter,
			sort,
			selected,
			disk: selectedDisk,
			scroll: window.scrollY,
		}),
	);
	$effect(() => {
		const url = page.url;
		untrack(() => {
			const requested = url.searchParams.get('tab');
			const volume = url.searchParams.get('volume');
			let hash = '';
			try {
				hash = decodeURIComponent(url.hash.slice(1));
			} catch {
				return;
			}
			if (requested === 'disks' || requested === 'volumes') {
				tab = requested;
				query = '';
				filter = 'all';
			}
			if (volume !== null) {
				volumeFilter = volume;
				query = '';
				filter = 'all';
				selectedDisk = '';
			}
			if (hash.startsWith('volume-') && requested !== 'disks') {
				tab = 'volumes';
				selected = hash.slice(7);
				query = '';
				filter = 'all';
			}
			if (hash.startsWith('disk-')) {
				tab = 'disks';
				pendingDisk = hash.slice(5);
				query = '';
				filter = 'all';
				volumeFilter = '';
			}
		});
	});
	onMount(() => {
		if (!location.hash && !location.search)
			requestAnimationFrame(() => window.scrollTo({ top: navigation.scroll }));
		if (!storage.status) void loadStatus();
	});

	// The shared poller stays quiet about failures; the first load here shows
	// them, so an empty list never stands for "no volumes".
	let statusError = $state(''),
		statusLoading = $state(false);
	async function loadStatus() {
		statusLoading = true;
		statusError = '';
		try {
			storage.status = await api.get<StorageStatus>('/storage');
		} catch (err) {
			statusError = apiErrorText(err);
		}
		statusLoading = false;
	}

	// What lives on the volume shown in the details: shares, VMs and Docker.
	// Read when a volume is opened, from the lists the other pages use.
	type Usage = { shares: string[]; vms: string[]; docker: string[] | null; failed: boolean };
	let usage = $state<{ volume: string; data: Usage | null }>({ volume: '', data: null });
	async function loadUsage(volume: string) {
		usage = { volume, data: null };
		const [shares, vms, docker] = await Promise.allSettled([
			api.get<Share[]>('/shares'),
			api.get<VMStatus>('/vms'),
			api.get<DockerStatus>('/docker'),
		]);
		if (usage.volume !== volume) return;
		// Answers of an unexpected shape count as failed reads, not as "nothing".
		const shareList =
			shares.status === 'fulfilled' && Array.isArray(shares.value) ? shares.value : null;
		const vmList =
			vms.status === 'fulfilled' && Array.isArray(vms.value?.vms) ? vms.value.vms : null;
		const engine = docker.status === 'fulfilled' ? docker.value : null;
		usage = {
			volume,
			data: {
				shares: (shareList ?? [])
					.filter((share) => share.volume === volume)
					.map((share) => share.name),
				vms: (vmList ?? [])
					.filter(
						(vm) => vm.volume === volume || (vm.disks ?? []).some((disk) => disk.volume === volume),
					)
					.map((vm) => vm.displayName || vm.name),
				docker:
					engine?.settings?.volume === volume && Array.isArray(engine.apps) ? engine.apps : null,
				failed: !shareList || !vmList || !engine,
			},
		};
	}
	// A string, so the 4-second storage poll does not read the lists again.
	const detailName = $derived(detail?.name ?? '');
	$effect(() => {
		const name = detailName;
		if (canAdminister && name && tab === 'volumes') untrack(() => loadUsage(name));
	});
	const usageFor = (volume: string) => (usage.volume === volume ? usage.data : null);

	$effect(() => {
		if (!pendingDisk) return;
		const disk = disks.find((d) => d.name === pendingDisk);
		if (disk) {
			selectedDisk = disk.identity || disk.serial || disk.name;
			pendingDisk = '';
		}
	});
	function switchTab(value: 'volumes' | 'disks') {
		tab = value;
		filter = 'all';
	}

	// Dialogs work on the live volume, so that they follow its state.
	const dialogVolume = $derived.by(() => {
		const current = dialog;
		if (!current || !('volume' in current)) return undefined;
		return volumes.find((volume) => volume.name === current.volume);
	});

	const summary = $derived.by(() => {
		const parts = [
			volumes.length === 1
				? t('storage.summaryVolume')
				: t('storage.summaryVolumes', { count: volumes.length }),
			disks.length === 1
				? t('storage.summaryDisk')
				: t('storage.summaryDisks', { count: disks.length }),
		];
		if (free.length > 0) parts.push(t('storage.summaryFree', { count: free.length }));
		return parts.join(' · ');
	});

	const hours = $derived(new Intl.NumberFormat(i18n.locale));
	// Volumes that take another disk; the same rule as the button in VolumeCard.
	const addTargets = $derived(volumes.filter(takesDisk));

	/** Runs an action that needs no dialog and shows what went wrong. */
	async function run(action: () => Promise<unknown>) {
		busy = true;
		error = null;
		try {
			await action();
		} catch (err) {
			const code = err instanceof ApiError ? err.code : 'internal';
			error = {
				text: errorText(code),
				detail: code === 'storage_failed' && err instanceof ApiError ? err.message : '',
			};
		}
		await refreshStorage();
		busy = false;
	}

	const rescan = () =>
		run(async () => (storage.status = await api.post<StorageStatus>('/storage/rescan')));
	const check = (volume: Volume, start: boolean) =>
		run(() =>
			api.post(`/storage/volumes/${volume.name}/check`, { action: start ? 'start' : 'stop' }),
		);
	// The dialog stays open and locked while the disk is removed and erased.
	async function removeDisk() {
		if (dialog?.kind !== 'remove' || busy) return;
		const { volume, disk } = dialog;
		await run(() => api.delete(`/storage/volumes/${volume}/disks/${disk}`));
		if (!error && dialog?.kind === 'remove' && dialog.disk === disk) dialog = null;
	}

	function done() {
		dialog = null;
		refreshStorage();
	}
</script>

<PageHead title={t('storage.title')}>
	{#if storage.status}<p class="muted">{summary}</p>{/if}
	{#snippet actions()}{#if canAdminister}
			<Dropdown label={t('storage.options')} text={t('storage.options')} icon="more" align="right">
				{#snippet children(close)}
					<a role="menuitem" href="/storage/import">{t('ops.import')}</a>
					<a role="menuitem" href="/tools/maintenance">{t('ops.maintenance')}</a>
					<a role="menuitem" href="/storage/smart">{t('smart.open')}</a>
					<button
						role="menuitem"
						disabled={busy}
						onclick={() => {
							close();
							rescan();
						}}>{t('storage.rescan')}</button
					>
					<button
						role="menuitem"
						onclick={() => {
							close();
							sleepOpen = true;
						}}>{t('storage.sleepTitle')}</button
					>
				{/snippet}
			</Dropdown>
			<button class="btn btn-primary" onclick={() => (dialog = { kind: 'create' })}
				>{t('storage.newVolume')}</button
			>
		{/if}{/snippet}
</PageHead>
{#if error}<div class="notice-error" role="alert">
		{error.text}{#if error.detail}<pre>{error.detail}</pre>{/if}
	</div>{/if}
<div class="storage-toolbar">
	<div class="tabs" role="group" aria-label={t('storage.title')}>
		<button
			class:chosen={tab === 'volumes'}
			aria-pressed={tab === 'volumes'}
			onclick={() => switchTab('volumes')}
			>{t('storage.tabVolumes')} <span>{volumes.length}</span></button
		>
		<button
			class:chosen={tab === 'disks'}
			aria-pressed={tab === 'disks'}
			onclick={() => switchTab('disks')}>{t('storage.tabDisks')} <span>{disks.length}</span></button
		>
	</div>
	<input
		class="field search"
		type="search"
		aria-label={t('storage.search')}
		placeholder={t('storage.search')}
		bind:value={query}
	/>
	<select class="field" aria-label={t('disk.status')} bind:value={filter}>
		<option value="all">{t('storage.all')}</option><option value="problems"
			>{t('storage.problems')}</option
		>
		{#if tab === 'disks'}<option value="free">{t('storage.unassigned')}</option><option
				value="assigned">{t('storage.assigned')}</option
			><option value="hdd">HDD</option><option value="ssd">SSD</option><option value="nvme"
				>NVMe</option
			>{/if}
	</select>
	{#if tab === 'disks'}<select class="field" aria-label={t('disk.volume')} bind:value={volumeFilter}
			><option value="">{t('storage.all')}</option>{#each volumes as v}<option value={v.name}
					>{v.name}</option
				>{/each}</select
		>{/if}
	<select class="field" aria-label={t('storage.sort')} bind:value={sort}
		><option value="name">{t('disk.device')}</option><option value="size">{t('disk.size')}</option
		>{#if tab === 'disks'}<option value="temperature">{t('disk.temp')}</option>{/if}</select
	>
</div>
<div class="storage-layout" class:with-detail={tab === 'volumes' ? !!detail : !!diskDetail}>
	<div class="tile table">
		{#if !storage.status}
			{#if statusError}<div class="notice-error table-state" role="alert">
					<span>{statusError}</span><button
						class="btn"
						disabled={statusLoading}
						onclick={loadStatus}>{t('common.retry')}</button
					>
				</div>{:else}<p class="muted table-state" role="status">{t('common.loading')}</p>{/if}
		{:else if tab === 'volumes'}
			<table>
				<thead
					><tr
						><th>{t('storage.volumes')}</th><th>{t('disk.status')}</th><th
							>RAID / {t('storage.filesystem')}</th
						><th>{t('storage.used')}</th><th class="num">{t('storage.disks')}</th><th
							>{t('storage.sleepColumn')}</th
						></tr
					></thead
				><tbody>
					{#each visibleVolumes as volume (volume.name)}{@const sleep = volumeSleep(
							diskSleep,
							volume.name,
						)}
						<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_noninteractive_element_interactions -->
						<tr
							class="clickable"
							class:selected={selected === volume.name}
							onclick={() => (selected = selected === volume.name ? '' : volume.name)}
						>
							<td
								><button class="row-name mono" aria-expanded={selected === volume.name}
									>{volume.name}<span class="chev" aria-hidden="true">›</span></button
								></td
							>
							<td
								><span class="chip" class:warn={!['ok'].includes(volume.state)}
									>{t(`state.${volume.state}`)}</span
								>{#if volume.sync}<div class="sync-inline">
										<progress max="100" value={volume.sync.percent}></progress><span class="mono"
											>{formatPercent(volume.sync.percent)}</span
										>
									</div>{/if}</td
							>
							<td
								><span>{t(`level.${volume.level}`)}</span><span class="sub"
									>{t(`fs.${volume.filesystem}`)}</span
								></td
							>
							<td
								>{#if volume.mounted && volume.sizeBytes > 0}<span class="mono"
										>{formatDiskBytes(volume.usedBytes)} / {formatDiskBytes(volume.sizeBytes)}</span
									>
									<div class="bar">
										<span
											style:width="{Math.min(100, (volume.usedBytes / volume.sizeBytes) * 100)}%"
										></span>
									</div>{:else}<span class="muted">{t('storage.notMounted')}</span>{/if}</td
							>
							<td class="num mono">{volume.members.length}</td>
							<td>
								{#if sleep}
									<button
										type="button"
										class="sleep-cell"
										title={canAdminister ? t('storage.sleepOpenHint') : undefined}
										disabled={!canAdminister}
										onclick={(event) => {
											event.stopPropagation();
											sleepFocus = { kind: 'volume', key: volume.name };
											sleepOpen = true;
										}}
									>
										{#if sleep.total > 0}
											<span>{sleepDuration(sleep.minutes)}</span>
											<span class="chip" class:ok={sleep.asleep === sleep.total}
												>{sleep.asleep === sleep.total
													? t('storage.sleepAsleep')
													: sleep.asleep > 0
														? t('storage.sleepPartly')
														: t('storage.sleepAwake')}</span
											>
											<span class="sub"
												>{t('storage.sleepCount', {
													asleep: sleep.asleep,
													total: sleep.total,
												})}</span
											>
										{:else}
											<span class="muted">–</span>
											<span class="sub">{t('storage.sleepNoSleeper')}</span>
										{/if}
									</button>
								{:else}<span class="muted">–</span>{/if}
							</td>
						</tr>
					{:else}<tr
							><td colspan="6"
								><EmptyState
									inline
									title={t(volumes.length ? 'ui.noResults' : 'storage.noVolumes')}
								/></td
							></tr
						>{/each}
				</tbody>
			</table>
		{:else}
			<table>
				<thead
					><tr
						><th>{t('disk.device')}</th><th>{t('disk.status')}</th><th class="num"
							>{t('disk.size')}</th
						><th class="num">{t('disk.temp')}</th><th>{t('disk.smart')}</th><th
							>{t('disk.volume')}</th
						><th>{t('storage.sleepColumn')}</th></tr
					></thead
				><tbody>
					{#each visibleDisks as disk (disk.identity || disk.serial || disk.name)}{@const status =
							diskStatus(disk)}{@const sleep = diskSleepOf(diskSleep, disk.name)}
						<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_noninteractive_element_interactions -->
						<tr
							class="clickable"
							class:selected={selectedDisk === (disk.identity || disk.serial || disk.name)}
							onclick={() =>
								(selectedDisk =
									selectedDisk === (disk.identity || disk.serial || disk.name)
										? ''
										: disk.identity || disk.serial || disk.name)}
						>
							<td
								><button
									class="row-name mono"
									aria-expanded={selectedDisk === (disk.identity || disk.serial || disk.name)}
									>{disk.name}<span class="chev" aria-hidden="true">›</span></button
								><span class="sub">{disk.model || diskType(disk)}</span></td
							>
							<td
								><span class="status"><span class="dot {status.dot}"></span>{status.text}</span></td
							><td class="mono num">{formatDiskBytes(disk.sizeBytes)}</td><td class="mono num"
								>{disk.smart?.temperatureC ? `${disk.smart.temperatureC} °C` : '–'}</td
							><td class:bad={disk.smart?.healthy === false}>{smartText(disk)}</td><td class="mono"
								>{disk.volume ||
									(disk.role === 'free'
										? t('storage.unassigned')
										: disk.contents?.join(', ') || '–')}</td
							>
							<td>
								{#if sleep?.disk}
									<button
										type="button"
										class="sleep-cell"
										title={canAdminister ? t('storage.sleepOpenHint') : undefined}
										disabled={!canAdminister}
										onclick={(event) => {
											event.stopPropagation();
											sleepFocus = { kind: 'disk', key: disk.name };
											sleepOpen = true;
										}}
									>
										<span>{sleepDuration(sleep.disk.effective)}</span>
										<span class="chip" class:ok={sleep.disk.state === 'asleep'}
											>{sleep.disk.state === 'asleep'
												? t('storage.sleepAsleep')
												: t('storage.sleepAwake')}</span
										>
										<span class="sub">{sleepOrigin(sleep.disk.origin)}</span>
									</button>
								{:else if sleep?.reason}
									<span class="muted">–</span>
									<span class="sub">{t(`storage.sleepReason.${sleep.reason}`)}</span>
								{:else}<span class="muted">–</span>{/if}
							</td>
						</tr>
					{:else}<tr
							><td colspan="7"
								><EmptyState
									inline
									title={t(disks.length ? 'ui.noResults' : 'storage.noDisks')}
								/></td
							></tr
						>{/each}
				</tbody>
			</table>
		{/if}
	</div>
	{#if tab === 'volumes' && detail}
		<aside>
			<div class="detail-heading">
				<h2>{t('storage.details')}</h2>
				<button class="btn" aria-label={t('common.close')} onclick={() => (selected = '')}
					><Icon name="close" size={16} /></button
				>
			</div>
			<!-- A new instance per volume: an open editor must not carry over. -->
			{#key detail.name}
				<VolumeCard
					volume={detail}
					{canAdminister}
					{busy}
					oncheck={(start) => check(detail, start)}
					onadd={() => (dialog = { kind: 'add', volume: detail.name })}
					onprofile={() => (dialog = { kind: 'profile', volume: detail.name })}
					{diskSleep}
					onsleepsaved={(updated) => {
						if (storage.status) storage.status.diskSleep = updated;
					}}
					onsleepopen={() => {
						sleepFocus = { kind: 'volume', key: detail.name };
						sleepOpen = true;
					}}
					ondelete={() => (dialog = { kind: 'delete', volume: detail.name })}
					onremove={(disk) => (
						(error = null),
						(dialog = { kind: 'remove', volume: detail.name, disk })
					)}
				/>
			{/key}
			{#if canAdminister}{@const used = usageFor(detail.name)}
				<section class="tile usage" aria-labelledby="volume-usage">
					<h3 id="volume-usage">{t('storage.onVolume')}</h3>
					{#if !used}<p class="muted loading" role="status">{t('common.loading')}</p>
					{:else}
						<dl>
							<div>
								<dt>{t('storage.onVolumeShares')}</dt>
								<dd>
									{#each used.shares as share, i (share)}{i ? ', ' : ''}<a
											class="mono"
											href="/shares?share={encodeURIComponent(share)}">{share}</a
										>{:else}<span class="muted">{t('storage.onVolumeNone')}</span>{/each}
								</dd>
							</div>
							<div>
								<dt>{t('storage.onVolumeVms')}</dt>
								<dd>
									{used.vms.join(', ') || t('storage.onVolumeNone')}
								</dd>
							</div>
							<div>
								<dt>{t('storage.onVolumeDocker')}</dt>
								<dd>
									{used.docker
										? used.docker.length
											? t('storage.onVolumeDockerApps', { apps: used.docker.join(', ') })
											: t('storage.onVolumeDockerEngine')
										: t('storage.onVolumeNone')}
								</dd>
							</div>
						</dl>
						{#if used.failed}<div class="notice-warn usage-failed" role="alert">
								<span>{t('storage.onVolumeIncomplete')}</span><button
									class="btn"
									onclick={() => loadUsage(detail.name)}>{t('common.retry')}</button
								>
							</div>{/if}
					{/if}
				</section>
			{/if}
		</aside>
	{:else if tab === 'disks' && diskDetail}
		<aside class="tile disk-details" use:revealTarget id="disk-{diskDetail.name}">
			<div class="detail-heading">
				<h2 class="mono">{diskDetail.name}</h2>
				<button class="btn" aria-label={t('common.close')} onclick={() => (selectedDisk = '')}
					><Icon name="close" size={16} /></button
				>
			</div>
			<dl>
				<div>
					<dt>{t('disk.device')}</dt>
					<dd>{diskDetail.model}</dd>
				</div>
				<div>
					<dt>{t('disk.serial')}</dt>
					<dd class="mono">{diskDetail.serial || '–'}</dd>
				</div>
				<div>
					<dt>{t('disk.type')}</dt>
					<dd>{diskType(diskDetail)}</dd>
				</div>
				<div>
					<dt>{t('disk.hours')}</dt>
					<dd>{diskDetail.smart ? hours.format(diskDetail.smart.powerOnHours) : '–'}</dd>
				</div>
				<div>
					<dt>{t('disk.errors')}</dt>
					<dd>{diskDetail.smart?.errors ?? '–'}</dd>
				</div>
				<div>
					<dt>{t('disk.wear')}</dt>
					<dd>
						{diskDetail.smart && diskDetail.smart.wearPercent >= 0
							? `${diskDetail.smart.wearPercent} %`
							: '–'}
					</dd>
				</div>
			</dl>
			{#if canAdminister}{@const disk = diskDetail}
				<div class="disk-actions">
					<a class="btn" href="/storage/smart?disk={encodeURIComponent(disk.name)}"
						>{t('smart.details')}</a
					>
					{#if disk.volume}
						<button
							class="btn"
							onclick={() => {
								tab = 'volumes';
								query = '';
								filter = 'all';
								selected = disk.volume ?? '';
							}}>{t('storage.openVolume', { volume: disk.volume })}</button
						>
					{:else if disk.role === 'free'}
						<a class="btn" href="/tools#tools-disks">{t('storage.testDisk')}</a>
						{#each addTargets as volume (volume.name)}
							<button
								class="btn"
								disabled={busy || disk.testing}
								onclick={() => (dialog = { kind: 'add', volume: volume.name, disk })}
								>{t('storage.addToVolume', { volume: volume.name })}</button
							>
						{/each}
					{/if}
				</div>
			{/if}
		</aside>
	{/if}
</div>
{#if sleepOpen}<DiskSleepDialog
		status={diskSleep}
		focus={sleepFocus}
		onclose={() => ((sleepOpen = false), (sleepFocus = undefined))}
		onsaved={(updated: DiskSleepStatus) => {
			if (storage.status) storage.status.diskSleep = updated;
		}}
	/>{/if}

{#if dialog?.kind === 'create'}
	<CreateVolumeDialog
		{disks}
		taken={volumes.map((volume) => volume.name)}
		onclose={() => (dialog = null)}
		ondone={done}
	/>
{:else if dialog?.kind === 'add' && dialogVolume}
	{#key dialog}<AddDiskDialog
			volume={dialogVolume}
			{disks}
			preselect={dialog.disk}
			onclose={() => (dialog = null)}
			ondone={done}
		/>{/key}
{:else if dialog?.kind === 'profile' && dialogVolume}
	{#key dialog}<ProfileDialog
			volume={dialogVolume}
			onclose={() => (dialog = null)}
			ondone={done}
		/>{/key}
{:else if dialog?.kind === 'delete' && dialogVolume}
	<DeleteVolumeDialog
		volume={dialogVolume}
		usage={usageFor(dialogVolume.name)}
		onclose={() => (dialog = null)}
		ondone={done}
	/>
{:else if dialog?.kind === 'remove'}
	<Dialog
		title={t('storage.removeDisk', { disk: dialog.disk })}
		locked={busy}
		onclose={() => (dialog = null)}
	>
		<p class="remove-text notice-warn">
			{t(
				dialogVolume?.filesystem === 'btrfs' ? 'btrfs.removeConfirm' : 'storage.removeDiskConfirm',
				{ disk: dialog.disk, volume: dialog.volume },
			)}
		</p>
		{#if error}<div class="notice-error" role="alert">
				{error.text}{#if error.detail}<pre>{error.detail}</pre>{/if}
			</div>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={busy} onclick={() => (dialog = null)}
				>{t('common.cancel')}</button
			>
			<button type="button" class="btn btn-danger" disabled={busy} onclick={removeDisk}
				>{t('storage.remove')}</button
			>
		</div>
	</Dialog>
{/if}

<style>
	.storage-toolbar,
	.tabs,
	.sync-inline,
	.detail-heading {
		display: flex;
		align-items: center;
		gap: 10px;
		flex-wrap: wrap;
	}
	.storage-toolbar {
		gap: var(--space-3);
	}
	.tabs {
		gap: 3px;
		border-bottom: 1px solid var(--border);
	}
	.tabs button {
		padding: var(--space-3);
		border: 0;
		background: transparent;
		color: var(--muted);
		border-bottom: 2px solid transparent;
		cursor: pointer;
	}
	.tabs .chosen {
		color: var(--text);
		border-color: var(--accent);
	}
	.tabs span {
		margin-left: 7px;
		font-family: var(--font-mono);
		font-size: var(--text-sm);
	}
	.search {
		flex: 1;
		min-width: 180px;
	}
	.storage-toolbar select {
		width: auto;
		max-width: 200px;
	}
	.storage-layout {
		display: grid;
		gap: 20px;
		align-items: start;
	}
	.storage-layout.with-detail {
		grid-template-columns: minmax(0, 1.4fr) minmax(310px, 1fr);
	}
	.table {
		min-width: 0;
		overflow-x: auto;
	}
	.table table {
		width: 100%;
	}
	td {
		padding: 14px var(--space-4);
	}
	tr.selected {
		background: color-mix(in srgb, var(--accent) 8%, transparent);
	}
	.row-name {
		border: 0;
		background: transparent;
		padding: 0;
		cursor: pointer;
		color: var(--accent);
		font-weight: 500;
		text-align: left;
		white-space: nowrap;
		text-decoration: underline;
		text-underline-offset: 3px;
		text-decoration-color: color-mix(in srgb, var(--accent) 45%, transparent);
	}
	.row-name:hover {
		text-decoration-color: currentColor;
	}
	.chev {
		margin-left: 6px;
		text-decoration: none;
		display: inline-block;
	}
	tr.clickable {
		cursor: pointer;
	}
	tr.clickable:hover {
		background: color-mix(in srgb, var(--accent) 6%, transparent);
	}
	.sleep-cell {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-1) var(--space-2);
		border: 0;
		background: transparent;
		padding: 0;
		color: var(--text);
		cursor: pointer;
		text-align: left;
		font: inherit;
	}
	.sleep-cell:hover:not(:disabled) > span:first-child {
		text-decoration: underline;
	}
	.sleep-cell:disabled {
		cursor: default;
		opacity: 1;
	}
	.sleep-cell .sub {
		flex-basis: 100%;
		margin-top: 0;
	}
	.sub {
		display: block;
		font-size: var(--text-sm);
		color: var(--muted);
		margin-top: var(--space-1);
	}
	.num {
		text-align: right;
		white-space: nowrap;
	}
	.bar {
		height: 5px;
		margin-top: 7px;
		min-width: 100px;
	}
	.sync-inline {
		font-size: var(--text-sm);
		margin-top: 7px;
		gap: 6px;
	}
	.sync-inline progress {
		width: 70px;
		height: 6px;
		accent-color: var(--accent);
	}
	aside {
		min-width: 0;
	}
	.detail-heading {
		justify-content: space-between;
		margin-bottom: var(--space-3);
	}
	.detail-heading h2 {
		font-size: var(--text-lg);
	}
	.detail-heading .btn {
		padding: 6px;
		min-height: 30px;
	}
	.disk-details {
		padding: 20px;
	}
	.disk-actions,
	.usage-failed,
	.table-state {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2);
	}
	.disk-actions {
		margin-top: 18px;
	}
	.table-state {
		justify-content: space-between;
		margin: var(--space-4);
	}
	.usage {
		margin-top: var(--space-4);
		padding: var(--space-4) 20px;
		display: grid;
		gap: var(--space-3);
		font-size: var(--text-md);
	}
	.usage h3 {
		margin: 0;
		font-size: var(--text-md);
	}
	.usage dl {
		display: grid;
		gap: 10px;
		margin: 0;
	}
	.usage dt {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.usage dd {
		margin: 2px 0 0;
		overflow-wrap: anywhere;
	}
	.disk-details dl {
		display: grid;
		gap: var(--space-4);
		margin: 0;
	}
	.disk-details dl > div {
		display: grid;
		gap: var(--space-1);
	}
	.disk-details dt {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.disk-details dd {
		margin: 0;
		overflow-wrap: anywhere;
	}
	.status {
		display: flex;
		align-items: center;
		gap: 7px;
	}
	.bad {
		color: var(--error-text);
	}
	@media (max-width: 1100px) {
		.storage-layout.with-detail {
			grid-template-columns: 1fr;
		}
	}
	@media (max-width: 760px) {
		.storage-toolbar {
			align-items: stretch;
		}
		.search {
			flex-basis: 100%;
		}
		td {
			padding: var(--space-3);
		}
		.tabs {
			width: 100%;
		}
	}
</style>
