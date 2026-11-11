<script lang="ts">
	// Ruhemodus in the disk details: value, origin, state with its time, or
	// why the disk cannot sleep. The disk's own value can be changed here; it
	// sends the same request as the sleep dialog with every other value kept.
	import { api, type DiskSleepStatus } from '#lib/api.ts';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import { overridesFrom } from './disk-sleep.ts';
	import { diskSleep, sleepDuration, sleepOrigin, sleepSince } from './sleep-view.ts';

	type Props = {
		name: string;
		status: DiskSleepStatus | undefined;
		canAdminister: boolean;
		onsaved: (status: DiskSleepStatus) => void;
		onopen: () => void;
	};
	let { name, status, canAdminister, onsaved, onopen }: Props = $props();

	const INHERIT = 'inherit';
	const minutes = [0, 5, 10, 15, 30, 60, 120, 180];
	const entry = $derived(diskSleep(status, name));
	const disk = $derived(entry?.disk);
	const since = $derived(disk ? sleepSince(disk) : undefined);

	let editing = $state(false);
	let busy = $state(false);
	let error = $state('');
	let draft = $state(INHERIT);

	function open() {
		draft = disk?.override == null ? INHERIT : String(disk.override);
		error = '';
		editing = true;
	}

	async function save() {
		if (!status || !disk?.id || busy) return;
		busy = true;
		error = '';
		const disks = { ...(status.disks ?? {}) };
		if (draft === INHERIT) delete disks[disk.id];
		else disks[disk.id] = Number(draft);
		try {
			const updated = await api.put<DiskSleepStatus>('/storage/disk-sleep', {
				idleMinutes: status.idleMinutes,
				volumes: overridesFrom(
					status.volumes ?? {},
					(status.volumeStatus ?? []).map((volume) => volume.name),
				),
				disks: overridesFrom(
					disks,
					(status.diskStatus ?? []).map((other) => other.id).filter(Boolean),
				),
			});
			editing = false;
			onsaved(updated);
		} catch (err) {
			error = apiErrorText(err);
		}
		busy = false;
	}
</script>

<!-- PLAN 78-5: a plain heading; the value with its state is a flat row, the
     disk's own time a row with its choice. -->
{#if entry}
	<div class="sleep">
		<SettingGroup title={t('storage.sleepCard')} id="disk-sleep-{name}">
			{#if disk}
				<SettingRow icon="power" title={sleepDuration(disk.effective)}>
					{#snippet description()}<span>({sleepOrigin(disk.origin)})</span>{#if since}<span
								class="since">{since}</span
							>{/if}{/snippet}
					{#snippet control()}<span class="chip" class:ok={disk.state === 'asleep'}
							>{disk.state === 'asleep' ? t('storage.sleepAsleep') : t('storage.sleepAwake')}</span
						>
						{#if disk.error}<span class="chip error" title={disk.error}
								>{t('storage.sleepStandbyFailed')}</span
							>{/if}{/snippet}
				</SettingRow>
			{:else}
				<p class="muted">{entry.reason ? t(`storage.sleepReason.${entry.reason}`) : '–'}</p>
			{/if}
			{#if disk && !disk.id}<p class="muted small">{t('storage.sleepNoIdentity')}</p>{/if}
			{#if disk && canAdminister}
				{#if editing}
					<SettingRow title={t('storage.sleepDiskOwn')} for="disk-sleep-edit-{name}">
						{#snippet control()}<select
								id="disk-sleep-edit-{name}"
								class="field"
								bind:value={draft}
								disabled={busy}
							>
								<option value={INHERIT}
									>{disk.volume
										? t('storage.sleepVolumeOption')
										: t('storage.sleepInherit')}</option
								>
								{#each minutes as value}<option value={String(value)}>{sleepDuration(value)}</option
									>{/each}
							</select>{/snippet}
					</SettingRow>
					{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
					<div class="row-actions">
						<button type="button" class="btn btn-primary" onclick={save} disabled={busy}
							>{t('storage.sleepSave')}</button
						>
						<button type="button" class="btn" onclick={() => (editing = false)} disabled={busy}
							>{t('storage.sleepCancel')}</button
						>
					</div>
				{:else}
					<div class="row-actions">
						{#if disk.id}<button type="button" class="btn small" onclick={open}
								>{t('storage.sleepChange')}</button
							>{/if}
						<button type="button" class="btn small" onclick={onopen}
							>{t('storage.sleepOpenHint')}</button
						>
					</div>
				{/if}
			{/if}
		</SettingGroup>
	</div>
{/if}

<style>
	.sleep {
		min-width: 0;
	}
	.small {
		font-size: var(--text-sm);
	}
	.since {
		display: block;
	}
	.row-actions {
		margin-top: var(--space-1-5);
	}
</style>
