<script lang="ts">
	// Ruhemodus in the disk details: value, origin, state with its time, or
	// why the disk cannot sleep. The disk's own value can be changed here; it
	// sends the same request as the sleep dialog with every other value kept.
	import { api, type DiskSleepStatus } from '#lib/api.ts';
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

{#if entry}
	<section class="sleep" aria-labelledby="disk-sleep-{name}">
		<div class="head">
			<h3 id="disk-sleep-{name}">{t('storage.sleepCard')}</h3>
			{#if disk}
				<span>{sleepDuration(disk.effective)}</span>
				<span class="muted small">({sleepOrigin(disk.origin)})</span>
				<span class="chip" class:ok={disk.state === 'asleep'}
					>{disk.state === 'asleep' ? t('storage.sleepAsleep') : t('storage.sleepAwake')}</span
				>
				{#if disk.error}<span class="chip error" title={disk.error}
						>{t('storage.sleepStandbyFailed')}</span
					>{/if}
			{:else}
				<span class="muted">{entry.reason ? t(`storage.sleepReason.${entry.reason}`) : '–'}</span>
			{/if}
		</div>
		{#if since}<p class="muted small since">{since}</p>{/if}
		{#if disk && !disk.id}<p class="muted small">{t('storage.sleepNoIdentity')}</p>{/if}
		{#if disk && canAdminister}
			{#if editing}
				<div class="editor">
					<label for="disk-sleep-edit-{name}">{t('storage.sleepDiskOwn')}</label>
					<select id="disk-sleep-edit-{name}" class="field" bind:value={draft} disabled={busy}>
						<option value={INHERIT}
							>{disk.volume ? t('storage.sleepVolumeOption') : t('storage.sleepInherit')}</option
						>
						{#each minutes as value}<option value={String(value)}>{sleepDuration(value)}</option
							>{/each}
					</select>
				</div>
				{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
				<div class="actions">
					<button type="button" class="btn btn-primary" onclick={save} disabled={busy}
						>{t('storage.sleepSave')}</button
					>
					<button type="button" class="btn" onclick={() => (editing = false)} disabled={busy}
						>{t('storage.sleepCancel')}</button
					>
				</div>
			{:else}
				<div class="actions">
					{#if disk.id}<button type="button" class="btn small" onclick={open}
							>{t('storage.sleepChange')}</button
						>{/if}
					<button type="button" class="btn small" onclick={onopen}
						>{t('storage.sleepOpenHint')}</button
					>
				</div>
			{/if}
		{/if}
	</section>
{/if}

<style>
	.sleep {
		display: grid;
		gap: 8px;
		border-top: 1px solid var(--border);
		padding-top: 12px;
	}
	.head {
		display: flex;
		align-items: center;
		gap: 10px;
		flex-wrap: wrap;
	}
	h3 {
		margin: 0;
		font-size: inherit;
		font-weight: 600;
	}
	.small {
		font-size: var(--text-sm);
	}
	.editor {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 8px 12px;
	}
	.editor select {
		width: auto;
		min-width: 150px;
	}
	.actions {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
	}
	p {
		margin: 0;
	}
</style>
