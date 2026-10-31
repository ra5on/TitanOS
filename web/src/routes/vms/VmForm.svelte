<script lang="ts">
	// The form a VM is made and changed with. With `existing` it edits that
	// VM: the internal identifier, template and firmware stay fixed. The
	// sections live in VmCpu, VmDisks, VmInstall and VmConnections; this
	// component owns the values, checks and sends them.
	import { untrack } from 'svelte';
	import { beforeNavigate, goto } from '$app/navigation';
	import {
		api,
		ApiError,
		type Hardware,
		type Share,
		type VM,
		type VMHost,
		type VMStatus,
	} from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import VirtCheck from '#lib/components/VirtCheck.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import VmConnections from './VmConnections.svelte';
	import VmCpu from './VmCpu.svelte';
	import VmDisks from './VmDisks.svelte';
	import VmInstall from './VmInstall.svelte';
	import VmSchedule from './VmSchedule.svelte';
	import { GB, diskBlocked, folderProblem, freeOn, type DiskDraft } from './vm-form.ts';
	import { MIB, formatBytes } from '#lib/format.ts';
	import { scheduleDraft, schedulePayload, scheduleProblems } from '#lib/vm-schedule.ts';
	import { apiErrorText, errorText, t } from '#lib/i18n/index.svelte.ts';
	import { storage } from '#lib/storage.svelte.ts';
	import { newVMIdentifier, validVMDisplayName, vmLabel } from '#lib/vm-name.ts';

	let { existing }: { existing?: VM } = $props();

	const TEMPLATES = ['linux', 'windows', 'other'] as const;

	let status = $state<VMStatus | null>(null);
	let host = $state<VMHost>({ cpus: [], images: [], disks: [] });
	let hardware = $state<Hardware | null>(null);
	let shares = $state<Share[]>([]);
	let media = $state<{ path: string; sizeBytes: number }[]>([]);

	// The form is made anew for every VM, so what it starts from is read once.
	const initial = untrack(() => existing);
	let template = $state<(typeof TEMPLATES)[number]>(initial?.template ?? 'linux');
	let chosenDirectory = $state(initial?.directory ?? '');
	let reuseImageFolder = $state(true);
	const directFolder = $derived.by(() => {
		const image = disks.find((d) => d.mode === 'direct');
		const match = image?.path.match(/^\/mnt\/([^/]+)\/vms\/([^/]+)\/[^/]+$/);
		return match ? { volume: match[1], directory: match[2] } : null;
	});
	let displayName = $state(initial ? vmLabel(initial) : '');
	let description = $state(initial?.description ?? '');
	let vcpus = $state(initial?.vcpus ?? 2);
	// Kept in MiB as titand stores it; the form shows decimal GB (2 GB for a new VM).
	let memoryMiB = $state(initial ? initial.memoryMiB : Math.round((2 * GB) / MIB));
	let pinning = $state((initial?.cpuPin?.length ?? 0) > 0);
	let pinned = $state<number[]>(initial?.cpuPin ?? []);
	let exclusive = $state(initial?.exclusive ?? false);
	let firmware = $state<'uefi' | 'bios'>(initial?.firmware || 'uefi');
	let secure = $state(initial ? initial.tpm : false);
	let disks = $state<DiskDraft[]>([]);
	let iso = $state(initial?.iso ?? '');
	let virtioISO = $state(initial?.virtioISO ?? '');
	let virtioPending = $state(false);
	$effect(() => {
		if (template !== 'windows') {
			virtioISO = '';
			virtioPending = false;
		}
	});
	let autostart = $state(initial?.autostart ?? false);
	let schedule = $state(scheduleDraft(initial?.powerSchedule));
	let folders = $state((initial?.folders ?? []).map((folder) => ({ ...folder })));
	let autoGPU = $state(initial?.autoGPU ?? false);
	let pci = $state<string[]>(initial?.pci ?? []);
	let usb = $state<string[]>(initial?.usb ?? []);
	let net = $state<VM['network']>(initial?.network ?? 'user');
	let bridgePending = $state(false);
	let bridgeChecked = false;
	async function bridgeState(ready: boolean, pending: boolean) {
		bridgePending = pending;
		if (ready && !bridgeChecked) {
			bridgeChecked = true;
			try {
				status = await api.get<VMStatus>('/vms');
				if (!existing && status.bridge) net = 'bridge';
			} catch (err) {
				fail(err);
			}
		} else if (!ready) bridgeChecked = false;
	}
	let grow = $state<Record<number, number>>({});
	let busy = $state(false);
	let error = $state<{ text: string; detail: string } | null>(null);
	let touched = false;

	const running = $derived(existing?.state === 'running');
	const volumes = $derived((storage.status?.volumes ?? []).filter((volume) => volume.mounted));
	const free = (volume: string) => freeOn(volumes, volume);
	const taken = $derived((status?.vms ?? []).map((vm) => vm.name));
	const name = $derived(initial?.name ?? newVMIdentifier(displayName, taken));
	const directory = $derived(
		chosenDirectory.trim() || (reuseImageFolder ? directFolder?.directory : '') || name,
	);

	const fail = (err: unknown) => (error = { text: apiErrorText(err), detail: '' });
	// What the sections offer comes from separate reads; a failed one says so
	// in its section, with a way to ask again, instead of looking empty.
	const PARTS = ['host', 'hardware', 'shares', 'media'] as const;
	type Part = (typeof PARTS)[number];
	let partErrors = $state<Partial<Record<Part, string>>>({});
	const readers: Record<Part, () => Promise<unknown>> = {
		host: () => api.get<VMHost>('/vms/host').then((res) => (host = res)),
		hardware: () => api.get<Hardware>('/hardware').then((res) => (hardware = res)),
		shares: () =>
			api.get<Share[]>('/shares').then((res) => (shares = res.filter((share) => share.available))),
		media: () => api.get<{ media: typeof media }>('/vms/media').then((res) => (media = res.media)),
	};
	function loadPart(part: Part) {
		delete partErrors[part];
		readers[part]().catch((err) => (partErrors[part] = apiErrorText(err)));
	}
	$effect(() => {
		api.get<VMStatus>('/vms').then((res) => {
			status = res;
			if (!existing) {
				vcpus = Math.min(2, res.host.cpus);
				if (!bridgeChecked) net = res.bridge ? 'bridge' : 'user';
			}
		}, fail);
		untrack(() => {
			for (const part of PARTS) loadPart(part);
		});
	});
	// A first disk, once it is known where it can lie.
	$effect(() => {
		if (!existing && disks.length === 0 && volumes.length > 0 && !touched)
			disks.push({ mode: 'new', format: 'qcow2', sizeGB: 64, volume: volumes[0].name, path: '' });
	});
	// Windows 11 asks for a TPM and Secure Boot: on with that template
	// unless switched off, and not offered with the others.
	$effect(() => {
		if (!existing) secure = template === 'windows' && firmware === 'uefi';
	});

	const vcpuCount = $derived(pinning ? pinned.length : vcpus);

	async function growDisk(index: number) {
		if (!existing) return;
		busy = true;
		error = null;
		try {
			await api.post(`/vms/${existing.name}/grow`, {
				disk: index,
				sizeBytes: Math.round(grow[index] * GB),
			});
			existing.disks[index].sizeBytes = Math.round(grow[index] * GB);
			delete grow[index];
		} catch (err) {
			fail(err);
		}
		busy = false;
	}

	// --- submit --------------------------------------------------------------

	const duplicateName = $derived(
		(status?.vms ?? []).some(
			(vm) =>
				vm.name !== existing?.name &&
				vmLabel(vm).toLowerCase() === displayName.trim().toLowerCase(),
		),
	);
	const nameProblem = $derived(
		displayName === ''
			? ''
			: !validVMDisplayName(displayName)
				? t('vms.displayNameInvalid')
				: duplicateName
					? t('vms.new.nameExists')
					: '',
	);
	// Everything that still keeps the form from being sent, said in words
	// right above the buttons instead of a silently disabled button.
	const openPoints = $derived.by(() => {
		const list: string[] = [];
		if (displayName.trim() === '') list.push(t('vms.form.openName'));
		else if (!validVMDisplayName(displayName)) list.push(t('vms.displayNameInvalid'));
		else if (duplicateName) list.push(t('vms.new.nameExists'));
		if (!existing && disks.length === 0 && iso.trim() === '') list.push(t('vms.form.openDisk'));
		disks.forEach((disk, index) => {
			if (diskBlocked(disk, host.images, free))
				list.push(t('vms.form.openDiskInvalid', { n: index + 1 }));
		});
		if (pinning && pinned.length === 0) list.push(t('vms.form.warnNone'));
		folders.forEach((folder, index) => {
			const issue = folderProblem(folders, folder, index);
			if (issue) list.push(t('vms.form.openFolder', { n: index + 1, problem: issue }));
		});
		if (bridgePending) list.push(t('vms.form.openBridgePending'));
		else if (net === 'bridge' && !status?.bridge) list.push(t('vms.form.openBridge'));
		if (virtioPending) list.push(t('vms.form.openVirtio'));
		for (const issue of scheduleProblems(schedule))
			list.push(
				t('vms.form.openSchedule', {
					problem: t(`vms.sched.problem.${issue.key}` as 'vms.sched.problem.noRule', {
						n: 'n' in issue ? issue.n : 0,
					}),
				}),
			);
		return list;
	});
	const blocked = $derived(busy || openPoints.length > 0);

	// --- unsaved changes ---------------------------------------------------

	// What the form shows, as one value. Until the user touches the form,
	// values filled in by the page itself (defaults, loaded data) only move
	// the starting point along.
	const formState = $derived(
		JSON.stringify({
			template,
			chosenDirectory,
			reuseImageFolder,
			displayName,
			description,
			vcpus,
			memoryMiB,
			pinning,
			pinned,
			exclusive,
			firmware,
			secure,
			disks,
			iso,
			virtioISO,
			autostart,
			schedule,
			folders,
			autoGPU,
			pci,
			usb,
			net,
		}),
	);
	let baseline = $state('');
	let edited = false;
	$effect(() => {
		const current = formState;
		if (!edited) baseline = current;
	});
	const markEdited = () => (edited = true);
	const dirty = $derived(formState !== baseline);
	let leaving = $state<URL | null>(null),
		allowNavigation = false;
	beforeNavigate((navigation) => {
		if (!dirty || busy || allowNavigation || navigation.willUnload) return;
		navigation.cancel();
		leaving = navigation.to?.url ?? null;
	});
	function unload(event: BeforeUnloadEvent) {
		if (dirty && !busy) {
			event.preventDefault();
			event.returnValue = '';
		}
	}
	function discard() {
		const url = leaving;
		leaving = null;
		if (!url) return;
		allowNavigation = true;
		void goto(url.href).finally(() => (allowNavigation = false));
	}

	function problem(err: unknown) {
		const code = err instanceof ApiError ? err.code : 'internal';
		error = {
			text: errorText(code),
			detail:
				['vm_failed', 'hardware_incomplete_group', 'invalid_folder'].includes(code) &&
				err instanceof ApiError
					? err.message
					: '',
		};
	}

	const hardwareSettings = () => ({
		vcpus: vcpuCount,
		memoryMiB,
		cpuPin: pinning ? pinned : [],
		exclusive: pinning && exclusive,
		iso: iso.trim(),
		virtioISO: virtioISO.trim(),
		folders: folders.map((folder) => ({
			path: folder.path.trim().replace(/\/+$/, ''),
			tag: folder.tag,
			readOnly: folder.readOnly,
		})),
		pci,
		autoGPU,
		usb,
		network: net,
	});

	async function create(start: boolean) {
		busy = true;
		error = null;
		try {
			await api.post('/vms', {
				name,
				directory: directory !== name ? directory : undefined,
				volume: reuseImageFolder && directFolder ? directFolder.volume : undefined,
				displayName: displayName.trim(),
				description,
				template,
				firmware,
				autostart,
				powerSchedule: schedulePayload(schedule),
				tpm: secure,
				secureBoot: secure,
				...hardwareSettings(),
				disks: disks.map((disk) =>
					disk.mode === 'direct' || disk.mode === 'block'
						? { path: disk.path }
						: {
								format: disk.format,
								sizeBytes: Math.round(disk.sizeGB * GB),
								volume: disk.volume,
								source: disk.mode === 'copy' ? disk.path : undefined,
							},
				),
				// A disk that is still being copied cannot be started from.
				start: start && !disks.some((disk) => disk.mode === 'copy'),
			});
			await goto(`/vms?vm=${name}`);
		} catch (err) {
			problem(err);
			busy = false;
		}
	}

	async function save() {
		if (!existing) return;
		busy = true;
		error = null;
		try {
			await api.put(`/vms/${existing.name}`, {
				displayName: displayName.trim(),
				description,
				autostart,
				powerSchedule: schedulePayload(schedule),
				...hardwareSettings(),
			});
			await goto(`/vms?vm=${existing.name}`);
		} catch (err) {
			problem(err);
			busy = false;
		}
	}
</script>

<svelte:window onbeforeunload={unload} />

<PageHead
	title={existing ? t('vms.dialog.edit', { name: vmLabel(existing) }) : t('vms.new.title')}
	parent={{ href: '/vms', label: t('vms.title') }}
	crumb={existing ? vmLabel(existing) : t('vms.new.crumb')}
/>

{#if host?.virt}<VirtCheck virt={host.virt} problemsOnly />{:else if status && !status.kvm}<p
		class="notice-warn"
		role="status"
	>
		{t('vms.noKvm')}
	</p>{/if}
{#if storage.status && volumes.length === 0}<p class="notice-warn" role="status">
		{t('vms.new.needVolume')}
	</p>{/if}
{#if running}<p class="notice-warn" role="status">{t('vms.form.running')}</p>{/if}

<div class="layout">
	<div
		class="steps"
		oninput={markEdited}
		onchange={markEdited}
		onpointerdown={markEdited}
		onkeydown={markEdited}
		role="presentation"
	>
		<section class="tile vm-card card-1" aria-labelledby="s1">
			<div class="step">
				<span class="section-icon"><Icon name="monitor" size={15} /></span>
				<h2 id="s1">{t('vms.form.general')}</h2>
			</div>
			<div class="content">
				{#if !existing}
					<div class="templates" role="radiogroup" aria-label={t('vms.new.template')}>
						{#each TEMPLATES as entry (entry)}
							<button
								type="button"
								role="radio"
								aria-checked={template === entry}
								onclick={() => (template = entry)}
							>
								<span class="name">{t(`vms.template.${entry}`)}</span>
								<span class="muted">{t(`vms.template.${entry}.brief`)}</span>
							</button>
						{/each}
					</div>
					{#if template === 'windows' && firmware === 'uefi'}
						<label class="check"
							><input type="checkbox" bind:checked={secure} /><span>{t('vms.form.secure')}</span
							></label
						>
						<small>{t('vms.form.secureNote')}</small>
					{/if}
				{/if}
				<div class="row">
					<label class="narrow"
						><span>{t('vms.new.name')}</span>
						<input
							class="field"
							type="text"
							bind:value={displayName}
							maxlength="160"
							aria-invalid={nameProblem !== ''}
							disabled={busy}
							placeholder={t('vms.form.displayNameExample')}
						/>
						{#if nameProblem}<small class="warn" role="alert">{nameProblem}</small>{/if}
					</label>
					<label class="wide"
						><span>{t('vms.new.description')}</span><input
							class="field"
							type="text"
							bind:value={description}
							maxlength="200"
						/></label
					>
				</div>
				<details class="advanced">
					<summary>{t('vms.directory')}</summary>
					<div class="advanced-body">
						{#if !existing}
							<label
								><span>{t('vms.directory')}</span><input
									class="field"
									bind:value={chosenDirectory}
									placeholder={directory}
									disabled={busy}
								/><small>{t('vms.directoryHint')}</small></label
							>
							{#if directFolder}<label class="check"
									><input
										type="checkbox"
										bind:checked={reuseImageFolder}
										disabled={busy || !!chosenDirectory.trim()}
									/><span>{t('vms.reuseDirectory', { name: directFolder.directory })}</span></label
								>{/if}
							<p class="mono muted">{t('vms.directoryPreview', { name: directory })}</p>
						{:else}<p class="mono muted">
								{t('vms.directoryPreview', { name: existing.directory || existing.name })}
							</p>{/if}
					</div>
				</details>
			</div>
		</section>
		<section class="tile vm-card card-2" aria-labelledby="s2">
			<div class="step">
				<span class="section-icon"><Icon name="chip" size={15} /></span>
				<h2 id="s2">{t('vms.new.step2')}</h2>
			</div>
			<VmCpu
				{status}
				cpus={host.cpus}
				vmName={existing?.name}
				editing={!!existing}
				{running}
				{busy}
				{template}
				{secure}
				hostError={partErrors.host}
				onretryhost={() => loadPart('host')}
				bind:vcpus
				bind:memoryMiB
				bind:pinning
				bind:pinned
				bind:exclusive
				bind:firmware
			/>
		</section>
		<section class="tile vm-card card-3" aria-labelledby="s3">
			<div class="step">
				<span class="section-icon"><Icon name="disk" size={15} /></span>
				<h2 id="s3">{t('vms.new.step3')}</h2>
			</div>
			<VmDisks
				{existing}
				{host}
				{volumes}
				{directory}
				{running}
				{busy}
				hostError={partErrors.host}
				onretryhost={() => loadPart('host')}
				ongrow={growDisk}
				ontouch={() => (touched = true)}
				bind:disks
				bind:grow
			/>
		</section>
		<section class="tile vm-card">
			<div class="step">
				<span class="section-icon"><Icon name="disc" size={15} /></span>
				<h2>{t('vms.form.installation')}</h2>
			</div>
			<VmInstall
				{media}
				{template}
				{running}
				virtioVolume={disks.find((d) => d.volume)?.volume ?? volumes[0]?.name ?? ''}
				virtioInitial={initial?.virtioISO ?? ''}
				virtioAuto={!existing}
				mediaError={partErrors.media}
				onretrymedia={() => loadPart('media')}
				bind:iso
				bind:virtioISO
				bind:virtioPending
				bind:autostart
			/>
		</section>
		<section class="tile vm-card">
			<div class="step">
				<span class="section-icon"><Icon name="globe" size={15} /></span>
				<h2>{t('vms.form.connections')}</h2>
			</div>
			<VmConnections
				{status}
				{hardware}
				{shares}
				vmName={existing?.name}
				{template}
				{running}
				hardwareError={partErrors.hardware}
				sharesError={partErrors.shares}
				onretryhardware={() => loadPart('hardware')}
				onretryshares={() => loadPart('shares')}
				onbridgestate={bridgeState}
				bind:net
				bind:pci
				bind:usb
				bind:autoGPU
				bind:folders
			/>
		</section>
		<section class="tile vm-card card-1" aria-labelledby="vm-schedule-title">
			<div class="step">
				<span class="section-icon"><Icon name="clock" size={15} /></span>
				<h2 id="vm-schedule-title">{t('vms.sched.title')}</h2>
			</div>
			<VmSchedule bind:draft={schedule} />
		</section>
	</div>

	<aside class="tile" aria-labelledby="summary-title">
		<h2 class="tile-title" id="summary-title">
			<Icon name="monitor" size={15} />
			{t('vms.new.summary')}
		</h2>
		<dl>
			<div>
				<dt>{t('vms.new.name')}</dt>
				<dd>{displayName.trim() || '–'}</dd>
			</div>
			<div>
				<dt>{t('vms.new.template')}</dt>
				<dd>{t(`vms.template.${template}`)}</dd>
			</div>
			<div>
				<dt>{t('vms.new.vcpus')}</dt>
				<dd class="mono">{vcpuCount}{pinning ? ` (CPU ${pinned.join(', ') || '–'})` : ''}</dd>
			</div>
			<div>
				<dt>{t('vms.new.memory')}</dt>
				<dd class="mono">{formatBytes(memoryMiB * MIB)}</dd>
			</div>
			{#if !existing}<div>
					<dt>{t('vms.new.step3')}</dt>
					<dd class="mono">
						{disks
							.map((disk) =>
								disk.mode === 'block' || disk.mode === 'direct'
									? disk.path.split('/').at(-1)
									: `${disk.sizeGB} GB ${disk.format === 'raw' ? 'img' : 'qcow2'} (${disk.volume})`,
							)
							.join(', ') || '–'}
					</dd>
				</div>{/if}
			<div>
				<dt>{t('vms.form.network')}</dt>
				<dd>
					{net.startsWith('network:')
						? (status?.networks?.find((n) => 'network:' + n.id === net)?.name ?? net)
						: t(net === 'bridge' ? 'vms.form.netBridge' : 'vms.form.netUser')}
				</dd>
			</div>
			<div>
				<dt>{t('vms.autostart')}</dt>
				<dd>{t(autostart ? 'vms.ov.yes' : 'vms.ov.no')}</dd>
			</div>
			<div>
				<dt>{t('vms.sched.title')}</dt>
				<dd>
					{schedule.enabled ? t('vms.sched.count', { n: schedule.rules.length }) : t('vms.ov.no')}
				</dd>
			</div>
			<div>
				<dt>{t('vms.new.medium')}</dt>
				<dd class="mono">{iso.trim().split('/').at(-1) || '–'}</dd>
			</div>
			<div>
				<dt>{t('vms.form.step4')}</dt>
				<dd class="mono">{[...pci, ...usb].join(', ') || '–'}</dd>
			</div>
			<div>
				<dt>{t('vms.form.step5')}</dt>
				<dd class="mono">{folders.map((folder) => folder.tag).join(', ') || '–'}</dd>
			</div>
		</dl>
		{#if error}<div class="notice-error" role="alert">
				{error.text}{#if error.detail}<pre>{error.detail}</pre>{/if}
			</div>{/if}
		{#if openPoints.length > 0 && !busy}
			<div class="open-points" role="status">
				<span>{t('vms.form.open')}</span>
				<ul>
					{#each openPoints as point, index (index)}<li>{point}</li>{/each}
				</ul>
			</div>
		{/if}
		{#if existing}
			<button type="button" class="btn btn-primary" onclick={save} disabled={blocked}
				>{t('vms.submit.edit')}</button
			>
			<a class="btn" href="/vms?vm={existing.name}">{t('common.cancel')}</a>
		{:else}
			{#if disks.some((disk) => disk.mode === 'copy')}<small>{t('vms.form.copyNote')}</small>{/if}
			<button
				type="button"
				class="btn btn-primary"
				onclick={() => create(true)}
				disabled={blocked || !status?.kvm || disks.some((disk) => disk.mode === 'copy')}
				>{t('vms.new.createStart')}</button
			>
			<button type="button" class="btn" onclick={() => create(false)} disabled={blocked}
				>{t('vms.new.createOnly')}</button
			>
		{/if}
	</aside>
</div>

{#if leaving}
	<Dialog title={t('shares.unsaved')} onclose={() => (leaving = null)}>
		<p>{t(existing ? 'vms.form.unsavedEdit' : 'vms.form.unsavedNew')}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (leaving = null)}
				>{t('shares.keepEditing')}</button
			><button type="button" class="btn btn-warn" onclick={discard}>{t('shares.discard')}</button>
		</div>
	</Dialog>
{/if}

<style>
	.open-points {
		display: flex;
		flex-direction: column;
		gap: 4px;
		font-size: var(--text-base);
		color: var(--warn-text);
	}
	.open-points ul {
		margin: 0;
		padding-left: 1.1rem;
	}
	.layout {
		display: grid;
		grid-template-columns: minmax(0, 1fr) 310px;
		align-items: start;
		gap: 16px;
	}
	.steps {
		min-width: 0;
		display: grid;
		grid-template-columns: repeat(2, minmax(0, 1fr));
		align-items: start;
		gap: 14px;
	}
	.vm-card {
		min-width: 0;
		overflow: hidden;
		padding: 0;
	}
	.card-1 {
		grid-column: 1 / -1;
	}
	.section-icon {
		color: var(--accent);
		display: inline-flex;
	}
	.advanced {
		border-top: 1px solid var(--panel-border);
		padding-top: 10px;
		min-width: 0;
	}
	summary {
		cursor: pointer;
		font-size: var(--text-base);
	}
	.advanced-body {
		display: flex;
		flex-direction: column;
		gap: 12px;
		padding-top: 12px;
		min-width: 0;
	}
	@media (max-width: 1100px) {
		.layout {
			grid-template-columns: minmax(0, 1fr) 280px;
		}
		.steps {
			grid-template-columns: minmax(0, 1fr);
		}
	}
	@media (max-width: 800px) {
		.layout {
			grid-template-columns: minmax(0, 1fr);
		}
		.layout > aside {
			position: static;
		}
	}
	.step {
		display: flex;
		align-items: center;
		gap: 12px;
		padding: 10px 16px;
		border-bottom: 1px solid var(--panel-border);
	}
	h2 {
		font-size: var(--text-sm);
		font-weight: 600;
		text-transform: uppercase;
		letter-spacing: 0.045em;
	}
	.content {
		display: flex;
		flex-direction: column;
		gap: 12px;
		padding: 16px;
	}
	.templates {
		display: grid;
		grid-template-columns: repeat(3, minmax(0, 1fr));
		gap: 10px;
	}
	.templates button {
		display: flex;
		flex-direction: column;
		gap: 4px;
		padding: 12px 14px;
		background: transparent;
		border: 2px solid var(--border);
		border-radius: var(--radius-ctl);
		color: var(--text);
		font: inherit;
		font-size: var(--text-base);
		text-align: left;
		cursor: pointer;
	}
	.templates .muted {
		font-size: var(--text-sm);
	}
	@media (max-width: 600px) {
		.templates {
			grid-template-columns: minmax(0, 1fr);
		}
	}
	.templates button[aria-checked='true'] {
		border-color: var(--accent);
	}
	.name {
		font-size: var(--text-md);
		font-weight: 600;
	}
	.row {
		display: flex;
		flex-wrap: wrap;
		gap: 14px;
	}
	label {
		display: flex;
		flex-direction: column;
		gap: 6px;
		font-size: var(--text-base);
	}
	label > span {
		color: var(--muted);
	}
	.narrow {
		flex: 1 1 260px;
	}
	.wide {
		flex: 2 1 320px;
	}
	small {
		font-size: var(--text-sm);
		color: var(--muted);
	}
	.warn {
		color: var(--warn-text);
	}
	aside {
		min-width: 0;
		position: sticky;
		top: 80px;
		display: flex;
		flex-direction: column;
		gap: 12px;
		padding: 18px;
	}
	dl {
		display: flex;
		flex-direction: column;
		gap: 8px;
		margin: 0;
		font-size: var(--text-base);
	}
	dl div {
		display: flex;
		justify-content: space-between;
		gap: 12px;
	}
	dt {
		color: var(--muted);
	}
	dd {
		margin: 0;
		text-align: right;
		overflow-wrap: anywhere;
	}
	pre {
		margin: 8px 0 0;
		font-family: var(--font-mono);
		font-size: var(--text-sm);
		white-space: pre-wrap;
		word-break: break-word;
	}
	.check {
		flex-direction: row;
		align-items: center;
		gap: 10px;
		font-size: var(--text-md);
	}
	.check span {
		color: var(--text);
	}
	.check input {
		width: 18px;
		height: 18px;
		margin: 0;
		flex: none;
	}
</style>
