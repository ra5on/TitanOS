<script lang="ts">
	// The section "Disks" of the VM form: the disks of a VM being edited,
	// which can grow, or the disks a new VM is made with.
	import type { Volume, VM, VMHost } from '#lib/api.ts';
	import RetryNotice from '#lib/components/RetryNotice.svelte';
	import { formatBytes } from '#lib/format.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { GIB, freeOn, imageOf, type DiskDraft } from './vm-form.ts';

	const SIZES = [16, 32, 64, 128, 256, 512];

	let {
		existing,
		host,
		volumes,
		directory,
		running,
		busy,
		hostError,
		onretryhost,
		ongrow,
		ontouch,
		disks = $bindable(),
		grow = $bindable(),
	}: {
		existing?: VM;
		host: VMHost;
		volumes: Volume[];
		/** The folder of the VM on its volume, for the paths shown. */
		directory: string;
		running: boolean;
		busy: boolean;
		hostError?: string;
		onretryhost: () => void;
		/** Grows a disk of the VM being edited to the size in `grow`. */
		ongrow: (index: number) => void;
		/** The owner changed the list of disks. */
		ontouch: () => void;
		disks: DiskDraft[];
		grow: Record<number, number>;
	} = $props();

	const newDisks = $derived(disks.filter((disk) => disk.mode === 'new' || disk.mode === 'copy'));
	const fileOf = (disk: DiskDraft) =>
		`vdisk${(existing?.disks.filter((old) => !old.path).length ?? 0) + newDisks.indexOf(disk) + 1}.${disk.format === 'raw' ? 'img' : 'qcow2'}`;
	const pathOf = (disk: DiskDraft) =>
		disk.mode === 'direct' || disk.mode === 'block'
			? disk.path
			: `/mnt/${disk.volume}/vms/${directory || '<name>'}/${fileOf(disk)}`;
	const directImages = $derived(
		host.images.filter(
			(image) => (image.format === 'qcow2' || image.format === 'raw') && !image.usedBy,
		),
	);
	const freeDisks = $derived(host.disks.filter((disk) => !disk.usedBy));

	function addDisk(mode: DiskDraft['mode']) {
		ontouch();
		const volume = disks.findLast((disk) => disk.volume)?.volume ?? volumes[0]?.name ?? '';
		if (mode === 'new') disks.push({ mode, format: 'qcow2', sizeGiB: 64, volume, path: '' });
		else if (mode === 'copy')
			pickImage(
				{ mode, format: 'qcow2', sizeGiB: 1, volume, path: '' },
				host.images[0]?.path ?? '',
				true,
			);
		else if (mode === 'block')
			disks.push({ mode, format: 'raw', sizeGiB: 0, volume: '', path: freeDisks[0]?.path ?? '' });
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
		const available = freeOn(volumes, disk.volume);
		const size = disk.sizeGiB * GIB;
		const image = imageOf(host.images, disk);
		if (disk.mode === 'copy' && image && size < image.virtualBytes)
			return {
				text: t('vms.form.copyTooSmall', { size: formatBytes(image.virtualBytes) }),
				warn: true,
			};
		if (disk.format === 'raw') {
			return size > available
				? { text: t('vms.new.rawTooLarge', { free: formatBytes(available) }), warn: true }
				: { text: t('vms.new.rawNote', { free: formatBytes(available - size) }), warn: false };
		}
		if (disk.mode === 'copy' && image && size > image.virtualBytes)
			return { text: t('vms.form.copyGrown'), warn: false };
		return size > available
			? { text: t('vms.new.qcowOver', { free: formatBytes(available) }), warn: true }
			: { text: t('vms.new.qcowNote'), warn: false };
	}
</script>

<fieldset class="content" disabled={running}>
	{#if hostError}<RetryNotice text={hostError} onretry={onretryhost} />{/if}
	{#if existing}
		{#each existing.disks as disk, index (index)}
			<div class="disk">
				<div class="line">
					<span class="mono file">{disk.path || disk.file}</span>
					<span class="muted"
						>{disk.path
							? t(disk.path.startsWith('/dev/') ? 'vms.form.wholeDisk' : 'vms.form.existingImage')
							: `${disk.format === 'raw' ? 'img' : 'qcow2'} · ${disk.volume}`}</span
					>
					<span class="grow"></span>
					<span class="mono">{formatBytes(disk.sizeBytes)}</span>
				</div>
				{#if !disk.path?.startsWith('/dev/')}
					<div class="size">
						<label for="disk-grow-{index}">{t('vms.form.growTo')}</label>
						<input
							id="disk-grow-{index}"
							class="field mono number"
							type="number"
							min={Math.ceil(disk.sizeBytes / GIB) + 1}
							max="16384"
							bind:value={grow[index]}
							placeholder={String(Math.ceil(disk.sizeBytes / GIB))}
						/><span class="muted">GiB</span>
						<button
							type="button"
							class="btn small"
							onclick={() => ongrow(index)}
							disabled={busy || !(grow[index] * GIB > disk.sizeBytes)}>{t('vms.form.grow')}</button
						>
					</div>
					<small>{t('vms.form.growNote')}</small>
				{/if}
			</div>
		{/each}
	{:else}
		{#each disks as disk, index (index)}
			{@const note = diskNote(disk)}
			{@const image = imageOf(host.images, disk)}
			<div class="disk">
				<div class="line">
					<span class="mono file"
						>{disk.mode === 'block'
							? t('vms.form.wholeDisk')
							: disk.mode === 'direct'
								? t('vms.form.existingImage')
								: fileOf(disk)}</span
					>
					<span class="muted">{t(index === 0 ? 'vms.new.systemDisk' : 'vms.new.dataDisk')}</span>
					<span class="grow"></span>
					{#if disk.mode === 'new' || disk.mode === 'copy'}
						<div
							class="segments"
							role="radiogroup"
							aria-label={t('vms.new.format', { file: fileOf(disk) })}
						>
							<button
								type="button"
								role="radio"
								aria-checked={disk.format === 'qcow2'}
								onclick={() => (disk.format = 'qcow2')}>qcow2</button
							>
							<button
								type="button"
								role="radio"
								aria-checked={disk.format === 'raw'}
								onclick={() => (disk.format = 'raw')}>img (raw)</button
							>
						</div>
					{/if}
					<button
						type="button"
						class="btn small"
						aria-label={t('vms.new.removeDisk', { file: pathOf(disk) })}
						onclick={() => (ontouch(), disks.splice(index, 1))}>✕</button
					>
				</div>
				{#if disk.mode === 'copy' || disk.mode === 'direct'}
					<label
						><span>{t('vms.form.image')}</span>
						<select
							class="field mono"
							value={disk.path}
							onchange={(event) => pickImage(disk, event.currentTarget.value)}
						>
							{#each disk.mode === 'direct' ? directImages : host.images as candidate (candidate.path)}
								<option value={candidate.path}
									>{candidate.path} · {candidate.format} · {formatBytes(
										candidate.virtualBytes,
									)}{candidate.usedBy
										? ` · ${t('vms.form.usedBy', { vm: candidate.usedBy })}`
										: ''}</option
								>
							{/each}
						</select>
					</label>
					<div class="place">
						<span class="muted">{t('vms.form.use')}</span>
						<div class="segments" role="radiogroup" aria-label={t('vms.form.use')}>
							<button
								type="button"
								role="radio"
								aria-checked={disk.mode === 'copy'}
								onclick={() => setUse(disk, 'copy')}>{t('vms.form.useCopy')}</button
							>
							<button
								type="button"
								role="radio"
								aria-checked={disk.mode === 'direct'}
								onclick={() => setUse(disk, 'direct')}
								disabled={!image ||
									(image.format !== 'qcow2' && image.format !== 'raw') ||
									!!image.usedBy}>{t('vms.form.useDirect')}</button
							>
						</div>
					</div>
				{/if}
				{#if disk.mode === 'block'}
					<label
						><span>{t('vms.form.disk')}</span>
						<select class="field mono" bind:value={disk.path}>
							{#each freeDisks as candidate (candidate.path)}<option value={candidate.path}
									>{candidate.device} · {candidate.path}</option
								>{/each}
						</select>
					</label>
				{/if}
				{#if disk.mode === 'new' || disk.mode === 'copy'}
					<div class="size">
						<label for="disk-size-{index}">{t('vms.new.size')}</label>
						<input
							type="range"
							min="8"
							max="2048"
							step="8"
							bind:value={disk.sizeGiB}
							aria-label={t('vms.new.sizeOf', { file: fileOf(disk) })}
						/>
						<input
							id="disk-size-{index}"
							class="field mono number"
							type="number"
							min="1"
							max="16384"
							bind:value={disk.sizeGiB}
						/><span class="muted">GiB</span>
					</div>
					{#if disk.mode === 'new'}
						<details class="size-presets">
							<summary>{t('vms.form.sizePresets')}</summary>
							<div class="presets">
								{#each SIZES as size (size)}
									<button
										type="button"
										aria-pressed={disk.sizeGiB === size}
										onclick={() => (disk.sizeGiB = size)}>{size} GiB</button
									>
								{/each}
							</div>
						</details>
					{/if}
					<div class="place">
						<span class="muted">{t('vms.new.place')}</span>
						<div class="segments" role="radiogroup" aria-label={t('vms.new.place')}>
							{#each volumes as volume (volume.name)}
								<button
									type="button"
									role="radio"
									aria-checked={disk.volume === volume.name}
									onclick={() => (disk.volume = volume.name)}
									>{t('vms.new.volume', {
										name: volume.name,
										free: formatBytes(volume.sizeBytes - volume.usedBytes),
									})}</button
								>
							{/each}
						</div>
					</div>
				{/if}
				<small class:warn={note.warn}>{note.text}</small>
				<span class="muted mono path">→ {pathOf(disk)}</span>
			</div>
		{/each}
		<div class="adds">
			<button
				type="button"
				class="add"
				onclick={() => addDisk('new')}
				disabled={volumes.length === 0 || disks.length >= 8}>+ {t('vms.form.addNew')}</button
			>
			<button
				type="button"
				class="add"
				onclick={() => addDisk('copy')}
				disabled={volumes.length === 0 || host.images.length === 0 || disks.length >= 8}
				>+ {t('vms.form.addImage')}</button
			>
			<button
				type="button"
				class="add"
				onclick={() => addDisk('block')}
				disabled={freeDisks.length === 0 || disks.length >= 8}>+ {t('vms.form.addBlock')}</button
			>
		</div>
		{#if host.images.length === 0}<small>{t('vms.form.noImages')}</small>{/if}
		<details class="advanced">
			<summary>{t('vms.form.diskHelp')}</summary>
			<p class="muted note">{t('vms.new.formats')}</p>
		</details>
	{/if}
</fieldset>

<style>
	.advanced {
		border-top: 1px solid var(--panel-border);
		padding-top: 10px;
		min-width: 0;
	}
	summary {
		cursor: pointer;
		font-size: var(--text-base);
	}
	/* Also on the inputs for a size, where `.size .number` takes most of it back. */
	.number {
		width: 1.75rem;
		height: 1.75rem;
		display: flex;
		align-items: center;
		justify-content: center;
		background: color-mix(in srgb, var(--accent) 18%, transparent);
		border-radius: 50%;
		font-size: var(--text-base);
		font-weight: 600;
	}
	.content {
		display: flex;
		flex-direction: column;
		gap: 12px;
		padding: 16px;
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
	small {
		font-size: var(--text-sm);
		color: var(--muted);
	}
	.warn {
		color: var(--warn-text);
	}
	.line {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: 8px 12px;
	}
	input[type='range'] {
		width: 100%;
		accent-color: var(--accent);
	}
	.note {
		font-size: var(--text-base);
	}
	.disk {
		display: flex;
		flex-direction: column;
		gap: 12px;
		padding: 14px;
		border: 1px solid var(--panel-border);
		border-radius: var(--radius-ctl);
		font-size: var(--text-base);
	}
	.file {
		font-size: var(--text-md);
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
		font-size: var(--text-base);
		cursor: pointer;
	}
	.segments button[aria-checked='true'] {
		background: var(--accent);
		color: var(--on-accent);
	}
	.small {
		height: 2rem;
		padding: 0 0.625rem;
		font-size: var(--text-base);
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
		font-size: var(--text-sm);
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
		font-size: var(--text-base);
		cursor: pointer;
	}
	fieldset {
		min-width: 0;
		margin: 0;
		border: 0;
	}
	.adds {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
	}
</style>
