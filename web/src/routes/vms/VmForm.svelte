<script lang="ts">
	// The form a VM is made and changed with. With `existing` it edits that
	// VM: the internal identifier, template and firmware stay fixed.
	import { untrack } from 'svelte';
	import { goto } from '$app/navigation';
	import { api, ApiError, type Hardware, type Share, type VM, type VMHost, type VMStatus } from '#lib/api.ts';
	import { formatBytes } from '#lib/format.ts';
	import { errorText, t, tOr } from '#lib/i18n/index.svelte.ts';
	import { storage } from '#lib/storage.svelte.ts';
	import { newVMIdentifier, validVMDisplayName, vmLabel } from '#lib/vm-name.ts';

	let { existing }: { existing?: VM } = $props();

	const TEMPLATES = ['linux', 'windows', 'other'] as const;
	const SIZES = [16, 32, 64, 128, 256, 512];
	const TAG = /^[A-Za-z0-9_-]{1,36}$/;
	const GIB = 1024 ** 3;

	type DiskDraft = {
		/** A new empty disk, a copy of an image, an image as it is, or a whole disk. */
		mode: 'new' | 'copy' | 'direct' | 'block';
		format: 'qcow2' | 'raw';
		sizeGiB: number;
		volume: string;
		/** The image or the whole disk, for everything but `new`. */
		path: string;
	};

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
  const image = disks.find(d => d.mode === 'direct');
  const match = image?.path.match(/^\/mnt\/([^/]+)\/vms\/([^/]+)\/[^/]+$/);
  return match ? { volume: match[1], directory: match[2] } : null;
 });
 let displayName = $state(initial ? vmLabel(initial) : '');
	let description = $state(initial?.description ?? '');
	let vcpus = $state(initial?.vcpus ?? 2);
	let memoryGiB = $state(initial ? initial.memoryMiB / 1024 : 2);
	let pinning = $state((initial?.cpuPin?.length ?? 0) > 0);
	let pinned = $state<number[]>(initial?.cpuPin ?? []);
	let exclusive = $state(initial?.exclusive ?? false);
	let secure = $state(initial ? initial.tpm : false);
	let disks = $state<DiskDraft[]>([]);
	let iso = $state(initial?.iso ?? '');
	let autostart = $state(initial?.autostart ?? false);
	let folders = $state((initial?.folders ?? []).map((folder) => ({ ...folder })));
	let autoGPU = $state(initial?.autoGPU ?? false);
	let pci = $state<string[]>(initial?.pci ?? []);
	let usb = $state<string[]>(initial?.usb ?? []);
	let net = $state<'user' | 'bridge'>(initial?.network ?? 'user');
	let grow = $state<Record<number, number>>({});
	let busy = $state(false);
	let error = $state<{ text: string; detail: string } | null>(null);
	let touched = false;

	const running = $derived(existing?.state === 'running');
	const volumes = $derived((storage.status?.volumes ?? []).filter((volume) => volume.mounted));
	const free = (volume: string) => {
		const found = volumes.find((candidate) => candidate.name === volume);
		return found ? found.sizeBytes - found.usedBytes : 0;
	};
	const machine = $derived(status?.host ?? { availableMemoryBytes: 0, cpus: 1, memoryBytes: 0, usedMemoryBytes: 0, cpuModel: '' });
	const maxMemory = $derived(Math.max(1, Math.floor(machine.memoryBytes / GIB)));
	const freeMemory = $derived(Math.max(0, machine.availableMemoryBytes ?? machine.memoryBytes - machine.usedMemoryBytes));
	const taken = $derived((status?.vms ?? []).map((vm) => vm.name));
	const name = $derived(initial?.name ?? newVMIdentifier(displayName, taken));
 const directory = $derived(chosenDirectory.trim() || (reuseImageFolder ? directFolder?.directory : '') || name);
	const hybrid = $derived(host.cpus.some((cpu) => cpu.kind !== ''));

	const fail = (err: unknown) => (error = { text: errorText(err instanceof ApiError ? err.code : 'internal'), detail: '' });
	$effect(() => {
		api.get<VMStatus>('/vms').then((res) => {
			status = res;
			if (!existing) vcpus = Math.min(2, res.host.cpus);
		}, fail);
		api.get<VMHost>('/vms/host').then((res) => (host = res), () => {});
		api.get<Hardware>('/hardware').then((res) => (hardware = res), () => {});
		api.get<Share[]>('/shares').then((res) => (shares = res.filter((share) => share.available)), () => {});
		api.get<{ media: typeof media }>('/vms/media').then((res) => (media = res.media), () => {});
	});
	// A first disk, once it is known where it can lie.
	$effect(() => {
		if (!existing && disks.length === 0 && volumes.length > 0 && !touched) disks.push({ mode: 'new', format: 'qcow2', sizeGiB: 64, volume: volumes[0].name, path: '' });
	});
	// Windows 11 asks for a TPM and Secure Boot: on with that template
	// unless switched off, and not offered with the others.
	$effect(() => {
		if (!existing) secure = template === 'windows';
	});

	// --- processor -------------------------------------------------------

	/** Threads of one core go to a VM together. */
	function togglePin(id: number) {
		const core = host.cpus.find((cpu) => cpu.id === id)?.siblings ?? [id];
		pinned = pinned.includes(id) ? pinned.filter((cpu) => !core.includes(cpu)) : [...new Set([...pinned, ...core])].sort((a, b) => a - b);
	}
	const others = (cpu: { usedBy: string[] }) => cpu.usedBy.filter((vm) => vm !== existing?.name);
	const preset = (pick: (cpu: VMHost['cpus'][number]) => boolean) => (pinned = host.cpus.filter(pick).map((cpu) => cpu.id));
	function cpuNote(cpu: VMHost['cpus'][number]): string {
		if (others(cpu).length > 0) return t('vms.form.cpuUsedBy', { vms: others(cpu).join(', ') });
		if (cpu.id === 0) return t('vms.form.cpuZero');
		const sibling = cpu.siblings.filter((id) => id !== cpu.id);
		return sibling.length > 0 ? t('vms.form.cpuSibling', { cpu: sibling.join(', ') }) : '';
	}
	const cpuWarnings = $derived.by(() => {
		if (!pinning) return [];
		const chosen = host.cpus.filter((cpu) => pinned.includes(cpu.id));
		const warnings: string[] = [];
		if (chosen.length === 0) warnings.push(t('vms.form.warnNone'));
		if (new Set(chosen.map((cpu) => cpu.kind)).size > 1) warnings.push(t('vms.form.warnMixed'));
		if (pinned.includes(0)) warnings.push(t('vms.form.warnZero'));
		if (chosen.some((cpu) => others(cpu).length > 0)) warnings.push(t('vms.form.warnShared'));
		return warnings;
	});
	const vcpuCount = $derived(pinning ? pinned.length : vcpus);

	// --- disks -------------------------------------------------------------

	const newDisks = $derived(disks.filter((disk) => disk.mode === 'new' || disk.mode === 'copy'));
	const fileOf = (disk: DiskDraft) => `vdisk${(existing?.disks.filter((old) => !old.path).length ?? 0) + newDisks.indexOf(disk) + 1}.${disk.format === 'raw' ? 'img' : 'qcow2'}`;
	const pathOf = (disk: DiskDraft) => (disk.mode === 'direct' || disk.mode === 'block' ? disk.path : `/mnt/${disk.volume}/vms/${name || '<name>'}/${fileOf(disk)}`);
	const imageOf = (disk: DiskDraft) => host.images.find((image) => image.path === disk.path);
	const directImages = $derived(host.images.filter((image) => (image.format === 'qcow2' || image.format === 'raw') && !image.usedBy));
	const freeDisks = $derived(host.disks.filter((disk) => !disk.usedBy));

	function addDisk(mode: DiskDraft['mode']) {
		touched = true;
		const volume = disks.findLast((disk) => disk.volume)?.volume ?? volumes[0]?.name ?? '';
		if (mode === 'new') disks.push({ mode, format: 'qcow2', sizeGiB: 64, volume, path: '' });
		else if (mode === 'copy') pickImage({ mode, format: 'qcow2', sizeGiB: 1, volume, path: '' }, host.images[0]?.path ?? '', true);
		else if (mode === 'block') disks.push({ mode, format: 'raw', sizeGiB: 0, volume: '', path: freeDisks[0]?.path ?? '' });
	}
	/** A copy starts as large as its original. */
	function pickImage(disk: DiskDraft, path: string, add = false) {
		disk.path = path;
		const image = host.images.find((candidate) => candidate.path === path);
		if (image) disk.sizeGiB = Math.max(disk.sizeGiB, Math.ceil(image.virtualBytes / GIB));
		if (add) disks.push(disk);
	}
	function setUse(disk: DiskDraft, mode: 'copy' | 'direct') {
		disk.mode = mode;
		if (mode === 'copy' && !disk.volume) disk.volume = volumes[0]?.name ?? '';
	}

	function diskNote(disk: DiskDraft): { text: string; warn: boolean } {
		if (disk.mode === 'block') return { text: t('vms.form.blockNote'), warn: true };
		if (disk.mode === 'direct') return { text: t('vms.form.directNote'), warn: false };
		const available = free(disk.volume);
		const size = disk.sizeGiB * GIB;
		const image = imageOf(disk);
		if (disk.mode === 'copy' && image && size < image.virtualBytes) return { text: t('vms.form.copyTooSmall', { size: formatBytes(image.virtualBytes) }), warn: true };
		if (disk.format === 'raw') {
			return size > available ? { text: t('vms.new.rawTooLarge', { free: formatBytes(available) }), warn: true } : { text: t('vms.new.rawNote', { free: formatBytes(available - size) }), warn: false };
		}
		if (disk.mode === 'copy' && image && size > image.virtualBytes) return { text: t('vms.form.copyGrown'), warn: false };
		return size > available ? { text: t('vms.new.qcowOver', { free: formatBytes(available) }), warn: true } : { text: t('vms.new.qcowNote'), warn: false };
	}
	const diskBlocked = (disk: DiskDraft) => {
		if (disk.mode === 'direct' || disk.mode === 'block') return disk.path === '';
		const image = imageOf(disk);
		return !(disk.sizeGiB >= 1) || (disk.mode === 'copy' && (!image || disk.sizeGiB * GIB < image.virtualBytes)) || (disk.format === 'raw' && disk.sizeGiB * GIB > free(disk.volume));
	};

	async function growDisk(index: number) {
		if (!existing) return;
		busy = true;
		error = null;
		try {
			await api.post(`/vms/${existing.name}/grow`, { disk: index, sizeBytes: Math.round(grow[index] * GIB) });
			existing.disks[index].sizeBytes = Math.round(grow[index] * GIB);
			delete grow[index];
		} catch (err) {
			fail(err);
		}
		busy = false;
	}

	// --- devices and folders ----------------------------------------------

	const usedElsewhere = (id: string) => (hardware?.usedBy[id] ?? []).filter((vm) => vm !== existing?.name);
	const toggle = (list: string[], id: string) => (list.includes(id) ? list.filter((entry) => entry !== id) : [...list, id]);
	function togglePCI(address: string, companions: string[]) {
		// Removing a card also removes its dependent selections. Adding it
		// never selects another function on the owner's behalf.
		pci = pci.includes(address) ? pci.filter((id) => id !== address && !companions.includes(id)) : [...pci, address];
	}
	const folderProblem = (folder: { path: string; tag: string }, index: number) =>
		!folder.path.startsWith('/mnt/') ? t('vms.form.folderPath') : !TAG.test(folder.tag) ? t('vms.form.folderTag') : folders.some((other, at) => at !== index && other.tag === folder.tag) ? t('vms.form.folderTwice') : '';

	// --- submit --------------------------------------------------------------

	const duplicateName = $derived((status?.vms ?? []).some((vm) => vm.name !== existing?.name && vmLabel(vm).toLowerCase() === displayName.trim().toLowerCase()));
	const nameProblem = $derived(displayName === '' ? '' : !validVMDisplayName(displayName) ? t('vms.displayNameInvalid') : duplicateName ? t('vms.new.nameExists') : '');
	const memoryNote = $derived(memoryGiB * GIB > freeMemory && !running ? t('vms.new.memoryOver', { free: formatBytes(freeMemory) }) : '');
	const blocked = $derived(
		busy ||
			!validVMDisplayName(displayName) || duplicateName ||
			(!existing && disks.length === 0 && iso.trim() === '') ||
			disks.some(diskBlocked) ||
			(pinning && pinned.length === 0) ||
			folders.some((folder, index) => folderProblem(folder, index) !== '')
	);

	function problem(err: unknown) {
		const code = err instanceof ApiError ? err.code : 'internal';
		error = { text: errorText(code), detail: ['vm_failed', 'hardware_incomplete_group', 'invalid_folder'].includes(code) && err instanceof ApiError ? err.message : '' };
	}

	const hardwareSettings = () => ({
		vcpus: vcpuCount,
		memoryMiB: Math.round(memoryGiB * 1024),
		cpuPin: pinning ? pinned : [],
		exclusive: pinning && exclusive,
		iso: iso.trim(),
		folders: folders.map((folder) => ({ path: folder.path.trim().replace(/\/+$/, ''), tag: folder.tag, readOnly: folder.readOnly })),
		pci,
		autoGPU,
		usb,
		network: net
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
				autostart,
				tpm: secure,
				secureBoot: secure,
				...hardwareSettings(),
				disks: disks.map((disk) =>
					disk.mode === 'direct' || disk.mode === 'block'
						? { path: disk.path }
						: { format: disk.format, sizeBytes: Math.round(disk.sizeGiB * GIB), volume: disk.volume, source: disk.mode === 'copy' ? disk.path : undefined }
				),
				// A disk that is still being copied cannot be started from.
				start: start && !disks.some((disk) => disk.mode === 'copy')
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
			await api.put(`/vms/${existing.name}`, { displayName: displayName.trim(), description, autostart, ...hardwareSettings() });
			await goto(`/vms?vm=${existing.name}`);
		} catch (err) {
			problem(err);
			busy = false;
		}
	}
</script>

<div class="title">
	<p class="muted crumbs"><a href="/vms">{t('vms.title')}</a> › {existing ? vmLabel(existing) : t('vms.new.crumb')}</p>
	<h1>{existing ? t('vms.dialog.edit', { name: vmLabel(existing) }) : t('vms.new.title')}</h1>
</div>

{#if status && !status.kvm}<p class="notice-warn" role="status">{t('vms.noKvm')}</p>{/if}
{#if storage.status && volumes.length === 0}<p class="notice-warn" role="status">{t('vms.new.needVolume')}</p>{/if}
{#if running}<p class="notice-warn" role="status">{t('vms.form.running')}</p>{/if}

<div class="layout">
	<div class="steps">
		<section class="tile" aria-labelledby="s1">
			<div class="step"><span class="number" aria-hidden="true">1</span><h2 id="s1">{t('vms.new.step1')}</h2></div>
			<div class="content">
				{#if !existing}
					<div class="templates" role="radiogroup" aria-label={t('vms.new.template')}>
						{#each TEMPLATES as entry (entry)}
							<button type="button" role="radio" aria-checked={template === entry} onclick={() => (template = entry)}>
								<span class="name">{t(`vms.template.${entry}`)}</span>
								<span class="muted">{t(`vms.template.${entry}.desc`)}</span>
							</button>
						{/each}
					</div>
					{#if template === 'windows'}
						<label class="check"><input type="checkbox" bind:checked={secure} /><span>{t('vms.form.secure')}</span></label>
						<small>{t('vms.form.secureNote')}</small>
					{/if}
				{/if}
				<div class="row">
					<label class="narrow"><span>{t('vms.new.name')}</span>
						<input class="field" type="text" bind:value={displayName} maxlength="160" aria-invalid={nameProblem !== ''} disabled={busy} placeholder="Home Assistant" />
						{#if nameProblem}<small class="warn" role="alert">{nameProblem}</small>{/if}
					</label>
					<label class="wide"><span>{t('vms.new.description')}</span><input class="field" type="text" bind:value={description} maxlength="200" /></label>
				</div>
				{#if !existing}
     <label><span>{t('vms.directory')}</span><input class="field" bind:value={chosenDirectory} placeholder={directory} disabled={busy} /><small>{t('vms.directoryHint')}</small></label>
     {#if directFolder}<label class="check"><input type="checkbox" bind:checked={reuseImageFolder} disabled={busy || !!chosenDirectory.trim()} /><span>{t('vms.reuseDirectory', { name: directFolder.directory })}</span></label>{/if}
     <p class="mono muted">{t('vms.directoryPreview', { name: directory })}</p>
    {:else}<p class="mono muted">{t('vms.directoryPreview', { name: existing.directory || existing.name })}</p>{/if}
    <label class="check"><input type="checkbox" bind:checked={autostart} /><span>{t('vms.autostartLabel')}</span></label>
			</div>
		</section>

		<section class="tile" aria-labelledby="s2">
			<div class="step"><span class="number" aria-hidden="true">2</span><h2 id="s2">{t('vms.new.step2')}</h2></div>
			<fieldset class="content" disabled={running}>
				<!-- Only once the limits are known: a slider made with a smaller maximum would show the wrong position. -->
				{#if status}
					<div class="line">
						<span>{t('vms.form.cores')}{#if machine.cpuModel} <span class="muted">· {machine.cpuModel}</span>{/if}</span>
						<div class="segments" role="radiogroup" aria-label={t('vms.form.cores')}>
							<button type="button" role="radio" aria-checked={!pinning} onclick={() => (pinning = false)}>{t('vms.form.auto')}</button>
							<button type="button" role="radio" aria-checked={pinning} onclick={() => (pinning = true)}>{t('vms.form.pin')}</button>
						</div>
					</div>
					{#if !pinning}
						<div class="slider">
							<div class="line"><label for="vm-vcpus">{t('vms.new.vcpus')}</label><span class="mono">{t('vms.new.of', { value: vcpus, max: machine.cpus })}</span></div>
							<input id="vm-vcpus" type="range" min="1" max={machine.cpus} step="1" bind:value={vcpus} />
							<small>{t('vms.form.autoNote')}</small>
						</div>
					{:else}
						<div class="presets">
							{#if hybrid}
								<button type="button" onclick={() => preset((cpu) => cpu.kind === 'p' && cpu.id !== 0 && !cpu.siblings.includes(0))}>{t('vms.form.presetP')}</button>
								<button type="button" onclick={() => preset((cpu) => cpu.kind === 'e' && others(cpu).length === 0)}>{t('vms.form.presetE')}</button>
							{/if}
							<button type="button" onclick={() => (pinned = [])}>{t('vms.form.presetNone')}</button>
						</div>
						<div class="cpus" role="group" aria-label={t('vms.form.pin')}>
							{#each host.cpus as cpu (cpu.id)}
								<button type="button" aria-pressed={pinned.includes(cpu.id)} onclick={() => togglePin(cpu.id)}>
									<span class="cpu-head"><span class="mono">CPU {cpu.id}</span>{#if cpu.kind}<span class="chip">{t(cpu.kind === 'p' ? 'vms.form.pCore' : 'vms.form.eCore')}</span>{/if}</span>
									<span class="muted cpu-note">{cpuNote(cpu)}</span>
								</button>
							{/each}
						</div>
						<small>{t('vms.form.pinSummary', { count: pinned.length, cpus: pinned.join(', ') || '–' })}</small>
						<small>{t('vms.form.pinNote')}</small>
						<label class="check"><input type="checkbox" bind:checked={exclusive} /><span>{t('vms.form.exclusive')}</span></label>
						<small>{t('vms.form.exclusiveNote')}</small>
						{#each cpuWarnings as warning (warning)}<small class="warn" role="status">{warning}</small>{/each}
					{/if}
					<div class="slider">
						<div class="line"><label for="vm-memory">{t('vms.new.memory')}</label><span class="mono">{t('vms.new.memoryValue', { value: memoryGiB, free: formatBytes(freeMemory) })}</span></div>
						<input id="vm-memory" type="range" min="1" max={maxMemory} step="1" bind:value={memoryGiB} />
						{#if memoryNote}<small class="warn">{memoryNote}</small>{/if}
					</div>
				{/if}
				<div class="facts">
					<span class="chip">UEFI (OVMF)</span>
					<span class="chip">{t('vms.new.machine')}</span>
					<span class="chip">{t('vms.new.cpuMode')}</span>
					{#if secure}<span class="chip">{t('vms.form.secureChip')}</span>{/if}
				</div>
			</fieldset>
		</section>

		<section class="tile" aria-labelledby="s3">
			<div class="step"><span class="number" aria-hidden="true">3</span><h2 id="s3">{t('vms.new.step3')}</h2></div>
			<fieldset class="content" disabled={running}>
				{#if existing}
					{#each existing.disks as disk, index (index)}
						<div class="disk">
							<div class="line">
								<span class="mono file">{disk.path || disk.file}</span>
								<span class="muted">{disk.path ? t(disk.path.startsWith('/dev/') ? 'vms.form.wholeDisk' : 'vms.form.existingImage') : `${disk.format === 'raw' ? 'img' : 'qcow2'} · ${disk.volume}`}</span>
								<span class="grow"></span>
								<span class="mono">{formatBytes(disk.sizeBytes)}</span>
							</div>
							{#if !disk.path?.startsWith('/dev/')}
								<div class="size">
									<label for="disk-grow-{index}">{t('vms.form.growTo')}</label>
									<input id="disk-grow-{index}" class="field mono number" type="number" min={Math.ceil(disk.sizeBytes / GIB) + 1} max="16384" bind:value={grow[index]} placeholder={String(Math.ceil(disk.sizeBytes / GIB))} /><span class="muted">GB</span>
									<button type="button" class="btn small" onclick={() => growDisk(index)} disabled={busy || !(grow[index] * GIB > disk.sizeBytes)}>{t('vms.form.grow')}</button>
								</div>
								<small>{t('vms.form.growNote')}</small>
							{/if}
						</div>
					{/each}
				{:else}
					<p class="muted note">{t('vms.new.formats')}</p>
					{#each disks as disk, index (index)}
						{@const note = diskNote(disk)}
						{@const image = imageOf(disk)}
						<div class="disk">
							<div class="line">
								<span class="mono file">{disk.mode === 'block' ? t('vms.form.wholeDisk') : disk.mode === 'direct' ? t('vms.form.existingImage') : fileOf(disk)}</span>
								<span class="muted">{t(index === 0 ? 'vms.new.systemDisk' : 'vms.new.dataDisk')}</span>
								<span class="grow"></span>
								{#if disk.mode === 'new' || disk.mode === 'copy'}
									<div class="segments" role="radiogroup" aria-label={t('vms.new.format', { file: fileOf(disk) })}>
										<button type="button" role="radio" aria-checked={disk.format === 'qcow2'} onclick={() => (disk.format = 'qcow2')}>qcow2</button>
										<button type="button" role="radio" aria-checked={disk.format === 'raw'} onclick={() => (disk.format = 'raw')}>img (raw)</button>
									</div>
								{/if}
								<button type="button" class="btn small" aria-label={t('vms.new.removeDisk', { file: pathOf(disk) })} onclick={() => ((touched = true), disks.splice(index, 1))}>✕</button>
							</div>
							{#if disk.mode === 'copy' || disk.mode === 'direct'}
								<label><span>{t('vms.form.image')}</span>
									<select class="field mono" value={disk.path} onchange={(event) => pickImage(disk, event.currentTarget.value)}>
										{#each disk.mode === 'direct' ? directImages : host.images as candidate (candidate.path)}
											<option value={candidate.path}>{candidate.path} · {candidate.format} · {formatBytes(candidate.virtualBytes)}{candidate.usedBy ? ` · ${t('vms.form.usedBy', { vm: candidate.usedBy })}` : ''}</option>
										{/each}
									</select>
								</label>
								<div class="place">
									<span class="muted">{t('vms.form.use')}</span>
									<div class="segments" role="radiogroup" aria-label={t('vms.form.use')}>
										<button type="button" role="radio" aria-checked={disk.mode === 'copy'} onclick={() => setUse(disk, 'copy')}>{t('vms.form.useCopy')}</button>
										<button type="button" role="radio" aria-checked={disk.mode === 'direct'} onclick={() => setUse(disk, 'direct')} disabled={!image || (image.format !== 'qcow2' && image.format !== 'raw') || !!image.usedBy}>{t('vms.form.useDirect')}</button>
									</div>
								</div>
							{/if}
							{#if disk.mode === 'block'}
								<label><span>{t('vms.form.disk')}</span>
									<select class="field mono" bind:value={disk.path}>
										{#each freeDisks as candidate (candidate.path)}<option value={candidate.path}>{candidate.device} · {candidate.path}</option>{/each}
									</select>
								</label>
							{/if}
							{#if disk.mode === 'new' || disk.mode === 'copy'}
								<div class="size">
									<label for="disk-size-{index}">{t('vms.new.size')}</label>
									<input type="range" min="8" max="2048" step="8" bind:value={disk.sizeGiB} aria-label={t('vms.new.sizeOf', { file: fileOf(disk) })} />
									<input id="disk-size-{index}" class="field mono number" type="number" min="1" max="16384" bind:value={disk.sizeGiB} /><span class="muted">GB</span>
								</div>
								{#if disk.mode === 'new'}
									<div class="presets">
										{#each SIZES as size (size)}
											<button type="button" aria-pressed={disk.sizeGiB === size} onclick={() => (disk.sizeGiB = size)}>{size} GB</button>
										{/each}
									</div>
								{/if}
								<div class="place">
									<span class="muted">{t('vms.new.place')}</span>
									<div class="segments" role="radiogroup" aria-label={t('vms.new.place')}>
										{#each volumes as volume (volume.name)}
											<button type="button" role="radio" aria-checked={disk.volume === volume.name} onclick={() => (disk.volume = volume.name)}>{t('vms.new.volume', { name: volume.name, free: formatBytes(volume.sizeBytes - volume.usedBytes) })}</button>
										{/each}
									</div>
								</div>
							{/if}
							<small class:warn={note.warn}>{note.text}</small>
							<span class="muted mono path">→ {pathOf(disk)}</span>
						</div>
					{/each}
					<div class="adds">
						<button type="button" class="add" onclick={() => addDisk('new')} disabled={volumes.length === 0 || disks.length >= 8}>+ {t('vms.form.addNew')}</button>
						<button type="button" class="add" onclick={() => addDisk('copy')} disabled={volumes.length === 0 || host.images.length === 0 || disks.length >= 8}>+ {t('vms.form.addImage')}</button>
						<button type="button" class="add" onclick={() => addDisk('block')} disabled={freeDisks.length === 0 || disks.length >= 8}>+ {t('vms.form.addBlock')}</button>
					</div>
					{#if host.images.length === 0}<small>{t('vms.form.noImages')}</small>{/if}
				{/if}

				<div class="medium">
					<label for="vm-iso">{t('vms.new.medium')}</label>
					{#if media.length > 0}
						<select class="field mono" value={media.some((entry) => entry.path === iso) ? iso : ''} onchange={(event) => (iso = event.currentTarget.value)} aria-label={t('vms.new.mediumFound')}>
							<option value="">{t('vms.new.mediumPick')}</option>
							{#each media as entry (entry.path)}<option value={entry.path}>{entry.path} · {formatBytes(entry.sizeBytes)}</option>{/each}
						</select>
					{/if}
					<input id="vm-iso" class="field mono" type="text" bind:value={iso} placeholder="/mnt/…/….iso" spellcheck="false" />
					<small>{t(media.length > 0 ? 'vms.new.mediumHint' : 'vms.new.mediumNone')}</small>
				</div>
			</fieldset>
		</section>

		<section class="tile" aria-labelledby="s4">
			<div class="step"><span class="number" aria-hidden="true">4</span><h2 id="s4">{t('vms.form.step4')}</h2><span class="grow"></span><a class="muted small-link" href="/settings/hardware">{t('vms.form.hardwarePage')}</a></div>
			<fieldset class="content" disabled={running}>
				<p class="muted note">{t('vms.form.explicitDevices')}</p>
				{#if hardware && !hardware.hardware.iommu && hardware.hardware.pci.length > 0}<p class="notice-warn">{t('hardware.noIommu')}</p>{/if}
				{#each hardware?.hardware.pci ?? [] as device (device.address)}
					{@const elsewhere = usedElsewhere(device.address)}
					<div class="device">
						<label class="check"><input type="checkbox" checked={pci.includes(device.address)} disabled={!device.passable} onchange={() => togglePCI(device.address, device.companions)} /><span>{t(`hardware.kind.${device.kind}`)} · {device.vendorName || device.vendor}</span></label>
						<span class="muted mono ids">{device.address} · {device.vendor}:{device.device}{device.companions.length > 0 ? ` · ${t('hardware.companions', { list: device.companions.join(', ') })}` : ''}</span>
						{#if pci.includes(device.address)}
							{#each device.companions as address (address)}
								<label class="check"><input type="checkbox" checked={pci.includes(address)} onchange={() => (pci = toggle(pci, address))} /><span>{t('vms.form.companion', { address })}</span></label>
							{/each}
							{#if device.companions.length}<small>{t('vms.form.requiredCompanions', { list: device.companions.join(', ') })}</small>{/if}
						{/if}
						{#if !device.passable}
							<small>{tOr(`hardware.reason.${device.reason}`, device.reason)}</small>
						{:else}
							{#if device.primary}<small class="warn">{t('hardware.warnPrimary')}</small>{/if}
							{#if device.kind === 'igpu'}<small class="warn">{t('hardware.warnIgpu')}</small>{/if}
							{#if elsewhere.length > 0}<small>{t('hardware.alsoIn', { vms: elsewhere.join(', ') })}</small>{/if}
						{/if}
					</div>
				{/each}
				{#each hardware?.hardware.usb ?? [] as device (device.port)}
					{@const elsewhere = usedElsewhere(device.id)}
					<div class="device">
						<label class="check"><input type="checkbox" checked={usb.includes(device.id)} disabled={device.stick} onchange={() => (usb = toggle(usb, device.id))} /><span>USB · {device.name || device.id}</span></label>
						<span class="muted mono ids">{device.id} · {t('hardware.port', { port: device.port })}</span>
						{#if device.stick}<small>{t('hardware.stick')}</small>{/if}
						{#if device.duplicate}<small class="warn">{t('hardware.duplicate')}</small>{/if}
						{#if elsewhere.length > 0}<small>{t('hardware.alsoIn', { vms: elsewhere.join(', ') })}</small>{/if}
					</div>
				{/each}
				{#if hardware && hardware.hardware.pci.length === 0 && hardware.hardware.usb.length === 0}<p class="muted note">{t('vms.form.noDevices')}</p>{/if}
				<label class="check"><input type="checkbox" bind:checked={autoGPU} /><span>{t('hardware.autoGPU')}</span></label>
				<p class="muted note">{t('hardware.autoGPUHint')}</p>
				{#if pci.length > 0}<small>{t('vms.form.passNote')}</small>{/if}
				<label class="netsel"><span>{t('vms.form.network')}</span>
					<select class="field" bind:value={net}>
						<option value="user">{t('vms.form.netUser')}</option>
						<option value="bridge" disabled={!status?.bridge}>{t('vms.form.netBridge')}</option>
					</select>
					{#if !status?.bridge}<small>{t('vms.form.netNoBridge')} <a href="/settings/network">{t('netcfg.title')}</a></small>{/if}
				</label>
			</fieldset>
		</section>

		<section class="tile" aria-labelledby="s5">
			<div class="step"><span class="number" aria-hidden="true">5</span><h2 id="s5">{t('vms.form.step5')}</h2></div>
			<fieldset class="content" disabled={running}>
				<datalist id="vm-shares">{#each shares as share (share.name)}<option value={share.path}></option>{/each}</datalist>
				{#each folders as folder, index (index)}
					{@const issue = folder.path || folder.tag ? folderProblem(folder, index) : ''}
					<div class="folder">
						<input class="field mono path-field" type="text" list="vm-shares" bind:value={folder.path} placeholder="/mnt/…" aria-label={t('vms.form.folderOnNas')} spellcheck="false" />
						<span class="muted" aria-hidden="true">→</span>
						<input class="field mono tag-field" type="text" bind:value={folder.tag} placeholder={t('vms.form.folderName')} aria-label={t('vms.form.folderInVm')} autocapitalize="none" spellcheck="false" />
						<select class="field ro-field" value={folder.readOnly ? 'ro' : 'rw'} onchange={(event) => (folder.readOnly = event.currentTarget.value === 'ro')} aria-label={t('vms.form.folderAccess')}>
							<option value="rw">{t('editor.readWrite')}</option>
							<option value="ro">{t('editor.readOnly')}</option>
						</select>
						<button type="button" class="btn small" aria-label={t('vms.form.folderRemove')} onclick={() => folders.splice(index, 1)}>✕</button>
						{#if issue}<small class="warn wide-note">{issue}</small>{/if}
					</div>
				{:else}
					<p class="muted note">{t('vms.form.noFolders')}</p>
				{/each}
				<button type="button" class="add" onclick={() => folders.push({ path: shares[0]?.path ?? '', tag: shares[0]?.name ?? '', readOnly: false })} disabled={folders.length >= 8}>+ {t('vms.form.addFolder')}</button>
				{#if folders.length > 0}
					<small>{t(template === 'windows' ? 'vms.form.folderWindows' : 'vms.form.folderLinux')}</small>
					{#if template !== 'windows'}<span class="mono path">mount -t virtiofs {folders[0].tag || '<name>'} /mnt</span>{/if}
				{/if}
			</fieldset>
		</section>
	</div>

	<aside class="tile" aria-labelledby="summary-title">
		<h2 class="tile-title" id="summary-title">{t('vms.new.summary')}</h2>
		<dl>
			<div><dt>{t('vms.new.name')}</dt><dd>{displayName.trim() || '–'}</dd></div>
			<div><dt>{t('vms.new.template')}</dt><dd>{t(`vms.template.${template}`)}</dd></div>
			<div><dt>{t('vms.new.vcpus')}</dt><dd class="mono">{vcpuCount}{pinning ? ` (CPU ${pinned.join(', ') || '–'})` : ''}</dd></div>
			<div><dt>{t('vms.new.memory')}</dt><dd class="mono">{memoryGiB} GB</dd></div>
			{#if !existing}<div><dt>{t('vms.new.step3')}</dt><dd class="mono">{disks.map((disk) => (disk.mode === 'block' || disk.mode === 'direct' ? disk.path.split('/').at(-1) : `${disk.sizeGiB} GB ${disk.format === 'raw' ? 'img' : 'qcow2'} (${disk.volume})`)).join(', ') || '–'}</dd></div>{/if}
			<div><dt>{t('vms.new.medium')}</dt><dd class="mono">{iso.trim().split('/').at(-1) || '–'}</dd></div>
			<div><dt>{t('vms.form.step4')}</dt><dd class="mono">{[...pci, ...usb].join(', ') || '–'}</dd></div>
			<div><dt>{t('vms.form.step5')}</dt><dd class="mono">{folders.map((folder) => folder.tag).join(', ') || '–'}</dd></div>
		</dl>
		{#if error}<div class="notice-error" role="alert">{error.text}{#if error.detail}<pre>{error.detail}</pre>{/if}</div>{/if}
		{#if existing}
			<button type="button" class="btn btn-primary" onclick={save} disabled={blocked}>{t('vms.submit.edit')}</button>
			<a class="btn" href="/vms?vm={existing.name}">{t('common.cancel')}</a>
		{:else}
			{#if disks.some((disk) => disk.mode === 'copy')}<small>{t('vms.form.copyNote')}</small>{/if}
			<button type="button" class="btn btn-primary" onclick={() => create(true)} disabled={blocked || !status?.kvm || disks.some((disk) => disk.mode === 'copy')}>{t('vms.new.createStart')}</button>
			<button type="button" class="btn" onclick={() => create(false)} disabled={blocked}>{t('vms.new.createOnly')}</button>
		{/if}
	</aside>
</div>

<style>
	.title {
		display: flex;
		flex-direction: column;
		gap: 4px;
	}
	.crumbs {
		font-size: 0.8125rem;
	}
	h1 {
		font-size: 1.5rem;
		font-weight: 600;
	}
	.layout {
		display: flex;
		flex-wrap: wrap;
		align-items: flex-start;
		gap: 20px;
	}
	.steps {
		flex: 999 1 640px;
		min-width: 0;
		display: flex;
		flex-direction: column;
		gap: 20px;
	}
	.step {
		display: flex;
		align-items: center;
		gap: 12px;
		padding: 14px 18px;
		border-bottom: 1px solid var(--panel-border);
	}
	.number {
		width: 1.75rem;
		height: 1.75rem;
		display: flex;
		align-items: center;
		justify-content: center;
		background: color-mix(in srgb, var(--accent) 18%, transparent);
		border-radius: 50%;
		font-size: 0.8125rem;
		font-weight: 600;
	}
	h2 {
		font-size: 1rem;
		font-weight: 600;
	}
	.content {
		display: flex;
		flex-direction: column;
		gap: 16px;
		padding: 18px;
	}
	.templates {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(170px, 1fr));
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
		font-size: 0.8125rem;
		text-align: left;
		cursor: pointer;
	}
	.templates button[aria-checked='true'] {
		border-color: var(--accent);
	}
	.name {
		font-size: 0.9375rem;
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
		font-size: 0.8125rem;
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
		font-size: 0.75rem;
		color: var(--muted);
	}
	.warn {
		color: var(--warn-text);
	}
	.slider,
	.medium {
		display: flex;
		flex-direction: column;
		gap: 8px;
		font-size: 0.8125rem;
	}
	.line {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: 8px 12px;
	}
	.line label {
		flex-direction: row;
		gap: 6px;
		font-size: 0.875rem;
	}
	input[type='range'] {
		width: 100%;
		accent-color: var(--accent);
	}
	.facts {
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
	}
	.note {
		font-size: 0.8125rem;
	}
	.disk {
		display: flex;
		flex-direction: column;
		gap: 12px;
		padding: 14px;
		border: 1px solid var(--panel-border);
		border-radius: var(--radius-ctl);
		font-size: 0.8125rem;
	}
	.file {
		font-size: 0.875rem;
		font-weight: 500;
	}
	.grow {
		flex: 1 1 auto;
	}
	.segments {
		display: inline-flex;
		flex-wrap: wrap;
		gap: 4px;
		padding: 3px;
		border: 1px solid var(--panel-border);
		border-radius: var(--radius-ctl);
	}
	.segments button,
	.presets button {
		height: 2rem;
		padding: 0 12px;
		background: transparent;
		border: 0;
		border-radius: var(--radius-ctl);
		color: var(--text);
		font: inherit;
		font-size: 0.8125rem;
		cursor: pointer;
	}
	.segments button[aria-checked='true'] {
		background: var(--accent);
		color: var(--on-accent);
	}
	.small {
		height: 2rem;
		padding: 0 0.625rem;
		font-size: 0.8125rem;
	}
	.size,
	.place {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 10px;
	}
	.size label {
		flex-direction: row;
		color: var(--muted);
	}
	.size input[type='range'] {
		flex: 1 1 220px;
		width: auto;
	}
	.number {
		flex: none;
	}
	.size .number {
		width: 6.5rem;
		height: 2.25rem;
		border-radius: var(--radius-ctl);
		background: var(--bg);
		font-weight: 400;
	}
	.presets {
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
	}
	.presets button {
		border: 1px solid var(--border);
	}
	.presets button[aria-pressed='true'] {
		border-color: var(--accent);
	}
	.path {
		font-size: 0.75rem;
		overflow-wrap: anywhere;
	}
	.add {
		align-self: flex-start;
		height: 2.25rem;
		padding: 0 12px;
		background: transparent;
		border: 1px dashed var(--border);
		border-radius: var(--radius-ctl);
		color: var(--text);
		font: inherit;
		font-size: 0.8125rem;
		cursor: pointer;
	}
	.medium label {
		font-size: 0.875rem;
	}
	aside {
		flex: 1 1 320px;
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
		font-size: 0.8125rem;
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
		font-size: 0.75rem;
		white-space: pre-wrap;
		word-break: break-word;
	}
	fieldset {
		min-width: 0;
		margin: 0;
		border: 0;
	}
	.check {
		flex-direction: row;
		align-items: center;
		gap: 10px;
		font-size: 0.875rem;
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
	.cpus {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(118px, 1fr));
		gap: 8px;
	}
	.cpus button {
		display: flex;
		flex-direction: column;
		gap: 4px;
		min-height: 3.75rem;
		padding: 8px 10px;
		background: transparent;
		border: 2px solid var(--border);
		border-radius: var(--radius-ctl);
		color: var(--text);
		font: inherit;
		font-size: 0.8125rem;
		text-align: left;
		cursor: pointer;
	}
	.cpus button[aria-pressed='true'] {
		border-color: var(--accent);
		background: color-mix(in srgb, var(--accent) 12%, transparent);
	}
	.cpu-head {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 6px;
	}
	.cpu-note {
		font-size: 0.6875rem;
		line-height: 1.3;
	}
	.adds {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
	}
	.device {
		display: flex;
		flex-direction: column;
		gap: 4px;
		padding: 12px 14px;
		border: 1px solid var(--panel-border);
		border-radius: var(--radius-ctl);
	}
	.ids {
		padding-left: 28px;
		font-size: 0.75rem;
	}
	.device small {
		padding-left: 28px;
	}
	.folder {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 8px;
	}
	.path-field {
		flex: 2 1 260px;
		width: auto;
	}
	.tag-field {
		flex: 1 1 140px;
		width: auto;
	}
	.ro-field {
		flex: 0 1 200px;
		width: auto;
	}
	.wide-note {
		flex: 1 1 100%;
	}
	.netsel {
		max-width: 26rem;
	}
	.small-link {
		font-size: 0.8125rem;
	}
	.step .grow {
		flex: 1 1 auto;
	}
</style>
