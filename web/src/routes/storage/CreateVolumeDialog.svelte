<script lang="ts">
	import { api, ApiError, type BtrfsProfile, type Disk, type Filesystem, type RaidLevel } from '#lib/api.ts';
	import { btrfsUsable } from './btrfs.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { formatDiskBytes } from '#lib/format.ts';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';
	import DiskPicker from './DiskPicker.svelte';
	import { isUsable } from './labels.ts';
	import { radioKeys } from '#lib/radio-group.ts';

	type Props = { disks: Disk[]; taken: string[]; onclose: () => void; ondone: () => void };
	let { disks, taken, onclose, ondone }: Props = $props();

	// Same rules as the backend (internal/storage); it checks again.
	const NAME = /^[A-Za-z][A-Za-z0-9-]{0,11}$/;
	const LEVELS: { id: RaidLevel; min: number }[] = [
		{ id: 'raid1', min: 2 },
		{ id: 'raid5', min: 3 },
		{ id: 'raid6', min: 4 },
		{ id: 'raid10', min: 4 },
		{ id: 'raid0', min: 1 }
	];
	const BTRFS: { id: BtrfsProfile; min: number }[] = [
		{ id: 'raid1', min: 2 },
		{ id: 'raid1c3', min: 3 },
		{ id: 'single', min: 1 }
	];
	const FILESYSTEMS: Filesystem[] = ['ext4', 'xfs', 'btrfs'];

	let name = $state('');
	let level = $state<RaidLevel | BtrfsProfile>('raid1');
	let filesystem = $state<Filesystem>('ext4');
	let selected = $state<string[]>([]);
    let selectionIdentities = $state<Record<string, string>>({});
    function remember(disk: Disk, checked: boolean) {
        if (checked && disk.identity) selectionIdentities[disk.name] = disk.identity;
        else delete selectionIdentities[disk.name];
    }
	let confirmed = $state(false);
	let working = $state(false);
	let error = $state<{ text: string; detail: string } | null>(null);

	const usable = $derived(disks.filter(isUsable));
	const chosen = $derived(usable.filter((disk) => selected.includes(disk.name)));
	const staleSelection = $derived(selected.length !== chosen.length || chosen.some(disk => disk.identity !== selectionIdentities[disk.name]));
	const levels = $derived<{ id: RaidLevel | BtrfsProfile; min: number }[]>(filesystem === 'btrfs' ? BTRFS : LEVELS);
	const min = $derived(levels.find((entry) => entry.id === level)?.min ?? 1);

	/** What the volume will hold; the same for every disk as the smallest one. */
	const capacity = $derived.by(() => {
		const n = chosen.length;
		if (n < min) return { text: t('create.capacityNeeds', { count: min }), ok: false };
		if (filesystem === 'btrfs') return { text: formatDiskBytes(btrfsUsable(level as BtrfsProfile, chosen.map((disk) => disk.sizeBytes))), ok: true };
		if (level === 'raid10' && n % 2 !== 0) return { text: t('create.capacityEven'), ok: false };
		const smallest = Math.min(...chosen.map((disk) => disk.sizeBytes));
		const data = { raid0: n, raid1: 1, raid5: n - 1, raid6: n - 2, raid10: n / 2 }[level as RaidLevel];
		return { text: formatDiskBytes(smallest * data), ok: true };
	});

	const nameError = $derived.by(() => {
		if (name === '') return '';
		if (!NAME.test(name)) return errorText('invalid_volume_name');
		if (taken.some(existing => existing.toLowerCase() === name.toLowerCase())) return errorText('volume_exists');
		return '';
	});
	const ready = $derived(NAME.test(name) && !nameError && capacity.ok && !staleSelection && confirmed && !working);

	// Btrfs and mdadm have their own lists; switching keeps RAID 1, which both know.
	function pickFilesystem(id: Filesystem) {
		filesystem = id;
		if (!levels.some((entry) => entry.id === level)) level = 'raid1';
	}

	// Whoever changes the selection has to confirm the erasing again.
	$effect(() => {
		selected;
		confirmed = false;
	});

	// Erases disks: runs only from a click on its button, never from Enter in the name field.
	async function submit() {
		if (!ready) return;
		working = true;
		error = null;
		try {
			await api.post('/storage/volumes', { name, level, filesystem, disks: $state.snapshot(selected), erase: $state.snapshot(selected), identities: $state.snapshot(selectionIdentities) });
			ondone();
		} catch (err) {
			const code = err instanceof ApiError ? err.code : 'internal';
			error = { text: errorText(code), detail: ['storage_failed', 'disk_unavailable', 'volume_busy'].includes(code) && err instanceof ApiError ? err.message : '' };
			working = false;
		}
	}
</script>

<Dialog title={t('create.title')} wide locked={working} {onclose}>
	<form onsubmit={(event) => event.preventDefault()}>
		<label class="field-row">
			<span class="label">{t('create.name')}</span>
			<!-- svelte-ignore a11y_autofocus -->
			<input class="field mono" type="text" bind:value={name} maxlength="12" autocapitalize="none" spellcheck="false" autofocus aria-describedby="volume-name-hint" />
			<small id="volume-name-hint" class:problem={nameError}>{nameError || t('create.nameHint')}</small>
		</label>

		<div class="field-row">
			<span class="label" id="fs-label">{t('create.filesystem')}</span>
			<div class="options two" role="radiogroup" aria-labelledby="fs-label" tabindex="-1" onkeydown={radioKeys}>
				{#each FILESYSTEMS as id (id)}
					<button type="button" class="option" role="radio" aria-checked={filesystem === id} tabindex={filesystem === id ? 0 : -1} onclick={() => pickFilesystem(id)}>
						<span class="option-name">{t(`fs.${id}`)}</span>
						<span class="muted small">{t(`fs.${id}Desc`)}</span>
					</button>
				{/each}
			</div>
		</div>

		<div class="field-row">
			<span class="label" id="level-label">{t('create.level')}</span>
			<div class="options" role="radiogroup" aria-labelledby="level-label" tabindex="-1" onkeydown={radioKeys}>
				{#each levels as entry (entry.id)}
					<button type="button" class="option" role="radio" aria-checked={level === entry.id} tabindex={level === entry.id ? 0 : -1} onclick={() => (level = entry.id)}>
						<span class="option-name">{t(`level.${entry.id}`)}</span>
						<span class="muted small">{t(`level.${entry.id}Desc`)}</span>
					</button>
				{/each}
			</div>
			{#if filesystem === 'btrfs'}<p class="muted small">{t('create.btrfsProfiles')}</p>{/if}
			{#if level === 'raid0' || level === 'single'}<p class="notice-warn" role="alert">{t(level === 'single' ? 'create.singleWarning' : 'create.raid0Warning')}</p>{/if}
		</div>

		<div class="field-row">
			<span class="label">{t('create.disks')}</span>
			<DiskPicker {disks} bind:selected name="volume-disks" onselect={remember} />
            {#if staleSelection}
                <p class="notice-warn" role="alert">{t('create.changedDisks')}</p>
                <button type="button" class="btn" onclick={() => { selected = []; selectionIdentities = {}; }}>{t('create.clearSelection')}</button>
            {/if}
		</div>

		<div class="capacity">
			<span>{t('create.capacity')}</span>
			<span class="mono" class:muted={!capacity.ok}>{capacity.text}</span>
		</div>
		{#if taken.length === 0}<p class="muted">{t('create.firstFolders')}</p>{/if}

		{#if capacity.ok}
			<label class="confirm">
				<input type="checkbox" bind:checked={confirmed} />
				<span>{t('create.eraseConfirm', { disks: chosen.map((disk) => disk.name).join(', ') })}</span>
			</label>
		{/if}

		{#if error}
			<div class="notice-error" role="alert">{error.text}{#if error.detail}<pre>{error.detail}</pre>{/if}</div>
		{/if}

		<div class="dialog-actions">
			<span class="working" role="status">{working ? t('create.working') : ''}</span>
			<button type="button" class="btn" onclick={onclose} disabled={working}>{t('common.cancel')}</button>
			<button type="button" class="btn btn-warn" disabled={!ready} onclick={submit}>{t('create.submit')}</button>
		</div>
	</form>
</Dialog>

<style>
	form {
		display: flex;
		flex-direction: column;
		gap: 18px;
	}
	.field-row {
		display: flex;
		flex-direction: column;
		gap: 8px;
	}
	.label {
		font-size: 0.875rem;
		font-weight: 500;
	}
	.field {
		max-width: 16rem;
	}
	small {
		font-size: 0.75rem;
		color: var(--muted);
	}
	small.problem {
		color: var(--warn-text);
	}
	.small {
		font-size: 0.75rem;
	}
	.options {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(190px, 1fr));
		gap: 8px;
	}
	.options.two {
		grid-template-columns: repeat(2, minmax(0, 1fr));
	}
	.option {
		display: flex;
		flex-direction: column;
		gap: 4px;
		padding: 12px;
		text-align: left;
		background: var(--bg);
		border: 2px solid var(--border);
		border-radius: var(--radius-ctl);
		color: var(--text);
		font: inherit;
		cursor: pointer;
	}
	.option:disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}
	.option[aria-checked='true'] {
		border-color: var(--accent);
	}
	.option-name {
		font-size: 0.875rem;
		font-weight: 600;
	}
	.capacity {
		display: flex;
		justify-content: space-between;
		gap: 12px;
		padding: 12px 14px;
		background: var(--chip);
		border-radius: var(--radius-ctl);
		font-size: 0.875rem;
	}
	.working {
		order: -2;
		margin-right: auto;
		align-self: center;
		font-size: 0.8125rem;
		color: var(--muted);
	}
	pre {
		margin: 8px 0 0;
		font-family: var(--font-mono);
		font-size: 0.75rem;
		white-space: pre-wrap;
		word-break: break-word;
	}
</style>
