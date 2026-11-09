<script lang="ts">
	import {
		api,
		ApiError,
		type BtrfsProfile,
		type Disk,
		type Filesystem,
		type RaidLevel,
	} from '#lib/api.ts';
	import { btrfsUsable } from './btrfs.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import HelpTip from '#lib/components/HelpTip.svelte';
	import { formatDiskBytes } from '#lib/format.ts';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';
	import DiskPicker from './DiskPicker.svelte';
	import SmrWarning from './SmrWarning.svelte';
	import { isUsable } from './labels.ts';
	import Segments from '#lib/components/Segments.svelte';

	type Props = { disks: Disk[]; taken: string[]; onclose: () => void; ondone: () => void };
	let { disks, taken, onclose, ondone }: Props = $props();

	// Same rules as the backend (internal/storage); it checks again.
	const NAME = /^[A-Za-z][A-Za-z0-9-]{0,11}$/;
	const LEVELS: { id: RaidLevel; min: number }[] = [
		{ id: 'raid1', min: 2 },
		{ id: 'raid5', min: 3 },
		{ id: 'raid6', min: 4 },
		{ id: 'raid10', min: 4 },
		{ id: 'raid0', min: 1 },
	];
	const BTRFS: { id: BtrfsProfile; min: number }[] = [
		{ id: 'raid1', min: 2 },
		{ id: 'raid1c3', min: 3 },
		{ id: 'single', min: 1 },
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
	// Optional LUKS2 encryption, off by default. The password is needed
	// after every start; nothing can recover it.
	let encrypt = $state(false);
	// Regular checks, all chosen by default (decided 31.10.2026).
	let checks = $state({ parity: true, smartShort: true, smartLong: true, snapshots: true });
	let password = $state('');
	let repeat = $state('');
	let understood = $state(false);
	const passwordProblem = $derived.by(() => {
		if (!encrypt) return '';
		if (password.length > 0 && password.length < 8) return t('crypt.tooShort');
		if (repeat !== '' && repeat !== password) return t('crypt.mismatch');
		return '';
	});
	const encryptionReady = $derived(
		!encrypt ||
			(password.length >= 8 && password.length <= 512 && repeat === password && understood),
	);
	let working = $state(false);
	let error = $state<{ text: string; detail: string } | null>(null);

	const usable = $derived(disks.filter(isUsable));
	const chosen = $derived(usable.filter((disk) => selected.includes(disk.name)));
	const staleSelection = $derived(
		selected.length !== chosen.length ||
			chosen.some((disk) => disk.identity !== selectionIdentities[disk.name]),
	);
	const levels = $derived<{ id: RaidLevel | BtrfsProfile; min: number }[]>(
		filesystem === 'btrfs' ? BTRFS : LEVELS,
	);
	const min = $derived(levels.find((entry) => entry.id === level)?.min ?? 1);

	/** What the volume will hold; the same for every disk as the smallest one. */
	const capacity = $derived.by(() => {
		const n = chosen.length;
		if (n < min) return { text: t('create.capacityNeeds', { count: min }), ok: false };
		if (filesystem === 'btrfs')
			return {
				text: formatDiskBytes(
					btrfsUsable(
						level as BtrfsProfile,
						chosen.map((disk) => disk.sizeBytes),
					),
				),
				ok: true,
			};
		if (level === 'raid10' && n % 2 !== 0) return { text: t('create.capacityEven'), ok: false };
		const smallest = Math.min(...chosen.map((disk) => disk.sizeBytes));
		const data = { raid0: n, raid1: 1, raid5: n - 1, raid6: n - 2, raid10: n / 2 }[
			level as RaidLevel
		];
		return { text: formatDiskBytes(smallest * data), ok: true };
	});

	const nameError = $derived.by(() => {
		if (name === '') return '';
		if (!NAME.test(name)) return errorText('invalid_volume_name');
		if (taken.some((existing) => existing.toLowerCase() === name.toLowerCase()))
			return errorText('volume_exists');
		return '';
	});
	const ready = $derived(
		NAME.test(name) &&
			!nameError &&
			capacity.ok &&
			!staleSelection &&
			confirmed &&
			encryptionReady &&
			!working,
	);

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
			await api.post('/storage/volumes', {
				name,
				level,
				filesystem,
				disks: $state.snapshot(selected),
				erase: $state.snapshot(selected),
				identities: $state.snapshot(selectionIdentities),
				encrypt: encrypt || undefined,
				checks: { ...checks, snapshots: filesystem === 'btrfs' && checks.snapshots },
				password: encrypt ? password : undefined,
			});
			password = '';
			repeat = '';
			ondone();
		} catch (err) {
			const code = err instanceof ApiError ? err.code : 'internal';
			error = {
				text: errorText(code),
				detail:
					['storage_failed', 'disk_unavailable', 'volume_busy'].includes(code) &&
					err instanceof ApiError
						? err.message
						: '',
			};
			working = false;
		}
	}
</script>

<Dialog title={t('create.title')} wide locked={working} {onclose}>
	<!-- PLAN 72: cards with rows like every settings card (label and hint left, control right). -->
	<form onsubmit={(event) => event.preventDefault()}>
		<section class="tile ui-card">
			<div class="set-row">
				<label class="set-label" for="volume-name"
					>{t('create.name')}<small id="volume-name-hint" class="set-hint" class:problem={nameError}
						>{nameError || t('create.nameHint')}</small
					></label
				>
				<span class="set-control">
					<!-- svelte-ignore a11y_autofocus -->
					<input
						id="volume-name"
						class="field mono name"
						type="text"
						bind:value={name}
						maxlength="12"
						autocapitalize="none"
						spellcheck="false"
						autofocus
						aria-describedby="volume-name-hint"
					/>
				</span>
			</div>
			<div class="set-row">
				<span class="set-label"
					><span>{t('create.filesystem')}<HelpTip term="filesystem" /></span><span class="set-hint"
						>{t(`fs.${filesystem}Desc`)}</span
					></span
				>
				<span class="set-control">
					<Segments
						compact
						label={t('create.filesystem')}
						options={FILESYSTEMS.map((id) => ({ value: id, label: t(`fs.${id}`) }))}
						value={filesystem}
						onchange={pickFilesystem}
					/>
				</span>
			</div>
			<div class="set-row">
				<span class="set-label"
					><span>{t('create.level')}<HelpTip term="raid" /></span><span class="set-hint"
						>{t(`level.${level}Desc`)}
						{#if filesystem === 'btrfs'}{t('create.btrfsProfiles')}{:else if level !== 'raid0'}{t(
								'create.bitmap',
							)}<HelpTip term="bitmap" />{/if}</span
					></span
				>
				<span class="set-control">
					<Segments
						compact
						label={t('create.level')}
						options={levels.map((entry) => ({ value: entry.id, label: t(`level.${entry.id}`) }))}
						bind:value={level}
					/>
				</span>
			</div>
			{#if level === 'raid0' || level === 'single'}<div class="set-row wide">
					<p class="notice-warn" role="alert">
						{t(level === 'single' ? 'create.singleWarning' : 'create.raid0Warning')}
					</p>
				</div>{/if}
		</section>

		<section class="tile ui-card">
			<div class="card-head">
				<h3>{t('create.disks')}</h3>
				<span class="card-meta"
					>{t('create.capacity')}: <span class="mono">{capacity.text}</span></span
				>
			</div>
			<div class="card-body">
				<DiskPicker {disks} bind:selected name="volume-disks" onselect={remember} />
				<SmrWarning {level} {selected} />
				{#if staleSelection}
					<p class="notice-warn" role="alert">{t('create.changedDisks')}</p>
					<button
						type="button"
						class="btn"
						onclick={() => {
							selected = [];
							selectionIdentities = {};
						}}>{t('create.clearSelection')}</button
					>
				{/if}
				{#if taken.length === 0}<p class="muted small">{t('create.firstFolders')}</p>{/if}
			</div>
		</section>

		<section class="tile ui-card">
			<div class="card-head"><h3>{t('create.checks')}<HelpTip term="checks" /></h3></div>
			<p class="set-row wide muted small">{t('create.checksIntro')}</p>
			<div class="set-row">
				<label class="set-label" for="check-parity"
					>{t(filesystem === 'btrfs' ? 'create.checkScrub' : 'create.checkParity')}<span
						class="set-hint"
						>{t(filesystem === 'btrfs' ? 'create.checkScrubHint' : 'create.checkParityHint')}</span
					></label
				>
				<span class="set-control end"
					><input
						id="check-parity"
						type="checkbox"
						class="switch"
						bind:checked={checks.parity}
					/></span
				>
			</div>
			<div class="set-row">
				<label class="set-label" for="check-short"
					>{t('create.checkSmartShort')}<span class="set-hint"
						>{t('create.checkSmartShortHint')}</span
					></label
				>
				<span class="set-control end"
					><input
						id="check-short"
						type="checkbox"
						class="switch"
						bind:checked={checks.smartShort}
					/></span
				>
			</div>
			<div class="set-row">
				<label class="set-label" for="check-long"
					>{t('create.checkSmartLong')}<span class="set-hint">{t('create.checkSmartLongHint')}</span
					></label
				>
				<span class="set-control end"
					><input
						id="check-long"
						type="checkbox"
						class="switch"
						bind:checked={checks.smartLong}
					/></span
				>
			</div>
			<div class="set-row">
				<label class="set-label" class:unavailable={filesystem !== 'btrfs'} for="check-snapshots"
					>{t('create.checkSnapshots')}<span class="set-hint" id="no-snapshots"
						>{t(filesystem === 'btrfs' ? 'create.checkSnapshotsHint' : 'create.noSnapshots')}</span
					></label
				>
				<span class="set-control end">
					{#if filesystem === 'btrfs'}<input
							id="check-snapshots"
							type="checkbox"
							class="switch"
							bind:checked={checks.snapshots}
						/>{:else}<input
							id="check-snapshots"
							type="checkbox"
							class="switch"
							checked={false}
							disabled
							aria-describedby="no-snapshots"
						/>{/if}
				</span>
			</div>
		</section>

		<section class="tile ui-card">
			<div class="set-row">
				<label class="set-label" for="volume-encrypt"
					><span>{t('crypt.createEncrypt')}<HelpTip term="luks" /></span><span class="set-hint"
						>{t('crypt.createEncryptDesc')}</span
					></label
				>
				<span class="set-control end"
					><input id="volume-encrypt" type="checkbox" class="switch" bind:checked={encrypt} /></span
				>
			</div>
			{#if encrypt}
				<div class="set-row">
					<label class="set-label" for="volume-password">{t('crypt.password')}</label>
					<span class="set-control"
						><input
							id="volume-password"
							class="field"
							type="password"
							autocomplete="new-password"
							bind:value={password}
							maxlength="512"
						/></span
					>
				</div>
				<div class="set-row">
					<label class="set-label" for="volume-repeat"
						>{t('crypt.repeat')}{#if passwordProblem}<small class="set-hint problem"
								>{passwordProblem}</small
							>{/if}</label
					>
					<span class="set-control"
						><input
							id="volume-repeat"
							class="field"
							type="password"
							autocomplete="new-password"
							bind:value={repeat}
							maxlength="512"
						/></span
					>
				</div>
				<div class="set-row wide">
					<p class="notice-warn" role="alert">{t('crypt.createWarning')}</p>
					<label class="confirm">
						<input type="checkbox" bind:checked={understood} />
						<span>{t('crypt.createUnderstood')}</span>
					</label>
				</div>
			{/if}
		</section>

		{#if capacity.ok}
			<label class="confirm">
				<input type="checkbox" bind:checked={confirmed} />
				<span
					>{t('create.eraseConfirm', { disks: chosen.map((disk) => disk.name).join(', ') })}</span
				>
			</label>
		{/if}

		{#if error}
			<div class="notice-error" role="alert">
				{error.text}{#if error.detail}<pre>{error.detail}</pre>{/if}
			</div>
		{/if}

		<div class="dialog-actions">
			<span class="working" role="status">{working ? t('create.working') : ''}</span>
			<button type="button" class="btn" onclick={onclose} disabled={working}
				>{t('common.cancel')}</button
			>
			<button type="button" class="btn btn-warn" disabled={!ready} onclick={submit}
				>{t('create.submit')}</button
			>
		</div>
	</form>
</Dialog>

<style>
	form {
		display: flex;
		flex-direction: column;
		gap: var(--gap, var(--space-4));
	}
	.tile.ui-card {
		flex-shrink: 0;
	}
	.set-row.wide {
		grid-template-columns: minmax(0, 1fr);
		margin: 0;
	}
	.set-row.wide > p {
		margin: 0;
	}
	.name {
		width: 12rem;
		max-width: 100%;
	}
	.problem {
		color: var(--warn-text);
	}
	.small {
		font-size: var(--text-sm);
	}
	.unavailable {
		color: var(--muted);
	}
	.working {
		order: -2;
		margin-right: auto;
		align-self: center;
		font-size: var(--text-base);
		color: var(--muted);
	}
	pre {
		margin: var(--space-2) 0 0;
		font-family: var(--font-mono);
		font-size: var(--text-sm);
		white-space: pre-wrap;
		word-break: break-word;
	}
</style>
