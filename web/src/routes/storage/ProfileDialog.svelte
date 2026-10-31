<script lang="ts">
	// Changes the profile of a complete Btrfs volume in place (btrfs balance).
	import { api, type BtrfsProfile, type Volume } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { formatDiskBytes } from '#lib/format.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import { storage } from '#lib/storage.svelte.ts';
	import { BTRFS_MIN, btrfsUsable, profileOf } from './btrfs.ts';

	type Props = { volume: Volume; onclose: () => void; ondone: () => void };
	let { volume, onclose, ondone }: Props = $props();

	const current = $derived(profileOf(volume));
	// Props are read once on purpose: the draft starts from them and later
	// changes of the props must not overwrite what is being edited; the storage page mounts this dialog anew for each opening ({#key dialog}).
	// svelte-ignore state_referenced_locally
	let profile = $state<BtrfsProfile>(profileOf(volume));
	let working = $state(false);
	let error = $state('');

	const sizes = $derived(
		(storage.status?.disks ?? [])
			.filter((disk) => disk.volume === volume.name)
			.map((disk) => disk.sizeBytes),
	);
	const profiles = $derived(
		(['raid1', 'raid1c3', 'single'] as BtrfsProfile[]).map((p) => ({
			id: p,
			possible: volume.members.length >= BTRFS_MIN[p],
		})),
	);
	// Less copies than now: the dialog says so before anything starts.
	const fewer = $derived(BTRFS_MIN[profile] < BTRFS_MIN[current]);

	async function submit() {
		if (profile === current || working) return;
		working = true;
		error = '';
		try {
			await api.put(`/storage/volumes/${volume.name}/profile`, { profile });
			ondone();
		} catch (err) {
			error = apiErrorText(err);
			working = false;
		}
	}
</script>

<Dialog title={t('btrfs.profileTitle', { volume: volume.name })} locked={working} {onclose}>
	<div class="body">
		<p class="muted">{t('btrfs.profileIntro', { profile: t(`level.${current}`) })}</p>
		<div class="options" role="radiogroup" aria-label={t('btrfs.profileAfter')}>
			{#each profiles as p (p.id)}
				<label class="option" class:disabled={!p.possible}>
					<input
						type="radio"
						name="btrfs-profile"
						value={p.id}
						bind:group={profile}
						disabled={!p.possible || working}
					/>
					<span>
						<span class="option-name"
							>{t(`level.${p.id}`)}{p.id === current ? ' · ' + t('btrfs.profileKeep') : ''}</span
						>
						<span class="muted small"
							>{p.possible
								? t(`level.${p.id}Desc`) + ' · ' + formatDiskBytes(btrfsUsable(p.id, sizes))
								: t('create.capacityNeeds', { count: BTRFS_MIN[p.id] })}</span
						>
					</span>
				</label>
			{/each}
		</div>
		{#if fewer}<p class="notice-warn" role="alert">
				{t(profile === 'single' ? 'create.singleWarning' : 'btrfs.fewerCopies')}
			</p>{/if}
		<p class="muted">{t('btrfs.balanceHint')}</p>
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={onclose} disabled={working}
				>{t('common.cancel')}</button
			>
			<button
				type="button"
				class={fewer ? 'btn btn-warn' : 'btn btn-primary'}
				disabled={profile === current || working}
				onclick={submit}>{t('btrfs.profileSubmit')}</button
			>
		</div>
	</div>
</Dialog>

<style>
	.body {
		display: flex;
		flex-direction: column;
		gap: 14px;
	}
	p {
		margin: 0;
		font-size: var(--text-md);
	}
	.options {
		display: grid;
		gap: 8px;
	}
	.option {
		display: flex;
		gap: 10px;
		align-items: flex-start;
		padding: 10px 12px;
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		cursor: pointer;
	}
	.option.disabled {
		opacity: 0.55;
		cursor: default;
	}
	.option-name {
		display: block;
		font-weight: 600;
	}
	.small {
		display: block;
		font-size: var(--text-base);
	}
</style>
