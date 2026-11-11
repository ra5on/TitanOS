<script lang="ts">
	// The section "Disks" of the VM form: the disks of a VM being edited,
	// which can grow, or the disks a new VM is made with.
	import type { Volume, VM, VMHost } from '#lib/api.ts';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import SettingSub from '#lib/components/settings/SettingSub.svelte';
	import RetryNotice from '#lib/components/RetryNotice.svelte';
	import { formatBytes } from '#lib/format.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { GB, freeOn, imageOf, type DiskDraft } from './vm-form.ts';

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
		if (mode === 'new') disks.push({ mode, format: 'qcow2', sizeGB: 64, volume, path: '' });
		else if (mode === 'copy')
			pickImage(
				{ mode, format: 'qcow2', sizeGB: 1, volume, path: '' },
				host.images[0]?.path ?? '',
				true,
			);
		else if (mode === 'block')
			disks.push({ mode, format: 'raw', sizeGB: 0, volume: '', path: freeDisks[0]?.path ?? '' });
	}
	/** A copy starts as large as its original. */
	function pickImage(disk: DiskDraft, path: string, add = false) {
		disk.path = path;
		const image = host.images.find((candidate) => candidate.path === path);
		if (image) disk.sizeGB = Math.max(disk.sizeGB, Math.ceil(image.virtualBytes / GB));
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
		const size = disk.sizeGB * GB;
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

<!-- PLAN 78-5: every disk is a row; its settings are the parts below its name. -->
<fieldset class="srows" disabled={running}>
	{#if hostError}<RetryNotice text={hostError} onretry={onretryhost} />{/if}
	{#if existing}
		{#each existing.disks as disk, index (index)}
			<div class="srow plain disk">
				<div class="srow-head">
					<div class="srow-text">
						<span class="srow-title mono file">{disk.path || disk.file}</span>
						<p class="srow-desc">
							{disk.path
								? t(disk.path.startsWith('/dev/') ? 'vms.form.wholeDisk' : 'vms.form.existingImage')
								: `${disk.format === 'raw' ? 'img' : 'qcow2'} · ${disk.volume}`}
						</p>
					</div>
					<div class="srow-ctl">
						<span class="mono size-now">{formatBytes(disk.sizeBytes)}</span>
					</div>
				</div>
				{#if !disk.path?.startsWith('/dev/')}
					<div class="srow-body">
						<SettingSub
							title={t('vms.form.growTo')}
							desc={t('vms.form.growNote')}
							for="disk-grow-{index}"
						>
							{#snippet control()}
								<input
									id="disk-grow-{index}"
									class="field mono number"
									type="number"
									min={Math.ceil(disk.sizeBytes / GB) + 1}
									max="16384"
									bind:value={grow[index]}
									placeholder={String(Math.ceil(disk.sizeBytes / GB))}
								/><span class="muted">GB</span>
								<button
									type="button"
									class="btn"
									onclick={() => ongrow(index)}
									disabled={busy || !(grow[index] * GB > disk.sizeBytes)}
									>{t('vms.form.grow')}</button
								>
							{/snippet}
						</SettingSub>
					</div>
				{/if}
			</div>
		{/each}
	{:else}
		{#each disks as disk, index (index)}
			{@const note = diskNote(disk)}
			{@const image = imageOf(host.images, disk)}
			<div class="srow plain disk">
				<div class="srow-head">
					<div class="srow-text">
						<span class="srow-title mono file"
							>{disk.mode === 'block'
								? t('vms.form.wholeDisk')
								: disk.mode === 'direct'
									? t('vms.form.existingImage')
									: fileOf(disk)}</span
						>
						<p class="srow-desc">{t(index === 0 ? 'vms.new.systemDisk' : 'vms.new.dataDisk')}</p>
					</div>
					<div class="srow-ctl">
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
							class="btn"
							aria-label={t('vms.new.removeDisk', { file: pathOf(disk) })}
							onclick={() => (ontouch(), disks.splice(index, 1))}>✕</button
						>
					</div>
				</div>
				<div class="srow-body">
					{#if disk.mode === 'copy' || disk.mode === 'direct'}
						<SettingSub title={t('vms.form.image')} for="disk-image-{index}">
							{#snippet control()}
								<select
									id="disk-image-{index}"
									class="field mono long"
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
							{/snippet}
						</SettingSub>
						<SettingSub title={t('vms.form.use')}>
							{#snippet control()}
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
							{/snippet}
						</SettingSub>
					{/if}
					{#if disk.mode === 'block'}
						<SettingSub title={t('vms.form.disk')} for="disk-block-{index}">
							{#snippet control()}
								<select id="disk-block-{index}" class="field mono long" bind:value={disk.path}>
									{#each freeDisks as candidate (candidate.path)}<option value={candidate.path}
											>{candidate.device} · {candidate.path}</option
										>{/each}
								</select>
							{/snippet}
						</SettingSub>
					{/if}
					{#if disk.mode === 'new' || disk.mode === 'copy'}
						<SettingSub title={t('vms.new.size')} for="disk-size-{index}">
							{#snippet control()}
								<input
									type="range"
									min="8"
									max="2048"
									step="8"
									bind:value={disk.sizeGB}
									aria-label={t('vms.new.sizeOf', { file: fileOf(disk) })}
								/>
								<input
									id="disk-size-{index}"
									class="field mono number"
									type="number"
									min="1"
									max="16384"
									bind:value={disk.sizeGB}
								/><span class="muted">GB</span>
							{/snippet}
						</SettingSub>
						{#if disk.mode === 'new'}
							<div class="srow-block">
								<details class="size-presets">
									<summary>{t('vms.form.sizePresets')}</summary>
									<div class="presets">
										{#each SIZES as size (size)}
											<button
												type="button"
												aria-pressed={disk.sizeGB === size}
												onclick={() => (disk.sizeGB = size)}>{size} GB</button
											>
										{/each}
									</div>
								</details>
							</div>
						{/if}
						<SettingSub title={t('vms.new.place')}>
							{#snippet control()}
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
							{/snippet}
						</SettingSub>
					{/if}
					<div class="srow-block notes">
						<small class:warn={note.warn}>{note.text}</small>
						<span class="muted mono path">→ {pathOf(disk)}</span>
					</div>
				</div>
			</div>
		{/each}
		<div class="adds">
			<button
				type="button"
				class="btn"
				onclick={() => addDisk('new')}
				disabled={volumes.length === 0 || disks.length >= 8}>+ {t('vms.form.addNew')}</button
			>
			<button
				type="button"
				class="btn"
				onclick={() => addDisk('copy')}
				disabled={volumes.length === 0 || host.images.length === 0 || disks.length >= 8}
				>+ {t('vms.form.addImage')}</button
			>
			<button
				type="button"
				class="btn"
				onclick={() => addDisk('block')}
				disabled={freeDisks.length === 0 || disks.length >= 8}>+ {t('vms.form.addBlock')}</button
			>
		</div>
		{#if host.images.length === 0}<small>{t('vms.form.noImages')}</small>{/if}
	{/if}
</fieldset>
{#if !existing}
	<!-- Outside the locked rows, like every row that only opens. -->
	<SettingRow id="vm-disk-help" title={t('vms.form.diskHelp')}>
		<div class="srow-block"><p class="muted note">{t('vms.new.formats')}</p></div>
	</SettingRow>
{/if}

<style>
	summary {
		cursor: pointer;
		font-size: var(--text-base);
	}
	small {
		font-size: var(--text-sm);
		color: var(--muted);
	}
	.warn {
		color: var(--warn-text);
	}
	.note {
		font-size: var(--text-base);
	}
	.file {
		overflow-wrap: anywhere;
	}
	.size-now {
		color: var(--text);
	}
	.notes {
		gap: var(--space-1);
	}
	.segments {
		display: inline-flex;
		flex-wrap: wrap;
		gap: var(--space-1);
		padding: 3px;
		border: 1px solid var(--panel-border);
		border-radius: var(--radius-ctl);
	}
	.segments button,
	.presets button {
		height: 2rem;
		padding: 0 var(--space-3);
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
	/* The slider takes the room the control has; the number keeps its width. */
	.disk :global(.srow-ctl input[type='range']) {
		flex: 1 1 10rem;
		min-width: 6rem;
		accent-color: var(--accent);
	}
	.disk :global(.srow-ctl .number) {
		flex: none;
		width: 6.5rem;
		min-width: 0;
	}
	.disk :global(.srow-ctl .long) {
		flex: 1 1 14rem;
		min-width: 0;
		max-width: 100%;
	}
	.presets {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-1-5);
		padding-top: var(--space-2);
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
	.adds {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2);
		margin: var(--space-2) 0;
	}
</style>
