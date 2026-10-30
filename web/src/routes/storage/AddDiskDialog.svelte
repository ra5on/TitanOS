<script lang="ts">
	import { api, ApiError, type BtrfsProfile, type Disk, type Volume } from '#lib/api.ts';
	import { BTRFS_MIN, btrfsUsable, profileOf } from './btrfs.ts';
	import { formatDiskBytes } from '#lib/format.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';
	import DiskPicker from './DiskPicker.svelte';
	import { isUsable } from './labels.ts';

	type Props = {
		volume: Volume;
		disks: Disk[];
		preselect?: Disk;
		onclose: () => void;
		ondone: () => void;
	};
	let { volume, disks, preselect, onclose, ondone }: Props = $props();

	// Opened from a disk's details: that disk is already picked.
	// Props are read once on purpose: the draft starts from them and later
	// changes of the props must not overwrite what is being edited; the storage page mounts this dialog anew for each opening ({#key dialog}).
	// svelte-ignore state_referenced_locally
	let selected = $state<string[]>(preselect ? [preselect.name] : []);
	// What the chosen disk was when it was picked. The NAS takes only the
	// name, so the dialog refuses to go on when a hot-plug changed the disk
	// behind that name meanwhile.
	// svelte-ignore state_referenced_locally
	let pickedIdentity = $state<string | undefined>(preselect?.identity);
	function remember(disk: Disk, checked: boolean) {
		pickedIdentity = checked ? disk.identity : undefined;
	}
	let confirmed = $state(false);
	// An encrypted Btrfs volume: the new disk gets its own LUKS container
	// with the volume's password (an mdadm volume needs none).
	const needsPassword = $derived(volume.filesystem === 'btrfs' && !!volume.encryption);
	let password = $state('');
	let working = $state(false);
	let error = $state<{ text: string; detail: string } | null>(null);

	const usable = $derived(disks.filter(isUsable));
	const working_members = $derived(
		volume.members.filter((member) => member.state === 'active' || member.state === 'rebuilding')
			.length,
	);
	const incomplete = $derived(working_members < volume.raidDisks);
	const chosen = $derived(usable.find((disk) => disk.name === selected[0]));
	const staleSelection = $derived(
		selected.length === 1 && (!chosen || chosen.identity !== pickedIdentity),
	);
	// A complete Btrfs volume grows by the disk and may change its profile
	// on the way, for example from RAID 1 to three copies.
	const grows = $derived(volume.filesystem === 'btrfs' && !incomplete);
	// svelte-ignore state_referenced_locally
	let profile = $state<BtrfsProfile>(profileOf(volume));
	const profiles = $derived(
		(['raid1', 'raid1c3', 'single'] as BtrfsProfile[]).filter(
			(p) => volume.members.length + 1 >= BTRFS_MIN[p],
		),
	);
	const memberSizes = $derived(
		disks.filter((disk) => disk.volume === volume.name).map((disk) => disk.sizeBytes),
	);
	const after = $derived(chosen ? btrfsUsable(profile, [...memberSizes, chosen.sizeBytes]) : 0);

	$effect(() => {
		selected;
		confirmed = false;
	});

	// Erases the disk: runs only from a click on its button, never from Enter.
	async function submit() {
		if (selected.length !== 1 || !confirmed || staleSelection || (needsPassword && !password))
			return;
		working = true;
		error = null;
		try {
			await api.post(`/storage/volumes/${volume.name}/disks`, {
				disk: selected[0],
				erase: true,
				identity: pickedIdentity,
				profile: grows ? profile : undefined,
				password: needsPassword ? password : undefined,
			});
			password = '';
			ondone();
		} catch (err) {
			const code = err instanceof ApiError ? err.code : 'internal';
			error = {
				text: errorText(code),
				detail: code === 'storage_failed' && err instanceof ApiError ? err.message : '',
			};
			working = false;
		}
	}
</script>

<Dialog title={t('addDisk.title', { volume: volume.name })} wide locked={working} {onclose}>
	<form onsubmit={(event) => event.preventDefault()}>
		<p class="muted">
			{t(incomplete ? 'addDisk.rebuild' : grows ? 'addDisk.btrfsGrow' : 'addDisk.spare')}
		</p>
		<DiskPicker disks={usable} bind:selected single name="add-disk" onselect={remember} />
		{#if grows}
			<label class="profile">
				<span class="label">{t('btrfs.profileAfter')}</span>
				<select class="field" bind:value={profile}>
					{#each profiles as p (p)}<option value={p}
							>{t(`level.${p}`)}{p === profileOf(volume)
								? ' · ' + t('btrfs.profileKeep')
								: ''}</option
						>{/each}
				</select>
				<small class="muted"
					>{t(`level.${profile}Desc`)}{#if chosen}
						· {t('btrfs.capacityAfter', { size: formatDiskBytes(after) })}{/if}</small
				>
			</label>
			{#if profile === 'single'}<p class="notice-warn" role="alert">
					{t('create.singleWarning')}
				</p>{/if}
			<p class="muted">{t('btrfs.balanceHint')}</p>
		{/if}
		{#if needsPassword}
			<label class="profile">
				<span class="label">{t('crypt.password')}</span>
				<input class="field" type="password" autocomplete="off" bind:value={password} />
				<small class="muted">{t('crypt.addDiskHint')}</small>
			</label>
		{/if}
		{#if staleSelection}
			<p class="notice-warn" role="alert">{t('create.changedDisks')}</p>
			<button
				type="button"
				class="btn"
				onclick={() => ((selected = []), (pickedIdentity = undefined))}
				>{t('create.clearSelection')}</button
			>
		{:else if selected.length === 1}
			<label class="confirm">
				<input type="checkbox" bind:checked={confirmed} />
				<span>{t('addDisk.eraseConfirm', { disk: selected[0] })}</span>
			</label>
		{/if}
		{#if error}
			<div class="notice-error" role="alert">
				{error.text}{#if error.detail}<pre>{error.detail}</pre>{/if}
			</div>
		{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={onclose} disabled={working}
				>{t('common.cancel')}</button
			>
			<button
				type="button"
				class="btn btn-warn"
				disabled={selected.length !== 1 ||
					!confirmed ||
					staleSelection ||
					working ||
					(needsPassword && !password)}
				onclick={submit}>{t('addDisk.submit')}</button
			>
		</div>
	</form>
</Dialog>

<style>
	form {
		display: flex;
		flex-direction: column;
		gap: 16px;
	}
	p {
		font-size: 0.875rem;
	}
	.profile {
		display: flex;
		flex-direction: column;
		gap: 6px;
	}
	pre {
		margin: 8px 0 0;
		font-family: var(--font-mono);
		font-size: 0.75rem;
		white-space: pre-wrap;
		word-break: break-word;
	}
</style>
